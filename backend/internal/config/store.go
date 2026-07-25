// Package config persists user preferences to a small JSON file under the OS
// config dir (~/.config/netsk8 on Linux, ~/Library/Application Support/netsk8 on
// macOS, %AppData%\netsk8 on Windows). Preference payloads are opaque JSON: the
// frontend owns their shape, so preferences can evolve without backend changes.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

type fileData struct {
	App      json.RawMessage            `json:"app,omitempty"`
	Clusters map[string]json.RawMessage `json:"clusters,omitempty"`
}

// Store is a concurrency-safe preferences store backed by a JSON file.
type Store struct {
	mu   sync.RWMutex
	path string
	data fileData
}

// NewStore resolves the config path, loading any existing preferences.
func NewStore() (*Store, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		// No $HOME/$XDG_CONFIG_HOME — e.g. a container running as an arbitrary
		// non-root UID with no HOME set (a common Kubernetes securityContext).
		// Fall back to a temp dir rather than refusing to start: preferences
		// are a convenience, not core functionality, so losing them across a
		// pod restart is an acceptable trade-off for staying up.
		dir = os.TempDir()
	}
	s := &Store{
		path: filepath.Join(dir, "netsk8", "config.json"),
		data: fileData{Clusters: map[string]json.RawMessage{}},
	}
	if raw, err := os.ReadFile(s.path); err == nil {
		_ = json.Unmarshal(raw, &s.data) // tolerate/ignore a corrupt file
		if s.data.Clusters == nil {
			s.data.Clusters = map[string]json.RawMessage{}
		}
	}
	return s, nil
}

// Path is the resolved config file location (useful for diagnostics).
func (s *Store) Path() string { return s.path }

// App returns the app-wide preferences payload ("{}" when unset).
func (s *Store) App() json.RawMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return orEmpty(s.data.App)
}

// SetApp replaces the app-wide preferences payload and persists.
func (s *Store) SetApp(raw json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.App = raw
	return s.save()
}

// Cluster returns a context's preferences payload ("{}" when unset).
func (s *Store) Cluster(ctx string) json.RawMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return orEmpty(s.data.Clusters[ctx])
}

// SetCluster replaces a context's preferences payload and persists.
func (s *Store) SetCluster(ctx string, raw json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Clusters[ctx] = raw
	return s.save()
}

// save writes the store atomically (temp file + rename).
func (s *Store) save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o750); err != nil {
		return err
	}
	out, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func orEmpty(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("{}")
	}
	return raw
}
