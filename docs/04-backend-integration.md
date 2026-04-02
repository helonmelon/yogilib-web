# Backend Integration Guide

This document describes exactly what needs to be connected to turn the current mock-data frontend into a fully functional production system.

## Database Schema (PostgreSQL)

```sql
-- Core document table
CREATE TABLE documents (
    id            SERIAL PRIMARY KEY,
    title         TEXT NOT NULL,
    title_np      TEXT,                        -- Nepali title (Unicode)
    category      TEXT,                        -- किताब | कागजात | रेकर्ड | पत्रिका | अंश | अन्य
    description   TEXT,
    body_html     TEXT,                        -- rich text body from Quill editor
    body_text     TEXT,                        -- plain text version for search
    lang          TEXT,                        -- en | ne | sa | ne-sa | mixed
    script        TEXT,                        -- devanagari | latin | iast | mixed
    orig_author   TEXT,                        -- original author (English)
    orig_author_np TEXT,                       -- original author (Nepali Unicode)
    orig_year     TEXT,                        -- original document year (free text)
    orig_month    TEXT,                        -- 01–12
    orig_day      INTEGER,                     -- 1–31
    file_path     TEXT,                        -- URL from object storage
    uploaded_by   INTEGER REFERENCES users(id),
    created_at    TIMESTAMPTZ DEFAULT now()    -- auto-recorded upload timestamp
);

-- Full-text search index (Nepali + English)
CREATE INDEX documents_fts ON documents
    USING GIN (to_tsvector('english', title || ' ' || COALESCE(description, '')));

-- Excerpts (transcribed passages)
CREATE TABLE excerpts (
    id         SERIAL PRIMARY KEY,
    slug       TEXT UNIQUE NOT NULL,
    title      TEXT NOT NULL,
    title_np   TEXT,
    body_html  TEXT,
    lang       TEXT DEFAULT 'ne',              -- ne | sa | ne-sa | en
    created_at TIMESTAMPTZ DEFAULT now()
);

-- Users / contributors
CREATE TABLE users (
    id            SERIAL PRIMARY KEY,
    email         TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,               -- bcrypt hash
    display_name  TEXT,
    created_at    TIMESTAMPTZ DEFAULT now()
);

-- Store items
CREATE TABLE store_items (
    id          SERIAL PRIMARY KEY,
    title       TEXT NOT NULL,
    title_np    TEXT,
    description TEXT,
    image_url   TEXT,
    buy_url     TEXT,
    sort_order  INT DEFAULT 0,
    active      BOOLEAN DEFAULT true
);
```

## Handler TODO Map

Each `main.go` handler has `// TODO:` comments. This table consolidates them:

| Route | Handler | Replace `mock*()` with |
|-------|---------|------------------------|
| `GET /` | `indexHandler` | `SELECT ... WHERE category = $1 AND (title ILIKE $2 OR title_np LIKE $2) ORDER BY created_at DESC` |
| `GET /document/{id}` | `documentHandler` | `SELECT * FROM documents WHERE id = $1` |
| `GET /document/{id}/edit` | `editGetHandler` | auth check + prefill form values |
| `POST /document/{id}/edit` | `editPostHandler` | auth + `UPDATE documents SET ... WHERE id = $1` |
| `GET /upload` | `uploadGetHandler` | no DB needed (static form) |
| `POST /upload` | `uploadPostHandler` | parse multipart, upload file, `INSERT INTO documents` |
| `GET /dashboard` | `dashboardHandler` | auth + `SELECT * FROM documents` with filters |
| `POST /login` | `loginPostHandler` | `SELECT password_hash FROM users WHERE email = $1` + bcrypt compare + set session |
| `POST /logout` | `logoutHandler` | clear session cookie |

## File Upload Flow

```
POST /upload
  ├── r.ParseMultipartForm(50 << 20)    // 50 MB limit
  ├── validate fields (title, category required)
  ├── r.FormFile("file") → upload to object storage
  │     → returns public URL
  ├── INSERT INTO documents (title, title_np, category, description,
  │     body_html, body_text, lang, script,
  │     orig_author, orig_author_np, orig_year, orig_month, orig_day,
  │     file_path, uploaded_by, created_at)
  │     VALUES ($1, $2, ...)
  └── http.Redirect → /document/{new_id}
```

### Object Storage Options

| Provider | Notes |
|----------|-------|
| **Cloudflare R2** | S3-compatible, no egress fees — recommended |
| **AWS S3** | Standard, well-documented |
| **MinIO** | Self-hosted S3-compatible |
| **Local `/static/docs/`** | Development only — not scalable |

`document.FilePath` must be a browser-accessible URL (e.g. `https://cdn.yogilib.com/docs/treaty-1815.pdf`).

## Authentication

The `requireAuth` middleware stub is in `main.go` (commented out). Pattern:

```go
func requireAuth(next http.HandlerFunc) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        user := sessionUser(r) // read from signed cookie / JWT
        if user == nil {
            http.Redirect(w, r, "/login", http.StatusSeeOther)
            return
        }
        next(w, r)
    }
}

// Protect routes:
mux.HandleFunc("GET /dashboard", requireAuth(dashboardHandler))
mux.HandleFunc("GET /upload",    requireAuth(uploadGetHandler))
```

**Options**:
- **Signed cookies** — `net/http` + `crypto/hmac` (zero deps)
- **gorilla/sessions** — battle-tested session store
- **JWT** — stateless, easy to add API access later

## Contributor Attribution

The upload form currently shows a placeholder note:
> "The account that submits this form will be automatically recorded as the contributor."

When auth is live:
1. Read `userID` from the session in `uploadPostHandler`
2. Include `uploaded_by = userID` in the `INSERT` statement
3. `documentHandler` can then JOIN to `users` and display the contributor's `display_name`

Original author (`orig_author`, `orig_author_np`) is a separate field for the **historical author** of the document (e.g. "Yogi Narharinath / योगी नरहरिनाथ").

## Search

Current implementation: in-memory Go filter (titles only).

For production, replace with:
```sql
SELECT * FROM documents
WHERE ($1 = '' OR category = $1)
  AND ($2 = '' OR
       title ILIKE '%' || $2 || '%' OR
       title_np LIKE '%' || $2 || '%' OR
       to_tsvector('simple', body_text) @@ plainto_tsquery('simple', $2))
ORDER BY created_at DESC
LIMIT 50;
```
