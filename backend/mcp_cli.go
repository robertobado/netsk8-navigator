package main

import (
	"context"
	"log"

	"github.com/robertobado/netsk8-navigator/backend/internal/api"
	"github.com/robertobado/netsk8-navigator/backend/internal/kube"
	"github.com/robertobado/netsk8-navigator/backend/internal/mcpinstall"
)

// runMCPStdio implements --mcp-stdio: serve this app's MCP tools over
// stdin/stdout instead of starting the HTTP server, for MCP clients that
// spawn the binary themselves on demand rather than depending on an
// already-running instance at some port. The wiring itself lives in
// api.RunMCPStdio (shared with backend/cmd/desktop/main.go); mustInit and
// version stay here since they live in this binary's own package main.
func runMCPStdio(args []string) {
	kube.InstallStderrTap()
	allowWrite := mcpinstall.HasFlag(args, "--mcp-allow-write")
	mgr, cfg := mustInit()
	if err := api.RunMCPStdio(context.Background(), mgr, cfg, version, allowWrite); err != nil {
		log.Fatalf("mcp stdio: %v", err)
	}
}
