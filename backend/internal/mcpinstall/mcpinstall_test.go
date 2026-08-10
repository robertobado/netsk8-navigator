package mcpinstall

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSelfStdioEntry(t *testing.T) {
	e, err := SelfStdioEntry(false)
	if err != nil {
		t.Fatal(err)
	}
	if e.Command == "" {
		t.Error("expected a resolved executable path")
	}
	if len(e.Args) != 1 || e.Args[0] != "--mcp-stdio" {
		t.Errorf("args = %v, want just [--mcp-stdio] when allowWrite is false", e.Args)
	}

	eWrite, err := SelfStdioEntry(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(eWrite.Args) != 2 || eWrite.Args[1] != "--mcp-allow-write" {
		t.Errorf("args = %v, want [--mcp-stdio --mcp-allow-write] when allowWrite is true", eWrite.Args)
	}
}

func TestInstallClaudeCode_SkipsWhenNeitherCLINorConfigFilePresent(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // a PATH with no `claude` binary on it
	t.Setenv("HOME", t.TempDir()) // no ~/.claude.json either
	r := installClaudeCode(Entry{Command: "/exe", Args: []string{"--mcp-stdio"}})
	if r.Client != "Claude Code" {
		t.Errorf("client = %q", r.Client)
	}
	if r.Status != "skipped: not installed on this machine" {
		t.Errorf("status = %q", r.Status)
	}
}

// TestInstallClaudeCode_FallsBackToDirectEditWhenCLIMissing covers the case
// the CLI-not-on-PATH branch exists for: Claude Code was used before (e.g.
// via the VS Code extension, which never puts `claude` on PATH), so
// ~/.claude.json exists, but the CLI itself doesn't. This should merge
// into that file's top-level "mcpServers" — the same shape "-s user"
// itself writes, confirmed against a real registration.
func TestInstallClaudeCode_FallsBackToDirectEditWhenCLIMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	home := t.TempDir()
	t.Setenv("HOME", home)
	claudeJSON := filepath.Join(home, ".claude.json")
	if err := os.WriteFile(claudeJSON, []byte(`{"oauthAccount":{"id":"redacted"},"mcpServers":{"other":{"command":"foo","args":[]}}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	r := installClaudeCode(Entry{Command: "/exe", Args: []string{"--mcp-stdio"}})
	if r.Status != "installed" {
		t.Fatalf("status = %q, want installed", r.Status)
	}

	var root map[string]json.RawMessage
	raw, err := os.ReadFile(claudeJSON) //nolint:gosec // claudeJSON is our own t.TempDir() fixture, not user input
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatal(err)
	}
	if _, ok := root["oauthAccount"]; !ok {
		t.Error("oauthAccount was dropped — must preserve unrelated top-level state")
	}
	var servers map[string]json.RawMessage
	if err := json.Unmarshal(root["mcpServers"], &servers); err != nil {
		t.Fatal(err)
	}
	if _, ok := servers["other"]; !ok {
		t.Error("the pre-existing \"other\" server was dropped")
	}
	if _, ok := servers[serverName]; !ok {
		t.Errorf("servers[%q] missing after install", serverName)
	}
}
