// netsk8-navigator desktop: a Wails-hosted native window around the exact
// same backend the CLI/browser binary (backend/main.go) serves — no sidecar
// process, no second toolchain. See /Users/bado/.claude/plans (Wails desktop
// prototype) for the full rationale.
package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	goruntime "runtime"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/robertobado/netsk8-navigator/backend/internal/api"
	"github.com/robertobado/netsk8-navigator/backend/internal/config"
	"github.com/robertobado/netsk8-navigator/backend/internal/kube"
	"github.com/robertobado/netsk8-navigator/backend/internal/kubeconfig"
	"github.com/robertobado/netsk8-navigator/backend/internal/mcpinstall"
	"github.com/robertobado/netsk8-navigator/backend/internal/web"
)

// version is stamped at build time via -ldflags "-X main.version=...", the
// same convention backend/main.go uses. No UI surface for it yet — just
// parity with the CLI for support/debugging.
var version = "dev"

// fixPathForGUILaunch resolves the user's real login-shell PATH and adopts
// it. An app launched from Finder/Dock/Spotlight (as opposed to a terminal)
// inherits launchd's bare PATH (/usr/bin:/bin:/usr/sbin:/sbin), which does
// not include Homebrew's /usr/local/bin or /opt/homebrew/bin. That breaks
// any kubeconfig using an exec-based credential plugin (aws eks get-token,
// aws-iam-authenticator, gke-gcloud-auth-plugin, ...), since client-go can't
// find those binaries. A marker string guards the parse against any startup
// noise a shell plugin (oh-my-zsh, direnv, ...) might print to stdout.
func fixPathForGUILaunch() {
	if goruntime.GOOS != "darwin" {
		return
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	const marker = "__netsk8_path__"
	out, err := exec.CommandContext(ctx, shell, "-ilc", "echo "+marker+"; echo -n \"$PATH\"").Output() //nolint:gosec // shell is the user's own $SHELL, not attacker input
	if err != nil {
		log.Printf("could not resolve login shell PATH (%v) - exec-credential kubeconfig plugins (aws, gke-gcloud-auth-plugin, ...) may not be found", err)
		return
	}
	idx := bytes.LastIndex(out, []byte(marker))
	if idx == -1 {
		return
	}
	shellPath := strings.TrimSpace(string(out[idx+len(marker):]))
	if shellPath == "" {
		return
	}
	if err := os.Setenv("PATH", shellPath); err != nil {
		log.Printf("could not set PATH: %v", err)
		return
	}
	log.Printf("adopted login shell PATH: %s", shellPath)
}

func printUsage(w io.Writer) {
	_, _ = fmt.Fprint(w, `Usage:
  netsk8-navigator
        Launch the app window (default).

  netsk8-navigator --mcp-stdio [--mcp-allow-write]
        Serve this app's MCP tools over stdin/stdout instead of launching
        the window, for an MCP client to spawn on demand.

  netsk8-navigator mcp install [--allow-write]
        Register --mcp-stdio with locally installed MCP clients
        (Claude Code, Claude Desktop, Cursor).

  netsk8-navigator --version
        Print the version and exit.

  netsk8-navigator --help
        Show this help.
`)
}

// mustInit mirrors backend/main.go's mustInit — duplicated rather than
// imported, since the CLI's version lives in that binary's own (unexported)
// package main and can't be imported from here.
func mustInit() (*kube.Manager, *config.Store) {
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
	return mgr, cfg
}

// wireKubeconfigEditor mirrors backend/main.go's own copy — duplicated
// rather than imported for the same reason mustInit is (see its comment):
// this binary's package main can't import the CLI's unexported package main.
func wireKubeconfigEditor(srv *api.Server, mgr *kube.Manager) {
	if mgr.InCluster() {
		return
	}
	editor, err := kubeconfig.NewEditor()
	if err != nil {
		log.Printf("kubeconfig editing unavailable: %v", err)
		return
	}
	srv.SetKubeconfigEditor(editor)
}

// buildMux mirrors backend/main.go's buildMux. AuthEnabled is left at its
// zero value (false) — this binary has no AUTH_PASSWORD/wrapWithAuth
// equivalent at all, so that's simply accurate here. Returns srv alongside
// the mux so main() can wire desktop-only hooks onto it (the About-menu
// event broadcaster, the native external-URL opener) that backend/main.go
// has no equivalent of. Returns the preferences store too, so startServer
// can persist the loopback port it binds.
func buildMux() (http.Handler, *api.Server, *config.Store) {
	mgr, cfg := mustInit()
	srv := api.NewServer(mgr, cfg, "")
	srv.Version = version
	srv.StartUpdateChecker(version) // see backend/main.go's buildMux — the desktop app never called this, so its update bubble never had anything to show
	wireKubeconfigEditor(srv, mgr)
	mux := http.NewServeMux()
	mux.Handle("/api/", srv.Routes())
	mux.Handle("/mcp", srv.MCPHandler()) // see backend/main.go's buildMux for the rationale
	if h := web.Handler(); h != nil {
		mux.Handle("/", h)
	} else {
		log.Fatal("no embedded frontend build — run `pnpm build` in frontend/ first")
	}
	return mux, srv, cfg
}

// preferredDesktopPort is the loopback port the desktop server tries to bind
// when no port has been persisted yet. Fixed (not OS-assigned) so the window
// loads the UI from a stable origin every launch — see listenStable. Well
// clear of the CLI's :8080 so running both at once doesn't collide.
const preferredDesktopPort = 8078

// startServer serves mux over a real TCP socket on 127.0.0.1. A real socket
// is required (not just Wails' in-process AssetServer.Handler bridge)
// because that bridge is confirmed to break long-lived/streaming responses:
// verified empirically that Server-Sent Events (the live pod list, events
// feed, log tail) hang as "reconnecting" through it, and Wails' own issue
// tracker documents the same gap for WebSocket upgrades (pod exec,
// port-forward) in production builds.
//
// The port is kept stable across launches (see listenStable): the window
// loads http://127.0.0.1:<port>/, and browser localStorage — the selected
// context, per-table sort order, ... — is scoped to that origin, so a port
// that changed every start would silently discard all of it. Returns the
// address to navigate the window to.
func startServer(mux http.Handler, cfg *config.Store) string {
	ln := listenStable(cfg)
	addr := ln.Addr().String()
	// ReadHeaderTimeout guards against slow-header attacks; Read/WriteTimeout are
	// deliberately left unset (0 = no limit) — mirrors backend/main.go, since
	// logs/exec/watch are long-lived SSE and WebSocket streams.
	httpSrv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		if err := httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("local server stopped: %v", err)
		}
	}()
	log.Printf("serving on http://%s", addr)
	return addr
}

// listenStable returns a loopback listener, preferring a port that stays the
// same across launches (the one persisted from last run, else
// preferredDesktopPort) so the window's origin — and its localStorage —
// doesn't change every start. If every preferred port is already taken
// (typically a second instance is running), it falls back to an OS-assigned
// port without persisting it, so the next solo launch still lands on the
// stable one.
func listenStable(cfg *config.Store) net.Listener {
	return listenPreferring(cfg, []int{cfg.DesktopPort(), preferredDesktopPort})
}

// listenPreferring tries each port in order, persisting and returning the
// first it can bind; on none, it binds an OS-assigned port and leaves the
// persisted value untouched. Split from listenStable so tests can pass a
// deterministic port list instead of racing the real preferred port.
func listenPreferring(cfg *config.Store, ports []int) net.Listener {
	for _, port := range ports {
		if port <= 0 || port > 65535 {
			continue
		}
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			continue
		}
		if err := cfg.SetDesktopPort(port); err != nil {
			log.Printf("could not persist desktop port %d: %v", port, err)
		}
		return ln
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatalf("failed to start local server: %v", err)
	}
	return ln
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
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--version", "-version":
			fmt.Println("netsk8-navigator " + version)
			return
		case "--help", "-help", "-h":
			printUsage(os.Stdout)
			return
		case "--mcp-stdio":
			runMCPStdio(os.Args[2:])
			return
		case "mcp":
			mcpinstall.RunCLI(os.Args[2:])
			return
		default:
			// A typo'd flag used to fall straight through to a normal GUI
			// launch — a silent extra instance instead of an error pointing
			// at the mistake.
			fmt.Fprintf(os.Stderr, "netsk8-navigator: unrecognized argument %q\n\n", os.Args[1])
			printUsage(os.Stderr)
			os.Exit(2)
		}
	}

	kube.InstallStderrTap() // see internal/kube/execstderr.go — enriches exec-credential failures surfaced over /mcp and /api
	log.Printf("netsk8-navigator %s", version)
	fixPathForGUILaunch()
	mux, srv, cfg := buildMux()
	addr := startServer(mux, cfg)
	url := fmt.Sprintf("http://%s/", addr)

	app := NewApp(srv)
	appMenu := menu.NewMenu()
	fileMenu := appMenu.AddSubmenu("File")
	// Wails' JS bridge (window.wails/window.runtime, which runtime.EventsEmit
	// would normally rely on) is never present in this window — see
	// bootstrapRedirect's comment below. So instead of an Events*/JS round
	// trip, this is a same-process Go call straight into srv, which fans it
	// out to the frontend over its own SSE connection (appevents.go). The
	// frontend listens via a plain EventSource on /api/app-events, guarded
	// so it's inert in the plain browser build (nothing ever broadcasts on
	// that route there — see App.tsx).
	fileMenu.AddText("About Netsk8 Navigator", nil, func(_ *menu.CallbackData) {
		srv.BroadcastAppEvent("show-about")
	})
	fileMenu.AddSeparator()
	fileMenu.AddText("Open in Browser", nil, func(_ *menu.CallbackData) {
		runtime.BrowserOpenURL(app.ctx, url)
	})
	// Without a native Edit menu, macOS's WKWebView doesn't reliably route
	// Cmd+C/Cmd+X/Cmd+V/Cmd+A into the focused element — the OS looks for
	// an NSMenu item bound to the standard copy:/cut:/paste:/selectAll:
	// selectors before forwarding the key event, so those shortcuts misbehave
	// app-wide (most noticeably when pasting large YAML manifests into the
	// editor). EditMenu() supplies the standard Cut/Copy/Paste/Undo/Redo/
	// Select All items wired to those selectors.
	appMenu.Append(menu.EditMenu())

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
