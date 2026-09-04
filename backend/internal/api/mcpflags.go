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
	mu                   sync.RWMutex
	enabled              bool
	allowWrite           bool
	readOnlyContexts     map[string]bool
	readDisabledContexts map[string]bool
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

// ReadAllowedFor reports whether a read-only tool may act against
// contextName: the global /mcp enabled gate, minus any context explicitly
// disabled for MCP reads (e.g. a cluster the operator doesn't want an agent
// looking at, even read-only). Mirrors WriteAllowedFor's shape.
func (f *MCPFlags) ReadAllowedFor(contextName string) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.enabled && !f.readDisabledContexts[contextName]
}

func (f *MCPFlags) set(enabled, allowWrite bool, readOnlyContexts, readDisabledContexts map[string]bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.enabled, f.allowWrite, f.readOnlyContexts, f.readDisabledContexts = enabled, allowWrite, readOnlyContexts, readDisabledContexts
}

// gatePayload is the wire/persisted shape of the /mcp security gate — the
// body of PUT /api/mcp/gate and the value of config.json's "mcpGate" key.
// Pointer fields so a PATCH-style partial body (e.g. just {"enabled":true})
// leaves the other three gates untouched when merged (see mergeGate).
type gatePayload struct {
	Enabled              *bool     `json:"enabled,omitempty"`
	AllowWrite           *bool     `json:"allowWrite,omitempty"`
	ReadOnlyContexts     *[]string `json:"readOnlyContexts,omitempty"`
	ReadDisabledContexts *[]string `json:"readDisabledContexts,omitempty"`
}

// applyFromGate re-derives the flags from a raw gate payload (the
// unwrapped {enabled, allowWrite, readOnlyContexts, readDisabledContexts}
// object — NOT wrapped in "mcp"). Any parse failure or absent field fails
// closed (fully off). allowWrite is AND-ed with enabled so a stale
// allowWrite:true left over from before disabling MCP can never silently
// re-arm writes just by re-enabling — the human has to grant it again
// explicitly.
func (f *MCPFlags) applyFromGate(raw json.RawMessage) {
	var g gatePayload
	_ = json.Unmarshal(raw, &g) // best-effort; zero value = disabled
	enabled := g.Enabled != nil && *g.Enabled
	allowWrite := enabled && g.AllowWrite != nil && *g.AllowWrite
	f.set(enabled, allowWrite, toSet(derefSlice(g.ReadOnlyContexts)), toSet(derefSlice(g.ReadDisabledContexts)))
}

// parseContextSets pulls readOnlyContexts and readDisabledContexts (each a
// []string of context names) out of a raw gate payload into lookup sets.
// Split out so newStdioMCPFlags can reuse it without also picking up the
// enabled/allowWrite fields, which stdio mode sources from a launch flag
// instead (see newStdioMCPFlags).
func parseContextSets(raw json.RawMessage) (readOnly, readDisabled map[string]bool) {
	var g gatePayload
	_ = json.Unmarshal(raw, &g)
	return toSet(derefSlice(g.ReadOnlyContexts)), toSet(derefSlice(g.ReadDisabledContexts))
}

// mergeGate overlays a partial PATCH body onto the current persisted gate,
// so a caller can send just the one field it's changing. Returns the merged
// payload as canonical JSON (all four keys present), ready to hand to both
// SetMCPGate and applyFromGate. The enabled/allowWrite invariant is left to
// applyFromGate — mergeGate only combines, it doesn't interpret.
func mergeGate(current, patch json.RawMessage) (json.RawMessage, error) {
	var cur, pat gatePayload
	_ = json.Unmarshal(current, &cur)
	if err := json.Unmarshal(patch, &pat); err != nil {
		return nil, err
	}
	if pat.Enabled != nil {
		cur.Enabled = pat.Enabled
	}
	if pat.AllowWrite != nil {
		cur.AllowWrite = pat.AllowWrite
	}
	if pat.ReadOnlyContexts != nil {
		cur.ReadOnlyContexts = pat.ReadOnlyContexts
	}
	if pat.ReadDisabledContexts != nil {
		cur.ReadDisabledContexts = pat.ReadDisabledContexts
	}
	return canonicalGate(cur), nil
}

// canonicalGate marshals a gatePayload with every key present and every
// slice non-nil ([] not null), so the persisted value and the GET
// /api/mcp/gate response are always a full, stable shape. It also applies
// the enabled/allowWrite invariant to the STORED bytes (not just the live
// flags): allowWrite is forced off whenever enabled is off, so a later
// `{"enabled":true}` patch can never silently re-arm writes off a stale
// persisted allowWrite:true. Mirrors applyFromGate's own AND.
func canonicalGate(g gatePayload) json.RawMessage {
	enabled := g.Enabled != nil && *g.Enabled
	out := struct {
		Enabled              bool     `json:"enabled"`
		AllowWrite           bool     `json:"allowWrite"`
		ReadOnlyContexts     []string `json:"readOnlyContexts"`
		ReadDisabledContexts []string `json:"readDisabledContexts"`
	}{
		Enabled:              enabled,
		AllowWrite:           enabled && g.AllowWrite != nil && *g.AllowWrite,
		ReadOnlyContexts:     orEmptySlice(derefSlice(g.ReadOnlyContexts)),
		ReadDisabledContexts: orEmptySlice(derefSlice(g.ReadDisabledContexts)),
	}
	b, _ := json.Marshal(out)
	return b
}

// mcpGateFromAppPrefs extracts the legacy gate location — the "mcp" sub-key
// of the opaque AppPreferences blob — as an unwrapped gate payload, for the
// one-time migration to the dedicated config.json key. Returns "{}" when
// absent or unparseable (fresh install → gate stays fully off).
func mcpGateFromAppPrefs(appPrefs json.RawMessage) json.RawMessage {
	var wrapper struct {
		MCP json.RawMessage `json:"mcp"`
	}
	if err := json.Unmarshal(appPrefs, &wrapper); err != nil || len(wrapper.MCP) == 0 {
		return json.RawMessage("{}")
	}
	return wrapper.MCP
}

func derefSlice(p *[]string) []string {
	if p == nil {
		return nil
	}
	return *p
}

func orEmptySlice(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func toSet(names []string) map[string]bool {
	set := make(map[string]bool, len(names))
	for _, c := range names {
		set[c] = true
	}
	return set
}

// newStdioMCPFlags builds the flags for `--mcp-stdio`: always enabled (the
// process only exists because it was spawned as an MCP server), allowWrite
// from the launch-time --mcp-allow-write flag (there's no running UI to
// toggle it live), but readOnlyContexts/readDisabledContexts still sourced
// from the persisted gate — a context pinned read-only or read-disabled
// stays that way regardless of which transport is talking to it.
func newStdioMCPFlags(gate json.RawMessage, allowWrite bool) *MCPFlags {
	f := &MCPFlags{}
	readOnly, readDisabled := parseContextSets(gate)
	f.set(true, allowWrite, readOnly, readDisabled)
	return f
}

// NewStdioMCPFlags is newStdioMCPFlags exported for --mcp-stdio's entry
// point in package main, which can't reach the unexported constructor. It
// resolves the persisted gate through the same one-time migration the HTTP
// server uses, so a pre-migration install still honors its pinned contexts.
func NewStdioMCPFlags(cfg *config.Store, allowWrite bool) *MCPFlags {
	return newStdioMCPFlags(resolveMCPGate(cfg), allowWrite)
}

// resolveMCPGate returns the persisted gate, running the one-time migration
// from the legacy App-blob location if the dedicated config.json key is
// still empty. Idempotent: once migrated, the second call is a plain read.
func resolveMCPGate(cfg *config.Store) json.RawMessage {
	gate := cfg.MCPGate()
	if len(gate) > 0 && string(gate) != "{}" {
		return gate
	}
	migrated := canonicalGate(gatePayloadFromRaw(mcpGateFromAppPrefs(cfg.App())))
	_ = cfg.SetMCPGate(migrated) // best-effort; a read-only config dir just means we re-migrate next boot
	return migrated
}

func gatePayloadFromRaw(raw json.RawMessage) gatePayload {
	var g gatePayload
	_ = json.Unmarshal(raw, &g)
	return g
}
