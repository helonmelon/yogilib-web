package main

// JSON API common helpers: write helpers, error envelope, middleware,
// CORS, request decoders, and pagination parsing.
//
// All v1 API handlers should funnel responses through writeJSON /
// writeError so the wire format stays consistent.

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// Error code constants — see PHASE1_API_SPEC.md §2.2
// ---------------------------------------------------------------------------

const (
	codeBadRequest         = "BAD_REQUEST"
	codeUnauthorized       = "UNAUTHORIZED"
	codeForbidden          = "FORBIDDEN"
	codeNotFound           = "NOT_FOUND"
	codeMethodNotAllowed   = "METHOD_NOT_ALLOWED"
	codeConflict           = "CONFLICT"
	codePayloadTooLarge    = "PAYLOAD_TOO_LARGE"
	codeUnsupportedMedia   = "UNSUPPORTED_MEDIA"
	codeRateLimited        = "RATE_LIMITED"
	codeInternalError      = "INTERNAL_ERROR"
	codeServiceUnavailable = "SERVICE_UNAVAILABLE"
	codeInvalidCredentials = "INVALID_CREDENTIALS"
	codeSessionError       = "SESSION_ERROR"
	codeSlugTaken          = "SLUG_TAKEN"
)

// version is reported by /api/v1/health.
const apiVersion = "0.1.0"

// ---------------------------------------------------------------------------
// Write helpers (v1 — replaces the old jsonResp/jsonError pair)
// ---------------------------------------------------------------------------

// writeJSON encodes data as JSON and writes it with the given status code.
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if data == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Println("writeJSON encode:", err)
	}
}

// writeError writes a canonical {"error","code"} envelope.
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, ApiErrorResp{Error: message, Code: code})
}

// writeErrorDetails writes an error with optional details payload.
func writeErrorDetails(w http.ResponseWriter, status int, code, message string, details interface{}) {
	writeJSON(w, status, ApiErrorResp{Error: message, Code: code, Details: details})
}

// ---------------------------------------------------------------------------
// Pagination
// ---------------------------------------------------------------------------

const (
	defaultPerPage = 50
	maxPerPage     = 200
)

// parsePagination reads ?page= & ?per_page= and returns sane defaults.
func parsePagination(r *http.Request) (page, perPage int) {
	page = 1
	perPage = defaultPerPage
	if v := r.URL.Query().Get("page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			page = n
		}
	}
	if v := r.URL.Query().Get("per_page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			perPage = n
		}
	}
	if perPage > maxPerPage {
		perPage = maxPerPage
	}
	return page, perPage
}

// ---------------------------------------------------------------------------
// Decoders
// ---------------------------------------------------------------------------

// decodeJSONBody decodes a request body into dst. Returns a wrapped error
// suitable for direct mapping to a 400 BAD_REQUEST response.
func decodeJSONBody(r *http.Request, dst interface{}) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("invalid json: %w", err)
	}
	// Reject trailing junk so we don't silently accept "{}{}{}"
	if dec.More() {
		return errors.New("trailing data after json body")
	}
	return nil
}

// ---------------------------------------------------------------------------
// Middleware
// ---------------------------------------------------------------------------

// Middleware is a standard http handler decorator.
type Middleware func(http.Handler) http.Handler

// chain composes middlewares around an http.HandlerFunc, leftmost first.
func chain(h http.HandlerFunc, mws ...Middleware) http.HandlerFunc {
	var handler http.Handler = h
	for i := len(mws) - 1; i >= 0; i-- {
		handler = mws[i](handler)
	}
	return handler.ServeHTTP
}

// allowedOrigins is the CORS allow-list. Extend via ALLOWED_ORIGINS env if needed.
var allowedOrigins = []string{
	"http://localhost:5173",
	"http://localhost:8080",
	"http://127.0.0.1:5173",
	"http://127.0.0.1:8080",
}

func isOriginAllowed(origin string) bool {
	if origin == "" {
		return false
	}
	for _, o := range allowedOrigins {
		if o == origin {
			return true
		}
	}
	return false
}

// withCORS sets CORS headers based on the Origin header. Same-origin
// requests pass through untouched (no Origin header). Pre-flight
// OPTIONS gets a 204.
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if isOriginAllowed(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// withRecover converts panics into a JSON 500.
func withRecover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("panic in %s %s: %v\n%s", r.Method, r.URL.Path, rec, debug.Stack())
				writeError(w, http.StatusInternalServerError, codeInternalError, "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// withJSONLog logs the method, path, and remote addr for API requests.
func withJSONLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("api %s %s", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}

// requireAuth returns 401 unless the request resolves to a User.
// On success it stores nothing in the request context (handlers can
// re-call extractAuth — cheap, single SQL row).
func requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, _ := extractAuth(r)
		if u == nil {
			writeError(w, http.StatusUnauthorized, codeUnauthorized, "authentication required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireRoleAPI returns a Middleware that enforces a minimum role.
// Use after requireAuth in the chain so 401 is returned before 403.
func requireRoleAPI(role string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u, _ := extractAuth(r)
			if u == nil {
				writeError(w, http.StatusUnauthorized, codeUnauthorized, "authentication required")
				return
			}
			if !hasRole(u.Role, role) {
				writeError(w, http.StatusForbidden, codeForbidden, "insufficient role: "+role+" required")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ---------------------------------------------------------------------------
// Convenience: the standard middleware stack for public/auth/admin routes
// ---------------------------------------------------------------------------

// apiPublic wraps a handler with the standard public-API middleware
// (CORS + recover + log).
func apiPublic(h http.HandlerFunc) http.HandlerFunc {
	return chain(h, withCORS, withRecover, withJSONLog)
}

// apiAuthed wraps with public middleware + auth requirement.
func apiAuthed(h http.HandlerFunc) http.HandlerFunc {
	return chain(h, withCORS, withRecover, withJSONLog, requireAuth)
}

// apiRole wraps with public middleware + role requirement.
func apiRole(role string, h http.HandlerFunc) http.HandlerFunc {
	return chain(h, withCORS, withRecover, withJSONLog, requireRoleAPI(role))
}

// ---------------------------------------------------------------------------
// Misc helpers
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Route registration
// ---------------------------------------------------------------------------

// registerAPIv1Routes wires every /api/v1/... endpoint into the mux.
// All handlers are wrapped in the CORS+recover+log middleware stack;
// auth/role enforcement happens in the per-handler chain.
func registerAPIv1Routes(mux *http.ServeMux) {
	// Meta
	mux.HandleFunc("GET /api/v1/health", apiPublic(apiHealth))
	mux.HandleFunc("GET /api/v1/categories", apiPublic(apiCategoriesList))

	// Auth
	mux.HandleFunc("POST /api/v1/auth/login", apiPublic(apiAuthLogin))
	mux.HandleFunc("POST /api/v1/auth/logout", apiPublic(apiAuthLogout))
	mux.HandleFunc("GET /api/v1/auth/me", apiPublic(apiAuthMe))

	// Documents (read)
	mux.HandleFunc("GET /api/v1/documents", apiPublic(apiDocumentsList))
	mux.HandleFunc("GET /api/v1/documents/{id}", apiPublic(apiDocumentGet))
	mux.HandleFunc("GET /api/v1/documents/{id}/revisions", apiPublic(apiDocumentRevisions))
	mux.HandleFunc("GET /api/v1/documents/{id}/diff", apiPublic(apiDocumentDiff))

	// Documents (write)
	mux.HandleFunc("POST /api/v1/documents", apiRole("uploader", apiDocumentsCreate))
	mux.HandleFunc("POST /api/v1/documents/{id}", apiRole("admin", apiDocumentUpdate))
	mux.HandleFunc("POST /api/v1/documents/{id}/revisions/{rev}/rollback", apiRole("admin", apiDocumentRollback))

	// Excerpts
	mux.HandleFunc("GET /api/v1/excerpts", apiPublic(apiExcerptsList))
	mux.HandleFunc("GET /api/v1/excerpts/{slug}", apiPublic(apiExcerptGet))

	// Wanted (red links)
	mux.HandleFunc("GET /api/v1/wanted", apiPublic(apiWantedList))

	// Admin
	mux.HandleFunc("GET /api/v1/admin/enrich/status", apiRole("admin", apiAdminEnrichStatus))
	mux.HandleFunc("POST /api/v1/admin/enrich/run", apiRole("admin", apiAdminEnrichRun))

	// Catch-all OPTIONS for any /api/v1/* path so CORS pre-flight works
	// even on routes we haven't defined yet.
	mux.HandleFunc("OPTIONS /api/v1/", apiPublic(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
}

// envOr returns the env var or fallback when unset/empty.
func envOr(key, fallback string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	return v
}
