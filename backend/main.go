// netsk8-navigator backend: a web-based Kubernetes navigator (Lens for the browser).
// Reads the local kubeconfig and serves a REST API the React frontend consumes.
package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"runtime"
	"time"

	"github.com/robertobado/netsk8-navigator/backend/internal/api"
	"github.com/robertobado/netsk8-navigator/backend/internal/config"
	"github.com/robertobado/netsk8-navigator/backend/internal/kube"
	"github.com/robertobado/netsk8-navigator/backend/internal/mcpinstall"
	"github.com/robertobado/netsk8-navigator/backend/internal/web"
)

// version is stamped at build time via -ldflags "-X main.version=...".
// GoReleaser sets it to the release tag; plain `go build` leaves it "dev".
var version = "dev"

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
			// A typo'd flag (--mcp-stdout, --mcp_stdio, ...) used to fall
			// straight through to a normal server start — a silent extra
			// instance instead of an error pointing at the mistake.
			fmt.Fprintf(os.Stderr, "netsk8-navigator: unrecognized argument %q\n\n", os.Args[1])
			printUsage(os.Stderr)
			os.Exit(2)
		}
	}

	kube.InstallStderrTap() // see internal/kube/execstderr.go — enriches exec-credential failures surfaced over /mcp and /api
	mgr, cfg := mustInit()
	srv := api.NewServer(mgr, cfg, os.Getenv("CORS_ORIGIN"))
	srv.Version = version
	srv.AuthEnabled = os.Getenv("AUTH_PASSWORD") != ""
	if os.Getenv("DEMO_MODE") == "true" {
		srv.DemoMode = true
		log.Print("DEMO_MODE enabled — pod exec and port-forward are disabled")
	}
	srv.StartUpdateChecker(version)
	handler := wrapWithAuth(buildMux(srv))

	addr := os.Getenv("ADDR")
	if addr == "" {
		// Loopback-only by default: this backend has no auth (unless AUTH_PASSWORD
		// is set) and can mutate the cluster (apply manifests, exec into pods, read
		// decoded Secret values). Set ADDR to bind elsewhere only if you understand
		// that exposure.
		addr = "127.0.0.1:8080"
	}
	// ReadHeaderTimeout guards against slow-header attacks; Read/WriteTimeout are
	// deliberately left unset (0 = no limit) — logs/exec/watch are long-lived SSE
	// and WebSocket streams that must not be cut off by a fixed deadline.
	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("netsk8-navigator %s backend listening on %s", version, addr)
	tlsEnabled := os.Getenv("TLS_CERT") != "" && os.Getenv("TLS_KEY") != ""
	if shouldOpenBrowser(version, runtime.GOOS, os.Getenv("DISPLAY"), os.Getenv("WAYLAND_DISPLAY"), os.Getenv("OPEN_BROWSER")) {
		maybeOpenBrowser(addr, browserURL(addr, tlsEnabled))
	}
	if err := serve(httpSrv); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func printUsage(w io.Writer) {
	_, _ = fmt.Fprint(w, `Usage:
  netsk8-navigator
        Start the web server (default). See README > Security model for
        the environment variables that configure it (ADDR, AUTH_PASSWORD, ...).

  netsk8-navigator --mcp-stdio [--mcp-allow-write]
        Serve this app's MCP tools over stdin/stdout instead of starting
        the web server, for an MCP client to spawn on demand.

  netsk8-navigator mcp install [--allow-write]
        Register --mcp-stdio with locally installed MCP clients
        (Claude Code, Claude Desktop, Cursor).

  netsk8-navigator --version
        Print the version and exit.

  netsk8-navigator --help
        Show this help.
`)
}

// mustInit loads the kubeconfig and preferences store, exiting the process on
// failure — main() has nothing useful it can do to recover from either.
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

// buildMux wires the API under /api/ and, when present, the embedded frontend
// build at the root.
func buildMux(srv *api.Server) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/api/", srv.Routes())
	// Sibling of /api/, not routed through srv.Routes()'s CORS/logging
	// middleware: MCP HTTP clients aren't browsers (no CORS involved), and
	// withLogging only logs after a handler returns, which is the wrong
	// shape for a long-lived Streamable HTTP session. Still inherits
	// wrapWithAuth below, same as everything else buildMux returns.
	mux.Handle("/mcp", srv.MCPHandler())
	if h := web.Handler(); h != nil {
		mux.Handle("/", h)
		log.Print("serving embedded frontend build")
	} else {
		log.Print("no embedded frontend build — run the Vite dev server separately (pnpm dev)")
	}
	return mux
}

// wrapWithAuth gates next behind HTTP Basic Auth when AUTH_PASSWORD is set,
// otherwise returns it unchanged.
func wrapWithAuth(next http.Handler) http.Handler {
	pass := os.Getenv("AUTH_PASSWORD")
	if pass == "" {
		log.Print("no AUTH_PASSWORD set — serving with no authentication (see README > Security model)")
		return next
	}
	user := os.Getenv("AUTH_USER")
	if user == "" {
		user = "admin"
	}
	log.Print("HTTP Basic Auth enabled (AUTH_PASSWORD set)")
	return withBasicAuth(user, pass, next)
}

// serve starts httpSrv, over TLS when both TLS_CERT and TLS_KEY are set.
func serve(httpSrv *http.Server) error {
	certFile, keyFile := os.Getenv("TLS_CERT"), os.Getenv("TLS_KEY")
	if certFile == "" && keyFile == "" {
		return httpSrv.ListenAndServe()
	}
	if certFile == "" || keyFile == "" {
		log.Fatal("TLS_CERT and TLS_KEY must both be set to enable TLS")
	}
	return httpSrv.ListenAndServeTLS(certFile, keyFile)
}

// withBasicAuth requires HTTP Basic Auth credentials matching user/password.
// Basic Auth (rather than a bearer token) is the right fit for a browser SPA
// here: once the browser authenticates against the origin, it automatically
// replays the same credentials on every later request to it — including the
// EventSource (SSE) and WebSocket (pod exec) connections, neither of which a
// bearer token could be attached to from browser JS. Credentials are hashed
// before the constant-time comparison so mismatched lengths don't leak via
// timing, and so the compared values are fixed-size regardless of input.
//
// SHA-256 (not bcrypt/scrypt/Argon2/PBKDF2) is deliberate and correct here:
// there is no stored password hash for an attacker to steal and crack
// offline — password comes straight from AUTH_PASSWORD in memory and is
// rehashed fresh on every request purely to get a fixed-length digest for
// ConstantTimeCompare. A slow KDF is the right tool against offline
// brute-forcing of an at-rest hash; used per-request here it would only add
// real latency to every API call for no corresponding security benefit.
func withBasicAuth(user, password string, next http.Handler) http.Handler {
	wantUser := sha256.Sum256([]byte(user))
	wantPass := sha256.Sum256([]byte(password)) // codeql[go/weak-sensitive-data-hashing] -- fixed-length digest for constant-time compare, not at-rest password storage; see doc comment above
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		gotUser := sha256.Sum256([]byte(u))
		gotPass := sha256.Sum256([]byte(p))
		if !ok ||
			subtle.ConstantTimeCompare(gotUser[:], wantUser[:]) != 1 ||
			subtle.ConstantTimeCompare(gotPass[:], wantPass[:]) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="netsk8-navigator"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
