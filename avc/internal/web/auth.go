// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

const tokenCookieName = "avc_token"

// newSessionToken generates a random per-server-run token used to gate every
// mutating /api/ request. A page in another browser tab cannot read this
// value (it's a same-site cookie, and Origin checks below back it up), so it
// cannot silently call AVC's local API the way an unauthenticated server
// bound to 127.0.0.1 could be.
func newSessionToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// withSessionCookie sets the session token as a readable (non-HttpOnly, so
// the frontend JS can attach it as a header), SameSite=Strict cookie on
// every response. This lets the frontend pick up the token on first load
// without changing how the server's URL is opened or printed.
func withSessionCookie(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{
			Name:     tokenCookieName,
			Value:    token,
			Path:     "/",
			SameSite: http.SameSiteStrictMode,
			HttpOnly: false,
		})
		next.ServeHTTP(w, r)
	})
}

// withAuth gates an /api/ handler behind the session token (Authorization:
// Bearer <token>, or ?token=<token> for convenience) and an Origin check.
// Together these close the gap the review flagged: the plain HTTP server had
// no auth and no Origin/Host validation, so any page open in another tab
// could fetch("http://127.0.0.1:3004/api/restore", {method: "POST"}) and the
// server would comply.
func withAuth(token, bindAddr string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !originAllowed(r, bindAddr) {
			writeError(w, http.StatusForbidden, "origin not allowed")
			return
		}
		if !tokenValid(r, token) {
			writeError(w, http.StatusUnauthorized, "missing or invalid session token")
			return
		}
		next(w, r)
	}
}

func tokenValid(r *http.Request, token string) bool {
	if auth := r.Header.Get("Authorization"); auth != "" {
		return auth == "Bearer "+token
	}
	return r.URL.Query().Get("token") == token
}

// originAllowed rejects cross-origin requests. Browsers set Origin on every
// fetch/XHR whose target origin differs from the page making the request —
// a request with no Origin header (same-origin navigation, curl, scripts)
// is allowed through; a request whose Origin doesn't match our own bound
// address is rejected. This is what actually stops a malicious page in
// another tab from invoking the API — the token cookie is SameSite=Strict
// and would not even be attached to such a request, but Origin validation
// closes the same gap for any client that doesn't send cookies at all.
func originAllowed(r *http.Request, bindAddr string) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	return origin == "http://"+bindAddr || origin == "https://"+bindAddr
}
