package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/robertobado/netsk8-navigator/backend/internal/kube"
)

// This file is the cross-boundary contract for the four cluster-mutating
// MCP tools. The bug it exists to prevent: a write path that is "covered" by
// per-unit tests (applyFromGate, newStdioMCPFlags, PUT /api/mcp/gate) yet
// broken at the seam between them. Two real bugs of exactly that shape have
// lived here: (1) the stdio transport freezing allowWrite at spawn and never
// reading the panel toggle the persisted gate carries at all, and (2), after
// (1) was fixed, freezing it at spawn time rather than re-reading it live —
// so the toggle only ever reached a stdio client's NEXT spawn, not an
// already-connected session, even though its read tools work in that same
// session with no restart. Every case here drives the flags through the SAME
// path the real system uses (PUT /api/mcp/gate for HTTP; a stdio session
// connected BEFORE the gate is armed, exactly like a real MCP client's
// process outliving any one toggle flip), never a raw s.mcpFlags.set().

// mutatingTool is one row of the write-tool matrix: the tool name, the
// arguments for its happy path against the web/prod deployment the matrix
// seeds, and a check that the mutation actually landed in the cluster
// (read back through the REST layer, not just "the tool returned ok").
type mutatingTool struct {
	name       string
	args       map[string]any
	verifyDone func(t *testing.T, s *Server)
}

func writeToolMatrix() []mutatingTool {
	base := func(extra map[string]any) map[string]any {
		m := map[string]any{"context": "test", "kind": "deployment", "namespace": "prod", "name": "web"}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	deployments := func(t *testing.T, s *Server) []kube.DeploymentView {
		t.Helper()
		rec := doRequest(t, s, "GET", "/api/contexts/test/resources/deployments?namespace=prod", "")
		var out []kube.DeploymentView
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("read deployments back: %v", err)
		}
		return out
	}
	return []mutatingTool{
		{
			name: "scale_resource",
			args: base(map[string]any{"replicas": 5}),
			verifyDone: func(t *testing.T, s *Server) {
				if d := deployments(t, s); len(d) != 1 || d[0].Ready != "0/5" {
					t.Errorf("after scale_resource, deployments = %+v, want one at replicas=5", d)
				}
			},
		},
		{
			name: "apply_manifest",
			args: base(map[string]any{"yaml": "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: web\n  namespace: prod\nspec:\n  replicas: 7\n"}),
			verifyDone: func(t *testing.T, s *Server) {
				if d := deployments(t, s); len(d) != 1 || d[0].Ready != "0/7" {
					t.Errorf("after apply_manifest, deployments = %+v, want one at replicas=7", d)
				}
			},
		},
		{
			name: "delete_resource",
			args: base(nil),
			verifyDone: func(t *testing.T, s *Server) {
				if d := deployments(t, s); len(d) != 0 {
					t.Errorf("after delete_resource, deployments = %+v, want none", d)
				}
			},
		},
		{
			name: "restart_rollout",
			args: base(nil),
			verifyDone: func(t *testing.T, s *Server) {
				rec := doRequest(t, s, "GET", "/api/contexts/test/manifest/deployment/prod/web", "")
				var out map[string]string
				if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
					t.Fatalf("read manifest back: %v", err)
				}
				if !strings.Contains(out["yaml"], "kubectl.kubernetes.io/restartedAt") {
					t.Errorf("after restart_rollout, manifest lacks the restartedAt annotation:\n%s", out["yaml"])
				}
			},
		},
	}
}

// writeTransport is one way an MCP client reaches these tools. arm() applies
// the gate payload through that transport's real configuration path and
// returns a connected session — so a test that passes here proves the
// wiring, not just the tool closure.
type writeTransport struct {
	name string
	arm  func(t *testing.T, s *Server, gate string) *mcp.ClientSession
}

func writeTransports() []writeTransport {
	return []writeTransport{
		{
			name: "http",
			arm: func(t *testing.T, s *Server, gate string) *mcp.ClientSession {
				putGate(t, s, gate) // handlePutMCPGate updates s.mcpFlags live
				return mcpConnect(t, s)
			},
		},
		{
			name: "stdio",
			arm: func(t *testing.T, s *Server, gate string) *mcp.ClientSession {
				// Build the flags and connect the session BEFORE the gate is
				// armed — a real stdio client's process is already running
				// and already talking to the server by the time a human
				// touches the panel. NO --mcp-allow-write launch flag, so
				// the only way this tool call can see the gate's state is a
				// live re-read on every check (see MCPFlags.store); arming
				// the gate up front (the old shape of this helper) would
				// only prove a freshly built MCPFlags sees it, not an
				// already-connected one.
				s.SetMCPFlags(NewStdioMCPFlags(s.cfg, false))
				session := stdioConnect(t, s)
				putGate(t, s, gate)
				return session
			},
		},
	}
}

func putGate(t *testing.T, s *Server, body string) {
	t.Helper()
	if rec := doRequest(t, s, "PUT", "/api/mcp/gate", body); rec.Code != 200 {
		t.Fatalf("PUT /api/mcp/gate %s = %d, body %s", body, rec.Code, rec.Body.String())
	}
}

// stdioConnect mirrors mcpConnect but over an in-memory pipe driven by the
// same buildMCPServer().Run the real --mcp-stdio entry point uses, so a
// stdio-transport test exercises the actual server body, not a stand-in.
func stdioConnect(t *testing.T, s *Server) *mcp.ClientSession {
	t.Helper()
	clientT, serverT := mcp.NewInMemoryTransports()
	runDone := make(chan error, 1)
	go func() { runDone <- s.buildMCPServer().Run(context.Background(), serverT) }()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.0"}, nil)
	session, err := client.Connect(t.Context(), clientT, nil)
	if err != nil {
		t.Fatalf("stdio connect: %v", err)
	}
	t.Cleanup(func() {
		_ = session.Close()
		select {
		case <-runDone:
		case <-time.After(2 * time.Second):
			t.Error("stdio MCP server didn't stop after the client session closed")
		}
	})
	return session
}

func seededDeploymentServer(t *testing.T) *Server {
	return newTestServer(t, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "prod"},
		Spec:       appsv1.DeploymentSpec{Replicas: replicas(2)},
	})
}

// writeGateCase is one gate state and the outcome every mutating tool must
// produce under it. Pulled out (with the per-case body in checkWriteGateCase)
// so the matrix loop itself stays flat.
type writeGateCase struct {
	name      string
	payload   string
	wantOK    bool
	wantErrIs string // substring the refusal must contain (when wantOK is false)
}

func writeGateCases() []writeGateCase {
	return []writeGateCase{
		{"write disabled", `{"enabled":true,"allowWrite":false}`, false, "write"},
		{"write enabled", `{"enabled":true,"allowWrite":true}`, true, ""},
		{"write enabled but context pinned read-only", `{"enabled":true,"allowWrite":true,"readOnlyContexts":["test"]}`, false, "read-only"},
	}
}

// TestMCPWriteTools_GateMatrix is the core guarantee: for every mutating
// tool, over every transport, each gate state produces the right outcome —
// a real cluster mutation when write is granted, a typed refusal otherwise.
func TestMCPWriteTools_GateMatrix(t *testing.T) {
	for _, tr := range writeTransports() {
		for _, tool := range writeToolMatrix() {
			for _, g := range writeGateCases() {
				t.Run(tr.name+"/"+tool.name+"/"+g.name, func(t *testing.T) {
					checkWriteGateCase(t, tr, tool, g)
				})
			}
		}
	}
}

func checkWriteGateCase(t *testing.T, tr writeTransport, tool mutatingTool, g writeGateCase) {
	t.Helper()
	s := seededDeploymentServer(t)
	session := tr.arm(t, s, g.payload)

	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: tool.name, Arguments: tool.args})
	if err != nil {
		t.Fatalf("CallTool %s: %v", tool.name, err)
	}

	if g.wantOK {
		assertToolMutated(t, s, tr, tool, result)
		return
	}
	assertToolRefused(t, s, tr, tool, g, result)
}

func assertToolMutated(t *testing.T, s *Server, tr writeTransport, tool mutatingTool, result *mcp.CallToolResult) {
	t.Helper()
	if result.IsError {
		t.Fatalf("%s over %s with write granted returned a tool error: %+v", tool.name, tr.name, result.Content)
	}
	tool.verifyDone(t, s)
}

func assertToolRefused(t *testing.T, s *Server, tr writeTransport, tool mutatingTool, g writeGateCase, result *mcp.CallToolResult) {
	t.Helper()
	if !result.IsError {
		t.Fatalf("%s over %s expected a refusal for gate %q, got success", tool.name, tr.name, g.name)
	}
	text, _ := result.Content[0].(*mcp.TextContent)
	if text == nil || !strings.Contains(strings.ToLower(text.Text), g.wantErrIs) {
		t.Errorf("%s refusal message = %+v, want it to contain %q", tool.name, result.Content, g.wantErrIs)
	}
	if tool.name == "delete_resource" {
		return // nothing to compare against a still-present baseline
	}
	rec := doRequest(t, s, "GET", "/api/contexts/test/resources/deployments?namespace=prod", "")
	if !strings.Contains(rec.Body.String(), `"0/2"`) {
		t.Errorf("%s was refused but the deployment changed anyway: %s", tool.name, rec.Body.String())
	}
}

// TestMCPGate_PanelToggleReachesFreshStdioServer is the regression test for
// the first of the two stdio bugs this file guards: the operator flips
// "Allow write" in the running app's MCP panel (the real HTTP handler), and
// a stdio server spawned afterward with no --mcp-allow-write flag must still
// see write granted. Before the fix, NewStdioMCPFlags read only
// readOnlyContexts/readDisabledContexts from the persisted gate and ignored
// allowWrite entirely.
func TestMCPGate_PanelToggleReachesFreshStdioServer(t *testing.T) {
	s := newTestServer(t)

	putGate(t, s, `{"enabled":true,"allowWrite":true}`)
	if fresh := NewStdioMCPFlags(s.cfg, false); !fresh.AllowWrite() {
		t.Fatal("panel's Allow write toggle did not reach a freshly-spawned stdio server")
	}

	// And turning it back off must reach the next spawn too — a stale
	// allowWrite:true can never linger.
	putGate(t, s, `{"allowWrite":false}`)
	if fresh := NewStdioMCPFlags(s.cfg, false); fresh.AllowWrite() {
		t.Fatal("clearing Allow write in the panel did not reach a freshly-spawned stdio server")
	}

	// The launch flag remains an independent grant: a server explicitly
	// installed with --mcp-allow-write writes even when the panel toggle is off.
	if fresh := NewStdioMCPFlags(s.cfg, true); !fresh.AllowWrite() {
		t.Fatal("--mcp-allow-write launch flag should grant write regardless of the panel toggle")
	}
}

// TestMCPGate_PanelToggleReachesAlreadyRunningStdioServer is the regression
// test for the second, subtler stdio bug: even after the first fix, the
// toggle only reached a stdio client's NEXT process spawn — an operator who
// flips "Allow write" mid-session, without restarting their already-running
// MCP client, kept getting refused, even though read tools worked fine in
// that same session (they don't depend on the gate at all — see
// MCPFlags.Enabled). The fix makes every check re-read the gate live off
// disk (see MCPFlags.store) instead of trusting a snapshot frozen at
// construction, exactly like reads never needed a snapshot in the first
// place.
func TestMCPGate_PanelToggleReachesAlreadyRunningStdioServer(t *testing.T) {
	s := newTestServer(t)
	// Flags built, and effectively "in use", BEFORE any gate is set — the
	// shape of an MCP client's long-lived process.
	flags := NewStdioMCPFlags(s.cfg, false)
	s.SetMCPFlags(flags)

	if flags.AllowWrite() {
		t.Fatal("a fresh install with no gate set should start write-blocked")
	}

	putGate(t, s, `{"enabled":true,"allowWrite":true}`)
	if !flags.AllowWrite() {
		t.Fatal("the panel's Allow write toggle did not reach the SAME already-running MCPFlags instance — a stdio client would need a restart it shouldn't need")
	}
	if !flags.WriteAllowedFor("staging") {
		t.Fatal("WriteAllowedFor should also observe the live toggle on the same instance")
	}

	putGate(t, s, `{"allowWrite":false}`)
	if flags.AllowWrite() {
		t.Fatal("clearing Allow write did not reach the same already-running MCPFlags instance")
	}

	// A context pinned read-only afterward must also apply live, without a
	// fresh MCPFlags.
	putGate(t, s, `{"enabled":true,"allowWrite":true,"readOnlyContexts":["prod"]}`)
	if flags.WriteAllowedFor("prod") {
		t.Fatal("pinning a context read-only did not reach the same already-running MCPFlags instance")
	}
	if !flags.WriteAllowedFor("staging") {
		t.Fatal("an unpinned context should still be writable on the same instance")
	}
}

// TestMCPGate_DisabledStillBlocksStdioWrites guards the enabled&&allowWrite
// invariant across the boundary: a hand-edited store with allowWrite:true
// but enabled:false must not grant stdio writes.
func TestMCPGate_DisabledStillBlocksStdioWrites(t *testing.T) {
	s := newTestServer(t)
	if err := s.cfg.SetMCPGate(json.RawMessage(`{"enabled":false,"allowWrite":true,"readOnlyContexts":[],"readDisabledContexts":[]}`)); err != nil {
		t.Fatal(err)
	}
	if NewStdioMCPFlags(s.cfg, false).AllowWrite() {
		t.Error("allowWrite:true with enabled:false must not grant write to a stdio server")
	}
}

// TestWriteBlockedFor_MessagesAreTransportAware checks that each transport's
// refusal is accurate about what fixes it: the stdio message must not claim
// a restart is needed (it isn't, since the toggle is live) but does mention
// the --mcp-allow-write launch flag as an alternative.
func TestWriteBlockedFor_MessagesAreTransportAware(t *testing.T) {
	stdio := newTestServer(t)
	stdio.SetMCPFlags(NewStdioMCPFlags(stdio.cfg, false))
	err := stdio.writeBlockedFor("staging")
	if err == nil {
		t.Fatal("expected stdio write to be blocked")
	}
	if strings.Contains(err.Error(), "new session") || strings.Contains(err.Error(), "needs a restart") || strings.Contains(err.Error(), "requires a restart") {
		t.Errorf("stdio refusal should not claim a restart is needed — the toggle is live now, got: %v", err)
	}
	if !strings.Contains(err.Error(), "no restart needed") && !strings.Contains(err.Error(), "immediately") {
		t.Errorf("stdio refusal should reassure the user no restart is needed, got: %v", err)
	}
	if !strings.Contains(err.Error(), "mcp install --allow-write") {
		t.Errorf("stdio refusal should still mention the launch-flag alternative, got: %v", err)
	}

	httpSrv := newTestServer(t)
	httpSrv.mcpFlags.applyFromGate(json.RawMessage(`{"enabled":true}`))
	err = httpSrv.writeBlockedFor("staging")
	if err == nil {
		t.Fatal("expected http write to be blocked")
	}
	if strings.Contains(err.Error(), "new session") {
		t.Errorf("http refusal should not mention session restarts, got: %v", err)
	}
	if !strings.Contains(err.Error(), "Allow write") {
		t.Errorf("http refusal should point at the panel toggle, got: %v", err)
	}
}
