package main

// Excerpts API endpoints.

import "net/http"

// apiExcerptsList handles GET /api/v1/excerpts.
func apiExcerptsList(w http.ResponseWriter, r *http.Request) {
	excs := getExcerpts()
	out := make([]ApiExcerpt, 0, len(excs))
	for _, e := range excs {
		// Hydrate body via getExcerptBySlug — getExcerpts() returns a list
		// without bodies; the spec wants body_html in the list response.
		full := getExcerptBySlug(e.Slug)
		body := ""
		if full != nil {
			body = string(full.Body)
		}
		out = append(out, ApiExcerpt{Slug: e.Slug, Title: e.Title, BodyHTML: body})
	}
	writeJSON(w, http.StatusOK, ApiExcerptsResp{Items: out})
}

// apiExcerptGet handles GET /api/v1/excerpts/{slug}.
func apiExcerptGet(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	exc := getExcerptBySlug(slug)
	if exc == nil {
		writeError(w, http.StatusNotFound, codeNotFound, "excerpt not found")
		return
	}
	writeJSON(w, http.StatusOK, ApiExcerpt{
		Slug:     exc.Slug,
		Title:    exc.Title,
		BodyHTML: string(exc.Body),
	})
}
