package main

// Unified auth helpers for the JSON API.
//
// The HTML routes use a `session=<token>` cookie. The API was originally
// using `Authorization: Bearer <token>`. Both write to (and read from)
// the same `sessions` table. This file unifies extraction so a Svelte
// SPA running in a browser can use cookies while curl/test clients can
// continue to use Bearer tokens.

import (
	"net/http"
	"strings"
	"time"
)

// extractAuth returns a token (cookie wins, Bearer fallback) and the
// resolved User, or (nil, "") when the request is unauthenticated.
//
// This is the only auth-extraction helper API code should call.
func extractAuth(r *http.Request) (*User, string) {
	// 1. Cookie first (preferred for browsers).
	if c, err := r.Cookie("session"); err == nil && c.Value != "" {
		if u := lookupSession(c.Value); u != nil {
			return u, c.Value
		}
	}
	// 2. Bearer fallback (CLI / tests).
	if tok := extractBearer(r); tok != "" {
		if u := lookupSession(tok); u != nil {
			return u, tok
		}
	}
	return nil, ""
}

// extractBearer pulls the token out of an Authorization: Bearer ... header.
func extractBearer(r *http.Request) string {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return ""
	}
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		return strings.TrimSpace(parts[1])
	}
	return ""
}

// lookupSession resolves a session token to a User, or nil when the
// token is unknown or expired (expired tokens are deleted).
func lookupSession(token string) *User {
	if token == "" {
		return nil
	}
	var u User
	var expiresAt string
	err := db.QueryRow(`
		SELECT u.id, u.email, u.role, s.expires_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token = ?
	`, token).Scan(&u.ID, &u.Email, &u.Role, &expiresAt)
	if err != nil {
		return nil
	}
	exp, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil || time.Now().After(exp) {
		deleteSession(token)
		return nil
	}
	return &u
}
