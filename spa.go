package main

// Static file server for the SvelteKit build, embedded into the binary.
//
// Build flow:
//   1. cd yogilib-sveltekit && npm run build
//   2. rsync (or cp -r) yogilib-sveltekit/build/* → yogilib-web/webdist/
//   3. go build -o yogilib ./...
//
// The Makefile wraps that into `make build`.
//
// Mounted at "/" as the catch-all (after all API and /static/ routes).
// When a requested path matches a real file under the embedded webdist
// directory it's served directly; otherwise we serve index.html so the
// SvelteKit client-side router can take over.

import (
	"embed"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:webdist
var spaFS embed.FS

// spaSubFS returns the embedded SvelteKit build root (webdist/).
func spaSubFS() (fs.FS, error) {
	return fs.Sub(spaFS, "webdist")
}

// hasIndex reports whether webdist/index.html is present in the embedded
// FS — i.e., whether `make build` has been run before `go build`.
func spaHasIndex() bool {
	sub, err := spaSubFS()
	if err != nil {
		return false
	}
	f, err := sub.Open("index.html")
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

// newSPAHandler returns an http.Handler that serves files from the
// embedded SvelteKit build with SPA fallback to index.html.
func newSPAHandler() http.Handler {
	sub, err := spaSubFS()
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("SPA embed FS unavailable: " + err.Error() + "\n"))
		})
	}
	if !spaHasIndex() {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("SPA build not embedded.\n" +
				"Run `make build` (or sync yogilib-sveltekit/build/* into yogilib-web/webdist/) and rebuild.\n"))
		})
	}

	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Defence in depth: never serve API or upload paths from here.
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/static/") {
			http.NotFound(w, r)
			return
		}

		urlPath := r.URL.Path
		if urlPath == "" || urlPath == "/" {
			urlPath = "/index.html"
		}

		// Try to open the requested file directly from the embedded FS.
		clean := strings.TrimPrefix(path.Clean(urlPath), "/")
		if clean == "" {
			clean = "index.html"
		}

		if f, err := sub.Open(clean); err == nil {
			st, sterr := f.Stat()
			if sterr == nil && !st.IsDir() {
				_ = f.Close()
				// Long-cache hashed assets under _app/.
				if strings.HasPrefix(clean, "_app/immutable/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				fileServer.ServeHTTP(w, r)
				return
			}
			_ = f.Close()
		}

		// SPA fallback — serve index.html for client-side routing.
		idx, err := sub.Open("index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer idx.Close()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache, must-revalidate")
		// Use io.Copy to stream — index.html is small.
		_, _ = io.Copy(w, idx)
	})
}
