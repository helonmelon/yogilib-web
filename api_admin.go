package main

// Admin API endpoints.

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"
)

// apiAdminEnrichStatus handles GET /api/v1/admin/enrich/status.
//
// Mirrors the legacy enrichStatusHandler payload but in the v1 spec shape.
func apiAdminEnrichStatus(w http.ResponseWriter, r *http.Request) {
	enabled := os.Getenv("ENRICH_DISABLED") != "1"

	var nDocs, nEmb, nSum, nEnt, nSugg int
	db.QueryRow(`SELECT COUNT(*) FROM documents`).Scan(&nDocs)
	db.QueryRow(`SELECT COUNT(*) FROM embeddings`).Scan(&nEmb)
	db.QueryRow(`SELECT COUNT(*) FROM summaries`).Scan(&nSum)
	db.QueryRow(`SELECT COUNT(*) FROM entities`).Scan(&nEnt)
	db.QueryRow(`SELECT COUNT(*) FROM link_suggestions WHERE status='pending'`).Scan(&nSugg)

	resp := ApiEnrichStatusResp{
		Enabled:   enabled,
		OllamaURL: ollamaURL(),
		LastRunAt: formatTime(lastEnrichStats.FinishedAt),
		Stats: ApiEnrichStats{
			TotalDocs:     nDocs,
			WithSummary:   nSum,
			WithEmbedding: nEmb,
			WithEntities:  nEnt,
			QueueDepth:    nSugg,
		},
		LastPass:  []ApiEnrichLastPass{}, // future: per-doc per-stage log
		LastError: lastEnrichStats.LastError,
	}
	writeJSON(w, http.StatusOK, resp)
}

// apiAdminEnrichRun handles POST /api/v1/admin/enrich/run.
//
// Kicks off a one-shot enrichment pass in the background.
func apiAdminEnrichRun(w http.ResponseWriter, r *http.Request) {
	if os.Getenv("ENRICH_DISABLED") == "1" {
		writeError(w, http.StatusServiceUnavailable, codeServiceUnavailable, "enrichment is disabled")
		return
	}
	pass := lastEnrichStats.Pass + 1
	go runEnrichOnce(context.Background(), pass)
	writeJSON(w, http.StatusAccepted, ApiEnrichRunResp{
		Started: true,
		PassID:  fmt.Sprintf("pass-%d-%d", pass, time.Now().Unix()),
	})
}
