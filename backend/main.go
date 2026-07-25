// netsk8-navigator backend: a web-based Kubernetes navigator (Lens for the browser).
// Reads the local kubeconfig and serves a REST API the React frontend consumes.
package main

import (
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
		// Loopback-only by default: this backend has no auth and can mutate the
		// cluster (apply manifests, exec into pods, read decoded Secret values).
		// Set ADDR to bind elsewhere only if you understand that exposure.
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

	srv := api.NewServer(mgr, cfg)

	mux := http.NewServeMux()
	mux.Handle("/api/", srv.Routes())
	if h := web.Handler(); h != nil {
		mux.Handle("/", h)
		log.Print("serving embedded frontend build")
	} else {
		log.Print("no embedded frontend build — run the Vite dev server separately (pnpm dev)")
	}

	// ReadHeaderTimeout guards against slow-header attacks; Read/WriteTimeout are
	// deliberately left unset (0 = no limit) — logs/exec/watch are long-lived SSE
	// and WebSocket streams that must not be cut off by a fixed deadline.
	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("netsk8-navigator %s backend listening on %s", version, addr)
	if err := httpSrv.ListenAndServe(); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
