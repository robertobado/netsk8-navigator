package mcpinstall

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
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

func TestFirstLine(t *testing.T) {
	fallback := errors.New("fallback")
	cases := []struct{ in, want string }{
		{"single line", "single line"},
		{"first\nsecond", "first"},
		{"", "fallback"},
		{"\nsecond", "fallback"}, // a leading newline (i==0) falls back too, not ""
	}
	for _, c := range cases {
		if got := firstLine(c.in, fallback); got != c.want {
			t.Errorf("firstLine(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestClaudeDesktopConfigDir(t *testing.T) {
	t.Run("not installed", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("APPDATA", home)
		if _, ok := claudeDesktopConfigDir(); ok {
			t.Error("want ok=false when no Claude Desktop dir exists")
		}
	})
	t.Run("installed", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("APPDATA", home)
		var dir string
		switch runtime.GOOS {
		case "darwin":
			dir = filepath.Join(home, "Library/Application Support/Claude")
		case "windows":
			dir = filepath.Join(home, "Claude")
		default:
			dir = filepath.Join(home, ".config/Claude")
		}
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
		path, ok := claudeDesktopConfigDir()
		if !ok {
			t.Fatal("want ok=true once the app-support dir exists")
		}
		if filepath.Base(path) != claudeDesktopConfigFileName {
			t.Errorf("path = %q", path)
		}
	})
}

func TestCursorConfigDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if _, ok := cursorConfigDir(); ok {
		t.Error("want ok=false before ~/.cursor exists")
	}
	if err := os.MkdirAll(filepath.Join(home, ".cursor"), 0o750); err != nil {
		t.Fatal(err)
	}
	path, ok := cursorConfigDir()
	if !ok || filepath.Base(path) != "mcp.json" {
		t.Errorf("path=%q ok=%v", path, ok)
	}
}

func TestInstallAll_EverySkippedWhenNothingIsInstalled(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("APPDATA", home)
	t.Setenv("USERPROFILE", home)

	results := InstallAll(Entry{Command: "/exe", Args: []string{"--mcp-stdio"}})
	if len(results) != 3 {
		t.Fatalf("want 3 results (Claude Code, Claude Desktop, Cursor), got %d: %+v", len(results), results)
	}
	for _, r := range results {
		if r.Status != statusSkippedNotInstalled {
			t.Errorf("%s: status = %q, want %q", r.Client, r.Status, statusSkippedNotInstalled)
		}
	}
}

func TestInstallFlatConfig_Failures(t *testing.T) {
	t.Run("path is a directory, not a file (read error)", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.json")
		if err := os.Mkdir(path, 0o750); err != nil {
			t.Fatal(err)
		}
		r := installFlatConfig("Test", path, Entry{Command: "/exe"})
		if r.Status == "installed" {
			t.Fatalf("want a failure status, got %q", r.Status)
		}
	})

	t.Run("existing file is not valid JSON", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.json")
		if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
			t.Fatal(err)
		}
		r := installFlatConfig("Test", path, Entry{Command: "/exe"})
		if r.Status == "installed" {
			t.Fatalf("want a failure status, got %q", r.Status)
		}
	})

	t.Run("existing mcpServers is not an object", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.json")
		if err := os.WriteFile(path, []byte(`{"mcpServers": "not an object"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		r := installFlatConfig("Test", path, Entry{Command: "/exe"})
		if r.Status == "installed" {
			t.Fatalf("want a failure status, got %q", r.Status)
		}
	})
}
