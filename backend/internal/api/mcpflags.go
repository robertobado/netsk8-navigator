package api

import (
	"encoding/json"
	"sync"

	"github.com/robertobado/netsk8-navigator/backend/internal/config"
)

// MCPFlags is the runtime-toggleable state gating the /mcp endpoint. It is
// deliberately separate from Server so it can be constructed before the
// Server exists and later re-derived from a preferences write.
type MCPFlags struct {
	mu               sync.RWMutex
	enabled          bool
	allowWrite       bool
	readOnlyContexts map[string]bool
}

// Enabled reports whether /mcp should serve requests at all.
func (f *MCPFlags) Enabled() bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.enabled
}

// AllowWrite reports whether mutating tools are permitted to act at all,
// independent of any specific context. Prefer WriteAllowedFor when a
// specific context is known — it also honors the per-context read-only
// override.
func (f *MCPFlags) AllowWrite() bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.allowWrite
}

// WriteAllowedFor reports whether a mutating tool may act against
// contextName: the global allow-write gate, minus any context explicitly
// pinned read-only (e.g. production clusters) regardless of that gate.
func (f *MCPFlags) WriteAllowedFor(contextName string) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.allowWrite && !f.readOnlyContexts[contextName]
}

func (f *MCPFlags) set(enabled, allowWrite bool, readOnlyContexts map[string]bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.enabled, f.allowWrite, f.readOnlyContexts = enabled, allowWrite, readOnlyContexts
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
	f.set(wrapper.MCP.Enabled, wrapper.MCP.Enabled && wrapper.MCP.AllowWrite, parseReadOnlyContexts(raw))
}

// parseReadOnlyContexts pulls mcp.readOnlyContexts out of a raw AppPreferences
// payload. Split out of applyFromAppPrefs so newStdioMCPFlags can reuse it
// without also picking up the enabled/allowWrite fields, which stdio mode
// sources from a launch flag instead (see newStdioMCPFlags).
func parseReadOnlyContexts(raw json.RawMessage) map[string]bool {
	var wrapper struct {
		MCP struct {
			ReadOnlyContexts []string `json:"readOnlyContexts"`
		} `json:"mcp"`
	}
	_ = json.Unmarshal(raw, &wrapper)
	set := make(map[string]bool, len(wrapper.MCP.ReadOnlyContexts))
	for _, c := range wrapper.MCP.ReadOnlyContexts {
		set[c] = true
	}
	return set
}

// newStdioMCPFlags builds the flags for `--mcp-stdio`: always enabled (the
// process only exists because it was spawned as an MCP server), allowWrite
// from the launch-time --mcp-allow-write flag (there's no running UI to
// toggle it live), but readOnlyContexts still sourced from the persisted
// preferences — a context pinned read-only stays read-only regardless of
// which transport is talking to it.
func newStdioMCPFlags(appPrefs json.RawMessage, allowWrite bool) *MCPFlags {
	f := &MCPFlags{}
	f.set(true, allowWrite, parseReadOnlyContexts(appPrefs))
	return f
}

// NewStdioMCPFlags is newStdioMCPFlags exported for --mcp-stdio's entry
// point in package main, which can't reach the unexported constructor.
func NewStdioMCPFlags(cfg *config.Store, allowWrite bool) *MCPFlags {
	return newStdioMCPFlags(cfg.App(), allowWrite)
}
