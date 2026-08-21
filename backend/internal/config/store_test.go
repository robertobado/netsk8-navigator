package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// newTestStore builds a Store pointed at a temp file, bypassing NewStore's
// os.UserConfigDir() resolution — same package, so the unexported fields are
// reachable directly.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	return &Store{path: filepath.Join(t.TempDir(), "config.json"), data: fileData{Clusters: map[string]json.RawMessage{}}}
}

func TestStore_AppRoundTrip(t *testing.T) {
	s := newTestStore(t)
	if got := s.App(); string(got) != "{}" {
		t.Errorf("empty App() = %s, want {}", got)
	}
	if err := s.SetApp(json.RawMessage(`{"theme":"dark"}`)); err != nil {
		t.Fatal(err)
	}
	if got := s.App(); string(got) != `{"theme":"dark"}` {
		t.Errorf("App() = %s", got)
	}
}

func TestStore_ClusterRoundTrip(t *testing.T) {
	s := newTestStore(t)
	if err := s.SetCluster("prod", json.RawMessage(`{"ns":"default"}`)); err != nil {
		t.Fatal(err)
	}
	if got := s.Cluster("prod"); string(got) != `{"ns":"default"}` {
		t.Errorf("Cluster(prod) = %s", got)
	}
	if got := s.Cluster("staging"); string(got) != "{}" {
		t.Errorf("Cluster(staging) (never set) = %s, want {}", got)
	}
}

func TestStore_MCPTokenLazyAndStable(t *testing.T) {
	s := newTestStore(t)
	tok1, err := s.MCPToken()
	if err != nil {
		t.Fatal(err)
	}
	if tok1 == "" {
		t.Fatal("expected a non-empty generated token")
	}
	tok2, err := s.MCPToken()
	if err != nil {
		t.Fatal(err)
	}
	if tok2 != tok1 {
		t.Errorf("MCPToken() changed across calls (%q -> %q), want it stable — not rotated automatically", tok1, tok2)
	}

	// Persists across a fresh Store at the same path (simulating a restart).
	restarted := NewStoreAt(s.path)
	tok3, err := restarted.MCPToken()
	if err != nil {
		t.Fatal(err)
	}
	if tok3 != tok1 {
		t.Errorf("token did not survive a restart: got %q, want %q", tok3, tok1)
	}
}

func TestStore_RegenerateMCPTokenRotates(t *testing.T) {
	s := newTestStore(t)
	tok1, err := s.MCPToken()
	if err != nil {
		t.Fatal(err)
	}
	tok2, err := s.RegenerateMCPToken()
	if err != nil {
		t.Fatal(err)
	}
	if tok2 == tok1 {
		t.Error("RegenerateMCPToken should produce a different token")
	}
	if got, _ := s.MCPToken(); got != tok2 {
		t.Errorf("MCPToken() after regenerate = %q, want the newly regenerated %q", got, tok2)
	}
}

func TestStore_PersistsToDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.json")
	s := &Store{path: path, data: fileData{Clusters: map[string]json.RawMessage{}}}
	if err := s.SetApp(json.RawMessage(`{"lang":"pt-BR"}`)); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(path) //nolint:gosec // path is t.TempDir()-derived, test-controlled
	if err != nil {
		t.Fatalf("expected the file to exist after SetApp: %v", err)
	}
	var fd fileData
	if err := json.Unmarshal(raw, &fd); err != nil {
		t.Fatal(err)
	}
	// save() pretty-prints the whole file (json.MarshalIndent reformats the
	// embedded json.RawMessage too), so compare parsed values, not raw bytes.
	var got map[string]string
	if err := json.Unmarshal(fd.App, &got); err != nil {
		t.Fatalf("persisted App isn't valid JSON: %s", fd.App)
	}
	if got["lang"] != "pt-BR" {
		t.Errorf("persisted App = %s", fd.App)
	}
}

func TestNewStore_ResolvesPathAndToleratesCorruptFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config")) // deterministic on Linux too

	wantDir, err := os.UserConfigDir()
	if err != nil {
		t.Skipf("no resolvable config dir in this environment: %v", err)
	}
	full := filepath.Join(wantDir, "netsk8")
	if err := os.MkdirAll(full, 0o750); err != nil {
		t.Fatal(err)
	}
	// A corrupt pre-existing file should be tolerated, not fatal.
	if err := os.WriteFile(filepath.Join(full, "config.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	s, err := NewStore()
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	if want := filepath.Join(full, "config.json"); s.Path() != want {
		t.Errorf("Path() = %q, want %q", s.Path(), want)
	}
	if got := s.App(); string(got) != "{}" {
		t.Errorf("App() after loading a corrupt file = %s, want {}", got)
	}
}

// A container running as an arbitrary non-root UID commonly has no $HOME (and
// no $XDG_CONFIG_HOME) — NewStore must still start up, falling back to a temp
// dir, rather than failing the whole server over a preferences nicety.
func TestNewStore_FallsBackWithoutHOME(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")

	if _, err := os.UserConfigDir(); err == nil {
		t.Skip("this environment still resolves a config dir without HOME/XDG_CONFIG_HOME")
	}

	s, err := NewStore()
	if err != nil {
		t.Fatalf("NewStore() error = %v, want a graceful fallback", err)
	}
	if want := filepath.Join(os.TempDir(), "netsk8", "config.json"); s.Path() != want {
		t.Errorf("Path() = %q, want %q", s.Path(), want)
	}
	if got := s.App(); string(got) != "{}" {
		t.Errorf("App() = %s, want {}", got)
	}
}

// randomToken's `if _, err := rand.Read(b); err != nil` branch (and the two
// MCPToken/RegenerateMCPToken branches around it) can't be exercised: as of
// Go 1.24, crypto/rand.Read no longer returns a normal error on a failed
// system-entropy read — it calls runtime.fatal and crashes the process (see
// https://go.dev/issue/66821), even with rand.Reader swapped out. Confirmed
// by trying exactly that override; it took the whole test binary down
// instead of returning an error. Left uncovered as genuinely unreachable in
// the current Go toolchain, not skipped for convenience.

// TestStore_MCPToken_SaveError and TestStore_RegenerateMCPToken_SaveError
// cover the save()-error branch in both token methods: a parent directory
// that can't be created (a file sits where a directory component needs to
// be) makes save's os.MkdirAll fail.
func TestStore_MCPToken_SaveError(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := &Store{path: filepath.Join(blocker, "sub", "config.json"), data: fileData{Clusters: map[string]json.RawMessage{}}}
	if _, err := s.MCPToken(); err == nil {
		t.Error("expected an error when save()'s MkdirAll fails")
	}
}

func TestStore_RegenerateMCPToken_SaveError(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := &Store{path: filepath.Join(blocker, "sub", "config.json"), data: fileData{Clusters: map[string]json.RawMessage{}}}
	if _, err := s.RegenerateMCPToken(); err == nil {
		t.Error("expected an error when save()'s MkdirAll fails")
	}
}

// TestStore_Save_MkdirAllError is the same MkdirAll failure exercised
// directly through SetApp, for save()'s own branch coverage independent of
// the MCPToken callers above.
func TestStore_Save_MkdirAllError(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := &Store{path: filepath.Join(blocker, "sub", "config.json"), data: fileData{Clusters: map[string]json.RawMessage{}}}
	if err := s.SetApp(json.RawMessage(`{}`)); err == nil {
		t.Error("expected an error when the config dir's parent can't be created")
	}
}

// TestStore_Save_MarshalError covers save()'s json.MarshalIndent error
// branch: an App payload that isn't actually valid JSON (SetApp doesn't
// validate — callers are expected to pass json.Valid bodies, but save()
// must still handle it) makes json.RawMessage's MarshalJSON fail.
func TestStore_Save_MarshalError(t *testing.T) {
	s := newTestStore(t)
	if err := s.SetApp(json.RawMessage("not valid json")); err == nil {
		t.Error("expected save() to fail marshaling an invalid App payload")
	}
}

// TestStore_Save_WriteFileError covers save()'s os.WriteFile error branch:
// a directory already occupying the ".tmp" path makes the write fail.
func TestStore_Save_WriteFileError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.Mkdir(path+".tmp", 0o750); err != nil {
		t.Fatal(err)
	}
	s := &Store{path: path, data: fileData{Clusters: map[string]json.RawMessage{}}}
	if err := s.SetApp(json.RawMessage(`{}`)); err == nil {
		t.Error("expected an error when the .tmp path is already a directory")
	}
}

// TestNewStoreAt_NullClustersInFileResetToEmptyMap covers the nil-Clusters
// recovery branch: an explicit "clusters": null in a loaded file overwrites
// NewStoreAt's pre-seeded empty map with nil, which must be re-initialized
// rather than left nil (a nil map panics on the write side of Cluster/SetCluster).
func TestNewStoreAt_NullClustersInFileResetToEmptyMap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"clusters": null}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s := NewStoreAt(path)
	if got := s.Cluster("prod"); string(got) != "{}" {
		t.Errorf("Cluster(prod) = %s, want {}", got)
	}
	if err := s.SetCluster("prod", json.RawMessage(`{"ns":"default"}`)); err != nil {
		t.Fatalf("SetCluster panicked/errored on a nil Clusters map: %v", err)
	}
}
