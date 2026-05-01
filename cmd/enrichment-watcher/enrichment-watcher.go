// enrichment-watcher.go — Standalone daemon for yogilib AI enrichment
//
// Monitors the yogilib SQLite database for new/edited documents and
// processes them through local Ollama to generate:
//   - Embeddings (nomic-embed-text)
//   - Summaries (mistral-nemo:12b)
//   - Entity extraction (mistral-nemo:12b)
//   - Link suggestions (cosine similarity over embeddings)
//
// Runs continuously; checks for new/updated docs every minute.
// Graceful shutdown on SIGTERM/SIGINT.
//
// Usage:
//   export DB_PATH=/home/dofdot/.openclaw/workspace/yogilib/yogilib-web/yogilib.db
//   export OLLAMA_URL=http://host.docker.internal:11434
//   export LOG_FILE=$HOME/yogilib-enrichment.log
//   ./enrichment-watcher

package main

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
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	_ "modernc.org/sqlite"
)

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

const (
	defaultOllamaURL    = "http://localhost:11434"
	embedModel          = "nomic-embed-text"
	genModel            = "mistral-nemo:12b"
	checkInterval       = 1 * time.Minute
	enrichRequestTimeout = 120 * time.Second
	maxEnrichBodyChars   = 8000
	cosineScoreThreshold = 0.65
	topKLinks            = 5
)

var (
	dbPath    string
	ollamaURL string
	logFile   string
	logger    *log.Logger
	db        *sql.DB
)

func init() {
	dbPath = os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = filepath.Join(os.Getenv("HOME"), ".openclaw/workspace/yogilib/yogilib-web/yogilib.db")
	}

	ollamaURL = os.Getenv("OLLAMA_URL")
	if ollamaURL == "" {
		ollamaURL = defaultOllamaURL
	}

	logFile = os.Getenv("LOG_FILE")
	if logFile == "" {
		logFile = filepath.Join(os.Getenv("HOME"), "yogilib-enrichment.log")
	}

	// Set up logging to file (and stdout for now)
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("failed to open log file %s: %v", logFile, err)
	}
	// Log to both file and stdout
	mw := io.MultiWriter(os.Stdout, f)
	logger = log.New(mw, "[enrichment-watcher] ", log.LstdFlags|log.Lshortfile)
}

func main() {
	logger.Printf("Starting enrichment-watcher")
	logger.Printf("  DB: %s", dbPath)
	logger.Printf("  Ollama: %s", ollamaURL)
	logger.Printf("  Log: %s", logFile)
	logger.Printf("  Models: embed=%s, gen=%s", embedModel, genModel)

	var err error
	db, err = sql.Open("sqlite", dbPath)
	if err != nil {
		logger.Fatalf("failed to open DB: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	// Test connection
	if err := db.Ping(); err != nil {
		logger.Fatalf("failed to ping DB: %v", err)
	}
	logger.Println("DB connected successfully")

	// Test Ollama connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := testOllamaConnection(ctx); err != nil {
		logger.Fatalf("failed to connect to Ollama: %v", err)
	}
	cancel()
	logger.Println("Ollama connected successfully")

	// Set up graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)

	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	go func() {
		sig := <-sigChan
		logger.Printf("Received signal %v; shutting down gracefully...", sig)
		cancel()
	}()

	// Main watch loop
	watchLoop(ctx)
	logger.Println("Shutting down")
}

func watchLoop(ctx context.Context) {
	pass := 0
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pass++
			processDocsOnce(ctx, pass)
		}
	}
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

func testOllamaConnection(ctx context.Context) error {
	_, err := ollamaEmbed(ctx, "test")
	return err
}

func ollamaEmbed(ctx context.Context, text string) ([]float32, error) {
	body := map[string]any{
		"model":  embedModel,
		"prompt": text,
	}
	buf, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(ctx, "POST",
		ollamaURL+"/api/embeddings", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: enrichRequestTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embed request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama embed status %d: %s", resp.StatusCode, string(b))
	}
	var er ollamaEmbedResp
	if err := json.NewDecoder(resp.Body).Decode(&er); err != nil {
		return nil, fmt.Errorf("decode embed response: %w", err)
	}
	if len(er.Embedding) == 0 {
		return nil, fmt.Errorf("ollama returned empty embedding")
	}
	return er.Embedding, nil
}

func ollamaGenerate(ctx context.Context, prompt string, jsonMode bool) (string, error) {
	body := map[string]any{
		"model":  genModel,
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
		ollamaURL+"/api/generate", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: enrichRequestTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("generate request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("ollama gen status %d: %s", resp.StatusCode, string(b))
	}
	var gr ollamaGenResp
	if err := json.NewDecoder(resp.Body).Decode(&gr); err != nil {
		return "", fmt.Errorf("decode gen response: %w", err)
	}
	return strings.TrimSpace(gr.Response), nil
}

// ---------------------------------------------------------------------------
// Vector utilities
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
// Document loading and enrichment
// ---------------------------------------------------------------------------

type docToProcess struct {
	id    int
	title string
	body  string
	hash  string
}

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

func loadDocsForEnrichment(limit int) ([]docToProcess, error) {
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
	var out []docToProcess
	for rows.Next() {
		var d docToProcess
		if err := rows.Scan(&d.id, &d.title, &d.body); err != nil {
			continue
		}
		d.hash = bodyHash(d.body)
		out = append(out, d)
	}
	return out, nil
}

func enrichEmbedding(ctx context.Context, d docToProcess) (changed bool, err error) {
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
	logger.Printf("[embed] Processing doc %d: %s", d.id, d.title)
	vec, err := ollamaEmbed(ctx, text)
	if err != nil {
		return false, fmt.Errorf("embed failed: %w", err)
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
	logger.Printf("[embed] Saved embedding for doc %d (%d dims)", d.id, len(vec))
	return true, nil
}

func enrichSummary(ctx context.Context, d docToProcess) (changed bool, err error) {
	body := strings.TrimSpace(d.body)
	if len(body) < 200 {
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

	logger.Printf("[summary] Processing doc %d: %s", d.id, d.title)
	primaryIsNepali := dominantScript(body) == "devanagari"

	var prompt string
	if primaryIsNepali {
		prompt = fmt.Sprintf(
			"You are writing a wiki article lead for yogilib, an encyclopaedia "+
				"of the writings of Yogi Naraharinath. The article is in Nepali (Devanagari script).\n\n"+
				"Return STRICT JSON only, no prose, no markdown:\n"+
				`{"summary":"<4-6 sentences in Nepali Devanagari, encyclopaedic tone>",`+
				`"summary_np":"<1-2 sentence Nepali précis (Devanagari)>"}`+"\n\n"+
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
				`"summary_np":"<1-2 sentence Devanagari Nepali précis of the same content>"}`+"\n\n"+
				"Rules:\n"+
				"- The Nepali précis MUST be in Devanagari script. NEVER use romanised Nepali "+
				"(do not write \"ma\", \"ko\", \"gareko\" — use मा, को, गरेको).\n"+
				"- Transliterate proper nouns into Devanagari naturally "+
				"(e.g. Treaty of Sugauli → सुगौलीको सन्धि, East India Company → ईस्ट इण्डिया कम्पनी).\n"+
				"- Be factual. No padding, no 'this article describes'.\n"+
				"- If you don't know the Nepali for a term, keep the English term in Devanagari quotes.\n\n"+
				"---\nTitle: %s\n\nArticle:\n%s\n---",
			d.title, body)
	}

	out, err := ollamaGenerate(ctx, prompt, true)
	if err != nil {
		return false, fmt.Errorf("summary gen failed: %w", err)
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
		if s := strings.Index(out, "{"); s >= 0 {
			if e := strings.LastIndex(out, "}"); e > s {
				if jerr2 := json.Unmarshal([]byte(out[s:e+1]), &parsed); jerr2 != nil {
					return false, fmt.Errorf("summary parse failed: %w (raw: %.200s)", jerr, out)
				}
			}
		} else {
			return false, fmt.Errorf("summary parse failed: %w (raw: %.200s)", jerr, out)
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
	logger.Printf("[summary] Saved summary for doc %d", d.id)
	return true, nil
}

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

type extractedEntity struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

func enrichEntities(ctx context.Context, d docToProcess) (changed bool, err error) {
	body := strings.TrimSpace(d.body)
	if len(body) < 200 {
		return false, nil
	}
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
	logger.Printf("[entities] Processing doc %d: %s", d.id, d.title)
	prompt := fmt.Sprintf(
		`Extract named entities from the article below. Return a JSON object: `+
			`{"entities":[{"kind":"...","value":"..."}]}. `+
			`kind must be one of: person, place, work, date, other. `+
			`value is the entity name as it appears in the text (preserve original script). `+
			`Limit to the 15 most important entities. No duplicates. No prose, JSON only.`+
			"\n\nTitle: %s\n\nArticle:\n%s",
		d.title, body)

	out, err := ollamaGenerate(ctx, prompt, true)
	if err != nil {
		return false, fmt.Errorf("entities gen failed: %w", err)
	}
	var parsed struct {
		Entities []extractedEntity `json:"entities"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		if start := strings.Index(out, "{"); start >= 0 {
			if end := strings.LastIndex(out, "}"); end > start {
				if err2 := json.Unmarshal([]byte(out[start:end+1]), &parsed); err2 != nil {
					return false, fmt.Errorf("entities parse failed: %w (raw: %.120s)", err, out)
				}
			}
		} else {
			return false, fmt.Errorf("entities parse failed: %w (raw: %.120s)", err, out)
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
	if err := tx.Commit(); err != nil {
		return false, err
	}
	logger.Printf("[entities] Saved %d entities for doc %d", len(parsed.Entities), d.id)
	return true, nil
}

func validEntityKind(k string) bool {
	switch k {
	case "person", "place", "work", "date", "other":
		return true
	}
	return false
}

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
		for len(top) > topK {
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
// Main processing loop
// ---------------------------------------------------------------------------

type processStats struct {
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

func processDocsOnce(ctx context.Context, pass int) {
	stats := processStats{Pass: pass, StartedAt: time.Now()}
	defer func() {
		stats.FinishedAt = time.Now()
		duration := stats.FinishedAt.Sub(stats.StartedAt)
		if stats.LastError != "" {
			logger.Printf("Pass %d failed after %v: %s", pass, duration, stats.LastError)
		} else {
			logger.Printf("Pass %d complete (%v): seen=%d embed+%d summary+%d entities+%d links=%d",
				pass, duration, stats.DocsSeen, stats.EmbedHits, stats.SummaryHits,
				stats.EntitiesHits, stats.LinkSuggs)
		}
	}()

	// Quick reachability check
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	if _, err := ollamaEmbed(cctx, "ping"); err != nil {
		cancel()
		stats.LastError = fmt.Sprintf("ollama unreachable: %v", err)
		logger.Printf("Ollama health check failed: %v", err)
		return
	}
	cancel()

	docs, err := loadDocsForEnrichment(500)
	if err != nil {
		stats.LastError = fmt.Sprintf("load docs: %v", err)
		logger.Printf("Failed to load docs: %v", err)
		return
	}
	stats.DocsSeen = len(docs)
	if len(docs) == 0 {
		logger.Printf("Pass %d: no docs to process", pass)
		return
	}

	for _, d := range docs {
		if ctx.Err() != nil {
			stats.LastError = "context cancelled"
			return
		}

		// Embedding
		if changed, err := enrichEmbedding(ctx, d); err != nil {
			logger.Printf("embed doc %d failed: %v", d.id, err)
		} else if changed {
			stats.EmbedHits++
		}

		// Summary
		if changed, err := enrichSummary(ctx, d); err != nil {
			logger.Printf("summary doc %d failed: %v", d.id, err)
		} else if changed {
			stats.SummaryHits++
		}

		// Entities
		if changed, err := enrichEntities(ctx, d); err != nil {
			logger.Printf("entities doc %d failed: %v", d.id, err)
		} else if changed {
			stats.EntitiesHits++
		}
	}

	// Link suggestions
	if n, err := suggestLinks(topKLinks, cosineScoreThreshold); err != nil {
		logger.Printf("suggestLinks failed: %v", err)
	} else {
		stats.LinkSuggs = n
		if n > 0 {
			logger.Printf("[links] Generated %d new link suggestions", n)
		}
	}
}
