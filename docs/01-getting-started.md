# Getting Started

## Requirements

- **Go 1.22+** — uses pattern matching in `http.NewServeMux` (introduced in Go 1.22)
- No Node.js, no Python, no external Go dependencies
- Internet access for CDN assets (Google Fonts, Quill.js) — can be self-hosted for offline use

## Running Locally

```bash
# Development — live template reloads on every request
go run main.go

# Build a production binary
go build -o yogilib .

# Run the binary (default port 8080)
./yogilib

# Custom port
PORT=9000 ./yogilib
```

The server starts at **http://localhost:8080**.

## Project Layout

```
yogilib-web/
├── main.go                  # routes, handlers, mock data, PageData struct
├── go.mod
├── docs/                    # ← this documentation folder
│   ├── 01-getting-started.md
│   ├── 02-architecture.md
│   ├── 03-design-system.md
│   ├── 04-backend-integration.md
│   ├── 05-language-support.md
│   └── 06-changelog.md
├── templates/
│   ├── base.html            # shared layout: header, nav, footer
│   ├── index.html           # homepage — document list + search
│   ├── about.html           # about Yogi Narharinath
│   ├── works.html           # bibliography / ग्रन्थावली
│   ├── excerpts.html        # list of transcribed text excerpts
│   ├── excerpt.html         # single excerpt viewer
│   ├── mission.html         # about the site's mission
│   ├── similar.html         # similar sites / resources
│   ├── document.html        # document viewer (PDF embed / download)
│   ├── edit.html            # edit document metadata (admin)
│   ├── upload.html          # contribute a new document (public)
│   ├── store.html           # book store / shop
│   ├── login.html           # admin login
│   └── dashboard.html       # admin document management grid
└── static/
    ├── css/
    │   └── style.css        # all site styles
    ├── fonts/               # Himalaya .woff / .woff2
    ├── imgs/                # Yogi Narharinath photos
    └── js/
        ├── preeti-unicode.js   # Preeti → Unicode converter
        └── itrans-unicode.js   # ITRANS → Devanagari converter
```

## Adding a New Page

1. Create `templates/mypage.html`:
   ```html
   {{define "content"}}
   <div class="container">
     <!-- page body -->
   </div>
   {{end}}
   ```
2. Add a handler in `main.go`:
   ```go
   func myPageHandler(w http.ResponseWriter, r *http.Request) {
       render(w, "mypage", PageData{Title: "My Page"})
   }
   ```
3. Register the route:
   ```go
   mux.HandleFunc("GET /mypage", myPageHandler)
   ```

Add any new data fields to the `PageData` struct in `main.go` — it is the single envelope passed to every template.
