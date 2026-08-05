package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/robertobado/netsk8-navigator/backend/internal/config"
	"github.com/robertobado/netsk8-navigator/backend/internal/kube"
)

// mcpConnect starts an httptest.Server around s.MCPHandler() and returns a
// connected client session, closing both on test cleanup.
func mcpConnect(t *testing.T, s *Server) *mcp.ClientSession {
	t.Helper()
	httpSrv := httptest.NewServer(s.MCPHandler())
	t.Cleanup(httpSrv.Close)

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.0"}, nil)
	session, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: httpSrv.URL}, nil)
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
	s.mcpFlags.set(true, allowWrite)
}

func TestMCPHandler_DisabledReturns404(t *testing.T) {
	s := newTestServer(t)
	httpSrv := httptest.NewServer(s.MCPHandler())
	defer httpSrv.Close()

	resp, err := httpSrv.Client().Get(httpSrv.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Errorf("status = %d, want 404 while MCP is disabled", resp.StatusCode)
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
	if len(tools.Tools) != 14 {
		t.Errorf("got %d tools, want 14 (10 read + 4 write)", len(tools.Tools))
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
