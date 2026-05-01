package main

// Document API endpoints:
//   GET    /api/v1/documents                            list with q/cat
//   POST   /api/v1/documents                            create (multipart or JSON)
//   GET    /api/v1/documents/{id}                       detail
//   POST   /api/v1/documents/{id}                       update (admin)
//   GET    /api/v1/documents/{id}/revisions             revision history
//   GET    /api/v1/documents/{id}/diff?from=&to=        diff two revisions
//   POST   /api/v1/documents/{id}/revisions/{rev}/rollback   admin rollback

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Conversion helpers
// ---------------------------------------------------------------------------

// docToSummary projects a Document into the lightweight list shape.
func docToSummary(d Document) DocumentSummary {
	return DocumentSummary{
		ID:           d.ID,
		Title:        d.Title,
		TitleNP:      d.TitleNP,
		Slug:         d.Slug,
		Category:     d.Category,
		Description:  d.Description,
		Lang:         d.Lang,
		Script:       d.Script,
		OrigAuthor:   d.OrigAuthor,
		OrigAuthorNP: d.OrigAuthorNP,
		OrigYear:     d.OrigYear,
		OrigMonth:    d.OrigMonth,
		OrigDay:      d.OrigDay,
		CreatedAt:    d.CreatedAt,
		HasFile:      d.FilePath != "",
		FileURL:      d.FilePath,
	}
}

// docToDetail projects a Document into the full detail shape.
// Loads backlinks, TOC, summary, entities, and (admin-only) link suggestions.
func docToDetail(d Document, isAdmin bool) DocumentDetail {
	id := d.ID

	// Inject TOC heading IDs at read time (presentational, not stored).
	bodyHTML := string(d.BodyHTML)
	var toc []ApiTOCEntry
	if bodyHTML != "" {
		newHTML, entries := extractTOC(bodyHTML)
		bodyHTML = newHTML
		for _, e := range entries {
			toc = append(toc, ApiTOCEntry{Level: e.Level, Text: e.Text, ID: e.ID})
		}
	}

	// Backlinks.
	backlinks := []ApiBacklinkEntry{}
	for _, b := range getBacklinks(id) {
		backlinks = append(backlinks, ApiBacklinkEntry{ID: b.ID, Title: b.Title})
	}

	// Entities.
	entities := []ApiEntityEntry{}
	for _, e := range getDocEntities(id) {
		entities = append(entities, ApiEntityEntry{Kind: e.Kind, Name: e.Value})
	}

	// Detail.
	det := DocumentDetail{
		DocumentSummary: docToSummary(d),
		BodyHTML:        bodyHTML,
		BodyText:        d.BodyText,
		TOC:             toc,
		Backlinks:       backlinks,
		Summary:         getDocSummary(id),
		SummaryNP:       getDocSummaryNP(id),
		Entities:        entities,
	}

	// Admin-only AI link suggestions.
	if isAdmin {
		var suggs []ApiLinkSuggestion
		for _, s := range getLinkSuggestionsForDoc(id) {
			suggs = append(suggs, ApiLinkSuggestion{
				ToDocID: s.ToID,
				Title:   s.Title,
				Score:   s.Score,
			})
		}
		det.LinkSuggestions = suggs
	}

	return det
}

// ---------------------------------------------------------------------------
// LIST
// ---------------------------------------------------------------------------

// apiDocumentsList handles GET /api/v1/documents?q=&cat=&page=&per_page=
func apiDocumentsList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	cat := r.URL.Query().Get("cat")
	page, perPage := parsePagination(r)

	all := queryDocuments(q, cat)
	total := len(all)

	// In-memory pagination (the doc set is small; switch to SQL LIMIT
	// once the corpus grows past a few thousand).
	start := (page - 1) * perPage
	end := start + perPage
	if start > total {
		start = total
	}
	if end > total {
		end = total
	}
	pageSlice := all[start:end]

	items := make([]DocumentSummary, 0, len(pageSlice))
	for _, d := range pageSlice {
		items = append(items, docToSummary(d))
	}
	writeJSON(w, http.StatusOK, PaginatedDocs{
		Items:   items,
		Total:   total,
		Page:    page,
		PerPage: perPage,
	})
}

// ---------------------------------------------------------------------------
// GET ONE
// ---------------------------------------------------------------------------

// apiDocumentGet handles GET /api/v1/documents/{id}
func apiDocumentGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	doc := getDocumentByID(id)
	if doc == nil {
		writeError(w, http.StatusNotFound, codeNotFound, "document not found")
		return
	}
	user, _ := extractAuth(r)
	isAdmin := user != nil && hasRole(user.Role, "admin")
	writeJSON(w, http.StatusOK, docToDetail(*doc, isAdmin))
}

// ---------------------------------------------------------------------------
// CREATE
// ---------------------------------------------------------------------------

// apiDocumentsCreate handles POST /api/v1/documents.
//
// Accepts either:
//   - multipart/form-data with text fields + optional `file` (≤50 MB)
//   - application/json with the same fields and no file
//
// Auth: requires uploader+ role (enforced via middleware).
func apiDocumentsCreate(w http.ResponseWriter, r *http.Request) {
	const maxUploadSize = 50 << 20 // 50 MB

	user, _ := extractAuth(r)
	uploadedBy := 0
	if user != nil {
		uploadedBy = user.ID
	}

	contentType := r.Header.Get("Content-Type")

	var (
		title, titleNP, category, description string
		lang, script                          string
		origAuthor, origAuthorNP              string
		origYear, origMonth, origDay          string
		rawBody                               string
		filePath                              string
	)

	if strings.HasPrefix(contentType, "multipart/form-data") {
		// Cap the request body before parsing.
		r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)
		if err := r.ParseMultipartForm(maxUploadSize); err != nil {
			if strings.Contains(err.Error(), "too large") || strings.Contains(err.Error(), "request body too large") {
				writeError(w, http.StatusRequestEntityTooLarge, codePayloadTooLarge, "upload exceeds 50 MB")
				return
			}
			writeError(w, http.StatusBadRequest, codeBadRequest, "invalid multipart form: "+err.Error())
			return
		}
		title = strings.TrimSpace(r.FormValue("title"))
		titleNP = r.FormValue("title_np")
		category = r.FormValue("category")
		description = r.FormValue("description")
		lang = r.FormValue("doc_lang")
		if lang == "" {
			lang = r.FormValue("lang")
		}
		script = r.FormValue("script")
		origAuthor = r.FormValue("orig_author")
		origAuthorNP = r.FormValue("orig_author_np")
		origYear = r.FormValue("orig_year")
		origMonth = r.FormValue("orig_month")
		origDay = r.FormValue("orig_day")
		rawBody = r.FormValue("body_html")

		// Optional file.
		if file, header, err := r.FormFile("file"); err == nil {
			defer file.Close()
			if err := os.MkdirAll("static/docs", 0755); err != nil {
				log.Println("mkdir static/docs:", err)
				writeError(w, http.StatusInternalServerError, codeInternalError, "failed to prepare upload directory")
				return
			}
			ext := filepath.Ext(header.Filename)
			safeName := fmt.Sprintf("%d%s", time.Now().UnixNano(), ext)
			dst := filepath.Join("static", "docs", safeName)
			out, err := os.Create(dst)
			if err != nil {
				log.Println("create file:", err)
				writeError(w, http.StatusInternalServerError, codeInternalError, "failed to save file")
				return
			}
			if _, err := io.Copy(out, file); err != nil {
				out.Close()
				os.Remove(dst)
				log.Println("copy file:", err)
				writeError(w, http.StatusInternalServerError, codeInternalError, "failed to save file")
				return
			}
			out.Close()
			filePath = "/static/docs/" + safeName
		}
	} else if strings.HasPrefix(contentType, "application/json") {
		var req struct {
			Title        string `json:"title"`
			TitleNP      string `json:"title_np"`
			Category     string `json:"category"`
			Description  string `json:"description"`
			Lang         string `json:"lang"`
			Script       string `json:"script"`
			OrigAuthor   string `json:"orig_author"`
			OrigAuthorNP string `json:"orig_author_np"`
			OrigYear     string `json:"orig_year"`
			OrigMonth    string `json:"orig_month"`
			OrigDay      string `json:"orig_day"`
			BodyHTML     string `json:"body_html"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, codeBadRequest, "invalid json body")
			return
		}
		title = strings.TrimSpace(req.Title)
		titleNP = req.TitleNP
		category = req.Category
		description = req.Description
		lang = req.Lang
		script = req.Script
		origAuthor = req.OrigAuthor
		origAuthorNP = req.OrigAuthorNP
		origYear = req.OrigYear
		origMonth = req.OrigMonth
		origDay = req.OrigDay
		rawBody = req.BodyHTML
	} else {
		writeError(w, http.StatusUnsupportedMediaType, codeUnsupportedMedia, "Content-Type must be multipart/form-data or application/json")
		return
	}

	if title == "" {
		writeError(w, http.StatusBadRequest, codeBadRequest, "title is required")
		return
	}

	rendered, refs := parseWikiLinks(rawBody)
	bodyText := stripHTML(rendered)
	slug := slugify(title)
	if slug == "" {
		slug = slugify(titleNP)
	}

	tx, err := db.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, codeInternalError, "transaction begin failed")
		return
	}
	defer func() {
		if tx != nil {
			tx.Rollback()
		}
	}()

	res, err := tx.Exec(`
		INSERT INTO documents (
			title, title_np, slug, category, description,
			body_html, body_text,
			lang, script,
			orig_author, orig_author_np,
			orig_year, orig_month, orig_day,
			file_path, uploaded_by, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		title, titleNP, slug,
		category, description,
		rendered, bodyText,
		lang, script,
		origAuthor, origAuthorNP,
		origYear, origMonth, origDay,
		filePath, uploadedBy,
		time.Now().Format("2 Jan 2006"),
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			writeError(w, http.StatusConflict, codeSlugTaken, "slug already in use")
			return
		}
		log.Println("apiDocumentsCreate insert:", err)
		writeError(w, http.StatusInternalServerError, codeInternalError, "failed to create document")
		return
	}

	docID, _ := res.LastInsertId()

	if _, err := tx.Exec(
		`INSERT INTO revisions (doc_id, title, title_np, body_html, body_text, edited_by, edited_at, comment)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		docID, title, titleNP, rendered, bodyText, uploadedBy,
		time.Now().UTC().Format(time.RFC3339), "initial upload",
	); err != nil {
		log.Println("apiDocumentsCreate revision:", err)
		writeError(w, http.StatusInternalServerError, codeInternalError, "failed to write revision")
		return
	}

	if err := saveLinksForDoc(tx, docID, refs); err != nil {
		log.Println("apiDocumentsCreate links:", err)
		writeError(w, http.StatusInternalServerError, codeInternalError, "failed to write links")
		return
	}

	if err := tx.Commit(); err != nil {
		log.Println("apiDocumentsCreate commit:", err)
		writeError(w, http.StatusInternalServerError, codeInternalError, "failed to commit transaction")
		return
	}
	tx = nil // prevent deferred rollback

	// Re-fetch the freshly-written document so we can return the full detail.
	idStr := strconv.FormatInt(docID, 10)
	doc := getDocumentByID(idStr)
	if doc == nil {
		writeError(w, http.StatusInternalServerError, codeInternalError, "document missing after insert")
		return
	}
	isAdmin := user != nil && hasRole(user.Role, "admin")
	writeJSON(w, http.StatusCreated, docToDetail(*doc, isAdmin))
}

// ---------------------------------------------------------------------------
// UPDATE
// ---------------------------------------------------------------------------

// apiDocumentUpdate handles POST /api/v1/documents/{id}.
//
// Body: ApiEditDocReq (JSON only — file changes go through a future
// dedicated endpoint).
//
// Auth: admin (enforced via middleware).
func apiDocumentUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	doc := getDocumentByID(id)
	if doc == nil {
		writeError(w, http.StatusNotFound, codeNotFound, "document not found")
		return
	}

	var req ApiEditDocReq
	if err := decodeJSONBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Title) == "" {
		writeError(w, http.StatusBadRequest, codeBadRequest, "title is required")
		return
	}

	// Optional optimistic concurrency check.
	if req.ExpectedRevisionID > 0 {
		var latestRev int
		err := db.QueryRow(`SELECT id FROM revisions WHERE doc_id = ? ORDER BY id DESC LIMIT 1`, id).Scan(&latestRev)
		if err == nil && latestRev != req.ExpectedRevisionID {
			writeError(w, http.StatusConflict, codeConflict, fmt.Sprintf("expected revision %d but latest is %d", req.ExpectedRevisionID, latestRev))
			return
		}
	}

	rendered, refs := parseWikiLinks(req.BodyHTML)
	bodyText := stripHTML(rendered)
	slug := slugify(req.Title)
	if slug == "" {
		slug = slugify(req.TitleNP)
	}

	user, _ := extractAuth(r)
	editedBy := 0
	if user != nil {
		editedBy = user.ID
	}
	docIDInt, _ := strconv.Atoi(id)

	tx, err := db.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, codeInternalError, "transaction begin failed")
		return
	}
	defer func() {
		if tx != nil {
			tx.Rollback()
		}
	}()

	if _, err := tx.Exec(`
		UPDATE documents SET
			title          = ?,
			title_np       = ?,
			slug           = ?,
			category       = ?,
			description    = ?,
			lang           = ?,
			script         = ?,
			orig_author    = ?,
			orig_author_np = ?,
			orig_year      = ?,
			orig_month     = ?,
			orig_day       = ?,
			body_html      = ?,
			body_text      = ?
		WHERE id = ?
	`,
		req.Title, req.TitleNP, slug,
		req.Category, req.Description,
		req.Lang, req.Script,
		req.OrigAuthor, req.OrigAuthorNP,
		req.OrigYear, req.OrigMonth, req.OrigDay,
		rendered, bodyText, id,
	); err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			writeError(w, http.StatusConflict, codeSlugTaken, "slug already in use")
			return
		}
		log.Println("apiDocumentUpdate update:", err)
		writeError(w, http.StatusInternalServerError, codeInternalError, "failed to update document")
		return
	}

	if _, err := tx.Exec(
		`INSERT INTO revisions (doc_id, title, title_np, body_html, body_text, edited_by, edited_at, comment)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		docIDInt, req.Title, req.TitleNP, rendered, bodyText, editedBy,
		time.Now().UTC().Format(time.RFC3339), strings.TrimSpace(req.EditComment),
	); err != nil {
		log.Println("apiDocumentUpdate revision:", err)
		writeError(w, http.StatusInternalServerError, codeInternalError, "failed to write revision")
		return
	}

	if err := saveLinksForDoc(tx, int64(docIDInt), refs); err != nil {
		log.Println("apiDocumentUpdate links:", err)
		writeError(w, http.StatusInternalServerError, codeInternalError, "failed to write links")
		return
	}

	if err := tx.Commit(); err != nil {
		log.Println("apiDocumentUpdate commit:", err)
		writeError(w, http.StatusInternalServerError, codeInternalError, "failed to commit transaction")
		return
	}
	tx = nil

	updated := getDocumentByID(id)
	if updated == nil {
		writeError(w, http.StatusInternalServerError, codeInternalError, "document missing after update")
		return
	}
	writeJSON(w, http.StatusOK, docToDetail(*updated, true)) // admin already
}

// ---------------------------------------------------------------------------
// REVISIONS LIST
// ---------------------------------------------------------------------------

// apiDocumentRevisions handles GET /api/v1/documents/{id}/revisions.
func apiDocumentRevisions(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if getDocumentByID(id) == nil {
		writeError(w, http.StatusNotFound, codeNotFound, "document not found")
		return
	}
	revs := listRevisions(id)
	out := make([]ApiRevision, 0, len(revs))
	for _, rev := range revs {
		// Fetch the editor user_id alongside the email (listRevisions only
		// joined on email; pull edited_by directly here for the API).
		var editedBy int
		db.QueryRow(`SELECT COALESCE(edited_by,0) FROM revisions WHERE id = ?`, rev.ID).Scan(&editedBy)
		out = append(out, ApiRevision{
			ID:          rev.ID,
			EditedBy:    editedBy,
			EditorEmail: rev.EditedBy, // RevisionEntry.EditedBy holds the email
			EditedAt:    rev.EditedAt,
			Comment:     rev.Comment,
		})
	}
	writeJSON(w, http.StatusOK, ApiRevisionsResp{Revisions: out})
}

// ---------------------------------------------------------------------------
// DIFF
// ---------------------------------------------------------------------------

// apiDocumentDiff handles GET /api/v1/documents/{id}/diff?from=&to=
//
// `from` and `to` are revision IDs. If `to` is omitted (or 0), the
// "current" document state is used. Always presents older→newer.
func apiDocumentDiff(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	doc := getDocumentByID(id)
	if doc == nil {
		writeError(w, http.StatusNotFound, codeNotFound, "document not found")
		return
	}
	fromID := r.URL.Query().Get("from")
	toID := r.URL.Query().Get("to")
	if fromID == "" {
		writeError(w, http.StatusBadRequest, codeBadRequest, "missing ?from=<revision_id>")
		return
	}
	revA := getRevision(fromID)
	if revA == nil || strconv.Itoa(revA.DocID) != id {
		writeError(w, http.StatusNotFound, codeNotFound, "from-revision not found")
		return
	}

	var revB *RevisionEntry
	if toID == "" || toID == "0" {
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
		revB = getRevision(toID)
		if revB == nil || strconv.Itoa(revB.DocID) != id {
			writeError(w, http.StatusNotFound, codeNotFound, "to-revision not found")
			return
		}
	}

	// Always show older → newer.
	if revA.ID != 0 && revB.ID != 0 && revA.ID > revB.ID {
		revA, revB = revB, revA
	}

	rawDiff := diffLines(revA.BodyText, revB.BodyText)
	lines := make([]ApiDiffLine, 0, len(rawDiff))
	for _, dl := range rawDiff {
		t := dl.Kind
		if t == "same" {
			t = "equal"
		}
		lines = append(lines, ApiDiffLine{Type: t, Text: dl.Text})
	}

	writeJSON(w, http.StatusOK, ApiDiffResp{
		From:         ApiDiffMeta{ID: revA.ID, EditedAt: revA.EditedAt, Comment: revA.Comment},
		To:           ApiDiffMeta{ID: revB.ID, EditedAt: revB.EditedAt, Comment: revB.Comment},
		Lines:        lines,
		TitleChanged: revA.Title != revB.Title,
		TitleFrom:    revA.Title,
		TitleTo:      revB.Title,
	})
}

// ---------------------------------------------------------------------------
// ROLLBACK
// ---------------------------------------------------------------------------

// apiDocumentRollback handles POST /api/v1/documents/{id}/revisions/{rev}/rollback.
//
// Replaces the current document body with the chosen revision's body and
// records a fresh revision capturing the rollback action. Admin only.
func apiDocumentRollback(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	revID := r.PathValue("rev")
	doc := getDocumentByID(id)
	if doc == nil {
		writeError(w, http.StatusNotFound, codeNotFound, "document not found")
		return
	}
	rev := getRevision(revID)
	if rev == nil || strconv.Itoa(rev.DocID) != id {
		writeError(w, http.StatusNotFound, codeNotFound, "revision not found")
		return
	}
	user, _ := extractAuth(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, codeUnauthorized, "auth required")
		return
	}

	// Re-parse wiki links against current DB.
	rendered, refs := parseWikiLinks(string(rev.BodyHTML))
	bodyText := stripHTML(rendered)

	tx, err := db.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, codeInternalError, "tx begin failed")
		return
	}
	defer func() {
		if tx != nil {
			tx.Rollback()
		}
	}()

	if _, err := tx.Exec(`
		UPDATE documents
		SET title = ?, title_np = ?, body_html = ?, body_text = ?
		WHERE id = ?
	`, rev.Title, rev.TitleNP, rendered, bodyText, id); err != nil {
		writeError(w, http.StatusInternalServerError, codeInternalError, "update failed: "+err.Error())
		return
	}

	docIDInt, _ := strconv.Atoi(id)
	if _, err := tx.Exec(`
		INSERT INTO revisions (doc_id, title, title_np, body_html, body_text, edited_by, edited_at, comment)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, docIDInt, rev.Title, rev.TitleNP, rendered, bodyText,
		user.ID, time.Now().UTC().Format(time.RFC3339),
		fmt.Sprintf("rolled back to revision #%d", rev.ID)); err != nil {
		writeError(w, http.StatusInternalServerError, codeInternalError, "insert revision failed: "+err.Error())
		return
	}

	if err := saveLinksForDoc(tx, int64(docIDInt), refs); err != nil {
		writeError(w, http.StatusInternalServerError, codeInternalError, "save links failed: "+err.Error())
		return
	}

	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, codeInternalError, "commit failed: "+err.Error())
		return
	}
	tx = nil

	updated := getDocumentByID(id)
	if updated == nil {
		writeError(w, http.StatusInternalServerError, codeInternalError, "document missing after rollback")
		return
	}
	writeJSON(w, http.StatusOK, docToDetail(*updated, true))
}

// Compile-time assertion that template.HTML is referenced (used in
// docToDetail via getDocumentByID — silences unused-import linters in
// rare build configurations).
var _ template.HTML = ""
