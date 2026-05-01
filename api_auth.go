package main

// Auth API endpoints: login, logout, me.
//
// All three set / clear the same `session` cookie that the legacy HTML
// routes use; the response body of /login also includes the token so
// non-browser clients can use it as a Bearer.

import (
	"net/http"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// apiAuthLogin handles POST /api/v1/auth/login.
//
//   Body: { "email": "...", "password": "..." }
//
// On success: 200 with body { user, token } AND Set-Cookie: session=...
func apiAuthLogin(w http.ResponseWriter, r *http.Request) {
	var req ApiLoginReq
	if err := decodeJSONBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, "invalid request body")
		return
	}
	req.Email = strings.TrimSpace(req.Email)
	if req.Email == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, codeInvalidCredentials, "email and password are required")
		return
	}

	var u ApiUser
	var hash string
	err := db.QueryRow(
		`SELECT id, email, password_hash, role FROM users WHERE email = ?`, req.Email,
	).Scan(&u.ID, &u.Email, &hash, &u.Role)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)) != nil {
		writeError(w, http.StatusUnauthorized, codeInvalidCredentials, "invalid email or password")
		return
	}

	token, err := createSession(u.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, codeSessionError, "failed to create session")
		return
	}

	// Set the same cookie the HTML routes use so SPA + legacy share state.
	setSessionCookie(w, token)

	writeJSON(w, http.StatusOK, ApiLoginResp{User: u, Token: token})
}

// apiAuthLogout handles POST /api/v1/auth/logout.
//
// Revokes whichever token was used to authenticate (cookie or bearer)
// and clears the session cookie. Always returns 200; logging out twice
// is harmless.
func apiAuthLogout(w http.ResponseWriter, r *http.Request) {
	_, token := extractAuth(r)
	if token != "" {
		deleteSession(token)
	}
	clearSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// apiAuthMe handles GET /api/v1/auth/me.
//
// Returns 200 with the User payload, or 401 when unauthenticated.
func apiAuthMe(w http.ResponseWriter, r *http.Request) {
	u, _ := extractAuth(r)
	if u == nil {
		writeError(w, http.StatusUnauthorized, codeUnauthorized, "not authenticated")
		return
	}
	writeJSON(w, http.StatusOK, ApiUser{ID: u.ID, Email: u.Email, Role: u.Role})
}
