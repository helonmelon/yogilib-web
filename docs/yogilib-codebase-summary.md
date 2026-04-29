# Yogilib Codebase Summary

## 1) Project Purpose

**Yogilib** is a **pure Go** web application that serves as a digital archive for the works of **Yogi Narharinath** (योगी नरहरिनाथ) — a Nepali scholar, historian, and religious figure (1971–2059). 

The site allows visitors to:
- Browse and search historical documents in **English**, **Nepali (Devanagari)**, and **Sanskrit**
- Read document content (PDF embed / download)
- Access transcribed text excerpts
- Contribute new material (requires login)
- Administrate content via a dashboard (admin-only)

It emphasizes **local, zero-cloud operation** with SQLite for data and no external build pipelines.

---

## 2) Tech Stack

```
Browser 
  → net/http Mux (Go 1.22+) 
    → Auth Middleware (session cookies, bcrypt)
      → Handlers 
        → html/template
```

| Layer | Technology | Notes |
|-------|------------|-------|
| **Server** | Go `net/http` + `html/template` | No frameworks, no build step |
| **Database** | SQLite + `modernc.org/sqlite` (pure Go, no CGO) | FTS5 full-text search with `unicode61` tokenizer |
| **Auth** | Session cookies (HttpOnly, 30-day expiry) + bcrypt passwords | Session data stored in `sessions` table |
| **File Storage** | In `static/docs/` (local) | Object storage (R2, S3) supported in design but not connected |
| **Static Assets** | Go `http.FileServer` | CSS, fonts, images, JS converters |
| **Rich Text Editor** | Quill.js 1.3.7 (CDN) | WYSIWYG + HTML source mode |
| **DevDependencies** | None — standard Go only | Requires Go 1.22+ for `http.NewServeMux` pattern |

---

## 3) Current Features (What Works)

### ✅ Functional
- **Document browsing** — home, works, document viewer with PDF download/embed
- **Search** — FTS5 full-text search on title, title_np, description, body_text (Nepali + English)
- **Excerpts page** — pre-loaded text excerpts (e.g., Treaty of Sugowlee)
- **Contribute form** — public upload of documents (title, body, file, metadata)
- **Admin dashboard** — admin-only document management
- **Document editing** — admin edit form (save document metadata)
- **Login system** — role-based auth (viewer / uploader / admin)
- **Preeti converter** — ASCII → Devanagari (live on fields & paste→paste)
- **ITRANS converter** — Sanskrit ASCII transliteration → Devanagari
- **Auto-date** — upload timestamp recorded in `created_at`
- **Migrations** — schema upgrades via `PRAGMA user_version`

### ✅ Mock Data
- Seed documents: Treaty of Sugowlee (सुगौली सन्धि)
- Seed users: `admin@yogilib.org` / `upload@yogilib.org` (bcrypt-hashed)
- Static excerpts map (`getExcerptBySlug`)

### ❌ Not Implemented
- **Object storage integration** — file uploads stored locally in `static/docs/`
- **Real search on body_text** — currently indexes via FTS5, but document body not fully populated
- **Email notifications** — new document alerts, admin alerts
- **Export / print** — PDF generation or bulk export
- **Document categorization filters** — category tabs present but not fully functional
- **User management** — adding/deleting users via UI (SQL-only)
- **Document versioning** — edits overwrite without history
- **Rich text editor persistence** — Quill syncs but no state management

---

## 4) What's Missing / TODO

### Backend
- [ ] **Object storage** — integrate S3/R2/MinIO for `file_path` column
- [ ] **File upload validation** — MIME type checks, virus scan, max size enforcement
- [ ] **Email notifications** — notify admins on upload, on edit, on delete
- [ ] **Full document body indexing** — `body_text` not fully populated in mock data
- [ ] **Document versioning** — track changes history
- [ ] **Export features** — PDF generation, Excel export
- [ ] **Bulk import** — CSV / JSON importer for bulk documents

### Frontend
- [ ] **Image upload** — drag-drop zone functional but no thumbnail preview
- [ ] **Responsive menus** — mobile nav improvements
- [ ] **Accessibility** — ARIA labels, keyboard navigation, screen reader support

### DevOps
- [ ] **CI/CD** — GitHub Actions / Fly deployment workflow (workflow file exists but not linked)
- [ ] **Docker container** — containerization for deployment
- [ ] **Environment config** — `.env` / `config.yaml` for secrets
- [ ] **Database backups** — automated SQLite backup strategy

### Documentation
- [ ] **API docs** — OpenAPI / Swagger spec
- [ ] **Deployment guide** — step-by-step Fly/Heroku deployment
- [ ] **Troubleshooting** — common error messages & fixes

---

## 5) Architecture Overview

### Data Flow (Request → Response)

```
Browser GET /document/1
  → net/http Mux
  → documentHandler(w, r)
  → getDocumentByID("1")
     → SQL: SELECT * FROM documents WHERE id = 1
  → scanDoc(rows) → Document struct
  → render(w, "document", PageData{Doc: doc})
     → template.ParseFiles("base.html", "document.html")
     → t.ExecuteTemplate(w, "base", data)
  → HTML response
```

### Template System
- **`base.html`** — full page shell (DOCTYPE, head, admin bar, header, nav, main, footer)
- **`{page}.html`** — defines `{{define "content"}}` slot for page-specific body
- **`PageData`** — single data envelope passed to all templates

### Routing Table

| Method | Route | Handler | Auth |
|--------|-------|---------|------|
| GET | `/{$}` | `indexHandler` | Public |
| GET | `/about` | `aboutHandler` | Public |
| GET | `/works` | `worksHandler` | Public |
| GET | `/excerpts` | `excerptsHandler` | Public |
| GET | `/excerpts/{slug}` | `excerptHandler` | Public |
| GET | `/document/{id}` | `documentHandler` | Public |
| GET | `/mission` | `missionHandler` | Public |
| GET | `/similar` | `similarHandler` | Public |
| GET | `/store` | `storeHandler` | Public |
| GET | `/login` | `loginGetHandler` | Public |
| POST | `/login` | `loginPostHandler` | Public |
| POST | `/logout` | `logoutHandler` | Public |
| GET | `/upload` | `uploadGetHandler` | Uploader+ |
| POST | `/upload` | `uploadPostHandler` | Uploader+ |
| GET | `/dashboard` | `dashboardHandler` | Admin |
| GET | `/document/{id}/edit` | `editGetHandler` | Admin |
| POST | `/document/{id}/edit` | `editPostHandler` | Admin |

### Database Schema

```sql
-- Documents
documents (
  id INT PRIMARY KEY AUTOINCREMENT,
  title TEXT NOT NULL,
  title_np TEXT,
  category TEXT,
  description TEXT,
  body_html TEXT,
  body_text TEXT,
  lang TEXT,
  script TEXT,
  orig_author TEXT,
  orig_author_np TEXT,
  orig_year TEXT,
  orig_month TEXT,
  orig_day TEXT,
  file_path TEXT,
  uploaded_by INT,
  created_at TEXT
)

-- FTS5 search
documents_fts (fts5 with title, title_np, description, body_text)

-- Auth
users (id, email, password_hash, role)
sessions (token, user_id, expires_at)
```

**Migration support** — schema versioning via `PRAGMA user_version` allows safe upgrades to existing DBs.

---

## 6) Key Functions in main.go

### Data Structures
- `Document` — full document metadata + body
- `Excerpt` — transcribed passage
- `StoreItem` — shop item (mock)
- `User` — authenticated user
- `PageData` — template data envelope

### Core Functions
- `initDB(path string) error` — creates SQLite DB, runs migrations, seeds data
- `queryDocuments(q, cat string) []Document` — FTS5 + category filter query
- `getDocumentByID(id string) *Document` — single document lookup
- `getExcerpts() []Excerpt` — list of excerpts
- `getExcerptBySlug(slug string) *Excerpt` — single excerpt
- `sessionUser(r *http.Request) *User` — reads session cookie, returns user
- `createSession(userID int) (string, error)` — generates 32-byte hex token
- `requireRole(role string, next http.HandlerFunc) http.HandlerFunc` — middleware for auth
- `render(w, page string, data PageData)` — executes template with data

### Handlers
- `indexHandler` — home page with search/category params
- `documentHandler` — document viewer
- `uploadGet/PostHandler` — document contribution
- `editGet/PostHandler` — admin edit document
- `dashboardHandler` — admin document grid
- `loginGet/PostHandler` — authentication
- `logoutHandler` — session clear

---

## Quick Start

```bash
go mod tidy
go run main.go          # http://localhost:8080
go build -o yogilib .   # production binary
PORT=9000 DB_PATH=yogilib.db ./yogilib
```

**Default accounts** (development only):
| Email | Password | Role |
|-------|----------|------|
| `admin@yogilib.org` | `admin123` | admin |
| `upload@yogilib.org` | `upload123` | uploader |
