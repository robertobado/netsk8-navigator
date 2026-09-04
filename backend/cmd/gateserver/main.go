// Command gateserver is a throwaway HTTP server exposing the real
// api.Server routes with no kube manager wired in — just enough to drive
// /api/mcp/gate and /api/preferences against real handler + config.Store
// code. It exists solely for frontend/src/lib/mcpGate.contract.test.ts (via
// frontend/testsupport/gateServerGlobalSetup.ts, which builds and spawns
// this binary and reads the URL it prints), so that test exercises an
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
)

func main() {
	configPath := flag.String("config", "", "path to a config.json file (created on first write if absent)")
	flag.Parse()
	if *configPath == "" {
		fmt.Fprintln(os.Stderr, "gateserver: -config is required")
		os.Exit(1)
	}

	cfg := config.NewStoreAt(*configPath)
	// mgr is nil: only the gate/preferences routes are ever hit by the
	// contract test, and neither touches the kube manager.
	srv := api.NewServer(nil, cfg, "")

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
