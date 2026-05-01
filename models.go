package main

// API request/response types for the JSON API (Phase 1 v1).
//
// These mirror PHASE1_API_SPEC.md §2 1:1. Keep them in sync with
// yogilib-sveltekit/src/lib/types.ts on the frontend.
//
// Naming convention: API-shape structs are prefixed `Api*` so they
// don't collide with the internal Go types (Document, User, ...).

// ApiUser is the canonical user shape returned by the API.
type ApiUser struct {
	ID    int    `json:"id"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

// ApiLoginReq is the request body for POST /api/v1/auth/login.
type ApiLoginReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// ApiLoginResp is the response body for POST /api/v1/auth/login.
type ApiLoginResp struct {
	User  ApiUser `json:"user"`
	Token string  `json:"token,omitempty"` // returned for non-browser clients
}

// ApiErrorResp is the canonical error envelope. Matches §2.2.
type ApiErrorResp struct {
	Error   string      `json:"error"`
	Code    string      `json:"code"`
	Details interface{} `json:"details,omitempty"`
}

// DocumentSummary is the *list* shape — omits heavy fields.
// See PHASE1_API_SPEC.md §2.3 (GET /api/v1/documents).
type DocumentSummary struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	TitleNP      string `json:"title_np"`
	Slug         string `json:"slug"`
	Category     string `json:"category"`
	Description  string `json:"description"`
	Lang         string `json:"lang"`
	Script       string `json:"script"`
	OrigAuthor   string `json:"orig_author"`
	OrigAuthorNP string `json:"orig_author_np"`
	OrigYear     string `json:"orig_year"`
	OrigMonth    string `json:"orig_month"`
	OrigDay      string `json:"orig_day"`
	CreatedAt    string `json:"created_at"`
	HasFile      bool   `json:"has_file"`
	FileURL      string `json:"file_url"`
}

// ApiTOCEntry is one heading entry for the table of contents.
type ApiTOCEntry struct {
	Level int    `json:"level"`
	Text  string `json:"text"`
	ID    string `json:"id"`
}

// ApiBacklinkEntry is one backlinking document.
type ApiBacklinkEntry struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
}

// ApiEntityEntry is one extracted entity (person/place/etc.).
type ApiEntityEntry struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

// ApiLinkSuggestion is an admin-only AI link suggestion.
type ApiLinkSuggestion struct {
	ToDocID int     `json:"to_doc_id"`
	Title   string  `json:"title"`
	Score   float32 `json:"score"`
}

// DocumentDetail extends DocumentSummary with all the heavy fields
// needed to render a single-document page.
type DocumentDetail struct {
	DocumentSummary
	BodyHTML        string              `json:"body_html"`
	BodyText        string              `json:"body_text"`
	TOC             []ApiTOCEntry       `json:"toc"`
	Backlinks       []ApiBacklinkEntry  `json:"backlinks"`
	Summary         string              `json:"summary"`
	SummaryNP       string              `json:"summary_np"`
	Entities        []ApiEntityEntry    `json:"entities"`
	LinkSuggestions []ApiLinkSuggestion `json:"link_suggestions,omitempty"`
}

// PaginatedDocs is the response envelope for the documents list.
type PaginatedDocs struct {
	Items   []DocumentSummary `json:"items"`
	Total   int               `json:"total"`
	Page    int               `json:"page"`
	PerPage int               `json:"per_page"`
}

// ApiCategory is one category entry in the categories list.
type ApiCategory struct {
	Slug     string `json:"slug"`
	Title    string `json:"title"`
	DocCount int    `json:"doc_count"`
}

// ApiCategoriesResp wraps the categories list.
type ApiCategoriesResp struct {
	Categories []ApiCategory `json:"categories"`
}

// ApiRevision is one row from the revisions table, API-shaped.
type ApiRevision struct {
	ID          int    `json:"id"`
	EditedBy    int    `json:"edited_by"`
	EditorEmail string `json:"editor_email"`
	EditedAt    string `json:"edited_at"`
	Comment     string `json:"comment"`
}

// ApiRevisionsResp wraps the revisions list.
type ApiRevisionsResp struct {
	Revisions []ApiRevision `json:"revisions"`
}

// ApiDiffLine is one line of a unified-style diff.
type ApiDiffLine struct {
	Type string `json:"type"` // "equal" | "add" | "del"
	Text string `json:"text"`
}

// ApiDiffMeta is the (id, edited_at, comment) tuple shown above each side.
type ApiDiffMeta struct {
	ID       int    `json:"id"`
	EditedAt string `json:"edited_at"`
	Comment  string `json:"comment"`
}

// ApiDiffResp is the response for /api/v1/documents/{id}/diff.
type ApiDiffResp struct {
	From          ApiDiffMeta   `json:"from"`
	To            ApiDiffMeta   `json:"to"`
	Lines         []ApiDiffLine `json:"lines"`
	TitleChanged  bool          `json:"title_changed"`
	TitleFrom     string        `json:"title_from"`
	TitleTo       string        `json:"title_to"`
}

// ApiExcerpt is the API shape for an excerpt.
type ApiExcerpt struct {
	Slug     string `json:"slug"`
	Title    string `json:"title"`
	BodyHTML string `json:"body_html"`
}

// ApiExcerptsResp wraps the excerpts list.
type ApiExcerptsResp struct {
	Items []ApiExcerpt `json:"items"`
}

// ApiWantedSource is one document that contains a wanted link.
type ApiWantedSource struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
}

// ApiWantedEntry is one red-link target.
type ApiWantedEntry struct {
	ToSlug    string            `json:"to_slug"`
	Anchor    string            `json:"anchor"`
	FromCount int               `json:"from_count"`
	FromDocs  []ApiWantedSource `json:"from_docs"`
}

// ApiWantedResp wraps the wanted-links list.
type ApiWantedResp struct {
	Items []ApiWantedEntry `json:"items"`
}

// ApiHealthResp is the response for GET /api/v1/health.
type ApiHealthResp struct {
	Status  string `json:"status"`
	Version string `json:"version"`
	DB      string `json:"db"`
	Enrich  string `json:"enrich"`
}

// ApiEnrichStats is the rolling counters block shown on /admin/enrich/status.
type ApiEnrichStats struct {
	TotalDocs     int `json:"total_docs"`
	WithSummary   int `json:"with_summary"`
	WithEmbedding int `json:"with_embedding"`
	WithEntities  int `json:"with_entities"`
	QueueDepth    int `json:"queue_depth"`
}

// ApiEnrichLastPass is one row in the last-pass log.
type ApiEnrichLastPass struct {
	DocID     int    `json:"doc_id"`
	Stage     string `json:"stage"`
	OK        bool   `json:"ok"`
	ElapsedMs int    `json:"elapsed_ms"`
}

// ApiEnrichStatusResp is the response for GET /api/v1/admin/enrich/status.
type ApiEnrichStatusResp struct {
	Enabled    bool                 `json:"enabled"`
	OllamaURL  string               `json:"ollama_url"`
	LastRunAt  string               `json:"last_run_at"`
	Stats      ApiEnrichStats       `json:"stats"`
	LastPass   []ApiEnrichLastPass  `json:"last_pass"`
	LastError  string               `json:"last_error,omitempty"`
}

// ApiEnrichRunResp is the response for POST /api/v1/admin/enrich/run.
type ApiEnrichRunResp struct {
	Started bool   `json:"started"`
	PassID  string `json:"pass_id"`
}

// ApiEditDocReq is the request body for POST /api/v1/documents/{id}.
type ApiEditDocReq struct {
	Title              string `json:"title"`
	TitleNP            string `json:"title_np"`
	Category           string `json:"category"`
	Description        string `json:"description"`
	Lang               string `json:"lang"`
	Script             string `json:"script"`
	OrigAuthor         string `json:"orig_author"`
	OrigAuthorNP       string `json:"orig_author_np"`
	OrigYear           string `json:"orig_year"`
	OrigMonth          string `json:"orig_month"`
	OrigDay            string `json:"orig_day"`
	BodyHTML           string `json:"body_html"`
	EditComment        string `json:"edit_comment"`
	ExpectedRevisionID int    `json:"expected_revision_id,omitempty"`
}
