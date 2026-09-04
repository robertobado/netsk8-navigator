// Package config persists user preferences to a small JSON file under the OS
// config dir (~/.config/netsk8 on Linux, ~/Library/Application Support/netsk8 on
// macOS, %AppData%\netsk8 on Windows). Preference payloads are opaque JSON: the
// frontend owns their shape, so preferences can evolve without backend changes.
package config

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

type fileData struct {
	App      json.RawMessage            `json:"app,omitempty"`
	Clusters map[string]json.RawMessage `json:"clusters,omitempty"`
	// MCPToken gates the /mcp HTTP endpoint (see internal/api/mcp.go). A
	// top-level sibling of App/Clusters, not nested inside App's opaque
	// frontend-owned blob — SetApp replaces that whole blob wholesale on
	// every preferences write, which would otherwise silently drop it.
	MCPToken string `json:"mcpToken,omitempty"`
	// MCPGate is the security gate for the /mcp endpoint and its mutating
	// tools: {enabled, allowWrite, readOnlyContexts, readDisabledContexts}.
	// A top-level sibling for the same reason MCPToken is — but here the
	// motivation is sharper than "don't drop it". It used to live inside the
	// App blob, and the frontend mirrored it there via the same best-effort,
	// unordered, fire-and-forget PUT /api/preferences as every other
	// preference; two quick toggles could land out of order and leave the
	// backend gate desynced from the UI (an "Allow write" that reads ON
	// but acts OFF). Its own key with its own ordered, awaited endpoint
	// (PUT /api/mcp/gate) makes the backend the single source of truth.
	MCPGate json.RawMessage `json:"mcpGate,omitempty"`
	// DesktopPort is the loopback port the desktop app's embedded HTTP
	// server bound on its last run. Persisted so the window reloads on a
	// stable origin every launch: browser localStorage (selected context,
	// per-table sort order, ...) is scoped to the origin, and the origin's
	// port would otherwise change each start and discard all of it. Only
	// the desktop binary reads or writes this; a top-level sibling for the
	// same reason MCPToken is.
	DesktopPort int `json:"desktopPort,omitempty"`
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
	return NewStoreAt(filepath.Join(dir, "netsk8", "config.json")), nil
}

// NewStoreAt builds a Store backed by an explicit path, bypassing the OS
// config-dir resolution NewStore does. Meant for tests that need a hermetic,
// disposable location (e.g. t.TempDir()) instead of touching the real user
// config file — exported so callers outside this package (e.g. internal/api's
// handler tests) can build one too.
func NewStoreAt(path string) *Store {
	s := &Store{path: path, data: fileData{Clusters: map[string]json.RawMessage{}}}
	if raw, err := os.ReadFile(s.path); err == nil {
		_ = json.Unmarshal(raw, &s.data) // tolerate/ignore a corrupt file
		if s.data.Clusters == nil {
			s.data.Clusters = map[string]json.RawMessage{}
		}
	}
	return s
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

// MCPGate returns the persisted /mcp security gate payload ("{}" when
// unset — e.g. a fresh install, or one upgrading from a build that still
// kept the gate inside the App blob; see internal/api's one-time
// migration).
func (s *Store) MCPGate() json.RawMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return orEmpty(s.data.MCPGate)
}

// SetMCPGate replaces the persisted /mcp security gate payload and
// persists. Called only from PUT /api/mcp/gate (and the one-time
// migration), never from a preferences write — that separation is the
// whole point of the dedicated key.
func (s *Store) SetMCPGate(raw json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.MCPGate = raw
	return s.save()
}

// DesktopPort returns the loopback port the desktop app bound on its last
// run, or 0 when none has been persisted yet (first launch).
func (s *Store) DesktopPort() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data.DesktopPort
}

// SetDesktopPort persists the loopback port the desktop app just bound, so
// the next launch reuses it and keeps the web origin (and its localStorage)
// stable. A no-op when the value is unchanged, so a normal launch that
// re-binds the same port doesn't rewrite the file.
func (s *Store) SetDesktopPort(port int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data.DesktopPort == port {
		return nil
	}
	s.data.DesktopPort = port
	return s.save()
}

// MCPToken returns the persisted /mcp auth token, lazily generating and
// persisting a new random one on first use. Not rotated on every call —
// callers wanting a fresh one use RegenerateMCPToken explicitly.
func (s *Store) MCPToken() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data.MCPToken != "" {
		return s.data.MCPToken, nil
	}
	tok, err := randomToken()
	if err != nil {
		return "", err
	}
	s.data.MCPToken = tok
	if err := s.save(); err != nil {
		return "", err
	}
	return s.data.MCPToken, nil
}

// RegenerateMCPToken discards the current /mcp auth token and persists a
// new one — the escape hatch for a leaked token. Normal operation never
// rotates it automatically: doing so on every boot would break any MCP
// client config set up via `mcp install`/a manual copy, which bakes the
// token into a static file.
func (s *Store) RegenerateMCPToken() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tok, err := randomToken()
	if err != nil {
		return "", err
	}
	s.data.MCPToken = tok
	if err := s.save(); err != nil {
		return "", err
	}
	return s.data.MCPToken, nil
}

func randomToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
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
