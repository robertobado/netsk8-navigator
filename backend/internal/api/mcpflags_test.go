package api

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/robertobado/netsk8-navigator/backend/internal/config"
)

func TestApplyFromGate_InvariantAndFailClosed(t *testing.T) {
	cases := []struct {
		name       string
		raw        string
		wantEn     bool
		wantWrite  bool
		wantRO     []string
		wantRODisa []string
	}{
		{"empty fails closed", `{}`, false, false, nil, nil},
		{"garbage fails closed", `not json`, false, false, nil, nil},
		{"enabled only", `{"enabled":true}`, true, false, nil, nil},
		{"write without enabled is dropped", `{"enabled":false,"allowWrite":true}`, false, false, nil, nil},
		{"write with enabled", `{"enabled":true,"allowWrite":true}`, true, true, nil, nil},
		{
			"context sets", `{"enabled":true,"readOnlyContexts":["prod"],"readDisabledContexts":["secret"]}`,
			true, false, []string{"prod"}, []string{"secret"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &MCPFlags{}
			f.applyFromGate(json.RawMessage(tc.raw))
			if f.Enabled() != tc.wantEn {
				t.Errorf("Enabled() = %v, want %v", f.Enabled(), tc.wantEn)
			}
			if f.AllowWrite() != tc.wantWrite {
				t.Errorf("AllowWrite() = %v, want %v", f.AllowWrite(), tc.wantWrite)
			}
			for _, c := range tc.wantRO {
				if f.WriteAllowedFor(c) {
					t.Errorf("WriteAllowedFor(%q) should be false (pinned read-only)", c)
				}
			}
			for _, c := range tc.wantRODisa {
				if f.ReadAllowedFor(c) {
					t.Errorf("ReadAllowedFor(%q) should be false (read-disabled)", c)
				}
			}
		})
	}
}

func TestMergeGate_PartialPatchLeavesOtherKeys(t *testing.T) {
	current := json.RawMessage(`{"enabled":true,"allowWrite":true,"readOnlyContexts":["prod"],"readDisabledContexts":[]}`)

	merged, err := mergeGate(current, json.RawMessage(`{"allowWrite":false}`))
	if err != nil {
		t.Fatal(err)
	}
	var g struct {
		Enabled          bool     `json:"enabled"`
		AllowWrite       bool     `json:"allowWrite"`
		ReadOnlyContexts []string `json:"readOnlyContexts"`
	}
	if err := json.Unmarshal(merged, &g); err != nil {
		t.Fatal(err)
	}
	if !g.Enabled {
		t.Error("enabled should have been preserved by the partial patch")
	}
	if g.AllowWrite {
		t.Error("allowWrite should have been overridden to false")
	}
	if len(g.ReadOnlyContexts) != 1 || g.ReadOnlyContexts[0] != "prod" {
		t.Errorf("readOnlyContexts = %v, want [prod] preserved", g.ReadOnlyContexts)
	}
}

func TestMergeGate_ReplacesContextArraysWholesale(t *testing.T) {
	merged, err := mergeGate(
		json.RawMessage(`{"enabled":true,"readOnlyContexts":["a","b"]}`),
		json.RawMessage(`{"readOnlyContexts":["c"]}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	var g struct {
		ReadOnlyContexts []string `json:"readOnlyContexts"`
	}
	_ = json.Unmarshal(merged, &g)
	if len(g.ReadOnlyContexts) != 1 || g.ReadOnlyContexts[0] != "c" {
		t.Errorf("readOnlyContexts = %v, want [c] (arrays replace, not append)", g.ReadOnlyContexts)
	}
}

func TestMergeGate_RejectsInvalidPatch(t *testing.T) {
	if _, err := mergeGate(json.RawMessage(`{}`), json.RawMessage(`{bad`)); err == nil {
		t.Error("mergeGate should surface an invalid patch body")
	}
}

func TestCanonicalGate_AlwaysFullShapeWithEmptySlices(t *testing.T) {
	got := string(canonicalGate(gatePayloadFromRaw(json.RawMessage(`{"enabled":true}`))))
	want := `{"enabled":true,"allowWrite":false,"readOnlyContexts":[],"readDisabledContexts":[]}`
	if got != want {
		t.Errorf("canonicalGate() = %s, want %s", got, want)
	}
}

func TestResolveMCPGate_MigratesFromLegacyAppBlobOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := config.NewStoreAt(path)
	// Pre-refactor installs kept the gate inside the opaque App blob.
	if err := cfg.SetApp(json.RawMessage(`{"theme":"dark","mcp":{"enabled":true,"allowWrite":true,"readOnlyContexts":["prod"]}}`)); err != nil {
		t.Fatal(err)
	}

	gate := resolveMCPGate(cfg)
	var g struct {
		Enabled          bool     `json:"enabled"`
		AllowWrite       bool     `json:"allowWrite"`
		ReadOnlyContexts []string `json:"readOnlyContexts"`
	}
	if err := json.Unmarshal(gate, &g); err != nil {
		t.Fatal(err)
	}
	if !g.Enabled || !g.AllowWrite || len(g.ReadOnlyContexts) != 1 {
		t.Fatalf("migrated gate = %s, want enabled+allowWrite+[prod]", gate)
	}

	// Migration persisted to the dedicated key and is idempotent.
	if got := cfg.MCPGate(); string(got) != string(gate) {
		t.Errorf("MCPGate() not persisted after migration: %s", got)
	}
	// A later App write with a stale mcp sub-object must not un-migrate it.
	if err := cfg.SetApp(json.RawMessage(`{"mcp":{"enabled":false}}`)); err != nil {
		t.Fatal(err)
	}
	if got := resolveMCPGate(cfg); string(got) != string(gate) {
		t.Errorf("resolveMCPGate() after stale App write = %s, want the migrated value", got)
	}
}

func TestResolveMCPGate_FreshInstallStaysOff(t *testing.T) {
	cfg := config.NewStoreAt(filepath.Join(t.TempDir(), "config.json"))
	f := &MCPFlags{}
	f.applyFromGate(resolveMCPGate(cfg))
	if f.Enabled() || f.AllowWrite() {
		t.Errorf("fresh install should have MCP fully off, got enabled=%v allowWrite=%v", f.Enabled(), f.AllowWrite())
	}
}

func TestHandlePutMCPGate_DisablingForcesAllowWriteOff(t *testing.T) {
	s := newTestServer(t)
	if rec := doRequest(t, s, "PUT", "/api/mcp/gate", `{"enabled":true,"allowWrite":true}`); rec.Code != 200 {
		t.Fatalf("seed: %d", rec.Code)
	}
	rec := doRequest(t, s, "PUT", "/api/mcp/gate", `{"enabled":false}`)
	if rec.Code != 200 {
		t.Fatalf("disable: %d", rec.Code)
	}
	if s.mcpFlags.AllowWrite() {
		t.Error("disabling MCP must also drop allowWrite so re-enabling never silently re-arms writes")
	}
	// And the persisted value reflects it.
	var g struct {
		AllowWrite bool `json:"allowWrite"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &g)
	if g.AllowWrite {
		t.Errorf("persisted allowWrite = true after disable, body=%s", rec.Body.String())
	}
}

func TestHandleGetMCPGate_ReturnsCanonicalShape(t *testing.T) {
	s := newTestServer(t)
	rec := doRequest(t, s, "GET", "/api/mcp/gate", "")
	if rec.Code != 200 {
		t.Fatalf("GET /api/mcp/gate = %d", rec.Code)
	}
	want := `{"enabled":false,"allowWrite":false,"readOnlyContexts":[],"readDisabledContexts":[]}`
	if got := rec.Body.String(); got != want {
		t.Errorf("GET /api/mcp/gate = %s, want %s", got, want)
	}
}
