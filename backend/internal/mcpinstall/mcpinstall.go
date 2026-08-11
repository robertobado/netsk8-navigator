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
	"path/filepath"
	"runtime"
)

// serverName is the key this app registers itself under in every client's
// config — kept identical everywhere so re-running install is idempotent
// and so a user recognizes the same entry across tools.
const serverName = "netsk8-navigator"

const (
	clientClaudeCode            = "Claude Code"
	statusSkippedNotInstalled   = "skipped: not installed on this machine"
	claudeDesktopConfigFileName = "claude_desktop_config.json"
)

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
		results = append(results, Result{Client: "Claude Desktop", Status: statusSkippedNotInstalled})
	}
	if path, ok := cursorConfigDir(); ok {
		results = append(results, installFlatConfig("Cursor", path, entry))
	} else {
		results = append(results, Result{Client: "Cursor", Status: statusSkippedNotInstalled})
	}
	return results
}

// installClaudeCode prefers shelling out to the claude CLI's own `mcp add`
// over hand-editing ~/.claude.json: that file carries tens of KB of
// unrelated state (OAuth tokens included), and the CLI already owns
// merge-safety and permission-preservation for its own config — safer than
// us reimplementing that against a format we don't control. But the CLI
// isn't a hard requirement to use Claude Code (many installs — e.g. the
// VS Code extension — never put `claude` on PATH at all), so when it's
// missing, fall back to editing ~/.claude.json directly: confirmed against
// a real "-s user" registration that Claude Code's "user" scope is simply
// a top-level "mcpServers" object in that file, the exact shape
// installFlatConfig already merges safely for Claude Desktop/Cursor.
func installClaudeCode(entry Entry) Result {
	if _, err := exec.LookPath("claude"); err == nil {
		args := append([]string{"mcp", "add", "-s", "user", serverName, "--", entry.Command}, entry.Args...)
		cmd := exec.Command("claude", args...) //nolint:gosec // args are our own constructed entry, not attacker input
		out, err := cmd.CombinedOutput()
		if err != nil {
			return Result{Client: clientClaudeCode, Status: "failed: " + firstLine(string(out), err)}
		}
		return Result{Client: clientClaudeCode, Status: "installed"}
	}

	path, ok := claudeCodeConfigPath()
	if !ok {
		return Result{Client: clientClaudeCode, Status: statusSkippedNotInstalled}
	}
	return installFlatConfig(clientClaudeCode, path, entry)
}

// claudeCodeConfigPath returns ~/.claude.json if it exists — Claude Code
// creates it on first run, so its presence is the same "is this client
// installed" signal claudeDesktopConfigDir/cursorConfigDir use, just for a
// bare file instead of an app-data subdirectory.
func claudeCodeConfigPath() (string, bool) {
	home := os.Getenv("HOME")
	if runtime.GOOS == "windows" {
		home = os.Getenv("USERPROFILE")
	}
	if home == "" {
		return "", false
	}
	path := filepath.Join(home, ".claude.json")
	if _, err := os.Stat(path); err != nil { //nolint:gosec // path is $HOME/.claude.json, not user input
		return "", false
	}
	return path, true
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
		return configDirIfExists(os.Getenv("HOME"), "Library/Application Support/Claude", claudeDesktopConfigFileName)
	case "windows":
		return configDirIfExists(os.Getenv("APPDATA"), "Claude", claudeDesktopConfigFileName)
	default: // best-effort on Linux — no official build, but community packaging follows this path
		return configDirIfExists(os.Getenv("HOME"), ".config/Claude", claudeDesktopConfigFileName)
	}
}

func cursorConfigDir() (string, bool) {
	home := os.Getenv("HOME")
	if runtime.GOOS == "windows" {
		home = os.Getenv("USERPROFILE")
	}
	return configDirIfExists(home, ".cursor", "mcp.json")
}
