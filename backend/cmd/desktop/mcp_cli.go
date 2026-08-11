package main

import (
	"context"
	"log"

	"github.com/robertobado/netsk8-navigator/backend/internal/api"
	"github.com/robertobado/netsk8-navigator/backend/internal/kube"
	"github.com/robertobado/netsk8-navigator/backend/internal/mcpinstall"
)

// runMCPStdio mirrors backend/mcp_cli.go's runMCPStdio — mustInit/version
// stay duplicated (each lives in this binary's own package main), but the
// actual wiring is shared via api.RunMCPStdio.
func runMCPStdio(args []string) {
	kube.InstallStderrTap()
	allowWrite := mcpinstall.HasFlag(args, "--mcp-allow-write")
	mgr, cfg := mustInit()
	if err := api.RunMCPStdio(context.Background(), mgr, cfg, version, allowWrite); err != nil {
		log.Fatalf("mcp stdio: %v", err)
	}
}
