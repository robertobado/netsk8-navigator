// netsk8-navigator backend: a web-based Kubernetes navigator (Lens for the browser).
// Reads the local kubeconfig and serves a REST API the React frontend consumes.
package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/robertobado/netsk8-navigator/backend/internal/api"
	"github.com/robertobado/netsk8-navigator/backend/internal/config"
	"github.com/robertobado/netsk8-navigator/backend/internal/kube"
	"github.com/robertobado/netsk8-navigator/backend/internal/web"
)

// version is stamped at build time via -ldflags "-X main.version=...".
// GoReleaser sets it to the release tag; plain `go build` leaves it "dev".
var version = "dev"

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-version") {
		fmt.Println("netsk8-navigator " + version)
		return
	}

	addr := os.Getenv("ADDR")
	if addr == "" {
		// Loopback-only by default: this backend has no auth (unless AUTH_PASSWORD
		// is set) and can mutate the cluster (apply manifests, exec into pods, read
		// decoded Secret values). Set ADDR to bind elsewhere only if you understand
		// that exposure.
		addr = "127.0.0.1:8080"
	}

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

	srv := api.NewServer(mgr, cfg, os.Getenv("CORS_ORIGIN"))

	mux := http.NewServeMux()
	mux.Handle("/api/", srv.Routes())
	if h := web.Handler(); h != nil {
		mux.Handle("/", h)
		log.Print("serving embedded frontend build")
	} else {
		log.Print("no embedded frontend build — run the Vite dev server separately (pnpm dev)")
	}

	var handler http.Handler = mux
	if pass := os.Getenv("AUTH_PASSWORD"); pass != "" {
		user := os.Getenv("AUTH_USER")
		if user == "" {
			user = "admin"
		}
		handler = withBasicAuth(user, pass, handler)
		log.Print("HTTP Basic Auth enabled (AUTH_PASSWORD set)")
	} else {
		log.Print("no AUTH_PASSWORD set — serving with no authentication (see README > Security model)")
	}

	// ReadHeaderTimeout guards against slow-header attacks; Read/WriteTimeout are
	// deliberately left unset (0 = no limit) — logs/exec/watch are long-lived SSE
	// and WebSocket streams that must not be cut off by a fixed deadline.
	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	certFile, keyFile := os.Getenv("TLS_CERT"), os.Getenv("TLS_KEY")
	log.Printf("netsk8-navigator %s backend listening on %s", version, addr)
	if certFile != "" || keyFile != "" {
		if certFile == "" || keyFile == "" {
			log.Fatal("TLS_CERT and TLS_KEY must both be set to enable TLS")
		}
		err = httpSrv.ListenAndServeTLS(certFile, keyFile)
	} else {
		err = httpSrv.ListenAndServe()
	}
	if err != nil {
		log.Fatalf("server error: %v", err)
	}
}

// withBasicAuth requires HTTP Basic Auth credentials matching user/password.
// Basic Auth (rather than a bearer token) is the right fit for a browser SPA
// here: once the browser authenticates against the origin, it automatically
// replays the same credentials on every later request to it — including the
// EventSource (SSE) and WebSocket (pod exec) connections, neither of which a
// bearer token could be attached to from browser JS. Credentials are hashed
// before the constant-time comparison so mismatched lengths don't leak via
// timing, and so the compared values are fixed-size regardless of input.
func withBasicAuth(user, password string, next http.Handler) http.Handler {
	wantUser := sha256.Sum256([]byte(user))
	wantPass := sha256.Sum256([]byte(password))
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
