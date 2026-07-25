package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWithCORS(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	t.Run("disabled when no origin configured", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
		withCORS("", inner).ServeHTTP(rec, req)
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("Access-Control-Allow-Origin = %q, want unset", got)
		}
	})

	t.Run("sets headers when an origin is configured", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
		withCORS("http://localhost:5173", inner).ServeHTTP(rec, req)
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
			t.Errorf("Access-Control-Allow-Origin = %q", got)
		}
	})

	t.Run("short-circuits OPTIONS preflight", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodOptions, "/api/health", nil)
		called := false
		withCORS("http://localhost:5173", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })).ServeHTTP(rec, req)
		if called {
			t.Error("inner handler should not run for OPTIONS")
		}
		if rec.Code != http.StatusNoContent {
			t.Errorf("status = %d, want 204", rec.Code)
		}
	})
}

func TestWsOriginAllowed(t *testing.T) {
	cases := []struct {
		name       string
		origin     string
		host       string
		configured string
		want       bool
	}{
		{"no Origin header (non-browser client)", "", "netsk8.example:8080", "", true},
		{"same-origin", "https://netsk8.example:8080", "netsk8.example:8080", "", true},
		{"documented Vite dev origin", "http://localhost:5173", "localhost:8080", "", true},
		{"127.0.0.1 Vite dev origin", "http://127.0.0.1:5173", "127.0.0.1:8080", "", true},
		{"explicitly configured origin", "https://navigator.internal", "backend:8080", "https://navigator.internal", true},
		{"unknown foreign origin", "https://evil.example", "netsk8.example:8080", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/contexts/x/pods/ns/name/exec", nil)
			req.Host = c.host
			if c.origin != "" {
				req.Header.Set("Origin", c.origin)
			}
			if got := wsOriginAllowed(c.configured)(req); got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}
