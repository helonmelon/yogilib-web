package main

// Meta endpoints: /api/v1/health, /api/v1/categories.

import (
	"log"
	"net/http"
	"os"
)

// apiHealth handles GET /api/v1/health.
//
// Returns a quick liveness probe: server status, version, DB reachability,
// and whether the enrichment worker is enabled.
func apiHealth(w http.ResponseWriter, r *http.Request) {
	dbStatus := "up"
	if err := db.Ping(); err != nil {
		dbStatus = "down"
	}
	enrich := "up"
	if os.Getenv("ENRICH_DISABLED") == "1" {
		enrich = "disabled"
	}
	writeJSON(w, http.StatusOK, ApiHealthResp{
		Status:  "ok",
		Version: apiVersion,
		DB:      dbStatus,
		Enrich:  enrich,
	})
}

// apiCategoriesList handles GET /api/v1/categories.
//
// Joins docCategories with a counts query so the UI can render filter chips
// with badges showing how many docs are in each category.
func apiCategoriesList(w http.ResponseWriter, r *http.Request) {
	counts := map[string]int{}
	rows, err := db.Query(`SELECT COALESCE(category,''), COUNT(*) FROM documents GROUP BY category`)
	if err != nil {
		log.Println("apiCategoriesList query:", err)
		writeError(w, http.StatusInternalServerError, codeInternalError, "failed to load categories")
		return
	}
	for rows.Next() {
		var cat string
		var n int
		if err := rows.Scan(&cat, &n); err == nil {
			counts[cat] = n
		}
	}
	rows.Close()

	out := make([]ApiCategory, 0, len(docCategories))
	for _, title := range docCategories {
		// Slug = ASCII slug of the Devanagari title (best-effort, may be empty).
		// Front-end can match by either slug or title; we keep both.
		slug := slugify(title)
		out = append(out, ApiCategory{
			Slug:     slug,
			Title:    title,
			DocCount: counts[title],
		})
	}
	writeJSON(w, http.StatusOK, ApiCategoriesResp{Categories: out})
}
