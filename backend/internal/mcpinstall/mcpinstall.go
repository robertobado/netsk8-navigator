// Package mcpinstall registers this app's --mcp-stdio entry point with
// locally-installed MCP clients (Claude Code, Claude Desktop, Cursor) —
// the implementation behind `netsk8-navigator mcp install`. Shared between
// backend/main.go and backend/cmd/desktop/main.go (two separate `main`
// packages, same reason internal/api and internal/kube are shared instead
// of duplicated).
package mcpinstall

import (
	"os"
	"os/exec"
	"runtime"
)

// serverName is the key this app registers itself under in every client's
// config — kept identical everywhere so re-running install is idempotent
// and so a user recognizes the same entry across tools.
const serverName = "netsk8-navigator"

// Entry is the MCP server registration this app writes into a client's
// config — always a stdio command, never an HTTP URL (see the package doc
// and the plan this implements: HTTP self-registration is out of scope,
// since the GUI binary's port is ephemeral by design and the CLI binary can
// already be wired manually).
type Entry struct {
	Command string
	Args    []string
}

// SelfStdioEntry builds the Entry for this running binary: its own resolved
// executable path plus --mcp-stdio (and --mcp-allow-write if allowWrite).
func SelfStdioEntry(allowWrite bool) (Entry, error) {
	exe, err := os.Executable()
	if err != nil {
		return Entry{}, err
	}
	args := []string{"--mcp-stdio"}
	if allowWrite {
		args = append(args, "--mcp-allow-write")
	}
	return Entry{Command: exe, Args: args}, nil
}

// Result is one client's install outcome.
type Result struct {
	Client string
	Status string // "installed", "skipped: <reason>", or "failed: <reason>"
}

// InstallAll tries every known client and returns one Result each,
// regardless of individual failures — a client not installed on this
// machine is a normal, expected outcome, not a fatal error.
func InstallAll(entry Entry) []Result {
	results := []Result{installClaudeCode(entry)}
	if path, ok := claudeDesktopConfigDir(); ok {
		results = append(results, installFlatConfig("Claude Desktop", path, entry))
	} else {
		results = append(results, Result{Client: "Claude Desktop", Status: "skipped: not installed on this machine"})
	}
	if path, ok := cursorConfigDir(); ok {
		results = append(results, installFlatConfig("Cursor", path, entry))
	} else {
		results = append(results, Result{Client: "Cursor", Status: "skipped: not installed on this machine"})
	}
	return results
}

// installClaudeCode shells out to the claude CLI's own `mcp add` rather
// than hand-editing ~/.claude.json: that file carries tens of KB of
// unrelated state (OAuth tokens included), and the CLI already owns
// merge-safety and permission-preservation for its own config — safer than
// us reimplementing that against a format we don't control.
func installClaudeCode(entry Entry) Result {
	if _, err := exec.LookPath("claude"); err != nil {
		return Result{Client: "Claude Code", Status: "skipped: claude CLI not found on PATH"}
	}
	args := append([]string{"mcp", "add", "-s", "user", serverName, "--", entry.Command}, entry.Args...)
	cmd := exec.Command("claude", args...) //nolint:gosec // args are our own constructed entry, not attacker input
	out, err := cmd.CombinedOutput()
	if err != nil {
		return Result{Client: "Claude Code", Status: "failed: " + firstLine(string(out), err)}
	}
	return Result{Client: "Claude Code", Status: "installed"}
}

func firstLine(s string, fallback error) string {
	for i, c := range s {
		if c == '\n' {
			if i == 0 {
				return fallback.Error()
			}
			return s[:i]
		}
	}
	if s == "" {
		return fallback.Error()
	}
	return s
}

func claudeDesktopConfigDir() (string, bool) {
	switch runtime.GOOS {
	case "darwin":
		return configDirIfExists(os.Getenv("HOME"), "Library/Application Support/Claude", "claude_desktop_config.json")
	case "windows":
		return configDirIfExists(os.Getenv("APPDATA"), "Claude", "claude_desktop_config.json")
	default: // best-effort on Linux — no official build, but community packaging follows this path
		return configDirIfExists(os.Getenv("HOME"), ".config/Claude", "claude_desktop_config.json")
	}
}

func cursorConfigDir() (string, bool) {
	home := os.Getenv("HOME")
	if runtime.GOOS == "windows" {
		home = os.Getenv("USERPROFILE")
	}
	return configDirIfExists(home, ".cursor", "mcp.json")
}
