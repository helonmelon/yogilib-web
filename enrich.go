package main

// Local-model enrichment pipeline.
//
// Goal: use the user's local Ollama instance to do the bulk work that
// would otherwise burn cloud credits — embeddings, TL;DR summaries,
// entity extraction, and (next pass) wiki-link suggestions.
//
// Opus is the conductor: it wrote this harness, picked the prompts, and
// reviews output quality. The N×document loops run locally and free.
//
// Models used:
//   - Embeddings:           nomic-embed-text     (~270 MB)
//   - Summaries / entities: llama3.1:8b          (Q4_K_M, ~5 GB)
//
// Endpoint: http://localhost:11434 (Ollama default)
// Override with the OLLAMA_URL env var if needed.

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

const (
	defaultOllamaURL    = "http://localhost:11434"
	embedModel          = "nomic-embed-text"
	genModel            = "llama3.1:8b"
	enrichTickInterval  = 5 * time.Minute
	enrichRequestTO     = 90 * time.Second
	maxEnrichBodyChars  = 8000 // truncate before sending to LLM
	enrichGoroutineName = "enrich-worker"
)

func ollamaURL() string {
	if v := strings.TrimSpace(os.Getenv("OLLAMA_URL")); v != "" {
		return v
	}
	return defaultOllamaURL
}

// ---------------------------------------------------------------------------
// Schema (migration3)
// ---------------------------------------------------------------------------

// migration3 creates the enrichment tables. Idempotent.
func migration3() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS embeddings (
			doc_id     INTEGER PRIMARY KEY REFERENCES documents(id) ON DELETE CASCADE,
			model      TEXT NOT NULL,
			vec        BLOB NOT NULL,
			dim        INTEGER NOT NULL,
			body_hash  TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
		// summary       = primary TL;DR in the body's main language
		// summary_np    = short Devanagari Nepali précis (1-2 sentences)
		//                 always present so the article header reads bilingually,
		//                 even when the body language is English.
		`CREATE TABLE IF NOT EXISTS summaries (
			doc_id     INTEGER PRIMARY KEY REFERENCES documents(id) ON DELETE CASCADE,
			model      TEXT NOT NULL,
			summary    TEXT NOT NULL,
			summary_np TEXT,
			body_hash  TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS entities (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			doc_id     INTEGER NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
			kind       TEXT NOT NULL, -- 'person' | 'place' | 'work' | 'date' | 'other'
			value      TEXT NOT NULL,
			value_np   TEXT,
			confidence REAL,
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_entities_doc  ON entities(doc_id)`,
		`CREATE INDEX IF NOT EXISTS idx_entities_kind ON entities(kind)`,
		`CREATE INDEX IF NOT EXISTS idx_entities_value ON entities(value)`,
		// Link suggestions surface in the editor; admin approves.
		`CREATE TABLE IF NOT EXISTS link_suggestions (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			from_doc_id  INTEGER NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
			to_doc_id    INTEGER NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
			anchor       TEXT NOT NULL,
			score        REAL,
			status       TEXT NOT NULL DEFAULT 'pending', -- pending|accepted|rejected
			created_at   TEXT NOT NULL,
			UNIQUE(from_doc_id, to_doc_id, anchor)
		)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return fmt.Errorf("migration3: %w", err)
		}
	}
	return nil
}

// migration4: add summary_np column to summaries table (idempotent).
// Existing migration3 deployments don't have it.
func migration4() error {
	if _, err := db.Exec("ALTER TABLE summaries ADD COLUMN summary_np TEXT"); err != nil {
		if !strings.Contains(err.Error(), "duplicate column name") {
			return fmt.Errorf("migration4: %w", err)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Ollama HTTP client
// ---------------------------------------------------------------------------

type ollamaEmbedResp struct {
	Embedding []float32 `json:"embedding"`
}

type ollamaGenResp struct {
	Response string `json:"response"`
	Done     bool   `json:"done"`
}

func ollamaEmbed(ctx context.Context, text string) ([]float32, error) {
	body := map[string]any{
		"model":  embedModel,
		"prompt": text,
	}
	buf, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(ctx, "POST",
		ollamaURL()+"/api/embeddings", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: enrichRequestTO}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama embed status %d: %s", resp.StatusCode, string(b))
	}
	var er ollamaEmbedResp
	if err := json.NewDecoder(resp.Body).Decode(&er); err != nil {
		return nil, err
	}
	if len(er.Embedding) == 0 {
		return nil, fmt.Errorf("ollama embed returned empty vector")
	}
	return er.Embedding, nil
}

func ollamaGenerate(ctx context.Context, model, prompt string, jsonMode bool) (string, error) {
	body := map[string]any{
		"model":  model,
		"prompt": prompt,
		"stream": false,
		"options": map[string]any{
			"temperature": 0.2,
			"num_predict": 400,
		},
	}
	if jsonMode {
		body["format"] = "json"
	}
	buf, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(ctx, "POST",
		ollamaURL()+"/api/generate", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: enrichRequestTO}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("ollama gen status %d: %s", resp.StatusCode, string(b))
	}
	var gr ollamaGenResp
	if err := json.NewDecoder(resp.Body).Decode(&gr); err != nil {
		return "", err
	}
	return strings.TrimSpace(gr.Response), nil
}

// ---------------------------------------------------------------------------
// Vector encoding (float32 → BLOB)
// ---------------------------------------------------------------------------

func encodeVec(v []float32) []byte {
	buf := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

func decodeVec(b []byte) []float32 {
	n := len(b) / 4
	out := make([]float32, n)
	for i := 0; i < n; i++ {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return out
}

func cosine(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return float32(dot / (math.Sqrt(na) * math.Sqrt(nb)))
}

// ---------------------------------------------------------------------------
// Fast hash (FNV-64) so we can detect when a doc body changed and
// refresh enrichment instead of re-running on every tick.
// ---------------------------------------------------------------------------

func bodyHash(s string) string {
	const (
		offset64 uint64 = 14695981039346656037
		prime64  uint64 = 1099511628211
	)
	h := offset64
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= prime64
	}
	return fmt.Sprintf("%016x", h)
}

// ---------------------------------------------------------------------------
// Per-doc passes
// ---------------------------------------------------------------------------

type docToEnrich struct {
	id    int
	title string
	body  string
	hash  string
}

func loadDocsForEnrichment(limit int) ([]docToEnrich, error) {
	rows, err := db.Query(`
		SELECT d.id, d.title, COALESCE(d.body_text,'')
		FROM documents d
		ORDER BY d.id ASC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []docToEnrich
	for rows.Next() {
		var d docToEnrich
		if err := rows.Scan(&d.id, &d.title, &d.body); err != nil {
			continue
		}
		d.hash = bodyHash(d.body)
		out = append(out, d)
	}
	return out, nil
}

func enrichEmbedding(ctx context.Context, d docToEnrich) (changed bool, err error) {
	if strings.TrimSpace(d.body) == "" {
		return false, nil
	}
	// Skip if up-to-date
	var existing string
	row := db.QueryRow(`SELECT body_hash FROM embeddings WHERE doc_id=?`, d.id)
	if err := row.Scan(&existing); err == nil && existing == d.hash {
		return false, nil
	}
	text := d.title + "\n\n" + d.body
	if len(text) > maxEnrichBodyChars {
		text = text[:maxEnrichBodyChars]
	}
	vec, err := ollamaEmbed(ctx, text)
	if err != nil {
		return false, fmt.Errorf("embed: %w", err)
	}
	_, err = db.Exec(`
		INSERT INTO embeddings (doc_id, model, vec, dim, body_hash, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(doc_id) DO UPDATE SET
			model=excluded.model, vec=excluded.vec, dim=excluded.dim,
			body_hash=excluded.body_hash, created_at=excluded.created_at
	`, d.id, embedModel, encodeVec(vec), len(vec), d.hash,
		time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return false, fmt.Errorf("save embedding: %w", err)
	}
	return true, nil
}

func enrichSummary(ctx context.Context, d docToEnrich) (changed bool, err error) {
	body := strings.TrimSpace(d.body)
	if len(body) < 200 { // too short to need summarizing
		return false, nil
	}
	var existing string
	row := db.QueryRow(`SELECT body_hash FROM summaries WHERE doc_id=?`, d.id)
	if err := row.Scan(&existing); err == nil && existing == d.hash {
		return false, nil
	}
	if len(body) > maxEnrichBodyChars {
		body = body[:maxEnrichBodyChars]
	}

	// Detect dominant script of the BODY (not title — the title is often
	// already bilingual and tricks the model). If most letters are
	// Devanagari, the article's main language is Nepali; otherwise English.
	primaryIsNepali := dominantScript(body) == "devanagari"

	var prompt string
	if primaryIsNepali {
		prompt = fmt.Sprintf(
			"You are writing a wiki article lead for yogilib, an encyclopaedia "+
				"of the writings of Yogi Naraharinath. The article is in Nepali (Devanagari script).\n\n"+
				"Return STRICT JSON only, no prose, no markdown:\n"+
				`{"summary":"<4-6 sentences in Nepali Devanagari, encyclopaedic tone>",`+
				`"summary_np":"<1-2 sentence Nepali précis (Devanagari)>"}`+"\n\n"+
				"Rules: NEVER use romanised Nepali. NEVER mix scripts. Be factual and tight. "+
				"If a name or term appears only in English in the source, you may keep it in English "+
				"inside parentheses on first mention.\n\n"+
				"---\nTitle: %s\n\nArticle:\n%s\n---",
			d.title, body)
	} else {
		prompt = fmt.Sprintf(
			"You are writing a wiki article lead for yogilib, an encyclopaedia "+
				"of the writings of Yogi Naraharinath. The article is in English.\n\n"+
				"Return STRICT JSON only, no prose, no markdown:\n"+
				`{"summary":"<4-6 sentence encyclopaedic English TL;DR>",`+
				`"summary_np":"<1-2 sentence Devanagari Nepali précis of the same content>"}`+"\n\n"+
				"Rules:\n"+
				"- The Nepali précis MUST be in Devanagari script. NEVER use romanised Nepali "+
				"(do not write \"ma\", \"ko\", \"gareko\" — use मा, को, गरेको).\n"+
				"- Transliterate proper nouns into Devanagari naturally "+
				"(e.g. Treaty of Sugauli → सुगौलीको सन्धि, East India Company → ईस्ट इण्डिया कम्पनी).\n"+
				"- Be factual. No padding, no 'this article describes'.\n"+
				"- If you don't know the Nepali for a term, keep the English term in Devanagari quotes.\n\n"+
				"---\nTitle: %s\n\nArticle:\n%s\n---",
			d.title, body)
	}

	out, err := ollamaGenerate(ctx, genModel, prompt, true) // JSON mode
	if err != nil {
		return false, fmt.Errorf("summary gen: %w", err)
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return false, nil
	}

	var parsed struct {
		Summary   string `json:"summary"`
		SummaryNP string `json:"summary_np"`
	}
	if jerr := json.Unmarshal([]byte(out), &parsed); jerr != nil {
		// brace-recovery (llama sometimes wraps JSON in prose)
		if s := strings.Index(out, "{"); s >= 0 {
			if e := strings.LastIndex(out, "}"); e > s {
				if jerr2 := json.Unmarshal([]byte(out[s:e+1]), &parsed); jerr2 != nil {
					return false, fmt.Errorf("summary parse: %w (raw: %.200s)", jerr, out)
				}
			}
		} else {
			return false, fmt.Errorf("summary parse: %w (raw: %.200s)", jerr, out)
		}
	}
	parsed.Summary = strings.TrimSpace(parsed.Summary)
	parsed.SummaryNP = strings.TrimSpace(parsed.SummaryNP)
	if parsed.Summary == "" {
		return false, nil
	}

	_, err = db.Exec(`
		INSERT INTO summaries (doc_id, model, summary, summary_np, body_hash, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(doc_id) DO UPDATE SET
			model=excluded.model, summary=excluded.summary,
			summary_np=excluded.summary_np,
			body_hash=excluded.body_hash, created_at=excluded.created_at
	`, d.id, genModel, parsed.Summary, parsed.SummaryNP, d.hash,
		time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return false, fmt.Errorf("save summary: %w", err)
	}
	return true, nil
}

// dominantScript returns "devanagari" if Devanagari letters dominate,
// else "latin". Counts letters only; ignores punctuation/digits.
func dominantScript(s string) string {
	var dev, lat int
	for _, r := range s {
		switch {
		case r >= 0x0900 && r <= 0x097F:
			dev++
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'):
			lat++
		}
	}
	if dev > lat {
		return "devanagari"
	}
	return "latin"
}

// extractedEntity is what the LLM returns in JSON mode.
type extractedEntity struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

func enrichEntities(ctx context.Context, d docToEnrich) (changed bool, err error) {
	body := strings.TrimSpace(d.body)
	if len(body) < 200 {
		return false, nil
	}
	// Cheap idempotency: skip if we already have any entities for this body_hash.
	var n int
	_ = db.QueryRow(`
		SELECT COUNT(*) FROM entities e
		JOIN summaries s ON s.doc_id=e.doc_id
		WHERE e.doc_id=? AND s.body_hash=?
	`, d.id, d.hash).Scan(&n)
	if n > 0 {
		return false, nil
	}

	if len(body) > maxEnrichBodyChars {
		body = body[:maxEnrichBodyChars]
	}
	prompt := fmt.Sprintf(
		`Extract named entities from the article below. Return a JSON object: `+
			`{"entities":[{"kind":"...","value":"..."}]}. `+
			`kind must be one of: person, place, work, date, other. `+
			`value is the entity name as it appears in the text (preserve original script). `+
			`Limit to the 15 most important entities. No duplicates. No prose, JSON only.`+
			"\n\nTitle: %s\n\nArticle:\n%s",
		d.title, body)

	out, err := ollamaGenerate(ctx, genModel, prompt, true)
	if err != nil {
		return false, fmt.Errorf("entities gen: %w", err)
	}
	var parsed struct {
		Entities []extractedEntity `json:"entities"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		// Llama sometimes wraps JSON in extra text; try to find a brace pair.
		if start := strings.Index(out, "{"); start >= 0 {
			if end := strings.LastIndex(out, "}"); end > start {
				if err2 := json.Unmarshal([]byte(out[start:end+1]), &parsed); err2 != nil {
					return false, fmt.Errorf("entities parse: %w (raw: %.120s)", err, out)
				}
			}
		} else {
			return false, fmt.Errorf("entities parse: %w (raw: %.120s)", err, out)
		}
	}
	if len(parsed.Entities) == 0 {
		return false, nil
	}

	tx, err := db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM entities WHERE doc_id=?`, d.id); err != nil {
		return false, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for _, e := range parsed.Entities {
		kind := strings.ToLower(strings.TrimSpace(e.Kind))
		val := strings.TrimSpace(e.Value)
		if val == "" {
			continue
		}
		if !validEntityKind(kind) {
			kind = "other"
		}
		if _, err := tx.Exec(`
			INSERT INTO entities (doc_id, kind, value, confidence, created_at)
			VALUES (?, ?, ?, ?, ?)
		`, d.id, kind, val, 0.5, now); err != nil {
			return false, err
		}
	}
	return true, tx.Commit()
}

func validEntityKind(k string) bool {
	switch k {
	case "person", "place", "work", "date", "other":
		return true
	}
	return false
}

// suggestLinks computes top-K most similar docs for each doc using
// cosine similarity over the cached embeddings, then writes pending
// link_suggestions rows. We don't ask the LLM for anchor phrasing yet —
// that's a separate (more expensive) pass we can run on demand.
//
// For now anchor defaults to the target doc's title; admin can edit
// later. This keeps the cheap pass cheap.
func suggestLinks(topK int, minScore float32) (int, error) {
	rows, err := db.Query(`SELECT doc_id, vec FROM embeddings`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	type vd struct {
		id  int
		vec []float32
	}
	var all []vd
	for rows.Next() {
		var v vd
		var b []byte
		if err := rows.Scan(&v.id, &b); err == nil {
			v.vec = decodeVec(b)
			all = append(all, v)
		}
	}
	if len(all) < 2 {
		return 0, nil
	}

	// Build a quick title lookup
	titles := map[int]string{}
	tRows, err := db.Query(`SELECT id, title FROM documents`)
	if err != nil {
		return 0, err
	}
	for tRows.Next() {
		var id int
		var t string
		if err := tRows.Scan(&id, &t); err == nil {
			titles[id] = t
		}
	}
	tRows.Close()

	// We replace the pending suggestion set on each run, but don't
	// touch accepted/rejected ones — admin choices are sticky.
	if _, err := db.Exec(`DELETE FROM link_suggestions WHERE status='pending'`); err != nil {
		return 0, err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	written := 0
	for i, a := range all {
		type pair struct {
			id    int
			score float32
		}
		var top []pair
		for j, b := range all {
			if i == j {
				continue
			}
			s := cosine(a.vec, b.vec)
			if s < minScore {
				continue
			}
			top = append(top, pair{id: b.id, score: s})
		}
		// keep top-K
		for len(top) > topK {
			// drop lowest
			minIdx := 0
			for k := 1; k < len(top); k++ {
				if top[k].score < top[minIdx].score {
					minIdx = k
				}
			}
			top = append(top[:minIdx], top[minIdx+1:]...)
		}
		for _, p := range top {
			anchor := titles[p.id]
			if anchor == "" {
				continue
			}
			if _, err := db.Exec(`
				INSERT OR IGNORE INTO link_suggestions
					(from_doc_id, to_doc_id, anchor, score, status, created_at)
				VALUES (?, ?, ?, ?, 'pending', ?)
			`, a.id, p.id, anchor, p.score, now); err != nil {
				continue
			}
			written++
		}
	}
	return written, nil
}

// ---------------------------------------------------------------------------
// Worker: runs in a background goroutine. One pass walks all docs; we
// retry on each interval. Cheap to re-run because each step is a no-op
// when body_hash matches.
// ---------------------------------------------------------------------------

type enrichStats struct {
	Pass         int
	StartedAt    time.Time
	FinishedAt   time.Time
	DocsSeen     int
	EmbedHits    int
	SummaryHits  int
	EntitiesHits int
	LinkSuggs    int
	LastError    string
}

var (
	lastEnrichStats enrichStats // read by /admin/enrich-status
)

func startEnrichWorker(ctx context.Context) {
	go func() {
		// First pass after a short delay so the server is warm.
		select {
		case <-time.After(15 * time.Second):
		case <-ctx.Done():
			return
		}
		pass := 0
		t := time.NewTicker(enrichTickInterval)
		defer t.Stop()
		for {
			pass++
			runEnrichOnce(ctx, pass)
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}

func runEnrichOnce(ctx context.Context, pass int) {
	stats := enrichStats{Pass: pass, StartedAt: time.Now()}
	defer func() {
		stats.FinishedAt = time.Now()
		lastEnrichStats = stats
	}()

	// Quick reachability check
	cctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	if _, err := ollamaEmbed(cctx, "ping"); err != nil {
		stats.LastError = fmt.Sprintf("ollama unreachable: %v", err)
		log.Printf("[%s] %s", enrichGoroutineName, stats.LastError)
		return
	}

	docs, err := loadDocsForEnrichment(500)
	if err != nil {
		stats.LastError = fmt.Sprintf("load docs: %v", err)
		log.Printf("[%s] %s", enrichGoroutineName, stats.LastError)
		return
	}
	stats.DocsSeen = len(docs)
	if len(docs) == 0 {
		log.Printf("[%s] pass %d: no docs", enrichGoroutineName, pass)
		return
	}

	for _, d := range docs {
		if ctx.Err() != nil {
			return
		}
		// Embedding (cheap, ~200ms with nomic-embed-text)
		if changed, err := enrichEmbedding(ctx, d); err != nil {
			log.Printf("[%s] embed doc %d: %v", enrichGoroutineName, d.id, err)
		} else if changed {
			stats.EmbedHits++
		}
		// Summary (slower, ~5-15s with llama3.1:8b warm)
		if changed, err := enrichSummary(ctx, d); err != nil {
			log.Printf("[%s] summary doc %d: %v", enrichGoroutineName, d.id, err)
		} else if changed {
			stats.SummaryHits++
		}
		// Entity extraction (slower)
		if changed, err := enrichEntities(ctx, d); err != nil {
			log.Printf("[%s] entities doc %d: %v", enrichGoroutineName, d.id, err)
		} else if changed {
			stats.EntitiesHits++
		}
	}

	// Link suggestions: only when at least 2 docs have embeddings.
	if n, err := suggestLinks(5, 0.65); err != nil {
		log.Printf("[%s] suggestLinks: %v", enrichGoroutineName, err)
	} else {
		stats.LinkSuggs = n
	}

	log.Printf("[%s] pass %d done: docs=%d embed+%d summary+%d entities+%d suggestions=%d",
		enrichGoroutineName, pass, stats.DocsSeen,
		stats.EmbedHits, stats.SummaryHits, stats.EntitiesHits, stats.LinkSuggs)
}

// ---------------------------------------------------------------------------
// Read accessors used by handlers
// ---------------------------------------------------------------------------

func getDocSummary(docID string) string {
	var s string
	row := db.QueryRow(`SELECT summary FROM summaries WHERE doc_id=?`, docID)
	_ = row.Scan(&s)
	return s
}

func getDocSummaryNP(docID string) string {
	var s sql.NullString
	row := db.QueryRow(`SELECT summary_np FROM summaries WHERE doc_id=?`, docID)
	_ = row.Scan(&s)
	return s.String
}

type entityEntry struct {
	Kind  string
	Value string
}

func getDocEntities(docID string) []entityEntry {
	rows, err := db.Query(`
		SELECT kind, value FROM entities
		WHERE doc_id=?
		ORDER BY CASE kind
			WHEN 'person' THEN 1
			WHEN 'place'  THEN 2
			WHEN 'work'   THEN 3
			WHEN 'date'   THEN 4
			ELSE 5 END, value
	`, docID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []entityEntry
	for rows.Next() {
		var e entityEntry
		if err := rows.Scan(&e.Kind, &e.Value); err == nil {
			out = append(out, e)
		}
	}
	return out
}

type linkSuggEntry struct {
	ID     int
	ToID   int
	Anchor string
	Score  float32
	Title  string
}

func getLinkSuggestionsForDoc(docID string) []linkSuggEntry {
	rows, err := db.Query(`
		SELECT s.id, s.to_doc_id, s.anchor, s.score, d.title
		FROM link_suggestions s
		JOIN documents d ON d.id = s.to_doc_id
		WHERE s.from_doc_id=? AND s.status='pending'
		ORDER BY s.score DESC
		LIMIT 8
	`, docID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []linkSuggEntry
	for rows.Next() {
		var e linkSuggEntry
		if err := rows.Scan(&e.ID, &e.ToID, &e.Anchor, &e.Score, &e.Title); err == nil {
			out = append(out, e)
		}
	}
	return out
}

// silence unused imports in builds where some helpers aren't called yet
var _ = sql.ErrNoRows

// ---------------------------------------------------------------------------
// Admin handlers
// ---------------------------------------------------------------------------

func enrichStatusHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	// Counts
	var nDocs, nEmb, nSum, nEnt, nSugg int
	db.QueryRow(`SELECT COUNT(*) FROM documents`).Scan(&nDocs)
	db.QueryRow(`SELECT COUNT(*) FROM embeddings`).Scan(&nEmb)
	db.QueryRow(`SELECT COUNT(*) FROM summaries`).Scan(&nSum)
	db.QueryRow(`SELECT COUNT(*) FROM entities`).Scan(&nEnt)
	db.QueryRow(`SELECT COUNT(*) FROM link_suggestions WHERE status='pending'`).Scan(&nSugg)

	payload := map[string]any{
		"ollama_url": ollamaURL(),
		"models": map[string]string{
			"embed": embedModel,
			"gen":   genModel,
		},
		"counts": map[string]int{
			"documents":               nDocs,
			"embeddings":              nEmb,
			"summaries":               nSum,
			"entities":                nEnt,
			"link_suggestions_pending": nSugg,
		},
		"last_pass": map[string]any{
			"pass":           lastEnrichStats.Pass,
			"started_at":     formatTime(lastEnrichStats.StartedAt),
			"finished_at":    formatTime(lastEnrichStats.FinishedAt),
			"docs_seen":      lastEnrichStats.DocsSeen,
			"embed_hits":     lastEnrichStats.EmbedHits,
			"summary_hits":   lastEnrichStats.SummaryHits,
			"entities_hits":  lastEnrichStats.EntitiesHits,
			"link_suggs":     lastEnrichStats.LinkSuggs,
			"last_error":     lastEnrichStats.LastError,
		},
		"tick_interval": enrichTickInterval.String(),
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(payload)
}

func enrichRunHandler(w http.ResponseWriter, r *http.Request) {
	go runEnrichOnce(context.Background(), lastEnrichStats.Pass+1)
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"ok":true,"note":"enrichment pass scheduled"}`))
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
