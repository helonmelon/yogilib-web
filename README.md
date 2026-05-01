# Yogilib — Web Frontend

A digital archive for the works of **Yogi Narharinath** (योगी नरहरिनाथ) — Nepali scholar, historian, and religious figure. The site lets visitors browse, search, and read historical documents in English, Nepali, and Sanskrit, and lets contributors upload new material.

Built with **Go** (`net/http`) for the API + a **SvelteKit** SPA for the UI. Both are shipped as a single Go binary — the SvelteKit build is `//go:embed`-ed into the executable, so deployment is one file plus a SQLite DB.

The database is **SQLite with FTS5** (full-text search) — embedded, zero-config.

---

## Quick start

The canonical build path goes through the Makefile:

```bash
make build              # 1) npm run build in ../yogilib-sveltekit
                        # 2) sync build/ → webdist/
                        # 3) go build -o yogilib .
./yogilib               # serves SPA + API on http://localhost:8080
```

Manual / dev-only equivalents:

```bash
go mod tidy             # install Go deps
cd ../yogilib-sveltekit && npm ci && npm run build && cd -
rm -rf webdist && cp -r ../yogilib-sveltekit/build/. webdist/
go build -o yogilib .   # produces single self-contained binary
```

Env knobs:

```bash
PORT=9000 ./yogilib              # custom port (default 8080)
DB_PATH=/data/yogi.db ./yogilib  # custom DB path (default yogilib.db)
LEGACY_HTML=1 ./yogilib          # also enable old html/template routes
ENRICH_DISABLED=1 ./yogilib      # skip Ollama background worker
OLLAMA_URL=http://host:11434 ./yogilib   # remote Ollama
```

Requires **Go 1.22+** and **Node 18+** (only at build time).

### Architecture (Phase 6)

```
Browser —→ Go binary :8080
             ├─ /api/v1/*   JSON API (auth, documents, excerpts, admin, ...)
             ├─ /static/*   uploaded artefacts (filesystem)
             └─ / (catch-all) embedded SvelteKit SPA — client-side routing
```

The legacy `html/template` routes still live in the codebase but are gated behind `LEGACY_HTML=1` (off by default). The SPA owns `/` in production.

### Deployment (WSL / live server)

```bash
# On the host (build):
cd ~/.openclaw/workspace/yogilib/yogilib-web
make build

# Stop any previous instance:
systemctl --user stop yogilib-live.scope 2>/dev/null || true

# Start under systemd-run so it survives the SSH channel closing:
systemd-run --user --scope --unit=yogilib-live --collect \
    bash -c 'cd ~/.openclaw/workspace/yogilib/yogilib-web && \
             env PORT=8080 ./yogilib >> yogilib.log 2>&1'

# Tail logs:
tail -f ~/.openclaw/workspace/yogilib/yogilib-web/yogilib.log
```

Live URL (Tailscale MagicDNS): <http://dofdot.tail462907.ts.net:8080>

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
