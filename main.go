// yogilib — Go HTTP server (stdlib only, no external deps)
// Run:  go run main.go
// Build: go build -o yogilib .
//
// All route handlers are stubs with TODO comments marking exactly where to plug
// in database queries, authentication, and file storage.
package main

import (
	"html/template"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Data types
// ---------------------------------------------------------------------------
// A programmer connects these to the real database (Postgres, SQLite, etc.)
// by replacing the mock slices below with actual queries.

type Document struct {
	ID          string
	Title       string
	TitleNP     string        // Nepali title
	Category    string        // किताब | कागजात | रेकर्ड | पत्रिका | अन्य
	Description string
	FilePath    string        // relative URL to the stored file
	CreatedAt   string        // formatted date string for display
}

type Excerpt struct {
	Slug  string
	Title string
	Body  template.HTML // stored as HTML in DB; mark safe before passing
}

type StoreItem struct {
	ID          string
	Title       string
	TitleNP     string
	Description string
	ImageURL    string
	BuyURL      string
}

// User is set by an auth middleware once sessions are wired up.
type User struct {
	ID    string
	Email string
}

// PageData is the single data envelope passed to every template.
// Only populate the fields relevant to each page.
type PageData struct {
	Title      string
	Query      string        // search query string
	Category   string        // active category filter
	Categories []string      // list of category tabs
	Flash      string        // success message
	Error      string        // error message
	Documents  []Document
	Doc        *Document
	Excerpts   []Excerpt
	Exc        *Excerpt
	StoreItems []StoreItem
	User       *User         // nil when not logged in
}

// ---------------------------------------------------------------------------
// Mock data
// ---------------------------------------------------------------------------
// TODO: replace every function below with a real DB query.
// Suggested schema is in README.md.

var docCategories = []string{"सबै", "किताब", "कागजात", "रेकर्ड", "पत्रिका", "अंश", "अन्य"}

func mockDocuments() []Document {
	// TODO: SELECT * FROM documents ORDER BY created_at DESC LIMIT 50
	return []Document{
		{
			ID: "1", Title: "Treaty of Sugowlee",
			TitleNP:     "सुगौली सन्धि",
			Category:    "कागजात",
			Description: "Signed December 2, 1815 between East India Company and the Kingdom of Nepal.",
			FilePath:    "", // TODO: object-storage URL
			CreatedAt:   time.Now().AddDate(0, 0, -3).Format("2 Jan 2006"),
		},
	}
}

func mockExcerpts() []Excerpt {
	// TODO: SELECT slug, title FROM excerpts ORDER BY created_at DESC
	return []Excerpt{
		{Slug: "sugowlee", Title: "Treaty of Sugowlee, Dec. 2, 1815"},
	}
}

func mockExcerptBySlug(slug string) *Excerpt {
	// TODO: SELECT * FROM excerpts WHERE slug = $1
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
	title := slug
	for _, e := range mockExcerpts() {
		if e.Slug == slug {
			title = e.Title
			break
		}
	}
	return &Excerpt{Slug: slug, Title: title, Body: b}
}

func mockDocumentByID(id string) *Document {
	// TODO: SELECT * FROM documents WHERE id = $1
	for _, d := range mockDocuments() {
		if d.ID == id {
			return &d
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Template rendering
// ---------------------------------------------------------------------------

func render(w http.ResponseWriter, page string, data PageData) {
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

	// TODO: pass q and cat to DB query instead of filtering in memory
	docs := mockDocuments()
	var filtered []Document
	for _, d := range docs {
		if cat != "" && cat != "सबै" && d.Category != cat {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(d.Title), strings.ToLower(q)) &&
			!strings.Contains(d.TitleNP, q) {
			continue
		}
		filtered = append(filtered, d)
	}

	render(w, "index", PageData{
		Title:      "Home",
		Query:      q,
		Category:   cat,
		Categories: docCategories,
		Documents:  filtered,
	})
}

func aboutHandler(w http.ResponseWriter, r *http.Request) {
	render(w, "about", PageData{Title: "About Yogi Narharinath — योगीबारे"})
}

func worksHandler(w http.ResponseWriter, r *http.Request) {
	render(w, "works", PageData{Title: "Works — ग्रन्थावली"})
}

func excerptsHandler(w http.ResponseWriter, r *http.Request) {
	render(w, "excerpts", PageData{
		Title:    "Excerpts — अंशहरू",
		Excerpts: mockExcerpts(),
	})
}

func excerptHandler(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	exc := mockExcerptBySlug(slug)
	if exc == nil {
		http.NotFound(w, r)
		return
	}
	render(w, "excerpt", PageData{Title: exc.Title, Exc: exc})
}

func documentHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	doc := mockDocumentByID(id)
	if doc == nil {
		http.NotFound(w, r)
		return
	}
	render(w, "document", PageData{Title: doc.Title, Doc: doc})
}

func editGetHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	doc := mockDocumentByID(id)
	if doc == nil {
		http.NotFound(w, r)
		return
	}
	// TODO: require auth — redirect to /login if no session
	render(w, "edit", PageData{Title: "Edit: " + doc.Title, Doc: doc})
}

func editPostHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	// TODO:
	//   1. Require auth
	//   2. Parse multipart form
	//   3. Validate fields
	//   4. UPDATE documents SET title=$1, title_np=$2, category=$3, description=$4 WHERE id=$5
	//   5. If new file: replace in object storage, update file_path
	//   6. Redirect to /document/{id}
	_ = id
	http.Redirect(w, r, "/document/"+id, http.StatusSeeOther)
}

func uploadGetHandler(w http.ResponseWriter, r *http.Request) {
	render(w, "upload", PageData{Title: "Contribute — योगदान"})
}

func uploadPostHandler(w http.ResponseWriter, r *http.Request) {
	// TODO:
	//   1. Parse multipart form (r.ParseMultipartForm)
	//   2. Validate fields and file
	//   3. Upload file to object storage → get URL
	//   4. INSERT INTO documents (title, title_np, category, description, file_path, created_at)
	//   5. Redirect to /document/{new_id}
	render(w, "upload", PageData{
		Title: "Contribute — योगदान",
		Flash: "Upload received. (Backend not yet connected — this is a placeholder.)",
	})
}

func missionHandler(w http.ResponseWriter, r *http.Request) {
	render(w, "mission", PageData{Title: "Mission — उद्देश्य"})
}

func similarHandler(w http.ResponseWriter, r *http.Request) {
	render(w, "similar", PageData{Title: "Similar Sites — अरु साइट"})
}

func storeHandler(w http.ResponseWriter, r *http.Request) {
	// TODO: SELECT * FROM store_items WHERE active = true ORDER BY sort_order
	render(w, "store", PageData{Title: "Store — पसल"})
}

func loginGetHandler(w http.ResponseWriter, r *http.Request) {
	render(w, "login", PageData{Title: "Login"})
}

func loginPostHandler(w http.ResponseWriter, r *http.Request) {
	// TODO:
	//   1. r.ParseForm()
	//   2. Fetch user by email: SELECT id, password_hash FROM users WHERE email = $1
	//   3. bcrypt.CompareHashAndPassword(hash, []byte(password))
	//   4. On success: create session (e.g. gorilla/sessions or a signed cookie)
	//   5. http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
	//   6. On failure: re-render login with .Error
	render(w, "login", PageData{
		Title: "Login",
		Error: "Login not yet connected to a backend.",
	})
}

func logoutHandler(w http.ResponseWriter, r *http.Request) {
	// TODO: invalidate session cookie
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func dashboardHandler(w http.ResponseWriter, r *http.Request) {
	// TODO: auth middleware — check session, redirect to /login if absent
	q := r.URL.Query().Get("q")
	cat := r.URL.Query().Get("cat")

	docs := mockDocuments() // TODO: DB query with q + cat filters
	render(w, "dashboard", PageData{
		Title:      "Dashboard",
		Query:      q,
		Category:   cat,
		Categories: docCategories,
		Documents:  docs,
	})
}

// ---------------------------------------------------------------------------
// Auth middleware stub
// ---------------------------------------------------------------------------
// Wire this up once sessions are implemented.
// Example:
//
//	func requireAuth(next http.HandlerFunc) http.HandlerFunc {
//	    return func(w http.ResponseWriter, r *http.Request) {
//	        user := sessionUser(r) // read from signed cookie or JWT
//	        if user == nil {
//	            http.Redirect(w, r, "/login", http.StatusSeeOther)
//	            return
//	        }
//	        // Optionally inject user into context:
//	        // ctx := context.WithValue(r.Context(), ctxUserKey, user)
//	        // next(w, r.WithContext(ctx))
//	        next(w, r)
//	    }
//	}
//
// Then protect routes:
//   mux.HandleFunc("GET /dashboard", requireAuth(dashboardHandler))
//   mux.HandleFunc("GET /upload",    requireAuth(uploadGetHandler))

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	mux := http.NewServeMux()

	// Static files (CSS, fonts, images)
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	// Public pages
	mux.HandleFunc("GET /{$}", indexHandler)
	mux.HandleFunc("GET /about", aboutHandler)
	mux.HandleFunc("GET /works", worksHandler)
	mux.HandleFunc("GET /document/{id}", documentHandler)
	mux.HandleFunc("GET /document/{id}/edit", editGetHandler)
	mux.HandleFunc("POST /document/{id}/edit", editPostHandler)
	mux.HandleFunc("GET /login", loginGetHandler)
	mux.HandleFunc("POST /login", loginPostHandler)
	mux.HandleFunc("POST /logout", logoutHandler)

	// Protected pages (add requireAuth wrapper once auth is implemented)
	mux.HandleFunc("GET /upload", uploadGetHandler)
	mux.HandleFunc("POST /upload", uploadPostHandler)
	mux.HandleFunc("GET /dashboard", dashboardHandler)

	log.Printf("yogilib → http://localhost:%s", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}
