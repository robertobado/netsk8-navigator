package api

import (
	"encoding/json"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestShapeItemList_NoLimitOrSinceLeavesBodyUntouched(t *testing.T) {
	body := []byte(`[{"name":"a"},{"name":"b"}]`)
	if got := shapeItemList(body, 0, "", ""); string(got) != string(body) {
		t.Errorf("got %s, want the body unchanged when neither limit nor since is set", got)
	}
}

func TestShapeItemList_NotABareArrayLeftAlone(t *testing.T) {
	body := []byte(`{"items":[{"name":"a"}]}`)
	if got := shapeItemList(body, 1, "", ""); string(got) != string(body) {
		t.Errorf("got %s, want a non-bare-array body left alone even with limit set", got)
	}
}

func TestShapeItemList_AppliesLimitAndWrapsWithTotal(t *testing.T) {
	body := []byte(`[{"name":"a"},{"name":"b"},{"name":"c"}]`)
	got := shapeItemList(body, 2, "", "")
	var out struct {
		Items    []map[string]any `json:"items"`
		Total    int              `json:"total"`
		Returned int              `json:"returned"`
	}
	if err := json.Unmarshal(got, &out); err != nil {
		t.Fatalf("unmarshal: %v, body=%s", err, got)
	}
	if out.Total != 3 || out.Returned != 2 || len(out.Items) != 2 {
		t.Errorf("got %+v, want total=3 returned=2", out)
	}
}

func TestShapeItemList_SinceFiltersBySinceField(t *testing.T) {
	body := []byte(`[{"name":"old","age":"2020-01-01T00:00:00Z"},{"name":"new","age":"2030-01-01T00:00:00Z"}]`)
	got := shapeItemList(body, 0, "2025-01-01T00:00:00Z", "age")
	var out struct {
		Items []map[string]any `json:"items"`
		Total int              `json:"total"`
	}
	if err := json.Unmarshal(got, &out); err != nil {
		t.Fatalf("unmarshal: %v, body=%s", err, got)
	}
	if out.Total != 1 || len(out.Items) != 1 || out.Items[0]["name"] != "new" {
		t.Errorf("got %+v, want only the item at/after the cutoff kept", out)
	}
}

func TestFilterSince_InvalidCutoffReturnsItemsUnchanged(t *testing.T) {
	items := []json.RawMessage{json.RawMessage(`{"age":"2020-01-01T00:00:00Z"}`)}
	got := filterSince(items, "age", "not-a-timestamp")
	if len(got) != 1 {
		t.Errorf("got %d items, want the original slice back when since fails to parse", len(got))
	}
}

func TestFilterSince_KeepsItemsMissingOrUnparseableField(t *testing.T) {
	items := []json.RawMessage{
		json.RawMessage(`{"other":"x"}`),                  // missing field
		json.RawMessage(`{"age":"not-a-timestamp"}`),      // unparseable field
		json.RawMessage(`{"age":"2020-01-01T00:00:00Z"}`), // before cutoff — dropped
		json.RawMessage(`{"age":"2030-01-01T00:00:00Z"}`), // at/after cutoff — kept
	}
	got := filterSince(items, "age", "2025-01-01T00:00:00Z")
	if len(got) != 3 {
		t.Errorf("got %d items, want 3 (missing + unparseable + after-cutoff kept, before-cutoff dropped)", len(got))
	}
}

func TestTruncateIssues_LimitAndSince(t *testing.T) {
	newItems := func() []issueItemShape {
		return []issueItemShape{
			{Name: "old", Since: "2020-01-01T00:00:00Z"},
			{Name: "new1", Since: "2030-01-01T00:00:00Z"},
			{Name: "new2", Since: "2030-01-02T00:00:00Z"},
		}
	}

	got := truncateIssues(newItems(), 0, "2025-01-01T00:00:00Z")
	if len(got) != 2 {
		t.Errorf("since only: got %d items, want the 1 before-cutoff item dropped", len(got))
	}

	got2 := truncateIssues(newItems(), 1, "")
	if len(got2) != 1 || got2[0].Name != "old" {
		t.Errorf("limit only: got %+v, want the first item only", got2)
	}
}

func TestTruncateIssues_InvalidSinceLeavesItemsUnchanged(t *testing.T) {
	items := []issueItemShape{{Name: "a", Since: "2020-01-01T00:00:00Z"}}
	if got := truncateIssues(items, 0, "not-a-timestamp"); len(got) != 1 {
		t.Errorf("got %d items, want the original items when since fails to parse", len(got))
	}
}

// registerResourceGetTool's handler closure (get_resource_detail, get_manifest)
// is only exercised by an actual tool call — registration alone (covered by
// every other MCP test's ListTools) doesn't run its body.
func TestMCPHandler_CallGetResourceDetailAndManifest(t *testing.T) {
	s := newTestServer(t, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "web-0", Namespace: "prod"},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning},
	})
	enableMCP(s, false)
	session := mcpConnect(t, s)
	ctx := t.Context()

	detail, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_resource_detail",
		Arguments: map[string]any{"context": "test", "kind": "pod", "namespace": "prod", "name": "web-0"},
	})
	if err != nil {
		t.Fatalf("CallTool get_resource_detail: %v", err)
	}
	if detail.IsError {
		t.Fatalf("get_resource_detail returned a tool error: %+v", detail.Content)
	}

	manifest, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_manifest",
		Arguments: map[string]any{"context": "test", "kind": "pod", "namespace": "prod", "name": "web-0"},
	})
	if err != nil {
		t.Fatalf("CallTool get_manifest: %v", err)
	}
	if manifest.IsError {
		t.Fatalf("get_manifest returned a tool error: %+v", manifest.Content)
	}
	text, ok := manifest.Content[0].(*mcp.TextContent)
	if !ok || text.Text == "" {
		t.Fatalf("get_manifest content = %+v, want non-empty manifest text", manifest.Content)
	}
}
