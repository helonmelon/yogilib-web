// yogilib — Go HTTP server
// Run:   go run main.go
// Build: go build -o yogilib .
package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
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
	ID          string
	Title       string
	TitleNP     string
	Category    string
	Description string
	FilePath    string
	CreatedAt   string
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
	User       *User // nil when not logged in
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
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		title       TEXT NOT NULL,
		title_np    TEXT,
		category    TEXT,
		description TEXT,
		file_path   TEXT,
		created_at  TEXT NOT NULL
	);

	CREATE VIRTUAL TABLE IF NOT EXISTS documents_fts USING fts5(
		title, title_np, description,
		content='documents', content_rowid='id',
		tokenize='unicode61'
	);

	CREATE TRIGGER IF NOT EXISTS docs_ai AFTER INSERT ON documents BEGIN
		INSERT INTO documents_fts(rowid, title, title_np, description)
		VALUES (new.id, new.title, COALESCE(new.title_np,''), COALESCE(new.description,''));
	END;

	CREATE TRIGGER IF NOT EXISTS docs_ad AFTER DELETE ON documents BEGIN
		INSERT INTO documents_fts(documents_fts, rowid, title, title_np, description)
		VALUES ('delete', old.id, old.title, COALESCE(old.title_np,''), COALESCE(old.description,''));
	END;

	CREATE TRIGGER IF NOT EXISTS docs_au AFTER UPDATE ON documents BEGIN
		INSERT INTO documents_fts(documents_fts, rowid, title, title_np, description)
		VALUES ('delete', old.id, old.title, COALESCE(old.title_np,''), COALESCE(old.description,''));
		INSERT INTO documents_fts(rowid, title, title_np, description)
		VALUES (new.id, new.title, COALESCE(new.title_np,''), COALESCE(new.description,''));
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

	return seedData()
}

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
// DB query functions
// ---------------------------------------------------------------------------

func queryDocuments(q, cat string) []Document {
	var rows *sql.Rows
	var err error

	catFilter := cat != "" && cat != "सबै"

	if q != "" {
		ftsQuery := strings.TrimSpace(q) + "*" // prefix match
		if catFilter {
			rows, err = db.Query(`
				SELECT d.id, d.title, COALESCE(d.title_np,''), d.category,
				       COALESCE(d.description,''), COALESCE(d.file_path,''), d.created_at
				FROM documents d
				WHERE d.id IN (SELECT rowid FROM documents_fts WHERE documents_fts MATCH ?)
				  AND d.category = ?
				ORDER BY d.created_at DESC
			`, ftsQuery, cat)
		} else {
			rows, err = db.Query(`
				SELECT d.id, d.title, COALESCE(d.title_np,''), d.category,
				       COALESCE(d.description,''), COALESCE(d.file_path,''), d.created_at
				FROM documents d
				WHERE d.id IN (SELECT rowid FROM documents_fts WHERE documents_fts MATCH ?)
				ORDER BY d.created_at DESC
			`, ftsQuery)
		}
	} else if catFilter {
		rows, err = db.Query(`
			SELECT id, title, COALESCE(title_np,''), category,
			       COALESCE(description,''), COALESCE(file_path,''), created_at
			FROM documents WHERE category = ?
			ORDER BY created_at DESC
		`, cat)
	} else {
		rows, err = db.Query(`
			SELECT id, title, COALESCE(title_np,''), category,
			       COALESCE(description,''), COALESCE(file_path,''), created_at
			FROM documents ORDER BY created_at DESC
		`)
	}

	if err != nil {
		log.Println("queryDocuments:", err)
		return nil
	}
	defer rows.Close()

	var docs []Document
	for rows.Next() {
		var d Document
		var id int
		if err := rows.Scan(&id, &d.Title, &d.TitleNP, &d.Category, &d.Description, &d.FilePath, &d.CreatedAt); err != nil {
			log.Println("scan:", err)
			continue
		}
		d.ID = strconv.Itoa(id)
		docs = append(docs, d)
	}
	return docs
}

func getDocumentByID(id string) *Document {
	var d Document
	var idInt int
	err := db.QueryRow(`
		SELECT id, title, COALESCE(title_np,''), category,
		       COALESCE(description,''), COALESCE(file_path,''), created_at
		FROM documents WHERE id = ?
	`, id).Scan(&idInt, &d.Title, &d.TitleNP, &d.Category, &d.Description, &d.FilePath, &d.CreatedAt)
	if err != nil {
		return nil
	}
	d.ID = strconv.Itoa(idInt)
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
	render(w, r, "document", PageData{Title: doc.Title, Doc: doc})
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
	_, err := db.Exec(`
		UPDATE documents SET title=?, title_np=?, category=?, description=? WHERE id=?
	`, r.FormValue("title"), r.FormValue("title_np"), r.FormValue("category"), r.FormValue("description"), id)
	if err != nil {
		log.Println("editPost:", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/document/"+id, http.StatusSeeOther)
}

func uploadGetHandler(w http.ResponseWriter, r *http.Request) {
	render(w, r, "upload", PageData{Title: "Contribute — योगदान"})
}

func uploadPostHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	title := r.FormValue("title")
	if title == "" {
		render(w, r, "upload", PageData{Title: "Contribute — योगदान", Error: "Title is required."})
		return
	}
	result, err := db.Exec(`
		INSERT INTO documents (title, title_np, category, description, file_path, created_at)
		VALUES (?, ?, ?, ?, '', ?)
	`, title, r.FormValue("title_np"), r.FormValue("category"), r.FormValue("description"),
		time.Now().Format("2 Jan 2006"))
	if err != nil {
		log.Println("uploadPost:", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	id, _ := result.LastInsertId()
	http.Redirect(w, r, "/document/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
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

	log.Printf("yogilib → http://localhost:%s", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}
