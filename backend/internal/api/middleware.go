package api

import (
	"log"
	"net/http"
	"net/url"
	"time"
)

// devOrigins are the fixed origins the documented dev flow uses (Vite's dev
// server proxies /api to the Go backend — see frontend/vite.config.ts). The
// proxy's changeOrigin rewrites the outgoing Host header but NOT the
// browser's original Origin header, so without this the exec terminal's
// WebSocket upgrade (which Vite proxies but doesn't rewrite) would fail
// origin checks even in the standard local dev setup.
var devOrigins = map[string]bool{
	"http://localhost:5173": true,
	"http://127.0.0.1:5173": true,
}

// withCORS only sets CORS headers when an allowed origin is configured
// (CORS_ORIGIN env var) — needed for split setups where the browser calls
// this API directly from a different origin. The default, embedded
// single-binary deployment is same-origin already (see main.go) and needs no
// CORS headers at all; leaving them off is the safer default.
func withCORS(allowedOrigin string, next http.Handler) http.Handler {
	if allowedOrigin == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// wsOriginAllowed reports whether a WebSocket upgrade's Origin header should
// be accepted: same-origin, the documented dev-server origins, or the
// explicitly configured CORS_ORIGIN. A missing Origin header (any
// non-browser client) is allowed through — only browsers send it, and only
// browsers are subject to same-origin policy in the first place.
func wsOriginAllowed(configuredOrigin string) func(*http.Request) bool {
	return func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true
		}
		if devOrigins[origin] || (configuredOrigin != "" && origin == configuredOrigin) {
			return true
		}
		u, err := url.Parse(origin)
		return err == nil && u.Host == r.Host
	}
}

func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s (%s)", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
	})
}
