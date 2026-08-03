// netsk8-navigator desktop: a Wails-hosted native window around the exact
// same backend the CLI/browser binary (backend/main.go) serves — no sidecar
// process, no second toolchain. See /Users/bado/.claude/plans (Wails desktop
// prototype) for the full rationale.
package main

import (
	"fmt"
	"log"
	"net"
	"net/http"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/robertobado/netsk8-navigator/backend/internal/api"
	"github.com/robertobado/netsk8-navigator/backend/internal/config"
	"github.com/robertobado/netsk8-navigator/backend/internal/kube"
	"github.com/robertobado/netsk8-navigator/backend/internal/web"
)

// version is stamped at build time via -ldflags "-X main.version=...", the
// same convention backend/main.go uses. No UI surface for it yet — just
// parity with the CLI for support/debugging.
var version = "dev"

// buildMux mirrors backend/main.go's buildMux/mustInit — duplicated rather
// than imported, since the CLI's version lives in that binary's own
// (unexported) package main and can't be imported from here.
func buildMux() http.Handler {
	mgr, err := kube.NewManager()
	if err != nil {
		log.Fatalf("failed to load kubeconfig: %v", err)
	}
	log.Printf("loaded kubeconfig from %s (%d contexts)", mgr.ConfigPath(), len(mgr.Contexts()))

	cfg, err := config.NewStore()
	if err != nil {
		log.Fatalf("failed to init preferences store: %v", err)
	}
	log.Printf("preferences at %s", cfg.Path())

	srv := api.NewServer(mgr, cfg, "")
	mux := http.NewServeMux()
	mux.Handle("/api/", srv.Routes())
	if h := web.Handler(); h != nil {
		mux.Handle("/", h)
	} else {
		log.Fatal("no embedded frontend build — run `pnpm build` in frontend/ first")
	}
	return mux
}

// startServer serves mux over a real TCP socket on 127.0.0.1, exactly like
// the CLI does. A real socket is required (not just Wails' in-process
// AssetServer.Handler bridge) because that bridge is confirmed to break
// long-lived/streaming responses: verified empirically that Server-Sent
// Events (the live pod list, events feed, log tail) hang as "reconnecting"
// through it, and Wails' own issue tracker documents the same gap for
// WebSocket upgrades (pod exec, port-forward) in production builds. Returns
// the address to navigate the window to.
func startServer(mux http.Handler) string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatalf("failed to start local server: %v", err)
	}
	addr := ln.Addr().String()
	go func() {
		if err := http.Serve(ln, mux); err != nil {
			log.Printf("local server stopped: %v", err)
		}
	}()
	log.Printf("serving on http://%s", addr)
	return addr
}

// bootstrapRedirect serves a minimal real HTML document that immediately
// navigates the window to url via client-side JS — see the AssetServer
// comment in main() for why a plain HTTP redirect doesn't work here.
func bootstrapRedirect(url string) http.Handler {
	page := []byte(fmt.Sprintf(`<!doctype html><html><head><script>location.replace(%q)</script></head><body></body></html>`, url))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(page)
	})
}

func main() {
	log.Printf("netsk8-navigator-desktop %s", version)
	addr := startServer(buildMux())
	url := fmt.Sprintf("http://%s/", addr)

	app := NewApp()
	appMenu := menu.NewMenu()
	fileMenu := appMenu.AddSubmenu("File")
	fileMenu.AddText("Open in Browser", nil, func(_ *menu.CallbackData) {
		runtime.BrowserOpenURL(app.ctx, url)
	})

	err := wails.Run(&options.App{
		Title:  "Netsk8 Navigator",
		Width:  1280,
		Height: 800,
		// The window's initial "page" is a one-line bootstrap that
		// immediately navigates the whole window to the real server above
		// — once that navigation lands, the window is fully on the real
		// origin (same as browser mode) and Wails' asset-server bridge is
		// out of the picture entirely: SSE and WebSocket work exactly as
		// they do in a browser, with no special-casing in the frontend.
		//
		// A plain HTTP redirect doesn't work here — confirmed empirically:
		// Wails' webview renders the redirect response's body as a page
		// instead of following it like a real browser's address bar would.
		// A tiny real HTML document with a client-side navigation does.
		AssetServer: &assetserver.Options{
			Handler: bootstrapRedirect(url),
		},
		Menu:             appMenu,
		OnStartup:        app.startup,
		BackgroundColour: &options.RGBA{R: 20, G: 22, B: 30, A: 255},
	})
	if err != nil {
		log.Fatal(err)
	}
}
