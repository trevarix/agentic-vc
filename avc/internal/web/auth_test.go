// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOriginAllowed(t *testing.T) {
	cases := []struct {
		name     string
		origin   string
		bindAddr string
		want     bool
	}{
		{"no origin header (same-origin nav, curl)", "", "127.0.0.1:3004", true},
		{"matching http origin", "http://127.0.0.1:3004", "127.0.0.1:3004", true},
		{"matching https origin", "https://127.0.0.1:3004", "127.0.0.1:3004", true},
		{"different host", "http://evil.example.com", "127.0.0.1:3004", false},
		{"different port (DNS-rebinding-style mismatch)", "http://127.0.0.1:9999", "127.0.0.1:3004", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/api/restore", nil)
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			if got := originAllowed(r, tc.bindAddr); got != tc.want {
				t.Errorf("originAllowed(origin=%q, bind=%q) = %v, want %v", tc.origin, tc.bindAddr, got, tc.want)
			}
		})
	}
}

func TestTokenValid(t *testing.T) {
	const token = "secret-token"

	t.Run("valid bearer header", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/api/restore", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		if !tokenValid(r, token) {
			t.Error("expected a valid Authorization header to pass")
		}
	})
	t.Run("wrong bearer token", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/api/restore", nil)
		r.Header.Set("Authorization", "Bearer wrong")
		if tokenValid(r, token) {
			t.Error("expected a wrong token to fail")
		}
	})
	t.Run("valid query param", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/api/restore?token="+token, nil)
		if !tokenValid(r, token) {
			t.Error("expected a valid ?token= query param to pass")
		}
	})
	t.Run("no token at all", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/api/restore", nil)
		if tokenValid(r, token) {
			t.Error("expected a missing token to fail")
		}
	})
}

// TestWithAuth_ClosesTheCSRFGap reproduces the exact scenario the review
// flagged: the web server had no auth and no Origin check, so any page open
// in another tab could fetch("http://127.0.0.1:3004/api/restore", {method:
// "POST"}) and the server would comply. withAuth must reject that.
func TestWithAuth_ClosesTheCSRFGap(t *testing.T) {
	const token = "secret-token"
	const bindAddr = "127.0.0.1:3004"

	called := false
	inner := func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}
	handler := withAuth(token, bindAddr, inner)

	// A malicious page's fetch: correct token (e.g. leaked some other way)
	// but wrong Origin — still must be rejected, since Origin is checked
	// independently of the token.
	called = false
	r := httptest.NewRequest(http.MethodPost, "/api/restore", nil)
	r.Header.Set("Origin", "http://evil.example.com")
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler(w, r)
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 for a cross-origin request", w.Code)
	}
	if called {
		t.Error("inner handler must not run for a rejected origin")
	}

	// Correct origin (or none), but no token — the pre-fix behavior, and
	// exactly what an unauthenticated attacker page would send.
	called = false
	r2 := httptest.NewRequest(http.MethodPost, "/api/restore", nil)
	w2 := httptest.NewRecorder()
	handler(w2, r2)
	if w2.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 for a missing token", w2.Code)
	}
	if called {
		t.Error("inner handler must not run without a token")
	}

	// The legitimate case: same-origin frontend with its session cookie's
	// token attached.
	called = false
	r3 := httptest.NewRequest(http.MethodPost, "/api/restore", nil)
	r3.Header.Set("Authorization", "Bearer "+token)
	w3 := httptest.NewRecorder()
	handler(w3, r3)
	if w3.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 when origin and token are both valid", w3.Code)
	}
	if !called {
		t.Error("inner handler should run once auth passes")
	}
}
