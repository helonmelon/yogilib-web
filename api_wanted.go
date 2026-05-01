package main

// Wanted-links API endpoint.

import "net/http"

// apiWantedList handles GET /api/v1/wanted.
//
// Returns the red-link index — slugs referenced by `[[..]]` markup that
// don't yet have a matching document.
func apiWantedList(w http.ResponseWriter, r *http.Request) {
	wanted := listWantedLinks()
	out := make([]ApiWantedEntry, 0, len(wanted))
	for _, we := range wanted {
		sources := make([]ApiWantedSource, 0, len(we.Sources))
		for _, s := range we.Sources {
			sources = append(sources, ApiWantedSource{ID: s.ID, Title: s.Title})
		}
		out = append(out, ApiWantedEntry{
			ToSlug:    we.Slug,
			Anchor:    we.Anchor,
			FromCount: we.Count,
			FromDocs:  sources,
		})
	}
	writeJSON(w, http.StatusOK, ApiWantedResp{Items: out})
}
