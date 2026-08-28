package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/robertobado/netsk8-navigator/backend/internal/config"
	"github.com/robertobado/netsk8-navigator/backend/internal/kube"
)

// headerInjectingTransport adds one fixed header to every request — used to
// attach the /mcp auth token the way a real MCP client's --header flag
// would, since http.Client has no built-in way to set a default header.
type headerInjectingTransport struct {
	key, value string
	base       http.RoundTripper
}

func (t headerInjectingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set(t.key, t.value)
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(r)
}

// mcpConnect starts an httptest.Server around s.MCPHandler() and returns a
// connected client session (with the correct auth token attached),
// closing both on test cleanup.
func mcpConnect(t *testing.T, s *Server) *mcp.ClientSession {
	t.Helper()
	httpSrv := httptest.NewServer(s.MCPHandler())
	t.Cleanup(httpSrv.Close)

	token, err := s.cfg.MCPToken()
	if err != nil {
		t.Fatalf("MCPToken: %v", err)
	}
	httpClient := &http.Client{Transport: headerInjectingTransport{key: mcpTokenHeader, value: token}}

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.0"}, nil)
	session, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: httpSrv.URL, HTTPClient: httpClient}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// enableMCP flips the server's runtime flags directly — the fast path used
// by most tests here; TestMCPHandler_PreferencesRoundTrip below additionally
// exercises the real PUT /api/preferences -> flag-update wiring end to end.
func enableMCP(s *Server, allowWrite bool) {
	s.mcpFlags.set(true, allowWrite, nil, nil)
}

func TestMCPHandler_DisabledReturns404(t *testing.T) {
	s := newTestServer(t)
	httpSrv := httptest.NewServer(s.MCPHandler())
	defer httpSrv.Close()

	resp, err := httpSrv.Client().Get(httpSrv.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 404 {
		t.Errorf("status = %d, want 404 while MCP is disabled", resp.StatusCode)
	}
}

func TestMCPHandler_TokenRequired(t *testing.T) {
	s := newTestServer(t)
	enableMCP(s, false)
	httpSrv := httptest.NewServer(s.MCPHandler())
	defer httpSrv.Close()

	// No token at all.
	resp, err := httpSrv.Client().Post(httpSrv.URL, "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no token: status = %d, want 401", resp.StatusCode)
	}

	// Wrong token.
	req, _ := http.NewRequest(http.MethodPost, httpSrv.URL, strings.NewReader("{}"))
	req.Header.Set(mcpTokenHeader, "wrong-token")
	resp2, err := httpSrv.Client().Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	_ = resp2.Body.Close()
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Errorf("wrong token: status = %d, want 401", resp2.StatusCode)
	}

	// Correct token round-trips through a real client connect.
	session := mcpConnect(t, s)
	if _, err := session.ListTools(t.Context(), nil); err != nil {
		t.Errorf("ListTools with correct token: %v", err)
	}
}

func TestMCPHandler_PreferencesRoundTrip(t *testing.T) {
	s := newTestServer(t)
	if s.mcpFlags.Enabled() {
		t.Fatal("expected MCP disabled by default")
	}
	rec := doRequest(t, s, "PUT", "/api/preferences", `{"mcp":{"enabled":true,"allowWrite":true}}`)
	if rec.Code != 200 {
		t.Fatalf("PUT preferences status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if !s.mcpFlags.Enabled() || !s.mcpFlags.AllowWrite() {
		t.Error("expected mcpFlags updated from the PUT /api/preferences body")
	}
}

func TestMCPFlags_PersistAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := config.NewStoreAt(path)
	if err := cfg.SetApp(json.RawMessage(`{"mcp":{"enabled":true,"allowWrite":false}}`)); err != nil {
		t.Fatal(err)
	}

	// Simulate a process restart: a fresh Store at the same path, fresh Server.
	restarted := config.NewStoreAt(path)
	s := NewServer(newFakeManager(), restarted, "")
	if !s.mcpFlags.Enabled() {
		t.Error("expected mcpFlags.Enabled() to survive a restart via the persisted preference")
	}
	if s.mcpFlags.AllowWrite() {
		t.Error("allowWrite was never set true, should still be false")
	}
}

func TestMCPFlags_ReadOnlyContextOverridesAllowWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := config.NewStoreAt(path)
	if err := cfg.SetApp(json.RawMessage(`{"mcp":{"enabled":true,"allowWrite":true,"readOnlyContexts":["prod"]}}`)); err != nil {
		t.Fatal(err)
	}
	s := NewServer(newFakeManager(), cfg, "")
	if s.mcpFlags.WriteAllowedFor("prod") {
		t.Error("prod is pinned read-only, WriteAllowedFor should be false regardless of the global allowWrite toggle")
	}
	if !s.mcpFlags.WriteAllowedFor("staging") {
		t.Error("staging isn't pinned read-only, WriteAllowedFor should follow the global allowWrite toggle")
	}
}

func TestMCPHandler_ListToolsAndCallReadTool(t *testing.T) {
	s := newTestServer(t, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "web-0", Namespace: "prod"},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning},
	})
	enableMCP(s, false)
	session := mcpConnect(t, s)
	ctx := t.Context()

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools.Tools) != 18 {
		t.Errorf("got %d tools, want 18 (14 read + 4 write)", len(tools.Tools))
	}
	for _, tool := range tools.Tools {
		if tool.Annotations == nil {
			t.Errorf("tool %q has no annotations", tool.Name)
		}
	}

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "list_pods",
		Arguments: map[string]any{"context": "test", "namespace": "prod"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Fatalf("list_pods returned a tool error: %+v", result.Content)
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content[0] = %T, want *mcp.TextContent", result.Content[0])
	}
	var pods []kube.PodView
	if err := json.Unmarshal([]byte(text.Text), &pods); err != nil {
		t.Fatalf("unmarshal tool output: %v", err)
	}
	if len(pods) != 1 || pods[0].Name != "web-0" {
		t.Errorf("got %+v, want the single seeded pod", pods)
	}
}

func TestMCPHandler_UnknownContextRejectedBySchema(t *testing.T) {
	s := newTestServer(t)
	enableMCP(s, false)
	session := mcpConnect(t, s)

	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      "list_pods",
		Arguments: map[string]any{"context": "totally-not-a-real-context"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected a schema-validation error (IsError=true) for a context name outside the enum")
	}
	text, _ := result.Content[0].(*mcp.TextContent)
	if text == nil || !strings.Contains(text.Text, "totally-not-a-real-context") {
		t.Errorf("error content = %+v, want it to mention the rejected value", result.Content)
	}
}

func TestMCPHandler_WriteToolBlockedUntilAllowWrite(t *testing.T) {
	s := newTestServer(t, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "prod"},
		Spec:       appsv1.DeploymentSpec{Replicas: replicas(2)},
	})
	enableMCP(s, false) // enabled, but writes not yet allowed
	session := mcpConnect(t, s)
	ctx := t.Context()

	scaleArgs := map[string]any{"context": "test", "kind": "deployment", "namespace": "prod", "name": "web", "replicas": 5}

	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "scale_resource", Arguments: scaleArgs})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected scale_resource to be blocked while allowWrite is false")
	}
	text, _ := result.Content[0].(*mcp.TextContent)
	if text == nil || !strings.Contains(strings.ToLower(text.Text), "write") {
		t.Errorf("error message = %+v, want it to mention write access", result.Content)
	}

	// Grant write access and retry — should now actually mutate the cluster.
	enableMCP(s, true)
	result2, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "scale_resource", Arguments: scaleArgs})
	if err != nil {
		t.Fatalf("CallTool (after allowWrite): %v", err)
	}
	if result2.IsError {
		t.Fatalf("scale_resource still blocked after allowWrite=true: %+v", result2.Content)
	}

	rec := doRequest(t, s, "GET", "/api/contexts/test/resources/deployments?namespace=prod", "")
	var out []kube.DeploymentView
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Ready != "0/5" {
		t.Errorf("after scale_resource, got %+v, want replicas=5 reflected", out)
	}
}

// TestMCPHandler_CallApplyManifest exercises apply_manifest's handler
// closure — registerWriteTools' registration alone (covered by every other
// MCP test's ListTools) doesn't run its body, only an actual CallTool does.
func TestMCPHandler_CallApplyManifest(t *testing.T) {
	s := newTestServer(t, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "prod"},
		Spec:       appsv1.DeploymentSpec{Replicas: replicas(2)},
	})
	enableMCP(s, true)
	session := mcpConnect(t, s)
	ctx := t.Context()

	yaml := "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: web\n  namespace: prod\nspec:\n  replicas: 7\n"
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "apply_manifest",
		Arguments: map[string]any{"context": "test", "kind": "deployment", "namespace": "prod", "name": "web", "yaml": yaml},
	})
	if err != nil {
		t.Fatalf("CallTool apply_manifest: %v", err)
	}
	if result.IsError {
		t.Fatalf("apply_manifest returned a tool error: %+v", result.Content)
	}

	rec := doRequest(t, s, "GET", "/api/contexts/test/resources/deployments?namespace=prod", "")
	var out []kube.DeploymentView
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Ready != "0/7" {
		t.Errorf("after apply_manifest, got %+v, want replicas=7 reflected", out)
	}
}

func TestMCPHandler_CallApplyManifest_BlockedWithoutWriteAccess(t *testing.T) {
	s := newTestServer(t, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "prod"},
		Spec:       appsv1.DeploymentSpec{Replicas: replicas(2)},
	})
	enableMCP(s, false)
	session := mcpConnect(t, s)

	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      "apply_manifest",
		Arguments: map[string]any{"context": "test", "kind": "deployment", "namespace": "prod", "name": "web", "yaml": "kind: Deployment"},
	})
	if err != nil {
		t.Fatalf("CallTool apply_manifest: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected apply_manifest to be blocked while allowWrite is false")
	}
}

// TestMCPHandler_CallDeleteResource exercises delete_resource's handler
// closure, uncovered by any other test.
func TestMCPHandler_CallDeleteResource(t *testing.T) {
	s := newTestServer(t, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "prod"},
		Spec:       appsv1.DeploymentSpec{Replicas: replicas(2)},
	})
	enableMCP(s, true)
	session := mcpConnect(t, s)

	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      "delete_resource",
		Arguments: map[string]any{"context": "test", "kind": "deployment", "namespace": "prod", "name": "web"},
	})
	if err != nil {
		t.Fatalf("CallTool delete_resource: %v", err)
	}
	if result.IsError {
		t.Fatalf("delete_resource returned a tool error: %+v", result.Content)
	}

	rec := doRequest(t, s, "GET", "/api/contexts/test/resources/deployments?namespace=prod", "")
	var out []kube.DeploymentView
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Errorf("after delete_resource, got %d deployments, want 0", len(out))
	}
}

func TestMCPHandler_CallDeleteResource_BlockedWithoutWriteAccess(t *testing.T) {
	s := newTestServer(t, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "prod"},
		Spec:       appsv1.DeploymentSpec{Replicas: replicas(2)},
	})
	enableMCP(s, false)
	session := mcpConnect(t, s)

	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      "delete_resource",
		Arguments: map[string]any{"context": "test", "kind": "deployment", "namespace": "prod", "name": "web"},
	})
	if err != nil {
		t.Fatalf("CallTool delete_resource: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected delete_resource to be blocked while allowWrite is false")
	}
}

// TestMCPHandler_CallRestartRollout exercises restart_rollout's handler
// closure, uncovered by any other test.
func TestMCPHandler_CallRestartRollout(t *testing.T) {
	s := newTestServer(t, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "prod"},
		Spec:       appsv1.DeploymentSpec{Replicas: replicas(2)},
	})
	enableMCP(s, true)
	session := mcpConnect(t, s)

	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      "restart_rollout",
		Arguments: map[string]any{"context": "test", "kind": "deployment", "namespace": "prod", "name": "web"},
	})
	if err != nil {
		t.Fatalf("CallTool restart_rollout: %v", err)
	}
	if result.IsError {
		t.Fatalf("restart_rollout returned a tool error: %+v", result.Content)
	}

	rec := doRequest(t, s, "GET", "/api/contexts/test/manifest/deployment/prod/web", "")
	var out map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out["yaml"], "kubectl.kubernetes.io/restartedAt") {
		t.Errorf("expected restartedAt annotation in manifest, got:\n%s", out["yaml"])
	}
}

func TestMCPHandler_CallRestartRollout_BlockedWithoutWriteAccess(t *testing.T) {
	s := newTestServer(t, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "prod"},
		Spec:       appsv1.DeploymentSpec{Replicas: replicas(2)},
	})
	enableMCP(s, false)
	session := mcpConnect(t, s)

	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      "restart_rollout",
		Arguments: map[string]any{"context": "test", "kind": "deployment", "namespace": "prod", "name": "web"},
	})
	if err != nil {
		t.Fatalf("CallTool restart_rollout: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected restart_rollout to be blocked while allowWrite is false")
	}
}

func TestMCPHandler_WriteToolBlockedForReadOnlyContext(t *testing.T) {
	s := newTestServer(t, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "prod"},
		Spec:       appsv1.DeploymentSpec{Replicas: replicas(2)},
	})
	s.mcpFlags.set(true, true, map[string]bool{"test": true}, nil) // globally allowed, but "test" pinned read-only
	session := mcpConnect(t, s)

	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      "scale_resource",
		Arguments: map[string]any{"context": "test", "kind": "deployment", "namespace": "prod", "name": "web", "replicas": 5},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected scale_resource to be blocked for a context pinned read-only")
	}
	text, _ := result.Content[0].(*mcp.TextContent)
	if text == nil || !strings.Contains(text.Text, "read-only") {
		t.Errorf("error message = %+v, want it to mention the context is pinned read-only", result.Content)
	}
}

func TestMCPHandler_GetLogsIsBoundedNotStreaming(t *testing.T) {
	s := newTestServer(t, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "web-0", Namespace: "prod"},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning},
	})
	enableMCP(s, false)
	session := mcpConnect(t, s)

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "get_logs",
		Arguments: map[string]any{"context": "test", "namespace": "prod", "name": "web-0", "tailLines": 50},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Fatalf("get_logs returned a tool error: %+v", result.Content)
	}
	// The fake clientset returns an empty log stream, but the call must
	// complete promptly (Follow:false) rather than hang the way a
	// replayed Follow:true SSE request would.
	if _, ok := result.Content[0].(*mcp.TextContent); !ok {
		t.Fatalf("content[0] = %T, want *mcp.TextContent", result.Content[0])
	}
}

// pendingPod builds a Pod that issues.go's pendingDetail classifies as
// Unschedulable, via the same PodScheduled=False condition a real scheduler
// failure produces.
func pendingPod(name, namespace string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Status: corev1.PodStatus{
			Phase:      corev1.PodPending,
			Conditions: []corev1.PodCondition{{Type: corev1.PodScheduled, Status: corev1.ConditionFalse, Message: "0/3 nodes are available"}},
		},
	}
}

func TestMCPHandler_GetIssuesSummaryAndLimit(t *testing.T) {
	s := newTestServer(t,
		pendingPod("p1", "ns-a"),
		pendingPod("p2", "ns-a"),
		pendingPod("p3", "ns-b"),
	)
	enableMCP(s, false)
	session := mcpConnect(t, s)

	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      "get_issues",
		Arguments: map[string]any{"context": "test", "limit": 1},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Fatalf("get_issues returned a tool error: %+v", result.Content)
	}
	text := result.Content[0].(*mcp.TextContent).Text //nolint:forcetypeassert // asserted shape from toolResult
	var out struct {
		Pending      []map[string]any `json:"pending"`
		PendingTotal int              `json:"pendingTotal"`
		Summary      []map[string]any `json:"summary"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("unmarshal: %v, body=%s", err, text)
	}
	if len(out.Pending) != 1 {
		t.Errorf("got %d pending items, want limit=1 applied", len(out.Pending))
	}
	if out.PendingTotal != 3 {
		t.Errorf("pendingTotal = %d, want 3 (pre-truncation total)", out.PendingTotal)
	}
	if len(out.Summary) != 1 || out.Summary[0]["count"].(float64) != 3 {
		t.Errorf("summary = %+v, want one entry grouping all 3 Unschedulable pods", out.Summary)
	}
}

func TestMCPHandler_ExecFailureHintEnrichesError(t *testing.T) {
	s := newTestServer(t)
	s.mgr.(*fakeManager).withExecInfo("test", "aws", "studio-stage") //nolint:forcetypeassert // test-only fake

	hint := s.execFailureHint("/api/contexts/test/nodes", "Error loading SSO Token: Token has expired\n")
	if hint == "" {
		t.Fatal("expected a hint to be derived from the captured stderr + exec info")
	}
	if !strings.Contains(hint, "studio-stage") || !strings.Contains(hint, "aws sso login") {
		t.Errorf("hint = %q, want it to mention the profile and the aws sso login command", hint)
	}
}

func TestDedupeRepeatedTokens(t *testing.T) {
	long := "123456789.dkr.ecr.us-east-1.amazonaws.com/foo:tag-name-is-long-enough"
	msg := "Back-off pulling image " + long + ": rpc error for " + long
	got := dedupeRepeatedTokens(msg)
	if strings.Count(got, long) != 1 {
		t.Errorf("dedupeRepeatedTokens(%q) = %q, want the repeated long token collapsed to one occurrence", msg, got)
	}
	short := "short message with no long repeated token"
	if dedupeRepeatedTokens(short) != short {
		t.Errorf("dedupeRepeatedTokens should leave a message with no long token unchanged")
	}
}

func TestNewStdioMCPFlags(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := config.NewStoreAt(path)
	if err := cfg.SetApp(json.RawMessage(`{"mcp":{"enabled":false,"allowWrite":false,"readOnlyContexts":["prod"]}}`)); err != nil {
		t.Fatal(err)
	}

	f := NewStdioMCPFlags(cfg, true) // simulates --mcp-allow-write
	if !f.Enabled() {
		t.Error("stdio flags should always report enabled — the process only exists because it was spawned as an MCP server")
	}
	if !f.AllowWrite() {
		t.Error("allowWrite should come from the launch flag (true), not the persisted (false) preference")
	}
	if f.WriteAllowedFor("prod") {
		t.Error("prod is pinned read-only in preferences — should stay read-only even with --mcp-allow-write")
	}
	if !f.WriteAllowedFor("staging") {
		t.Error("staging isn't pinned read-only — should follow --mcp-allow-write")
	}
}

// TestServer_RunStdioServesToolsOverAnyPersistentTransport exercises
// RunStdio's actual body (buildMCPServer().Run(ctx, transport)) via an
// in-memory transport instead of real stdin/stdout pipes — a faithful test
// of the logic RunStdio adds (stdio-specific flags + tool registration
// reuse) without re-testing the SDK's own already-tested StdioTransport.
func TestServer_RunStdioServesToolsOverAnyPersistentTransport(t *testing.T) {
	s := newTestServer(t, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "web-0", Namespace: "prod"},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning},
	})
	s.SetMCPFlags(NewStdioMCPFlags(s.cfg, false))

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	runDone := make(chan error, 1)
	go func() { runDone <- s.buildMCPServer().Run(t.Context(), serverTransport) }()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.0"}, nil)
	session, err := client.Connect(t.Context(), clientTransport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	tools, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools.Tools) != 18 {
		t.Errorf("got %d tools, want 18", len(tools.Tools))
	}

	if err := session.Close(); err != nil {
		t.Fatalf("session.Close: %v", err)
	}
	select {
	case err := <-runDone:
		if err != nil {
			t.Errorf("Run returned %v after the client closed, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run (RunStdio's body) didn't return after the client session closed")
	}
}

func TestMCPTokenEndpoints_Audited(t *testing.T) {
	s := newTestServer(t)

	out := captureLog(t, func() {
		doRequest(t, s, "GET", "/api/mcp/token", "")
	})
	if !strings.Contains(out, "AUDIT action=mcp-token-read") {
		t.Errorf("GET token: expected an audit line, got: %s", out)
	}

	out = captureLog(t, func() {
		doRequest(t, s, "POST", "/api/mcp/token/regenerate", "")
	})
	if !strings.Contains(out, "AUDIT action=mcp-token-regenerate") {
		t.Errorf("regenerate token: expected an audit line, got: %s", out)
	}
}

func TestExecFailureHint_NoStderr(t *testing.T) {
	s := newTestServer(t)
	if hint := s.execFailureHint("/api/contexts/test/nodes", ""); hint != "" {
		t.Errorf("execFailureHint with empty stderr = %q, want empty", hint)
	}
}

func TestExecFailureHint_UnrecognizedPathHasNoContext(t *testing.T) {
	s := newTestServer(t)
	if hint := s.execFailureHint("/api/health", "Token has expired\n"); hint != "" {
		t.Errorf("execFailureHint for a path with no context segment = %q, want empty", hint)
	}
}

func TestExecFailureHint_NoExecInfoForContext(t *testing.T) {
	s := newTestServer(t) // no withExecInfo call — ExecInfoFor returns ok=false
	if hint := s.execFailureHint("/api/contexts/test/nodes", "Token has expired\n"); hint != "" {
		t.Errorf("execFailureHint with no exec info for the context = %q, want empty", hint)
	}
}

func TestExecFailureHint_StderrDoesntMatchAnyKnownHint(t *testing.T) {
	s := newTestServer(t)
	s.mgr.(*fakeManager).withExecInfo("test", "aws", "studio-stage") //nolint:forcetypeassert // test-only fake
	if hint := s.execFailureHint("/api/contexts/test/nodes", "some unrelated plugin failure\n"); hint != "" {
		t.Errorf("execFailureHint for unmatched stderr = %q, want empty", hint)
	}
}

func TestExecFailureHint_MatchedButNoProfileYieldsNoHint(t *testing.T) {
	s := newTestServer(t)
	s.mgr.(*fakeManager).withExecInfo("test", "aws", "") //nolint:forcetypeassert // test-only fake, empty profile
	if hint := s.execFailureHint("/api/contexts/test/nodes", "Token has expired\n"); hint != "" {
		t.Errorf("execFailureHint with an empty profile = %q, want empty (no actionable command to suggest)", hint)
	}
}

func TestContextNameFromPath(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"/api/contexts/prod/nodes", "prod"},
		{"/api/contexts/prod", "prod"},
		{"/api/contexts/prod?foo=bar", "prod"},
		{"/api/health", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := contextNameFromPath(c.path); got != c.want {
			t.Errorf("contextNameFromPath(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

func TestToolResult_ErrorStatus(t *testing.T) {
	_, _, err := toolResult(http.StatusInternalServerError, []byte(`{"error":"boom"}`))
	if err == nil {
		t.Fatal("expected an error for a non-2xx status")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("error = %v, want it to include the response body", err)
	}
}

func TestPathNamespace(t *testing.T) {
	if got := pathNamespace(""); got != "-" {
		t.Errorf("pathNamespace(\"\") = %q, want \"-\"", got)
	}
	if got := pathNamespace("prod"); got != "prod" {
		t.Errorf("pathNamespace(%q) = %q, want it unchanged", "prod", got)
	}
}

// TestMCPHandler_TokenLookupErrorReturns500 points the store's config file at
// a path nested under /dev/null (a file, not a directory), so MkdirAll — and
// so cfg.MCPToken()'s lazy-persist step — fails, exercising MCPHandler's
// "internal error" branch that a broken token store would hit.
func TestMCPHandler_TokenLookupErrorReturns500(t *testing.T) {
	cfg := config.NewStoreAt("/dev/null/nested/config.json")
	s := NewServer(newFakeManager(), cfg, "")
	enableMCP(s, false)
	httpSrv := httptest.NewServer(s.MCPHandler())
	defer httpSrv.Close()

	resp, err := httpSrv.Client().Get(httpSrv.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 when MCPToken() fails", resp.StatusCode)
	}
}

func TestMCPHandler_ReportsAppVersion(t *testing.T) {
	s := newTestServer(t)
	s.Version = "9.9.9"
	enableMCP(s, false)

	httpSrv := httptest.NewServer(s.MCPHandler())
	defer httpSrv.Close()
	token, err := s.cfg.MCPToken()
	if err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.0"}, nil)
	httpClient := &http.Client{Transport: headerInjectingTransport{key: mcpTokenHeader, value: token}}
	session, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: httpSrv.URL, HTTPClient: httpClient}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = session.Close() }()

	info := session.InitializeResult().ServerInfo
	if info.Version != "9.9.9" {
		t.Errorf("serverInfo.version = %q, want the app's own Version (9.9.9), matching the binary and bundle version instead of a separate hardcoded number", info.Version)
	}
}

func TestReadToolAnnotations_IdempotentAlongsideReadOnly(t *testing.T) {
	ann := readOnly()
	if !ann.ReadOnlyHint || !ann.IdempotentHint {
		t.Errorf("readOnly() = %+v, want both ReadOnlyHint and IdempotentHint true — a read is idempotent by definition", ann)
	}
}
