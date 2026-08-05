package api

import (
	"encoding/json"
	"sync"
)

// MCPFlags is the runtime-toggleable state gating the /mcp endpoint. It is
// deliberately separate from Server so it can be constructed before the
// Server exists and later re-derived from a preferences write.
type MCPFlags struct {
	mu         sync.RWMutex
	enabled    bool
	allowWrite bool
}

// Enabled reports whether /mcp should serve requests at all.
func (f *MCPFlags) Enabled() bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.enabled
}

// AllowWrite reports whether mutating tools are permitted to act.
func (f *MCPFlags) AllowWrite() bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.allowWrite
}

func (f *MCPFlags) set(enabled, allowWrite bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.enabled, f.allowWrite = enabled, allowWrite
}

// applyFromAppPrefs re-derives the flags from a raw AppPreferences payload —
// the ONE sub-key ("mcp") the backend ever reads out of that otherwise-opaque
// frontend-owned blob. Any parse failure or absent key fails closed (fully
// off). allowWrite is AND-ed with enabled so a stale allowWrite:true left
// over from before disabling MCP can never silently re-arm writes just by
// re-enabling — the human has to grant it again explicitly.
func (f *MCPFlags) applyFromAppPrefs(raw json.RawMessage) {
	var wrapper struct {
		MCP struct {
			Enabled    bool `json:"enabled"`
			AllowWrite bool `json:"allowWrite"`
		} `json:"mcp"`
	}
	_ = json.Unmarshal(raw, &wrapper) // best-effort; zero value = disabled
	f.set(wrapper.MCP.Enabled, wrapper.MCP.Enabled && wrapper.MCP.AllowWrite)
}
