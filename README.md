# Yogilib — Web Frontend

A digital archive for the works of **Yogi Narharinath** (योगी नरहरिनाथ) — Nepali scholar, historian, and religious figure. The site lets visitors browse, search, and read historical documents in English, Nepali, and Sanskrit, and lets contributors upload new material.

Built as a **pure Go stdlib web server** — no Node, no Python, no external Go dependencies. Every page is server-rendered via `html/template`. The frontend is complete and running on mock data; the backend (database, auth, file storage) is ready to be wired in.

---

## Quick start

```bash
go run main.go           # dev server at http://localhost:8080
go build -o yogilib .    # production binary
PORT=9000 ./yogilib      # custom port
```

Requires **Go 1.22+**.

---

## What's in the box

| Area | Status |
|---|---|
| All public pages (home, about, works, document viewer, excerpts, store, similar sites) | Done |
| Contribute / upload form with rich text editor (Quill.js) | Done |
| Admin pages (dashboard, edit, login) | Done — auth stub in place |
| Preeti → Unicode converter (legacy Nepali encoding) | Done |
| ITRANS → Devanagari converter (Sanskrit transliteration) | Done |
| PostgreSQL schema | Designed — see `docs/04-backend-integration.md` |
| Auth middleware | Stubbed — see `docs/04-backend-integration.md` |
| File / object storage (R2, S3) | Not connected |

Every handler in `main.go` currently calls an in-memory `mock*()` function. Each one has a `// TODO:` comment with the exact DB query to replace it with.

---

## Stack at a glance

```
Browser → net/http Mux → Handler → html/template → HTML
```

- **Server**: `net/http` + `html/template` (Go stdlib only)
- **Styles**: single `static/css/style.css`, Himalaya font for Devanagari
- **Editor**: Quill.js v1.3.7 (CDN, no build step)
- **Language support**: Unicode Devanagari for Nepali and Sanskrit; Preeti and ITRANS converters for legacy text
- **Planned DB**: PostgreSQL with full-text search across English and Nepali titles
- **Planned storage**: Cloudflare R2 or S3-compatible for document files

---

## Docs

The `docs/` folder has everything a developer needs to go deeper:

| File | What it covers |
|---|---|
| [`01-getting-started.md`](docs/01-getting-started.md) | Running the server, project layout, adding new pages |
| [`02-architecture.md`](docs/02-architecture.md) | Request lifecycle, core types, routing table, template system |
| [`03-design-system.md`](docs/03-design-system.md) | Typography, colour palette, UI components |
| [`04-backend-integration.md`](docs/04-backend-integration.md) | DB schema, handler TODO map, file upload flow, auth, search |
| [`05-language-support.md`](docs/05-language-support.md) | Unicode, Nepali typing, Preeti encoding, ITRANS, Sanskrit |
| [`06-changelog.md`](docs/06-changelog.md) | Change history |
