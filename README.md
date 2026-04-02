# Yogilib — Web Frontend

A Go HTTP server serving the Yogilib digital archive. Pure stdlib (`net/http`,
`html/template`). No Node, no Python, no external Go dependencies.

## Run

```
go run main.go           # development
go build -o yogilib .   # production binary
PORT=9000 ./yogilib      # custom port (default 8080)
```

Requires **Go 1.22+** (uses pattern matching in `http.NewServeMux`).

---

## Project layout

```
yogilib-web/
├── main.go              # routes, handlers, mock data
├── go.mod
├── templates/
│   ├── base.html        # shared layout: header, nav, footer
│   ├── index.html       # homepage — document list + search
│   ├── about.html       # about Yogi Narharinath
│   ├── works.html       # bibliography
│   ├── excerpts.html    # list of transcribed excerpts
│   ├── excerpt.html     # single excerpt viewer
│   ├── mission.html     # about the site
│   ├── similar.html     # similar sites
│   ├── document.html    # document viewer (PDF embed / download)
│   ├── edit.html        # edit a document's metadata
│   ├── upload.html      # contribute a new document
│   ├── store.html       # store / shop
│   ├── login.html       # admin login
│   └── dashboard.html   # admin document grid
└── static/
    ├── css/style.css
    ├── fonts/           # Himalaya woff/woff2
    └── imgs/            # yogi photos
```

---

## Connecting the backend

Every handler in `main.go` has `// TODO:` comments describing exactly what DB
call to make. The pattern is:

1. Replace the `mock*()` helper calls with real queries.
2. Add an auth middleware (stub in `main.go` comments) and wrap protected routes.
3. Wire file upload to your object storage of choice.

### Suggested database schema (Postgres)

```sql
CREATE TABLE documents (
    id          SERIAL PRIMARY KEY,
    title       TEXT NOT NULL,
    title_np    TEXT,
    category    TEXT,
    description TEXT,
    file_path   TEXT,         -- URL / path returned by object storage
    user_id     INTEGER REFERENCES users(id),
    created_at  TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE excerpts (
    id         SERIAL PRIMARY KEY,
    slug       TEXT UNIQUE NOT NULL,
    title      TEXT NOT NULL,
    body_html  TEXT,          -- render from Markdown or store raw HTML
    created_at TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE users (
    id            SERIAL PRIMARY KEY,
    email         TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,  -- bcrypt
    created_at    TIMESTAMPTZ DEFAULT now()
);

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

### File storage

`document.FilePath` should be a URL accessible by the browser. Point it to:
- **Cloudflare R2 / S3** — store on upload, save public URL to DB
- **Local** — serve under `/static/docs/` and store the relative path

### Authentication

The `requireAuth` middleware stub in `main.go` shows the pattern. Pick one:
- **Signed cookies** — `net/http` + `crypto/hmac`
- **gorilla/sessions** — battle-tested session store
- **JWT** — stateless, easy to plug into a separate API later

### Page-by-page TODO reference

| Route | Handler | What to connect |
|---|---|---|
| `GET /` | `indexHandler` | `SELECT` documents with optional search/category filter |
| `GET /excerpts` | `excerptsHandler` | `SELECT` all excerpts |
| `GET /excerpts/{slug}` | `excerptHandler` | `SELECT` excerpt by slug |
| `GET /document/{id}` | `documentHandler` | `SELECT` document by id |
| `GET /document/{id}/edit` | `editGetHandler` | auth + prefill form |
| `POST /document/{id}/edit` | `editPostHandler` | auth + `UPDATE` document |
| `GET /upload` | `uploadGetHandler` | auth (optional for public contrib) |
| `POST /upload` | `uploadPostHandler` | parse file, object storage, `INSERT` |
| `GET /dashboard` | `dashboardHandler` | auth + `SELECT` with filters |
| `POST /login` | `loginPostHandler` | bcrypt check + set session cookie |
| `POST /logout` | `logoutHandler` | clear session |

---

## Designing pages step by step

Templates live in `templates/`. Each file has two parts:

- `base.html` — the shared shell (header, nav, footer). Edit this to change
  anything site-wide.
- `{page}.html` — defines `{{define "content"}}…{{end}}`. Only contains the
  unique body of that page.

To add a new page:

1. Create `templates/mypage.html` with `{{define "content"}}…{{end}}`.
2. Add a handler function in `main.go` that calls `render(w, "mypage", data)`.
3. Register the route: `mux.HandleFunc("GET /mypage", myPageHandler)`.

The `PageData` struct in `main.go` is the shared envelope. Add fields to it as
your new pages need them.
