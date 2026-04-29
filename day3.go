package main

// Day 3 features: revision history, diff viewer, admin rollback,
// wanted-links index.
//
// Schema note: the `revisions` table is created by migration2 (Day 2).
// Every save in upload/edit handlers writes a new row there. This file
// exposes those revisions via HTTP and lets admins compare or roll back.

import (
	"database/sql"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// RevisionEntry is one row from the revisions table, ready for templates.
type RevisionEntry struct {
	ID        int
	DocID     int
	Title     string
	TitleNP   string
	BodyHTML  template.HTML
	BodyText  string
	EditedBy  string // user email; empty if user deleted or NULL
	EditedAt  string // RFC3339 string
	EditedAgo string // pretty "3 hours ago"
	Comment   string
}

// WantedEntry is a red-link target — a slug referenced by `[[..]]`
// that has no matching document yet. Tracks how many docs want it.
type WantedEntry struct {
	Slug    string
	Anchor  string // a representative anchor text for display
	Count   int    // number of docs linking to this slug
	Sources []struct {
		ID    int
		Title string
	}
}

// ---------------------------------------------------------------------------
// Queries
// ---------------------------------------------------------------------------

// listRevisions returns all revisions for a document, newest first.
func listRevisions(docID string) []RevisionEntry {
	rows, err := db.Query(`
		SELECT r.id, r.doc_id,
		       COALESCE(r.title,''),
		       COALESCE(r.title_np,''),
		       COALESCE(r.body_html,''),
		       COALESCE(r.body_text,''),
		       COALESCE(u.email,''),
		       r.edited_at,
		       COALESCE(r.comment,'')
		FROM revisions r
		LEFT JOIN users u ON u.id = r.edited_by
		WHERE r.doc_id = ?
		ORDER BY r.id DESC
	`, docID)
	if err != nil {
		log.Println("listRevisions:", err)
		return nil
	}
	defer rows.Close()

	var out []RevisionEntry
	for rows.Next() {
		var r RevisionEntry
		var bodyHTML string
		if err := rows.Scan(&r.ID, &r.DocID, &r.Title, &r.TitleNP,
			&bodyHTML, &r.BodyText, &r.EditedBy, &r.EditedAt, &r.Comment); err != nil {
			log.Println("listRevisions scan:", err)
			continue
		}
		r.BodyHTML = template.HTML(bodyHTML)
		r.EditedAgo = humanAgo(r.EditedAt)
		out = append(out, r)
	}
	return out
}

// getRevision fetches a single revision by id (or nil).
func getRevision(id string) *RevisionEntry {
	row := db.QueryRow(`
		SELECT r.id, r.doc_id,
		       COALESCE(r.title,''),
		       COALESCE(r.title_np,''),
		       COALESCE(r.body_html,''),
		       COALESCE(r.body_text,''),
		       COALESCE(u.email,''),
		       r.edited_at,
		       COALESCE(r.comment,'')
		FROM revisions r
		LEFT JOIN users u ON u.id = r.edited_by
		WHERE r.id = ?
	`, id)
	var r RevisionEntry
	var bodyHTML string
	if err := row.Scan(&r.ID, &r.DocID, &r.Title, &r.TitleNP,
		&bodyHTML, &r.BodyText, &r.EditedBy, &r.EditedAt, &r.Comment); err != nil {
		if err != sql.ErrNoRows {
			log.Println("getRevision:", err)
		}
		return nil
	}
	r.BodyHTML = template.HTML(bodyHTML)
	r.EditedAgo = humanAgo(r.EditedAt)
	return &r
}

// listWantedLinks returns red-link targets, sorted by reference count desc.
// A "wanted" link is a row in `links` with to_doc_id IS NULL.
func listWantedLinks() []WantedEntry {
	rows, err := db.Query(`
		SELECT l.to_slug,
		       MAX(l.anchor)        AS anchor,
		       COUNT(*)             AS cnt
		FROM links l
		WHERE l.to_doc_id IS NULL
		  AND l.to_slug IS NOT NULL
		  AND l.to_slug != ''
		GROUP BY l.to_slug
		ORDER BY cnt DESC, l.to_slug ASC
	`)
	if err != nil {
		log.Println("listWantedLinks:", err)
		return nil
	}
	defer rows.Close()

	var out []WantedEntry
	for rows.Next() {
		var w WantedEntry
		if err := rows.Scan(&w.Slug, &w.Anchor, &w.Count); err == nil {
			out = append(out, w)
		}
	}
	// fill sources for each
	for i := range out {
		srcRows, err := db.Query(`
			SELECT DISTINCT d.id, d.title
			FROM links l JOIN documents d ON d.id = l.from_doc_id
			WHERE l.to_slug = ? AND l.to_doc_id IS NULL
			ORDER BY d.title
			LIMIT 20
		`, out[i].Slug)
		if err != nil {
			continue
		}
		for srcRows.Next() {
			var s struct {
				ID    int
				Title string
			}
			if err := srcRows.Scan(&s.ID, &s.Title); err == nil {
				out[i].Sources = append(out[i].Sources, s)
			}
		}
		srcRows.Close()
	}
	return out
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// historyHandler renders the revision list for a document.
func historyHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	doc := getDocumentByID(id)
	if doc == nil {
		http.NotFound(w, r)
		return
	}
	revs := listRevisions(id)
	render(w, r, "history", PageData{
		Title:     "History: " + doc.Title,
		Doc:       doc,
		Revisions: revs,
	})
}

// revisionDiffHandler shows a side-by-side comparison of two revisions
// of the same document. Query params: a=<rev_id>&b=<rev_id>.
// If b is omitted, b defaults to the *current* document state.
func revisionDiffHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	doc := getDocumentByID(id)
	if doc == nil {
		http.NotFound(w, r)
		return
	}
	aID := r.URL.Query().Get("a")
	bID := r.URL.Query().Get("b")
	if aID == "" {
		http.Error(w, "missing ?a=<revision_id>", http.StatusBadRequest)
		return
	}
	revA := getRevision(aID)
	if revA == nil || strconv.Itoa(revA.DocID) != id {
		http.NotFound(w, r)
		return
	}
	var revB *RevisionEntry
	if bID == "" {
		// "current" = synthetic revision built from the live document
		revB = &RevisionEntry{
			ID:       0,
			DocID:    revA.DocID,
			Title:    doc.Title,
			TitleNP:  doc.TitleNP,
			BodyHTML: doc.BodyHTML,
			BodyText: doc.BodyText,
			EditedAt: doc.CreatedAt,
			Comment:  "(current)",
		}
	} else {
		revB = getRevision(bID)
		if revB == nil || strconv.Itoa(revB.DocID) != id {
			http.NotFound(w, r)
			return
		}
	}
	// Always show older on left, newer on right.
	if revA.ID != 0 && revB.ID != 0 && revA.ID > revB.ID {
		revA, revB = revB, revA
	}
	render(w, r, "revision_diff", PageData{
		Title: "Diff: " + doc.Title,
		Doc:   doc,
		RevA:  revA,
		RevB:  revB,
		Diff:  diffLines(revA.BodyText, revB.BodyText),
	})
}

// revisionRollbackHandler is admin-only. POST /document/{id}/revisions/{rev}/rollback
// Replaces the current document body with the chosen revision's body,
// records a fresh revision capturing the rollback action.
func revisionRollbackHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	revID := r.PathValue("rev")
	doc := getDocumentByID(id)
	if doc == nil {
		http.NotFound(w, r)
		return
	}
	rev := getRevision(revID)
	if rev == nil || strconv.Itoa(rev.DocID) != id {
		http.NotFound(w, r)
		return
	}
	user := sessionUser(r)
	if user == nil {
		http.Error(w, "auth required", http.StatusUnauthorized)
		return
	}

	// Re-parse wiki links against the current DB so any new docs that
	// have appeared since the old revision get resolved properly.
	rendered, refs := parseWikiLinks(string(rev.BodyHTML))
	bodyText := stripHTML(rendered)

	tx, err := db.Begin()
	if err != nil {
		http.Error(w, "tx error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`
		UPDATE documents
		SET title = ?, title_np = ?, body_html = ?, body_text = ?
		WHERE id = ?
	`, rev.Title, rev.TitleNP, rendered, bodyText, id); err != nil {
		http.Error(w, "update doc: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// New revision row for the rollback action itself
	docIDInt, _ := strconv.Atoi(id)
	if _, err := tx.Exec(`
		INSERT INTO revisions (doc_id, title, title_np, body_html, body_text, edited_by, edited_at, comment)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, docIDInt, rev.Title, rev.TitleNP, rendered, bodyText,
		user.ID, time.Now().UTC().Format(time.RFC3339),
		fmt.Sprintf("rolled back to revision #%d", rev.ID)); err != nil {
		http.Error(w, "insert revision: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := saveLinksForDoc(tx, int64(docIDInt), refs); err != nil {
		http.Error(w, "save links: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, "commit: "+err.Error(), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/document/"+id+"/history", http.StatusSeeOther)
}

// wantedHandler renders the red-link index — slugs referenced by
// `[[..]]` markup that don't yet have a matching document.
func wantedHandler(w http.ResponseWriter, r *http.Request) {
	wanted := listWantedLinks()
	render(w, r, "wanted", PageData{
		Title:       "Wanted pages — सिर्जना गर्न बाँकी पृष्ठहरू",
		WantedLinks: wanted,
	})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// humanAgo returns a short relative time like "3 minutes ago".
func humanAgo(rfc3339 string) string {
	t, err := time.Parse(time.RFC3339, rfc3339)
	if err != nil {
		return rfc3339
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d / time.Minute)
		if m == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", m)
	case d < 24*time.Hour:
		h := int(d / time.Hour)
		if h == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", h)
	case d < 30*24*time.Hour:
		days := int(d / (24 * time.Hour))
		if days == 1 {
			return "yesterday"
		}
		return fmt.Sprintf("%d days ago", days)
	default:
		return t.Format("2006-01-02")
	}
}

// DiffLine is one line of a unified-style line diff.
type DiffLine struct {
	Kind string // "same" | "add" | "del"
	Text string
}

// diffLines computes a small line-by-line diff between two strings using
// a simple LCS algorithm. Cheap, correct enough for short article bodies;
// we don't need anything fancier in beta.
func diffLines(a, b string) []DiffLine {
	aLines := strings.Split(strings.TrimRight(a, "\n"), "\n")
	bLines := strings.Split(strings.TrimRight(b, "\n"), "\n")

	// LCS table
	la, lb := len(aLines), len(bLines)
	dp := make([][]int, la+1)
	for i := range dp {
		dp[i] = make([]int, lb+1)
	}
	for i := la - 1; i >= 0; i-- {
		for j := lb - 1; j >= 0; j-- {
			if aLines[i] == bLines[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}

	var out []DiffLine
	i, j := 0, 0
	for i < la && j < lb {
		if aLines[i] == bLines[j] {
			out = append(out, DiffLine{Kind: "same", Text: aLines[i]})
			i++
			j++
		} else if dp[i+1][j] >= dp[i][j+1] {
			out = append(out, DiffLine{Kind: "del", Text: aLines[i]})
			i++
		} else {
			out = append(out, DiffLine{Kind: "add", Text: bLines[j]})
			j++
		}
	}
	for ; i < la; i++ {
		out = append(out, DiffLine{Kind: "del", Text: aLines[i]})
	}
	for ; j < lb; j++ {
		out = append(out, DiffLine{Kind: "add", Text: bLines[j]})
	}
	return out
}

