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
