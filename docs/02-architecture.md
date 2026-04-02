# Architecture

## Overview

Yogilib is a **pure Go stdlib web server** — no frameworks, no build pipeline, no JavaScript bundler. Every page is server-rendered via Go's `html/template`. Static assets (CSS, fonts, images, JS helpers) are served directly from the `static/` directory.

```
Browser → net/http Mux → Handler → html/template → HTML response
                                      ↑
                                 (mock data / future: DB)
```

## Request Lifecycle

1. Browser sends `GET /upload`
2. `mux.HandleFunc("GET /upload", uploadGetHandler)` matches
3. `uploadGetHandler` populates a `PageData` struct
4. `render(w, "upload", data)` parses `base.html` + `upload.html`
5. `base.html` renders the shell; `upload.html` fills the `{{template "content" .}}` slot
6. Response is streamed to the browser

## Core Types (`main.go`)

### `PageData`
The single data envelope passed to every template. Only populate the fields relevant to each page — unused fields are zero-valued.

```go
type PageData struct {
    Title      string
    Query      string       // search query string
    Category   string       // active category filter
    Categories []string     // list of category tab labels
    Flash      string       // success/info banner
    Error      string       // error banner
    Documents  []Document
    Doc        *Document    // single document view
    Excerpts   []Excerpt
    Exc        *Excerpt     // single excerpt view
    StoreItems []StoreItem
    User       *User        // nil when not logged in
}
```

### `Document`
```go
type Document struct {
    ID          string
    Title       string       // English title
    TitleNP     string       // Nepali title (Unicode Devanagari)
    Category    string       // किताब | कागजात | रेकर्ड | पत्रिका | अंश | अन्य
    Description string
    FilePath    string       // URL to stored file (object storage)
    CreatedAt   string       // formatted display date
}
```

### `Excerpt`
```go
type Excerpt struct {
    Slug  string
    Title string
    Body  template.HTML    // stored as HTML; marked safe before passing
}
```

## Routing Table

| Method | Route | Handler | Auth |
|--------|-------|---------|------|
| GET | `/` | `indexHandler` | Public |
| GET | `/about` | `aboutHandler` | Public |
| GET | `/works` | `worksHandler` | Public |
| GET | `/document/{id}` | `documentHandler` | Public |
| GET | `/document/{id}/edit` | `editGetHandler` | Admin* |
| POST | `/document/{id}/edit` | `editPostHandler` | Admin* |
| GET | `/upload` | `uploadGetHandler` | Public |
| POST | `/upload` | `uploadPostHandler` | Public |
| GET | `/dashboard` | `dashboardHandler` | Admin* |
| GET | `/login` | `loginGetHandler` | Public |
| POST | `/login` | `loginPostHandler` | Public |
| POST | `/logout` | `logoutHandler` | Public |

*Auth middleware stub exists in `main.go` — see `04-backend-integration.md`.

## Template System

- **`base.html`** defines `{{define "base"}}` — the full page shell (DOCTYPE, `<head>`, nav, footer).
- Every other template defines `{{define "content"}}` — only the unique body of that page.
- `render()` in `main.go` always parses both `base.html` and the page template together, then executes `"base"`.

### Template Variables

All data flows through `PageData`. Access in templates with `{{.FieldName}}`. Example:

```html
{{if .Flash}}
<div class="flash flash-ok">{{.Flash}}</div>
{{end}}

{{range .Documents}}
<p>{{.Title}} — {{.TitleNP}}</p>
{{end}}
```

## Static Assets

Served by `http.FileServer` under `/static/`:

```go
mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
```

| Path | Contents |
|------|----------|
| `/static/css/style.css` | All site CSS |
| `/static/fonts/` | Himalaya woff/woff2 |
| `/static/imgs/` | Yogi Narharinath photos |
| `/static/js/preeti-unicode.js` | Preeti → Unicode converter |
| `/static/js/itrans-unicode.js` | ITRANS → Devanagari converter |

## Mock Data Layer

All data currently comes from in-memory Go functions (`mockDocuments()`, `mockExcerpts()`, etc.). Every function has a `// TODO:` comment with the exact SQL query to replace it with. See `04-backend-integration.md` for the full database schema.

## Categories

Defined as a server-side slice and passed to templates:

```go
var docCategories = []string{"सबै", "किताब", "कागजात", "रेकर्ड", "पत्रिका", "अंश", "अन्य"}
```

These appear as filter tabs on the homepage and as options in the contribute form.
