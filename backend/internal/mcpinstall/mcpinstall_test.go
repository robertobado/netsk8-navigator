package mcpinstall

import "testing"

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

func TestInstallClaudeCode_SkipsWhenCLINotFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // a PATH with no `claude` binary on it
	r := installClaudeCode(Entry{Command: "/exe", Args: []string{"--mcp-stdio"}})
	if r.Client != "Claude Code" {
		t.Errorf("client = %q", r.Client)
	}
	if r.Status != "skipped: claude CLI not found on PATH" {
		t.Errorf("status = %q", r.Status)
	}
}
