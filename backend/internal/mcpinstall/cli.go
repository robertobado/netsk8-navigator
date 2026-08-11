package mcpinstall

import (
	"fmt"
	"log"
	"os"
)

// RunCLI implements `netsk8-navigator mcp <subcommand>` — shared between
// backend/main.go and backend/cmd/desktop/main.go (see the package doc).
func RunCLI(args []string) {
	if len(args) == 0 || args[0] != "install" {
		fmt.Println("usage: netsk8-navigator mcp install [--allow-write]")
		os.Exit(2)
	}
	allowWrite := HasFlag(args[1:], "--allow-write")

	entry, err := SelfStdioEntry(allowWrite)
	if err != nil {
		log.Fatalf("resolving own executable path: %v", err)
	}
	for _, r := range InstallAll(entry) {
		fmt.Printf("%-16s %s\n", r.Client, r.Status)
	}
}

// HasFlag reports whether flag is present anywhere in args.
func HasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}
