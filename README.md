# Yogilib — Web Frontend

A digital archive for the works of **Yogi Narharinath** (योगी नरहरिनाथ) — Nepali scholar, historian, and religious figure. The site lets visitors browse, search, and read historical documents in English, Nepali, and Sanskrit, and lets contributors upload new material.

Built with **Go** using `net/http` and `html/template`. Every page is server-rendered. The database is **SQLite with FTS5** (full-text search) — embedded, zero-config, single file.

---

## Quick start

```bash
go mod tidy             # install dependencies
go run main.go          # dev server at http://localhost:8080
go build -o yogilib .   # production binary
PORT=9000 ./yogilib     # custom port
DB_PATH=/data/yogi.db ./yogilib   # custom DB location (default: yogilib.db)
```

Requires **Go 1.22+**.

---

## What's in the box

| Area | Status |
|---|---|
| All public pages (home, about, works, document viewer, excerpts, store, similar sites) | Done |
| SQLite database with FTS5 full-text search | Done |
| Login system with session-based auth and role tiers | Done |
| Contribute / upload form | Done |
| Admin pages (dashboard, edit) | Done |
| Preeti → Unicode converter (legacy Nepali encoding) | Done |
| ITRANS → Devanagari converter (Sanskrit transliteration) | Done |
| File / object storage (R2, S3) | Not connected |

---

## Stack

```
Browser → net/http Mux → Auth Middleware → Handler → html/template → HTML
                                ↓
                           SQLite + FTS5
```

- **Server**: `net/http` + `html/template`
- **Database**: SQLite via `modernc.org/sqlite` (pure Go, no CGO)
- **Search**: SQLite FTS5 with `unicode61` tokenizer — handles Devanagari and English
- **Auth**: bcrypt passwords (`golang.org/x/crypto`), session tokens in SQLite, `HttpOnly` cookie
- **Styles**: single `static/css/style.css`, Himalaya font for Devanagari
- **Language support**: Unicode Devanagari, Preeti and ITRANS converters for legacy text

---

## Authentication & access tiers

The site is **publicly readable** — no login needed to browse documents, excerpts, or any reading page. Login is only required to contribute or administrate.

### Roles

| Role | Access |
|---|---|
| *(public)* | All reading pages — home, documents, excerpts, about, works, store |
| `uploader` | Everything public + `/upload` |
| `admin` | Everything above + `/dashboard`, `/document/{id}/edit` |

### Seeded accounts (development only)

On first run, two mock accounts are created automatically:

| Email | Password | Role |
|---|---|---|
| `admin@yogilib.org` | `admin123` | `admin` |
| `upload@yogilib.org` | `upload123` | `uploader` |

**Change these before deploying to production** — see [Adding users](#adding-users) below.

### Sessions

- Sessions are stored in the `sessions` table in SQLite
- A random 64-character token is set as an `HttpOnly` cookie named `session`
- Sessions expire after **30 days**
- Logging out deletes the session from the database and clears the cookie

### Adding users

Insert directly into the database. Passwords must be bcrypt-hashed:

```bash
# Generate a bcrypt hash (Go one-liner)
go run -e 'import "golang.org/x/crypto/bcrypt"; fmt.Println(string(must(bcrypt.GenerateFromPassword([]byte("yourpassword"), 12))))'

# Or use any bcrypt tool, then insert:
sqlite3 yogilib.db "INSERT INTO users (email, password_hash, role) VALUES ('you@example.com', '\$2a\$12\$...', 'admin');"
```

---

## Database

The database lives at `yogilib.db` by default (set `DB_PATH` to change). It is created automatically on first run.

### Schema overview

```sql
-- Main documents table
documents (id, title, title_np, category, description, file_path, created_at)

-- FTS5 full-text search index (auto-synced via triggers)
documents_fts — searches title, title_np, description

-- Users
users (id, email, password_hash, role)

-- Sessions
sessions (token, user_id, expires_at)
```

### Search

Search uses SQLite FTS5 with prefix matching. Typing `सुगौ` will find `सुगौली सन्धि`. Both Nepali (Devanagari) and English are indexed. The `unicode61` tokenizer handles Unicode correctly out of the box.

When a document is inserted, updated, or deleted, triggers automatically keep `documents_fts` in sync — no manual indexing needed.

---

## Docs

| File | What it covers |
|---|---|
| [`01-getting-started.md`](docs/01-getting-started.md) | Running the server, project layout, adding new pages |
| [`02-architecture.md`](docs/02-architecture.md) | Request lifecycle, core types, routing table, template system |
| [`03-design-system.md`](docs/03-design-system.md) | Typography, colour palette, UI components |
| [`04-backend-integration.md`](docs/04-backend-integration.md) | DB schema, handler map, file upload flow, search |
| [`05-language-support.md`](docs/05-language-support.md) | Unicode, Nepali typing, Preeti encoding, ITRANS, Sanskrit |
| [`06-changelog.md`](docs/06-changelog.md) | Change history |
