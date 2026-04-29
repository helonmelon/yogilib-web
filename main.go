// yogilib — Go HTTP server
// Run:   go run main.go
// Build: go build -o yogilib .
package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

// ---------------------------------------------------------------------------
// Data types
// ---------------------------------------------------------------------------

type Document struct {
	ID           string
	Title        string
	TitleNP      string
	Slug         string
	Category     string
	Description  string
	BodyHTML     template.HTML
	BodyText     string
	Lang         string
	Script       string
	OrigAuthor   string
	OrigAuthorNP string
	OrigYear     string
	OrigMonth    string
	OrigDay      string
	FilePath     string
	UploadedBy   int
	CreatedAt    string
}

type Excerpt struct {
	Slug  string
	Title string
	Body  template.HTML
}

type StoreItem struct {
	ID          string
	Title       string
	TitleNP     string
	Description string
	ImageURL    string
	BuyURL      string
}

type User struct {
	ID    int
	Email string
	Role  string // "admin" | "uploader" | "viewer"
}

// BacklinkEntry is a document that links to the current document.
type BacklinkEntry struct {
	ID    int
	Title string
}

type PageData struct {
	Title      string
	Query      string
	Category   string
	Categories []string
	Flash      string
	Error      string
	Documents  []Document
	Doc        *Document
	Excerpts   []Excerpt
	Exc        *Excerpt
	StoreItems []StoreItem
	User       *User        // nil when not logged in
	Backlinks       []BacklinkEntry
	TOC             []TOCEntry
	Revisions       []RevisionEntry
	RevA            *RevisionEntry
	RevB            *RevisionEntry
	Diff            []DiffLine
	WantedLinks     []WantedEntry
	Summary         string
	Entities        []entityEntry
	LinkSuggestions []linkSuggEntry
}

// ---------------------------------------------------------------------------
// Role hierarchy
// ---------------------------------------------------------------------------

var roleLevel = map[string]int{
	"viewer":   1,
	"uploader": 2,
	"admin":    3,
}

func hasRole(userRole, required string) bool {
	return roleLevel[userRole] >= roleLevel[required]
}

// ---------------------------------------------------------------------------
// Database
// ---------------------------------------------------------------------------

var db *sql.DB

var docCategories = []string{"सबै", "किताब", "कागजात", "रेकर्ड", "पत्रिका", "अंश", "अन्य"}

func initDB(path string) error {
	var err error
	db, err = sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1) // SQLite: single writer

	schema := `
	PRAGMA journal_mode=WAL;
	PRAGMA foreign_keys=ON;

	CREATE TABLE IF NOT EXISTS documents (
		id             INTEGER PRIMARY KEY AUTOINCREMENT,
		title          TEXT NOT NULL,
		title_np       TEXT,
		slug           TEXT,
		category       TEXT,
		description    TEXT,
		body_html      TEXT,
		body_text      TEXT,
		lang           TEXT,
		script         TEXT,
		orig_author    TEXT,
		orig_author_np TEXT,
		orig_year      TEXT,
		orig_month     TEXT,
		orig_day       TEXT,
		file_path      TEXT,
		uploaded_by    INTEGER,
		created_at     TEXT NOT NULL
	);

	-- NOTE: the unique index on documents.slug is created by migration2,
	-- not here. On an existing pre-migration2 database the documents
	-- table won't yet have a slug column when this inline schema runs,
	-- so creating the index here would fail.

	CREATE TABLE IF NOT EXISTS revisions (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		doc_id     INTEGER NOT NULL REFERENCES documents(id),
		title      TEXT,
		title_np   TEXT,
		body_html  TEXT,
		body_text  TEXT,
		edited_by  INTEGER REFERENCES users(id),
		edited_at  TEXT NOT NULL,
		comment    TEXT
	);

	CREATE TABLE IF NOT EXISTS links (
		id           INTEGER PRIMARY KEY AUTOINCREMENT,
		from_doc_id  INTEGER NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
		to_doc_id    INTEGER REFERENCES documents(id) ON DELETE SET NULL,
		to_slug      TEXT,
		anchor       TEXT
	);

	CREATE INDEX IF NOT EXISTS idx_links_from    ON links(from_doc_id);
	CREATE INDEX IF NOT EXISTS idx_links_to_doc  ON links(to_doc_id);
	CREATE INDEX IF NOT EXISTS idx_links_to_slug ON links(to_slug);

	CREATE TABLE IF NOT EXISTS authors (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		slug       TEXT UNIQUE NOT NULL,
		name       TEXT NOT NULL,
		name_np    TEXT,
		bio_html   TEXT,
		born       TEXT,
		died       TEXT,
		created_at TEXT NOT NULL
	);

	CREATE VIRTUAL TABLE IF NOT EXISTS documents_fts USING fts5(
		title, title_np, description, body_text,
		content='documents', content_rowid='id',
		tokenize='unicode61'
	);

	CREATE TRIGGER IF NOT EXISTS docs_ai AFTER INSERT ON documents BEGIN
		INSERT INTO documents_fts(rowid, title, title_np, description, body_text)
		VALUES (new.id, new.title, COALESCE(new.title_np,''), COALESCE(new.description,''), COALESCE(new.body_text,''));
	END;

	CREATE TRIGGER IF NOT EXISTS docs_ad AFTER DELETE ON documents BEGIN
		INSERT INTO documents_fts(documents_fts, rowid, title, title_np, description, body_text)
		VALUES ('delete', old.id, old.title, COALESCE(old.title_np,''), COALESCE(old.description,''), COALESCE(old.body_text,''));
	END;

	CREATE TRIGGER IF NOT EXISTS docs_au AFTER UPDATE ON documents BEGIN
		INSERT INTO documents_fts(documents_fts, rowid, title, title_np, description, body_text)
		VALUES ('delete', old.id, old.title, COALESCE(old.title_np,''), COALESCE(old.description,''), COALESCE(old.body_text,''));
		INSERT INTO documents_fts(rowid, title, title_np, description, body_text)
		VALUES (new.id, new.title, COALESCE(new.title_np,''), COALESCE(new.description,''), COALESCE(new.body_text,''));
	END;

	CREATE TABLE IF NOT EXISTS users (
		id            INTEGER PRIMARY KEY AUTOINCREMENT,
		email         TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL,
		role          TEXT NOT NULL DEFAULT 'viewer'
	);

	CREATE TABLE IF NOT EXISTS sessions (
		token      TEXT PRIMARY KEY,
		user_id    INTEGER NOT NULL REFERENCES users(id),
		expires_at TEXT NOT NULL
	);
	`

	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("schema: %w", err)
	}

	if err := runMigrations(); err != nil {
		return fmt.Errorf("migrations: %w", err)
	}

	return seedData()
}

// ---------------------------------------------------------------------------
// Migrations
// ---------------------------------------------------------------------------

// runMigrations uses PRAGMA user_version to apply schema changes to existing DBs.
// New DBs get the full schema above; existing DBs get ALTER TABLE patches.
func runMigrations() error {
	var version int
	db.QueryRow("PRAGMA user_version").Scan(&version)

	if version < 1 {
		if err := migration1(); err != nil {
			return fmt.Errorf("migration1: %w", err)
		}
		if _, err := db.Exec("PRAGMA user_version = 1"); err != nil {
			return err
		}
		log.Println("migration1: applied")
	}

	if version < 2 {
		if err := migration2(); err != nil {
			return fmt.Errorf("migration2: %w", err)
		}
		if _, err := db.Exec("PRAGMA user_version = 2"); err != nil {
			return err
		}
		log.Println("migration2: applied")
	}

	if version < 3 {
		if err := migration3(); err != nil {
			return fmt.Errorf("migration3: %w", err)
		}
		if _, err := db.Exec("PRAGMA user_version = 3"); err != nil {
			return err
		}
		log.Println("migration3: applied")
	}

	return nil
}

// migration1: add extended document columns + rebuild FTS5 to include body_text.
// Safe to run on both old (missing columns) and new (full schema) DBs.
func migration1() error {
	// Add new columns — ignore "duplicate column name" if already present
	newCols := []string{
		"ALTER TABLE documents ADD COLUMN body_html TEXT",
		"ALTER TABLE documents ADD COLUMN body_text TEXT",
		"ALTER TABLE documents ADD COLUMN lang TEXT",
		"ALTER TABLE documents ADD COLUMN script TEXT",
		"ALTER TABLE documents ADD COLUMN orig_author TEXT",
		"ALTER TABLE documents ADD COLUMN orig_author_np TEXT",
		"ALTER TABLE documents ADD COLUMN orig_year TEXT",
		"ALTER TABLE documents ADD COLUMN orig_month TEXT",
		"ALTER TABLE documents ADD COLUMN orig_day TEXT",
		"ALTER TABLE documents ADD COLUMN uploaded_by INTEGER",
	}
	for _, stmt := range newCols {
		if _, err := db.Exec(stmt); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
			return fmt.Errorf("%s: %w", stmt, err)
		}
	}

	// Rebuild FTS5 table and triggers to include body_text.
	// Drop old triggers first so they don't fire against the wrong schema.
	ftsStmts := []string{
		"DROP TRIGGER IF EXISTS docs_ai",
		"DROP TRIGGER IF EXISTS docs_ad",
		"DROP TRIGGER IF EXISTS docs_au",
		"DROP TABLE IF EXISTS documents_fts",
		`CREATE VIRTUAL TABLE documents_fts USING fts5(
			title, title_np, description, body_text,
			content='documents', content_rowid='id',
			tokenize='unicode61'
		)`,
		`CREATE TRIGGER docs_ai AFTER INSERT ON documents BEGIN
			INSERT INTO documents_fts(rowid, title, title_np, description, body_text)
			VALUES (new.id, new.title, COALESCE(new.title_np,''), COALESCE(new.description,''), COALESCE(new.body_text,''));
		END`,
		`CREATE TRIGGER docs_ad AFTER DELETE ON documents BEGIN
			INSERT INTO documents_fts(documents_fts, rowid, title, title_np, description, body_text)
			VALUES ('delete', old.id, old.title, COALESCE(old.title_np,''), COALESCE(old.description,''), COALESCE(old.body_text,''));
		END`,
		`CREATE TRIGGER docs_au AFTER UPDATE ON documents BEGIN
			INSERT INTO documents_fts(documents_fts, rowid, title, title_np, description, body_text)
			VALUES ('delete', old.id, old.title, COALESCE(old.title_np,''), COALESCE(old.description,''), COALESCE(old.body_text,''));
			INSERT INTO documents_fts(rowid, title, title_np, description, body_text)
			VALUES (new.id, new.title, COALESCE(new.title_np,''), COALESCE(new.description,''), COALESCE(new.body_text,''));
		END`,
		// Repopulate from existing rows
		`INSERT INTO documents_fts(rowid, title, title_np, description, body_text)
		 SELECT id, title, COALESCE(title_np,''), COALESCE(description,''), COALESCE(body_text,'')
		 FROM documents`,
	}
	for _, stmt := range ftsStmts {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("FTS rebuild: %w", err)
		}
	}

	return nil
}

// migration2: adds slug column to documents, plus revisions/links/authors tables.
// Idempotent — ignores "duplicate column name" and "already exists" errors.
func migration2() error {
	// 1. Add slug column to documents.
	if _, err := db.Exec("ALTER TABLE documents ADD COLUMN slug TEXT"); err != nil {
		if !strings.Contains(err.Error(), "duplicate column name") {
			return fmt.Errorf("add slug column: %w", err)
		}
	}

	// 2. Partial unique index on slug (allows multiple NULL / empty values).
	if _, err := db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_documents_slug
		ON documents(slug)
		WHERE slug IS NOT NULL AND slug != ''
	`); err != nil {
		return fmt.Errorf("idx_documents_slug: %w", err)
	}

	// 3. New tables.
	newTables := []string{
		`CREATE TABLE IF NOT EXISTS revisions (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			doc_id     INTEGER NOT NULL REFERENCES documents(id),
			title      TEXT,
			title_np   TEXT,
			body_html  TEXT,
			body_text  TEXT,
			edited_by  INTEGER REFERENCES users(id),
			edited_at  TEXT NOT NULL,
			comment    TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS links (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			from_doc_id  INTEGER NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
			to_doc_id    INTEGER REFERENCES documents(id) ON DELETE SET NULL,
			to_slug      TEXT,
			anchor       TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_links_from    ON links(from_doc_id)`,
		`CREATE INDEX IF NOT EXISTS idx_links_to_doc  ON links(to_doc_id)`,
		`CREATE INDEX IF NOT EXISTS idx_links_to_slug ON links(to_slug)`,
		`CREATE TABLE IF NOT EXISTS authors (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			slug       TEXT UNIQUE NOT NULL,
			name       TEXT NOT NULL,
			name_np    TEXT,
			bio_html   TEXT,
			born       TEXT,
			died       TEXT,
			created_at TEXT NOT NULL
		)`,
	}
	for _, stmt := range newTables {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("migration2 table/index: %w", err)
		}
	}

	// 4. Backfill slugs for existing documents that don't have one yet.
	rows, err := db.Query(`SELECT id, title, COALESCE(title_np,'') FROM documents WHERE slug IS NULL OR slug = ''`)
	if err != nil {
		return fmt.Errorf("backfill query: %w", err)
	}
	type row struct {
		id      int
		title   string
		titleNP string
	}
	var toUpdate []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.title, &r.titleNP); err == nil {
			toUpdate = append(toUpdate, r)
		}
	}
	rows.Close()

	for _, r := range toUpdate {
		slug := slugify(r.title)
		if slug == "" {
			slug = slugify(r.titleNP)
		}
		if slug == "" {
			// Nothing to slugify — leave slug NULL.
			continue
		}
		// Ensure uniqueness: append doc id if a collision would occur.
		var existing int
		err := db.QueryRow(`SELECT id FROM documents WHERE slug = ? AND id != ?`, slug, r.id).Scan(&existing)
		if err == nil {
			// Collision — make unique.
			slug = fmt.Sprintf("%s-%d", slug, r.id)
		}
		if _, err := db.Exec(`UPDATE documents SET slug = ? WHERE id = ?`, slug, r.id); err != nil {
			log.Printf("backfill slug for doc %d: %v", r.id, err)
		}
	}

	return nil
}

// ---------------------------------------------------------------------------
// Seed data
// ---------------------------------------------------------------------------

func seedData() error {
	var count int

	// Seed documents
	db.QueryRow("SELECT COUNT(*) FROM documents").Scan(&count)
	if count == 0 {
		_, err := db.Exec(`
			INSERT INTO documents (title, title_np, category, description, created_at) VALUES
			('Treaty of Sugowlee', 'सुगौली सन्धि', 'कागजात',
			 'Signed December 2, 1815 between East India Company and the Kingdom of Nepal.',
			 ?)
		`, time.Now().AddDate(0, 0, -3).Format("2 Jan 2006"))
		if err != nil {
			return err
		}
	}

	// Seed users
	db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	if count == 0 {
		seed := []struct{ email, password, role string }{
			{"admin@yogilib.org", "admin123", "admin"},
			{"upload@yogilib.org", "upload123", "uploader"},
		}
		for _, u := range seed {
			hash, err := bcrypt.GenerateFromPassword([]byte(u.password), bcrypt.DefaultCost)
			if err != nil {
				return err
			}
			if _, err := db.Exec(
				`INSERT INTO users (email, password_hash, role) VALUES (?, ?, ?)`,
				u.email, string(hash), u.role,
			); err != nil {
				return err
			}
		}
		log.Println("seed users: admin@yogilib.org/admin123  upload@yogilib.org/upload123")
	}
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

var reHTMLTags = regexp.MustCompile(`<[^>]+>`)

// stripHTML removes HTML tags, leaving plain text suitable for FTS indexing.
func stripHTML(s string) string {
	plain := reHTMLTags.ReplaceAllString(s, " ")
	// Collapse whitespace
	return strings.Join(strings.Fields(plain), " ")
}

// ---------------------------------------------------------------------------
// DB query functions
// ---------------------------------------------------------------------------

const docSelectCols = `
	d.id,
	d.title,
	COALESCE(d.title_np,''),
	COALESCE(d.slug,''),
	COALESCE(d.category,''),
	COALESCE(d.description,''),
	COALESCE(d.body_html,''),
	COALESCE(d.body_text,''),
	COALESCE(d.lang,''),
	COALESCE(d.script,''),
	COALESCE(d.orig_author,''),
	COALESCE(d.orig_author_np,''),
	COALESCE(d.orig_year,''),
	COALESCE(d.orig_month,''),
	COALESCE(d.orig_day,''),
	COALESCE(d.file_path,''),
	COALESCE(d.uploaded_by,0),
	d.created_at
`

func scanDoc(rows interface{ Scan(...any) error }) (Document, error) {
	var d Document
	var id int
	var bodyHTML string
	err := rows.Scan(
		&id, &d.Title, &d.TitleNP, &d.Slug, &d.Category, &d.Description,
		&bodyHTML, &d.BodyText,
		&d.Lang, &d.Script,
		&d.OrigAuthor, &d.OrigAuthorNP,
		&d.OrigYear, &d.OrigMonth, &d.OrigDay,
		&d.FilePath, &d.UploadedBy, &d.CreatedAt,
	)
	d.ID = strconv.Itoa(id)
	d.BodyHTML = template.HTML(bodyHTML) // trusted: uploaded by authenticated users only
	return d, err
}

func queryDocuments(q, cat string) []Document {
	var rows *sql.Rows
	var err error

	catFilter := cat != "" && cat != "सबै"

	if q != "" {
		ftsQuery := strings.TrimSpace(q) + "*" // prefix match
		if catFilter {
			rows, err = db.Query(`
				SELECT `+docSelectCols+`
				FROM documents d
				WHERE d.id IN (SELECT rowid FROM documents_fts WHERE documents_fts MATCH ?)
				  AND d.category = ?
				ORDER BY d.created_at DESC
			`, ftsQuery, cat)
		} else {
			rows, err = db.Query(`
				SELECT `+docSelectCols+`
				FROM documents d
				WHERE d.id IN (SELECT rowid FROM documents_fts WHERE documents_fts MATCH ?)
				ORDER BY d.created_at DESC
			`, ftsQuery)
		}
	} else if catFilter {
		rows, err = db.Query(`
			SELECT `+docSelectCols+`
			FROM documents d
			WHERE d.category = ?
			ORDER BY d.created_at DESC
		`, cat)
	} else {
		rows, err = db.Query(`
			SELECT `+docSelectCols+`
			FROM documents d
			ORDER BY d.created_at DESC
		`)
	}

	if err != nil {
		log.Println("queryDocuments:", err)
		return nil
	}
	defer rows.Close()

	var docs []Document
	for rows.Next() {
		d, err := scanDoc(rows)
		if err != nil {
			log.Println("scan:", err)
			continue
		}
		docs = append(docs, d)
	}
	return docs
}

func getDocumentByID(id string) *Document {
	row := db.QueryRow(`
		SELECT `+docSelectCols+`
		FROM documents d
		WHERE d.id = ?
	`, id)
	d, err := scanDoc(row)
	if err != nil {
		return nil
	}
	return &d
}

func getExcerpts() []Excerpt {
	return []Excerpt{
		{Slug: "sugowlee", Title: "Treaty of Sugowlee, Dec. 2, 1815"},
	}
}

func getExcerptBySlug(slug string) *Excerpt {
	bodies := map[string]template.HTML{
		"sugowlee": `<p>Articles of Treaty concluded between the Honourable East India Company
		and the Rajah of Nepaul, signed by Lieutenant-Colonel Paris Bradshaw,
		Acting Political Agent at Nepaul, on the part of the Honourable East India Company,
		and by Gaj Raj Misser and Chunder Seekur Oophadaya, authorised Sirdars of the Rajah
		of Nepaul, on the part of His Highness the Rajah, on the 2nd day of December 1815.</p>`,
	}
	b, ok := bodies[slug]
	if !ok {
		return nil
	}
	return &Excerpt{Slug: slug, Title: "Treaty of Sugowlee, Dec. 2, 1815", Body: b}
}

// getBacklinks returns documents that link to the given document ID.
func getBacklinks(docID string) []BacklinkEntry {
	rows, err := db.Query(`
		SELECT d.id, d.title
		FROM links l
		JOIN documents d ON d.id = l.from_doc_id
		WHERE l.to_doc_id = ?
		ORDER BY d.title ASC
	`, docID)
	if err != nil {
		log.Println("getBacklinks:", err)
		return nil
	}
	defer rows.Close()
	var out []BacklinkEntry
	for rows.Next() {
		var e BacklinkEntry
		if err := rows.Scan(&e.ID, &e.Title); err == nil {
			out = append(out, e)
		}
	}
	return out
}

// saveLinksForDoc persists wiki-link refs for a document within a transaction.
// It first deletes all existing link rows for the doc, then re-inserts.
func saveLinksForDoc(tx *sql.Tx, docID int64, refs []LinkRef) error {
	if _, err := tx.Exec(`DELETE FROM links WHERE from_doc_id = ?`, docID); err != nil {
		return err
	}
	for _, ref := range refs {
		var toDocID interface{}
		if ref.ToDocID != nil {
			toDocID = *ref.ToDocID
		}
		if _, err := tx.Exec(
			`INSERT INTO links (from_doc_id, to_doc_id, to_slug, anchor) VALUES (?, ?, ?, ?)`,
			docID, toDocID, ref.ToSlug, ref.Anchor,
		); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Session management
// ---------------------------------------------------------------------------

func newToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func createSession(userID int) (string, error) {
	token := newToken()
	expires := time.Now().Add(30 * 24 * time.Hour).Format(time.RFC3339)
	_, err := db.Exec(`INSERT INTO sessions (token, user_id, expires_at) VALUES (?, ?, ?)`,
		token, userID, expires)
	return token, err
}

func deleteSession(token string) {
	db.Exec(`DELETE FROM sessions WHERE token = ?`, token)
}

func sessionUser(r *http.Request) *User {
	cookie, err := r.Cookie("session")
	if err != nil {
		return nil
	}
	var u User
	var expiresAt string
	err = db.QueryRow(`
		SELECT u.id, u.email, u.role, s.expires_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token = ?
	`, cookie.Value).Scan(&u.ID, &u.Email, &u.Role, &expiresAt)
	if err != nil {
		return nil
	}
	exp, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil || time.Now().After(exp) {
		deleteSession(cookie.Value)
		return nil
	}
	return &u
}

func setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   30 * 24 * 60 * 60,
	})
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
}

// ---------------------------------------------------------------------------
// Auth middleware
// ---------------------------------------------------------------------------

func requireRole(role string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := sessionUser(r)
		if u == nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		if !hasRole(u.Role, role) {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

// ---------------------------------------------------------------------------
// Template rendering
// ---------------------------------------------------------------------------

func render(w http.ResponseWriter, r *http.Request, page string, data PageData) {
	if data.User == nil {
		data.User = sessionUser(r)
	}
	t, err := template.ParseFiles(
		"templates/base.html",
		"templates/"+page+".html",
	)
	if err != nil {
		http.Error(w, "Template error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := t.ExecuteTemplate(w, "base", data); err != nil {
		log.Println("render:", err)
	}
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func indexHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	cat := r.URL.Query().Get("cat")
	render(w, r, "index", PageData{
		Title:      "Home",
		Query:      q,
		Category:   cat,
		Categories: docCategories,
		Documents:  queryDocuments(q, cat),
	})
}

func aboutHandler(w http.ResponseWriter, r *http.Request) {
	render(w, r, "about", PageData{Title: "About Yogi Narharinath — योगीबारे"})
}

func worksHandler(w http.ResponseWriter, r *http.Request) {
	render(w, r, "works", PageData{Title: "Works — ग्रन्थावली"})
}

func excerptsHandler(w http.ResponseWriter, r *http.Request) {
	render(w, r, "excerpts", PageData{
		Title:    "Excerpts — अंशहरू",
		Excerpts: getExcerpts(),
	})
}

func excerptHandler(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	exc := getExcerptBySlug(slug)
	if exc == nil {
		http.NotFound(w, r)
		return
	}
	render(w, r, "excerpt", PageData{Title: exc.Title, Exc: exc})
}

func documentHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	doc := getDocumentByID(id)
	if doc == nil {
		http.NotFound(w, r)
		return
	}

	// Inject TOC heading IDs at read time (presentational, not stored).
	var toc []TOCEntry
	if doc.BodyHTML != "" {
		newHTML, entries := extractTOC(string(doc.BodyHTML))
		doc.BodyHTML = template.HTML(newHTML)
		toc = entries
	}

	render(w, r, "document", PageData{
		Title:           doc.Title,
		Doc:             doc,
		Backlinks:       getBacklinks(id),
		TOC:             toc,
		Summary:         getDocSummary(id),
		Entities:        getDocEntities(id),
		LinkSuggestions: getLinkSuggestionsForDoc(id),
	})
}

func editGetHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	doc := getDocumentByID(id)
	if doc == nil {
		http.NotFound(w, r)
		return
	}
	render(w, r, "edit", PageData{Title: "Edit: " + doc.Title, Doc: doc})
}

func editPostHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	title := r.FormValue("title")
	titleNP := r.FormValue("title_np")

	// Parse wiki links before storing.
	rawBody := r.FormValue("body_html")
	renderedBody, refs := parseWikiLinks(rawBody)
	bodyText := stripHTML(renderedBody)

	slug := slugify(title)
	if slug == "" {
		slug = slugify(titleNP)
	}

	docID, _ := strconv.Atoi(id)
	editedBy := 0
	if u := sessionUser(r); u != nil {
		editedBy = u.ID
	}

	tx, err := db.Begin()
	if err != nil {
		log.Println("editPost begin tx:", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	_, err = tx.Exec(`
		UPDATE documents SET
			title          = ?,
			title_np       = ?,
			slug           = ?,
			category       = ?,
			description    = ?,
			lang           = ?,
			script         = ?,
			orig_author    = ?,
			orig_author_np = ?,
			orig_year      = ?,
			orig_month     = ?,
			orig_day       = ?,
			body_html      = ?,
			body_text      = ?
		WHERE id = ?
	`,
		title, titleNP, slug,
		r.FormValue("category"),
		r.FormValue("description"),
		r.FormValue("lang"),
		r.FormValue("script"),
		r.FormValue("orig_author"),
		r.FormValue("orig_author_np"),
		r.FormValue("orig_year"),
		r.FormValue("orig_month"),
		r.FormValue("orig_day"),
		renderedBody, bodyText,
		id,
	)
	if err != nil {
		tx.Rollback()
		log.Println("editPost update:", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	// Save revision (post-write state). Comment is optional but encouraged.
	editComment := strings.TrimSpace(r.FormValue("edit_comment"))
	_, err = tx.Exec(
		`INSERT INTO revisions (doc_id, title, title_np, body_html, body_text, edited_by, edited_at, comment)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		docID, title, titleNP, renderedBody, bodyText, editedBy,
		time.Now().UTC().Format(time.RFC3339), editComment,
	)
	if err != nil {
		tx.Rollback()
		log.Println("editPost revision:", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	// Rewrite links table for this document.
	if err := saveLinksForDoc(tx, int64(docID), refs); err != nil {
		tx.Rollback()
		log.Println("editPost links:", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(); err != nil {
		log.Println("editPost commit:", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/document/"+id, http.StatusSeeOther)
}

func uploadGetHandler(w http.ResponseWriter, r *http.Request) {
	render(w, r, "upload", PageData{Title: "Contribute — योगदान"})
}

func uploadPostHandler(w http.ResponseWriter, r *http.Request) {
	const maxUploadSize = 50 << 20 // 50 MB
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		render(w, r, "upload", PageData{Title: "Contribute — योगदान", Error: "Title is required."})
		return
	}

	// Handle optional file attachment
	filePath := ""
	file, header, fileErr := r.FormFile("file")
	if fileErr == nil {
		defer file.Close()

		// Ensure upload directory exists
		if err := os.MkdirAll("static/docs", 0755); err != nil {
			log.Println("mkdir static/docs:", err)
			http.Error(w, "Server error", http.StatusInternalServerError)
			return
		}

		// Build a unique filename: timestamp + original name
		ext := filepath.Ext(header.Filename)
		safeName := fmt.Sprintf("%d%s", time.Now().UnixNano(), ext)
		dst := filepath.Join("static", "docs", safeName)

		out, err := os.Create(dst)
		if err != nil {
			log.Println("create file:", err)
			http.Error(w, "Server error", http.StatusInternalServerError)
			return
		}
		defer out.Close()

		if _, err := io.Copy(out, file); err != nil {
			log.Println("copy file:", err)
			http.Error(w, "Server error", http.StatusInternalServerError)
			return
		}
		filePath = "/static/docs/" + safeName
	}

	// Body content from Quill — parse wiki links before storing.
	titleNP := r.FormValue("title_np")
	rawBody := r.FormValue("body_html")
	renderedBody, refs := parseWikiLinks(rawBody)
	bodyText := stripHTML(renderedBody)

	slug := slugify(title)
	if slug == "" {
		slug = slugify(titleNP)
	}

	// Uploader attribution.
	uploadedBy := 0
	if u := sessionUser(r); u != nil {
		uploadedBy = u.ID
	}

	tx, err := db.Begin()
	if err != nil {
		log.Println("uploadPost begin tx:", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	result, err := tx.Exec(`
		INSERT INTO documents (
			title, title_np, slug, category, description,
			body_html, body_text,
			lang, script,
			orig_author, orig_author_np,
			orig_year, orig_month, orig_day,
			file_path, uploaded_by, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		title, titleNP, slug,
		r.FormValue("category"),
		r.FormValue("description"),
		renderedBody, bodyText,
		r.FormValue("doc_lang"),
		r.FormValue("script"),
		r.FormValue("orig_author"),
		r.FormValue("orig_author_np"),
		r.FormValue("orig_year"),
		r.FormValue("orig_month"),
		r.FormValue("orig_day"),
		filePath, uploadedBy,
		time.Now().Format("2 Jan 2006"),
	)
	if err != nil {
		tx.Rollback()
		log.Println("uploadPost insert:", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	docID, _ := result.LastInsertId()

	// Save initial revision (comment defaults to "initial upload").
	_, err = tx.Exec(
		`INSERT INTO revisions (doc_id, title, title_np, body_html, body_text, edited_by, edited_at, comment)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		docID, title, titleNP, renderedBody, bodyText, uploadedBy,
		time.Now().UTC().Format(time.RFC3339), "initial upload",
	)
	if err != nil {
		tx.Rollback()
		log.Println("uploadPost revision:", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	// Save wiki links.
	if err := saveLinksForDoc(tx, docID, refs); err != nil {
		tx.Rollback()
		log.Println("uploadPost links:", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(); err != nil {
		log.Println("uploadPost commit:", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/document/"+strconv.FormatInt(docID, 10), http.StatusSeeOther)
}

func missionHandler(w http.ResponseWriter, r *http.Request) {
	render(w, r, "mission", PageData{Title: "Mission — उद्देश्य"})
}

func similarHandler(w http.ResponseWriter, r *http.Request) {
	render(w, r, "similar", PageData{Title: "Similar Sites — अरु साइट"})
}

func storeHandler(w http.ResponseWriter, r *http.Request) {
	render(w, r, "store", PageData{Title: "Store — पसल"})
}

func loginGetHandler(w http.ResponseWriter, r *http.Request) {
	if sessionUser(r) != nil {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}
	render(w, r, "login", PageData{Title: "Login"})
}

func loginPostHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	email := strings.TrimSpace(r.FormValue("email"))
	password := r.FormValue("password")

	var u User
	var hash string
	err := db.QueryRow(
		`SELECT id, email, password_hash, role FROM users WHERE email = ?`, email,
	).Scan(&u.ID, &u.Email, &hash, &u.Role)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		render(w, r, "login", PageData{Title: "Login", Error: "Invalid email or password."})
		return
	}

	token, err := createSession(u.ID)
	if err != nil {
		http.Error(w, "Session error", http.StatusInternalServerError)
		return
	}
	setSessionCookie(w, token)
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func logoutHandler(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("session"); err == nil {
		deleteSession(cookie.Value)
	}
	clearSessionCookie(w)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func dashboardHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	cat := r.URL.Query().Get("cat")
	render(w, r, "dashboard", PageData{
		Title:      "Dashboard",
		Query:      q,
		Category:   cat,
		Categories: docCategories,
		Documents:  queryDocuments(q, cat),
	})
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

func main() {
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "yogilib.db"
	}
	if err := initDB(dbPath); err != nil {
		log.Fatal("db:", err)
	}
	defer db.Close()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	mux := http.NewServeMux()

	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	// Public — no login needed
	mux.HandleFunc("GET /{$}", indexHandler)
	mux.HandleFunc("GET /about", aboutHandler)
	mux.HandleFunc("GET /works", worksHandler)
	mux.HandleFunc("GET /excerpts", excerptsHandler)
	mux.HandleFunc("GET /excerpts/{slug}", excerptHandler)
	mux.HandleFunc("GET /document/{id}", documentHandler)
	mux.HandleFunc("GET /mission", missionHandler)
	mux.HandleFunc("GET /similar", similarHandler)
	mux.HandleFunc("GET /store", storeHandler)
	mux.HandleFunc("GET /login", loginGetHandler)
	mux.HandleFunc("POST /login", loginPostHandler)
	mux.HandleFunc("POST /logout", logoutHandler)

	// Uploader+ only
	mux.HandleFunc("GET /upload", requireRole("uploader", uploadGetHandler))
	mux.HandleFunc("POST /upload", requireRole("uploader", uploadPostHandler))

	// Admin only
	mux.HandleFunc("GET /dashboard", requireRole("admin", dashboardHandler))
	mux.HandleFunc("GET /document/{id}/edit", requireRole("admin", editGetHandler))
	mux.HandleFunc("POST /document/{id}/edit", requireRole("admin", editPostHandler))

	// Day 3: revision history, diff, rollback, wanted links.
	mux.HandleFunc("GET /document/{id}/history", historyHandler)
	mux.HandleFunc("GET /document/{id}/diff", revisionDiffHandler)
	mux.HandleFunc("POST /document/{id}/revisions/{rev}/rollback",
		requireRole("admin", revisionRollbackHandler))
	mux.HandleFunc("GET /wanted", wantedHandler)

	// Admin: enrichment status (read-only) + manual trigger.
	mux.HandleFunc("GET /admin/enrich", requireRole("admin", enrichStatusHandler))
	mux.HandleFunc("POST /admin/enrich/run", requireRole("admin", enrichRunHandler))

	// Kick off the local-model enrichment worker (background).
	// Disable with ENRICH_DISABLED=1 if Ollama isn't available.
	if os.Getenv("ENRICH_DISABLED") != "1" {
		startEnrichWorker(context.Background())
		log.Printf("%s scheduled (ollama=%s)", enrichGoroutineName, ollamaURL())
	}

	log.Printf("yogilib → http://localhost:%s", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}
