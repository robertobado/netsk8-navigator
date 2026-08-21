package mcpinstall

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallFlatConfig_CreatesFreshFileAt0600(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	r := installFlatConfig("Test Client", path, Entry{Command: "/usr/local/bin/netsk8-navigator", Args: []string{"--mcp-stdio"}})
	if r.Status != "installed" {
		t.Fatalf("status = %q, want %q", r.Status, "installed")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %o, want 0600", perm)
	}

	var root map[string]json.RawMessage
	raw, _ := os.ReadFile(path) //nolint:gosec // path is our own t.TempDir() fixture, not user input
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatal(err)
	}
	var servers map[string]struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
	}
	if err := json.Unmarshal(root["mcpServers"], &servers); err != nil {
		t.Fatal(err)
	}
	entry, ok := servers[serverName]
	if !ok || entry.Command != "/usr/local/bin/netsk8-navigator" || len(entry.Args) != 1 || entry.Args[0] != "--mcp-stdio" {
		t.Errorf("servers[%q] = %+v, want the entry we installed", serverName, servers)
	}
}

func TestInstallFlatConfig_MergesWithoutTouchingUnrelatedKeysOrMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	existing := `{
		"someUnrelatedSetting": {"nested": true, "value": 42},
		"mcpServers": {"someOtherServer": {"command": "other", "args": ["--flag"]}}
	}`
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil { //nolint:gosec // deliberately looser than 0600 — this test asserts installFlatConfig preserves it as-is
		t.Fatal(err)
	}

	r := installFlatConfig("Test Client", path, Entry{Command: "/exe", Args: []string{"--mcp-stdio"}})
	if r.Status != "installed" {
		t.Fatalf("status = %q, want %q", r.Status, "installed")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o644 {
		t.Errorf("mode = %o, want the file's original 0644 preserved (not reset to 0600 or the process umask)", perm)
	}

	var root map[string]json.RawMessage
	raw, _ := os.ReadFile(path) //nolint:gosec // path is our own t.TempDir() fixture, not user input
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatal(err)
	}
	if _, ok := root["someUnrelatedSetting"]; !ok {
		t.Error("someUnrelatedSetting was dropped — merge must preserve unrelated top-level keys byte-for-byte")
	}
	var servers map[string]json.RawMessage
	if err := json.Unmarshal(root["mcpServers"], &servers); err != nil {
		t.Fatal(err)
	}
	if _, ok := servers["someOtherServer"]; !ok {
		t.Error("someOtherServer was dropped — merge must preserve other registered MCP servers")
	}
	if _, ok := servers[serverName]; !ok {
		t.Errorf("servers[%q] missing after install", serverName)
	}
}

func TestInstallFlatConfig_RerunIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	entry := Entry{Command: "/exe", Args: []string{"--mcp-stdio"}}
	if r := installFlatConfig("Test Client", path, entry); r.Status != "installed" {
		t.Fatalf("first install: status = %q", r.Status)
	}
	entry.Args = []string{"--mcp-stdio", "--mcp-allow-write"}
	if r := installFlatConfig("Test Client", path, entry); r.Status != "installed" {
		t.Fatalf("second install: status = %q", r.Status)
	}

	var root map[string]json.RawMessage
	raw, _ := os.ReadFile(path) //nolint:gosec // path is our own t.TempDir() fixture, not user input
	_ = json.Unmarshal(raw, &root)
	var servers map[string]struct {
		Args []string `json:"args"`
	}
	_ = json.Unmarshal(root["mcpServers"], &servers)
	if len(servers) != 1 {
		t.Fatalf("got %d servers registered, want exactly 1 (re-running install must update in place, not duplicate)", len(servers))
	}
	if len(servers[serverName].Args) != 2 {
		t.Errorf("args = %v, want the second install's args to have replaced the first", servers[serverName].Args)
	}
}

func TestConfigDirIfExists(t *testing.T) {
	dir := t.TempDir()
	if _, ok := configDirIfExists(dir, "NotInstalled", "config.json"); ok {
		t.Error("expected ok=false when the client's subdirectory doesn't exist")
	}
	if err := os.Mkdir(filepath.Join(dir, "Installed"), 0o750); err != nil {
		t.Fatal(err)
	}
	path, ok := configDirIfExists(dir, "Installed", "config.json")
	if !ok {
		t.Fatal("expected ok=true when the client's subdirectory exists")
	}
	if want := filepath.Join(dir, "Installed", "config.json"); path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
}

func TestConfigDirIfExists_EmptyBase(t *testing.T) {
	if _, ok := configDirIfExists("", "sub", "config.json"); ok {
		t.Error("want ok=false when base is empty")
	}
}

// TestLoadFlatConfig_StatErrorNotNotExist covers loadFlatConfig's os.Stat
// error branch that isn't a plain "doesn't exist" — a path with a regular
// file (not a directory) as one of its parent components reliably produces
// ENOTDIR rather than ENOENT.
func TestLoadFlatConfig_StatErrorNotNotExist(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(blocker, "config.json")

	r := installFlatConfig("Test", path, Entry{Command: "/exe"})
	if r.Status == "installed" {
		t.Fatalf("want a failure status, got %q", r.Status)
	}
}

func TestLoadFlatConfig_EmptyFileTreatedAsEmptyObject(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}

	r := installFlatConfig("Test", path, Entry{Command: "/exe", Args: []string{"--mcp-stdio"}})
	if r.Status != "installed" {
		t.Fatalf("status = %q, want installed for a pre-existing but empty file", r.Status)
	}
}

// TestInstallFlatConfig_WriteFails covers installFlatConfig's own
// writeAtomicPreservingMode-error branch: a parent directory that doesn't
// exist makes the temp-file write fail.
func TestInstallFlatConfig_WriteFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing-parent", "config.json")
	r := installFlatConfig("Test", path, Entry{Command: "/exe"})
	if r.Status == "installed" {
		t.Fatalf("want a failure status when the parent directory doesn't exist, got %q", r.Status)
	}
}

func TestWriteAtomicPreservingMode_RenameFails(t *testing.T) {
	// path is an existing directory — a regular file can never be renamed
	// onto it, so the final os.Rename in writeAtomicPreservingMode fails.
	dir := t.TempDir()
	target := filepath.Join(dir, "adir")
	if err := os.Mkdir(target, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomicPreservingMode(target, []byte("{}"), 0o600); err == nil {
		t.Error("expected an error renaming a file onto an existing directory")
	}
}
