// Command gateserver is a throwaway HTTP server exposing the real
// api.Server routes with no kube manager wired in — just enough to drive
// /api/mcp/gate, /api/preferences and (with -kubeconfig) /api/kubeconfig/*
// against real handler + config.Store + kubeconfig.Editor code. It exists
// solely for the frontend contract tests (frontend/src/lib/*.contract.test.ts,
// via frontend/testsupport/gateServerGlobalSetup.ts, which builds and spawns
// this binary and reads the URL it prints), so those tests exercise an
// actual HTTP round trip instead of a stubbed fetch. Never built into a
// release binary and not registered anywhere main.go or the desktop build
// touches.
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/robertobado/netsk8-navigator/backend/internal/api"
	"github.com/robertobado/netsk8-navigator/backend/internal/config"
	"github.com/robertobado/netsk8-navigator/backend/internal/kubeconfig"
)

func main() {
	configPath := flag.String("config", "", "path to a config.json file (created on first write if absent)")
	kubeconfigPath := flag.String("kubeconfig", "", "if set, wire a real kubeconfig.Editor over this file (KUBECONFIG) and serve /api/kubeconfig/*")
	flag.Parse()
	if *configPath == "" {
		fmt.Fprintln(os.Stderr, "gateserver: -config is required")
		os.Exit(1)
	}

	cfg := config.NewStoreAt(*configPath)
	// mgr is nil: the contract tests only hit gate/preferences/kubeconfig
	// routes, none of which touch the kube manager.
	srv := api.NewServer(nil, cfg, "")

	if *kubeconfigPath != "" {
		// kubeconfig.NewEditor resolves its target from $KUBECONFIG, same as
		// the real app — point it at the disposable file the test seeded.
		if err := os.Setenv("KUBECONFIG", *kubeconfigPath); err != nil {
			log.Fatal(err)
		}
		ed, err := kubeconfig.NewEditor()
		if err != nil {
			log.Fatalf("wiring kubeconfig editor: %v", err)
		}
		srv.SetKubeconfigEditor(ed)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	// The one line the Node side parses out of stdout to learn our port.
	fmt.Printf("READY http://%s\n", ln.Addr().String())

	httpSrv := &http.Server{Handler: srv.Routes(), ReadHeaderTimeout: 5 * time.Second}
	if err := httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
