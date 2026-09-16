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
	stdio                bool
	enabled              bool
	allowWrite           bool
	readOnlyContexts     map[string]bool
	readDisabledContexts map[string]bool

	// store and launchAllowWrite back a --mcp-stdio server only (see
	// newStdioMCPFlags); both are nil/false for the HTTP path. When store is
	// set, AllowWrite/WriteAllowedFor/ReadAllowedFor answer from a FRESH
	// on-disk read of the persisted gate (config.Store.ReloadMCPGate) on
	// every call, instead of the snapshot fields above. That snapshot is
	// only ever set once, at construction, and a stdio process holds its
	// own config.Store for its entire lifetime — a human flipping "Allow
	// write" happens in a completely different OS process (the app serving
	// the panel) that writes the same file, so without a live re-read the
	// toggle would never reach an already-running, already-connected stdio
	// client at all, only its NEXT spawn. launchAllowWrite (the
	// --mcp-allow-write launch flag) doesn't need reloading — it can't
	// change without a restart anyway — and remains an independent OR'd-in
	// grant.
	store            *config.Store
	launchAllowWrite bool
}

// Stdio reports whether these flags back a --mcp-stdio server (vs the HTTP
// /mcp endpoint). Write-once at construction, so no lock — it only shapes
// the wording of the "write disabled" error, which mentions the
// --mcp-allow-write launch flag as a stdio-only alternative to the panel.
func (f *MCPFlags) Stdio() bool { return f.stdio }

// GatePath reports the on-disk config.json path this stdio server reads the
// gate from ("" for the HTTP path, where store is nil). Surfaced in the
// "write disabled" error specifically because "I turned the toggle on and
// it's still refused" is otherwise nearly impossible to diagnose remotely —
// the number one cause in practice is the MCP client's process and the app
// serving the panel resolving DIFFERENT config directories (a stdio client
// spawned with a different $HOME/$XDG_CONFIG_HOME/%AppData%, e.g. a
// sandboxed launcher), so the two are durably reading and writing two
// unrelated files no amount of live-reloading can ever reconcile. Comparing
// this path against the app's own "configPath" (see GET /api/health)
// answers that in one look instead of a guessing match.
func (f *MCPFlags) GatePath() string {
	if f.store == nil {
		return ""
	}
	return f.store.Path()
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
	if f.store != nil {
		return f.launchAllowWrite || gateAllowsWrite(f.store.ReloadMCPGate())
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.allowWrite
}

// WriteAllowedFor reports whether a mutating tool may act against
// contextName: the global allow-write gate, minus any context explicitly
// pinned read-only (e.g. production clusters) regardless of that gate.
func (f *MCPFlags) WriteAllowedFor(contextName string) bool {
	if f.store != nil {
		gate := f.store.ReloadMCPGate()
		readOnly, _ := parseContextSets(gate)
		return (f.launchAllowWrite || gateAllowsWrite(gate)) && !readOnly[contextName]
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.allowWrite && !f.readOnlyContexts[contextName]
}

// ReadAllowedFor reports whether a read-only tool may act against
// contextName: the global /mcp enabled gate, minus any context explicitly
// disabled for MCP reads (e.g. a cluster the operator doesn't want an agent
// looking at, even read-only). Mirrors WriteAllowedFor's shape.
func (f *MCPFlags) ReadAllowedFor(contextName string) bool {
	if f.store != nil {
		_, readDisabled := parseContextSets(f.store.ReloadMCPGate())
		return f.Enabled() && !readDisabled[contextName]
	}
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
// process only exists because it was spawned as an MCP server). Every write
// or per-context check reads store live (see MCPFlags.store's own doc) —
// so the "Allow write" toggle and the readOnlyContexts/readDisabledContexts
// pins the human sets in the running app's MCP panel reach this process
// immediately, no restart required. allowWrite is granted by EITHER that
// live toggle OR the launch-time --mcp-allow-write flag — both are
// explicit, deliberate grants.
func newStdioMCPFlags(store *config.Store, allowWrite bool) *MCPFlags {
	return &MCPFlags{stdio: true, enabled: true, store: store, launchAllowWrite: allowWrite}
}

// gateAllowsWrite reports the persisted gate's effective allowWrite,
// applying the same enabled&&allowWrite invariant applyFromGate does — a
// stored allowWrite:true alongside enabled:false (only reachable by
// hand-editing config.json; canonicalGate never writes that pair) counts as
// false.
func gateAllowsWrite(raw json.RawMessage) bool {
	g := gatePayloadFromRaw(raw)
	return g.Enabled != nil && *g.Enabled && g.AllowWrite != nil && *g.AllowWrite
}

// NewStdioMCPFlags is newStdioMCPFlags exported for --mcp-stdio's entry
// point in package main, which can't reach the unexported constructor. It
// runs the persisted gate through the same one-time migration the HTTP
// server uses (so a pre-migration install still honors its pinned
// contexts), purely for that migration's side effect of writing the
// dedicated key to disk — cfg's live reads take over from there.
func NewStdioMCPFlags(cfg *config.Store, allowWrite bool) *MCPFlags {
	resolveMCPGate(cfg)
	return newStdioMCPFlags(cfg, allowWrite)
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
