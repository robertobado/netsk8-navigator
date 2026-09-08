package api

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestOutputFilter_Empty_IsPassthrough(t *testing.T) {
	body := []byte(`{"a":1,"b":[2,3]}`)
	got, err := (outputFilter{}).apply(body, false)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(body) {
		t.Errorf("empty filter changed the body: %s", got)
	}
}

func TestOutputFilter_Jq_JSON(t *testing.T) {
	body := []byte(`{"items":[{"name":"a","phase":"Running"},{"name":"b","phase":"Pending"}],"total":2}`)

	cases := []struct {
		name, jq, want string
	}{
		{"scalar length", ".items | length", "2"},
		{"single object projection", `.items[0] | {n: .name}`, `{"n":"a"}`},
		{"multiple outputs are newline-joined", ".items[].name", "\"a\"\n\"b\""},
		{"select filters", `.items[] | select(.phase=="Pending") | .name`, `"b"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := (outputFilter{Jq: c.jq}).apply(body, false)
			if err != nil {
				t.Fatalf("apply: %v", err)
			}
			if string(got) != c.want {
				t.Errorf("jq %q => %q, want %q", c.jq, got, c.want)
			}
		})
	}
}

func TestOutputFilter_Jq_YAMLInAndOut(t *testing.T) {
	body := []byte("apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: web\nspec:\n  replicas: 3\n")

	got, err := (outputFilter{Jq: ".spec"}).apply(body, true)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	// Re-emitted as YAML, not JSON.
	if !strings.Contains(string(got), "replicas: 3") || strings.Contains(string(got), "{") {
		t.Errorf("YAML jq result = %q, want a YAML fragment with replicas: 3", got)
	}
}

func TestOutputFilter_Jq_MultipleYAMLDocsSeparated(t *testing.T) {
	body := []byte("items:\n  - name: a\n  - name: b\n")
	got, err := (outputFilter{Jq: ".items[]"}).apply(body, true)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !strings.Contains(string(got), "---\n") {
		t.Errorf("multiple YAML outputs = %q, want them separated by ---", got)
	}
}

func TestOutputFilter_Jq_InvalidProgram_IsError(t *testing.T) {
	_, err := (outputFilter{Jq: ".items[ |"}).apply([]byte(`{}`), false)
	if err == nil {
		t.Fatal("expected an error for a malformed jq program")
	}
	if !strings.Contains(err.Error(), "jq") {
		t.Errorf("error should mention jq, got: %v", err)
	}
}

func TestOutputFilter_Jq_OnNonJSON_IsError(t *testing.T) {
	_, err := (outputFilter{Jq: "."}).apply([]byte("plain log line\nanother\n"), false)
	if err == nil {
		t.Fatal("expected an error applying jq to non-JSON text")
	}
}

func TestOutputFilter_Jq_RuntimeError_IsSurfaced(t *testing.T) {
	// .foo on an array is a jq runtime error ("Cannot index array with \"foo\"").
	_, err := (outputFilter{Jq: ".foo"}).apply([]byte(`[1,2,3]`), false)
	if err == nil {
		t.Fatal("expected the jq runtime error to be surfaced, not swallowed")
	}
}

func TestOutputFilter_GrepAndGrepV(t *testing.T) {
	body := []byte("2026-01-01 INFO started\n2026-01-01 ERROR boom\n2026-01-01 INFO ok\n2026-01-01 WARN slow\n")

	got, err := (outputFilter{Grep: "ERROR|WARN"}).apply(body, false)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "2026-01-01 ERROR boom\n2026-01-01 WARN slow\n" {
		t.Errorf("grep result = %q", got)
	}

	got, err = (outputFilter{GrepV: "INFO"}).apply(body, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "INFO") {
		t.Errorf("grepV result still has INFO lines: %q", got)
	}
}

func TestOutputFilter_HeadTail(t *testing.T) {
	body := []byte("l1\nl2\nl3\nl4\nl5\n")

	got, _ := (outputFilter{Head: 2}).apply(body, false)
	if string(got) != "l1\nl2\n" {
		t.Errorf("head 2 = %q", got)
	}
	got, _ = (outputFilter{Tail: 2}).apply(body, false)
	if string(got) != "l4\nl5\n" {
		t.Errorf("tail 2 = %q", got)
	}
	// head then tail: first 4, then last 2 of those.
	got, _ = (outputFilter{Head: 4, Tail: 2}).apply(body, false)
	if string(got) != "l3\nl4\n" {
		t.Errorf("head 4 tail 2 = %q", got)
	}
}

func TestOutputFilter_GrepThenJq_PipelineOrder(t *testing.T) {
	// jq runs first (produces newline-joined names), then grep keeps one.
	body := []byte(`{"items":[{"name":"alpha"},{"name":"beta"},{"name":"gamma"}]}`)
	got, err := (outputFilter{Jq: ".items[].name", Grep: "^\"a"}).apply(body, false)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `"alpha"` {
		t.Errorf("jq|grep = %q, want just \"alpha\"", got)
	}
}

func TestOutputFilter_MaxBytes_TruncatesAtLineBoundaryWithMarker(t *testing.T) {
	body := []byte("aaaaaaaaaa\nbbbbbbbbbb\ncccccccccc\ndddddddddd\n")
	got, err := (outputFilter{MaxBytes: 25}).apply(body, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(got), "aaaaaaaaaa\nbbbbbbbbbb") {
		t.Errorf("truncated result lost the head: %q", got)
	}
	if !strings.Contains(string(got), "truncated") {
		t.Errorf("truncated result has no marker: %q", got)
	}
	if strings.Contains(string(got), "dddddddddd") {
		t.Errorf("truncated result kept content past the cap: %q", got)
	}
}

func TestOutputFilter_MaxBytes_UnderLimit_Untouched(t *testing.T) {
	body := []byte("small\n")
	got, _ := (outputFilter{MaxBytes: 1000}).apply(body, false)
	if string(got) != "small\n" {
		t.Errorf("maxBytes above size changed the body: %q", got)
	}
}

// TestMCPHandler_FilterArgReachesToolViaSchema is the wiring check: the
// nested "filter" object survives schema generation + argument unmarshaling
// and actually reshapes a real tool's result.
func TestMCPHandler_FilterArgReachesToolViaSchema(t *testing.T) {
	s := newTestServer(t,
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "cfg-a", Namespace: "prod"}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "cfg-b", Namespace: "prod"}},
	)
	enableMCP(s, false)
	session := mcpConnect(t, s)

	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name: "list_resources",
		Arguments: map[string]any{
			"context":   "test",
			"resource":  "configmaps",
			"namespace": "prod",
			"filter":    map[string]any{"jq": "length"},
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool error: %+v", result.Content)
	}
	text := result.Content[0].(*mcp.TextContent) //nolint:forcetypeassert // asserted shape in sibling tests
	if strings.TrimSpace(text.Text) != "2" {
		t.Errorf("filter.jq=length over 2 configmaps => %q, want \"2\"", text.Text)
	}
}

func TestMCPHandler_FilterJqError_IsToolError(t *testing.T) {
	s := newTestServer(t, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "cfg", Namespace: "prod"}})
	enableMCP(s, false)
	session := mcpConnect(t, s)

	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name: "list_resources",
		Arguments: map[string]any{
			"context":  "test",
			"resource": "configmaps",
			"filter":   map[string]any{"jq": "this is not jq ("},
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !result.IsError {
		t.Fatal("a malformed jq program should come back as a tool error, not a silent passthrough")
	}
}
