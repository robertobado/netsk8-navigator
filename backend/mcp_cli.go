package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/robertobado/netsk8-navigator/backend/internal/api"
	"github.com/robertobado/netsk8-navigator/backend/internal/kube"
	"github.com/robertobado/netsk8-navigator/backend/internal/mcpinstall"
)

// runMCPStdio implements --mcp-stdio: serve this app's MCP tools over
// stdin/stdout instead of starting the HTTP server, for MCP clients that
// spawn the binary themselves on demand rather than depending on an
// already-running instance at some port.
func runMCPStdio(args []string) {
	kube.InstallStderrTap()
	allowWrite := hasFlag(args, "--mcp-allow-write")

	mgr, cfg := mustInit()
	srv := api.NewServer(mgr, cfg, "")
	srv.SetMCPFlags(api.NewStdioMCPFlags(cfg, allowWrite))

	if err := srv.RunStdio(context.Background()); err != nil {
		log.Fatalf("mcp stdio: %v", err)
	}
}

// runMCPCLI implements `netsk8-navigator mcp <subcommand>`.
func runMCPCLI(args []string) {
	if len(args) == 0 || args[0] != "install" {
		fmt.Println("usage: netsk8-navigator mcp install [--allow-write]")
		os.Exit(2)
	}
	allowWrite := hasFlag(args[1:], "--allow-write")

	entry, err := mcpinstall.SelfStdioEntry(allowWrite)
	if err != nil {
		log.Fatalf("resolving own executable path: %v", err)
	}
	for _, r := range mcpinstall.InstallAll(entry) {
		fmt.Printf("%-16s %s\n", r.Client, r.Status)
	}
}

func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}
