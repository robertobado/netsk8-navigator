package api

import (
	"encoding/json"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

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

func TestWithListQuery(t *testing.T) {
	cases := []struct {
		name                    string
		namespace, label, field string
		want                    string
	}{
		{"none", "", "", "", "/pods"},
		{"namespace only", "prod", "", "", "/pods?namespace=prod"},
		{"label + field", "", "app=web", "status.phase=Running", "/pods?fieldSelector=status.phase%3DRunning&labelSelector=app%3Dweb"},
		{"all three", "prod", "app=web", "spec.nodeName=n1", "/pods?fieldSelector=spec.nodeName%3Dn1&labelSelector=app%3Dweb&namespace=prod"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := withListQuery("/pods", tc.namespace, tc.label, tc.field); got != tc.want {
				t.Errorf("withListQuery = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCompactPods_TrimsToEssentialFields(t *testing.T) {
	body := []byte(`[{"name":"web-0","namespace":"prod","status":"Running","ready":1,"total":1,"restarts":0,"node":"n1","age":"2024-01-01T00:00:00Z","reason":"","ip":"10.0.0.1","containers":["app","sidecar"],"ownerKind":"ReplicaSet","ownerName":"web-abc","finalizers":["x"]}]`)
	got := compactPods(body)

	var out []map[string]any
	if err := json.Unmarshal(got, &out); err != nil {
		t.Fatalf("unmarshal: %v, body=%s", err, got)
	}
	if len(out) != 1 {
		t.Fatalf("got %d pods, want 1", len(out))
	}
	for _, dropped := range []string{"ip", "containers", "ownerKind", "ownerName", "finalizers"} {
		if _, present := out[0][dropped]; present {
			t.Errorf("compactPods kept %q, want it dropped", dropped)
		}
	}
	for _, kept := range compactPodFields {
		if _, present := out[0][kept]; !present {
			t.Errorf("compactPods dropped %q, want it kept", kept)
		}
	}
}

func TestCompactPods_LeavesNonArrayBodyAlone(t *testing.T) {
	body := []byte(`{"error":"boom"}`)
	if got := compactPods(body); string(got) != string(body) {
		t.Errorf("got %s, want an error object passed through untouched", got)
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

// registerSimpleGetTool's handler closure (list_namespaces, list_nodes,
// get_overview) and registerReadTools' list_contexts/list_resources closures
// are only exercised by an actual tool call — registration alone (covered by
// every other MCP test's ListTools) doesn't run their bodies.
func TestMCPHandler_ReadToolBlockedForDisabledContext(t *testing.T) {
	s := newTestServer(t, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "web-0", Namespace: "prod"},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning},
	})
	s.mcpFlags.set(true, false, nil, map[string]bool{"test": true}) // enabled, but "test" disabled for reads
	session := mcpConnect(t, s)

	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      "list_pods",
		Arguments: map[string]any{"context": "test"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected list_pods to be blocked for a context disabled for MCP reads")
	}
	text, _ := result.Content[0].(*mcp.TextContent)
	if text == nil || !strings.Contains(text.Text, "read operations are disabled") {
		t.Errorf("error message = %+v, want it to mention reads are disabled", result.Content)
	}
}

func TestMCPHandler_ReadToolAllowedForOtherContexts(t *testing.T) {
	s := newTestServer(t, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "web-0", Namespace: "prod"},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning},
	})
	// Some other context is read-disabled — "test" itself must be unaffected.
	s.mcpFlags.set(true, false, nil, map[string]bool{"some-other-context": true})
	session := mcpConnect(t, s)

	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      "list_pods",
		Arguments: map[string]any{"context": "test"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Fatalf("list_pods returned a tool error: %+v", result.Content)
	}
}

func TestMCPHandler_CallListPods_Compact(t *testing.T) {
	s := newTestServer(t, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "web-0", Namespace: "prod"},
		Spec:       corev1.PodSpec{NodeName: "n1", Containers: []corev1.Container{{Name: "app"}, {Name: "sidecar"}}},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "10.0.0.1"},
	})
	enableMCP(s, false)
	session := mcpConnect(t, s)

	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      "list_pods",
		Arguments: map[string]any{"context": "test", "compact": true},
	})
	if err != nil {
		t.Fatalf("CallTool list_pods: %v", err)
	}
	if result.IsError {
		t.Fatalf("list_pods returned a tool error: %+v", result.Content)
	}
	text := result.Content[0].(*mcp.TextContent).Text //nolint:forcetypeassert // asserted shape from toolResult
	if !strings.Contains(text, `"web-0"`) {
		t.Errorf("compact list_pods = %q, want it to still name the pod", text)
	}
	for _, dropped := range []string{"containers", "10.0.0.1", "ownerKind"} {
		if strings.Contains(text, dropped) {
			t.Errorf("compact list_pods still contains %q: %s", dropped, text)
		}
	}
}

func TestMCPHandler_CallListPods_LabelSelector(t *testing.T) {
	s := newTestServer(t,
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "web-0", Namespace: "prod", Labels: map[string]string{"app": "web"}}, Status: corev1.PodStatus{Phase: corev1.PodRunning}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "db-0", Namespace: "prod", Labels: map[string]string{"app": "db"}}, Status: corev1.PodStatus{Phase: corev1.PodRunning}},
	)
	enableMCP(s, false)
	session := mcpConnect(t, s)

	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      "list_pods",
		Arguments: map[string]any{"context": "test", "labelSelector": "app=web"},
	})
	if err != nil {
		t.Fatalf("CallTool list_pods: %v", err)
	}
	if result.IsError {
		t.Fatalf("list_pods returned a tool error: %+v", result.Content)
	}
	text := result.Content[0].(*mcp.TextContent).Text //nolint:forcetypeassert // asserted shape from toolResult
	if !strings.Contains(text, `"web-0"`) || strings.Contains(text, `"db-0"`) {
		t.Errorf("labelSelector app=web returned %q, want only web-0", text)
	}
}

func TestMCPHandler_CallSimpleGetTools(t *testing.T) {
	s := newTestServer(t, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "web-0", Namespace: "prod"},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning},
	})
	enableMCP(s, false)
	session := mcpConnect(t, s)
	ctx := t.Context()

	for _, name := range []string{"list_contexts", "list_namespaces", "list_nodes", "get_overview"} {
		result, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      name,
			Arguments: map[string]any{"context": "test"},
		})
		if err != nil {
			t.Fatalf("CallTool %s: %v", name, err)
		}
		if result.IsError {
			t.Fatalf("%s returned a tool error: %+v", name, result.Content)
		}
		if _, ok := result.Content[0].(*mcp.TextContent); !ok {
			t.Fatalf("%s content[0] = %T, want *mcp.TextContent", name, result.Content[0])
		}
	}
}

func TestMCPHandler_CallListResources(t *testing.T) {
	s := newTestServer(t, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "cfg", Namespace: "prod"},
	})
	enableMCP(s, false)
	session := mcpConnect(t, s)

	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      "list_resources",
		Arguments: map[string]any{"context": "test", "resource": "configmaps", "namespace": "prod"},
	})
	if err != nil {
		t.Fatalf("CallTool list_resources: %v", err)
	}
	if result.IsError {
		t.Fatalf("list_resources returned a tool error: %+v", result.Content)
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content[0] = %T, want *mcp.TextContent", result.Content[0])
	}
	if !strings.Contains(text.Text, "cfg") {
		t.Errorf("list_resources result = %q, want it to include the seeded configmap", text.Text)
	}
}

func TestMCPHandler_CallListResources_LimitApplied(t *testing.T) {
	s := newTestServer(t,
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "prod"}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "b", Namespace: "prod"}},
	)
	enableMCP(s, false)
	session := mcpConnect(t, s)

	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      "list_resources",
		Arguments: map[string]any{"context": "test", "resource": "configmaps", "limit": 1},
	})
	if err != nil {
		t.Fatalf("CallTool list_resources: %v", err)
	}
	if result.IsError {
		t.Fatalf("list_resources returned a tool error: %+v", result.Content)
	}
	text := result.Content[0].(*mcp.TextContent).Text //nolint:forcetypeassert // asserted shape from toolResult
	var out struct {
		Total    int `json:"total"`
		Returned int `json:"returned"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("unmarshal: %v, body=%s", err, text)
	}
	if out.Total != 2 || out.Returned != 1 {
		t.Errorf("got %+v, want total=2 returned=1", out)
	}
}

func TestMCPHandler_CallListResources_UnknownResourceIsToolError(t *testing.T) {
	s := newTestServer(t)
	enableMCP(s, false)
	session := mcpConnect(t, s)

	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      "list_resources",
		Arguments: map[string]any{"context": "test", "resource": "not-a-real-resource"},
	})
	if err != nil {
		t.Fatalf("CallTool list_resources: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected a tool error for an unresolvable resource name")
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

// TestMCPHandler_CRDToolsRoundTrip covers the gap a real MCP client hit: none
// of list_resources/get_resource_detail/get_manifest can reach a CRD (they
// only know the fixed built-in catalog/manifest-slug map), so an agent asking
// about e.g. a SecretProviderClass got a bare 404 with no path forward. These
// four tools are the fix — addressed by group/version/resource from
// list_crd_kinds instead of a fixed kind slug.
func TestMCPHandler_CRDToolsRoundTrip(t *testing.T) {
	crd := apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: "secretproviderclasses.secrets-store.csi.x-k8s.io"},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "secrets-store.csi.x-k8s.io", Scope: apiextensionsv1.NamespaceScoped,
			Names:    apiextensionsv1.CustomResourceDefinitionNames{Kind: "SecretProviderClass", Plural: "secretproviderclasses"},
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{Name: "v1", Served: true, Storage: true}},
		},
	}
	instance := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "secrets-store.csi.x-k8s.io/v1", "kind": "SecretProviderClass",
		"metadata": map[string]any{"name": "azure-kv", "namespace": "prod"},
	}}
	s := newTestServerWithCRDs(t, []apiextensionsv1.CustomResourceDefinition{crd}, instance)
	enableMCP(s, false)
	session := mcpConnect(t, s)
	ctx := t.Context()

	kinds, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "list_crd_kinds", Arguments: map[string]any{"context": "test"}})
	if err != nil || kinds.IsError {
		t.Fatalf("CallTool list_crd_kinds: err=%v result=%+v", err, kinds)
	}
	kindsText, ok := kinds.Content[0].(*mcp.TextContent)
	if !ok || !strings.Contains(kindsText.Text, "SecretProviderClass") {
		t.Fatalf("list_crd_kinds content = %+v, want it to include SecretProviderClass", kinds.Content)
	}

	crdArgs := map[string]any{"context": "test", "group": "secrets-store.csi.x-k8s.io", "version": "v1", "resource": "secretproviderclasses"}

	list, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "list_crd_resources", Arguments: crdArgs})
	if err != nil || list.IsError {
		t.Fatalf("CallTool list_crd_resources: err=%v result=%+v", err, list)
	}
	listText, ok := list.Content[0].(*mcp.TextContent)
	if !ok || !strings.Contains(listText.Text, "azure-kv") {
		t.Fatalf("list_crd_resources content = %+v, want it to include azure-kv", list.Content)
	}

	getArgs := map[string]any{"context": "test", "group": "secrets-store.csi.x-k8s.io", "version": "v1", "resource": "secretproviderclasses", "namespace": "prod", "name": "azure-kv"}

	detail, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "get_crd_detail", Arguments: getArgs})
	if err != nil || detail.IsError {
		t.Fatalf("CallTool get_crd_detail: err=%v result=%+v", err, detail)
	}

	manifest, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "get_crd_manifest", Arguments: getArgs})
	if err != nil || manifest.IsError {
		t.Fatalf("CallTool get_crd_manifest: err=%v result=%+v", err, manifest)
	}
	manifestText, ok := manifest.Content[0].(*mcp.TextContent)
	if !ok || !strings.Contains(manifestText.Text, "azure-kv") {
		t.Fatalf("get_crd_manifest content = %+v, want it to include azure-kv", manifest.Content)
	}
}
