// netsk8s-navigator backend: a web-based Kubernetes navigator (Lens for the browser).
// Reads the local kubeconfig and serves a REST API the React frontend consumes.
package main

import (
	"log"
	"net/http"
	"os"

	"github.com/robertobado/netsk8-navigator/backend/internal/api"
	"github.com/robertobado/netsk8-navigator/backend/internal/config"
	"github.com/robertobado/netsk8-navigator/backend/internal/kube"
)

func main() {
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
	log.Printf("netsk8s-navigator backend listening on %s", addr)
	if err := http.ListenAndServe(addr, srv.Routes()); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
