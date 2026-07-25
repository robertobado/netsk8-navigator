// Package web embeds the built frontend SPA (frontend/`pnpm build`, output
// into ./dist by vite.config.ts) so a single Go binary can serve both the API
// and the UI. dist/ only has a placeholder committed to source control — a
// plain `go build` without a preceding frontend build still succeeds, it just
// has nothing to embed (Handler returns nil, see below).
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

// Handler serves the embedded SPA, falling back to index.html for
// client-side routes (anything not matching a real file). Returns nil when
// no frontend build is embedded, so main.go can fall back to API-only mode —
// the case during local dev, where the Vite dev server serves the UI instead.
func Handler() http.Handler {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return nil
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil
	}
	files := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := fs.Stat(sub, strings.TrimPrefix(r.URL.Path, "/")); err != nil {
			r.URL.Path = "/"
		}
		files.ServeHTTP(w, r)
	})
}
