package api

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func toolText(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) (string, bool) {
	t.Helper()
	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	tc, _ := res.Content[0].(*mcp.TextContent)
	if tc == nil {
		t.Fatalf("%s: no text content: %+v", name, res.Content)
	}
	return tc.Text, res.IsError
}

func testEvent(name, typ, reason, obj string, ago time.Duration) *corev1.Event {
	last := metav1.NewTime(time.Now().Add(-ago))
	return &corev1.Event{
		ObjectMeta:     metav1.ObjectMeta{Name: name, Namespace: "prod"},
		Type:           typ,
		Reason:         reason,
		LastTimestamp:  last,
		InvolvedObject: corev1.ObjectReference{Kind: "Pod", Namespace: "prod", Name: obj},
	}
}

func TestMCPGetEvents(t *testing.T) {
	s := newTestServer(t,
		testEvent("e1", "Warning", "BackOff", "web-1", 1*time.Minute),
		testEvent("e2", "Normal", "Pulled", "web-1", 2*time.Minute),
		testEvent("e3", "Warning", "Failed", "web-2", 3*time.Minute),
		testEvent("e4", "Warning", "Evicted", "web-1", 4*time.Minute),
	)
	enableMCP(s, false)
	session := mcpConnect(t, s)

	reasons := func(args map[string]any) []string {
		t.Helper()
		args["context"] = "test"
		text, isErr := toolText(t, session, "get_events", args)
		if isErr {
			t.Fatalf("get_events(%v) failed: %s", args, text)
		}
		var evs []struct{ Reason string }
		if err := json.Unmarshal([]byte(text), &evs); err != nil {
			t.Fatalf("decode %q: %v", text, err)
		}
		out := make([]string, len(evs))
		for i, e := range evs {
			out[i] = e.Reason
		}
		return out
	}

	if got := strings.Join(reasons(map[string]any{}), ","); got != "BackOff,Pulled,Failed,Evicted" {
		t.Errorf("all events, most recent first = %s", got)
	}
	if got := strings.Join(reasons(map[string]any{"warningsOnly": true}), ","); got != "BackOff,Failed,Evicted" {
		t.Errorf("warningsOnly = %s, want the three Warnings", got)
	}
	if got := strings.Join(reasons(map[string]any{"limit": 2}), ","); got != "BackOff,Pulled" {
		t.Errorf("limit 2 = %s, want the two most recent", got)
	}
	if got := strings.Join(reasons(map[string]any{"namespace": "prod", "name": "web-1", "kind": "Pod", "warningsOnly": true}), ","); got != "BackOff,Evicted" {
		t.Errorf("web-1 warnings = %s, want BackOff,Evicted", got)
	}

	if text, isErr := toolText(t, session, "get_events", map[string]any{"context": "test", "name": "web-1"}); !isErr || !strings.Contains(text, "namespace is required") {
		t.Errorf("name without namespace should be refused, got isErr=%v %s", isErr, text)
	}
}

func TestReshapePodUsage(t *testing.T) {
	const body = `{"available":true,"items":{
		"a/idle":{"cpu":{"used":0.01},"memory":{"used":314572800}},
		"a/busy":{"cpu":{"used":0.5,"request":0.1,"limit":1},"memory":{"used":104857600,"request":0,"limit":209715200}},
		"b/mid":{"cpu":{"used":0.05},"memory":{"used":52428800}}}}`
	decode := func(v any) (items []podUsageOut, total, returned int) {
		t.Helper()
		enc, _ := json.Marshal(v)
		var out struct {
			Total, Returned int
			Items           []podUsageOut
		}
		if err := json.Unmarshal(enc, &out); err != nil {
			t.Fatal(err)
		}
		return out.Items, out.Total, out.Returned
	}

	v, err := reshapePodUsage([]byte(body), "cpu", 0)
	if err != nil {
		t.Fatal(err)
	}
	items, total, returned := decode(v)
	if total != 3 || returned != 3 || items[0].Name != "busy" || items[1].Name != "mid" || items[2].Name != "idle" {
		t.Fatalf("cpu order = %+v (total %d, returned %d)", items, total, returned)
	}
	if b := items[0]; b.Namespace != "a" || b.CPUMillicores != 500 || b.CPURequestMillicores != 100 || b.CPULimitMillicores != 1000 ||
		b.MemoryMiB != 100 || b.MemoryLimitMiB != 200 {
		t.Errorf("busy pod units wrong: %+v", b)
	}

	v, _ = reshapePodUsage([]byte(body), "memory", 0)
	if items, _, _ = decode(v); items[0].Name != "idle" { // 300 MiB is the most memory
		t.Errorf("memory order = %+v", items)
	}

	v, _ = reshapePodUsage([]byte(body), "cpu", 2)
	if items, total, returned = decode(v); len(items) != 2 || total != 3 || returned != 2 {
		t.Errorf("limit 2: %d items, total %d, returned %d", len(items), total, returned)
	}

	v, _ = reshapePodUsage([]byte(`{"available":false}`), "cpu", 0)
	if m, _ := v.(map[string]any); m["available"] != false || m["note"] == nil {
		t.Errorf("unavailable should say so, got %+v", v)
	}
}

func TestMCPGetUsage(t *testing.T) {
	rt := metricsRoundTripper{responses: map[string]string{
		metricsAPIPath:            "{}",
		metricsAPIPath + "/pods":  podMetricsJSON,
		metricsAPIPath + "/nodes": nodeMetricsJSON,
	}}
	s := newTestServerWithMetrics(t, rt, testPod, testNode)
	enableMCP(s, false)
	session := mcpConnect(t, s)

	text, isErr := toolText(t, session, "get_usage", map[string]any{"context": "test", "scope": "pods"})
	if isErr {
		t.Fatalf("pods usage failed: %s", text)
	}
	var pods struct {
		Available bool
		SortedBy  string
		Items     []podUsageOut
	}
	if err := json.Unmarshal([]byte(text), &pods); err != nil {
		t.Fatalf("decode %q: %v", text, err)
	}
	if !pods.Available || pods.SortedBy != "cpu" || len(pods.Items) != 1 {
		t.Fatalf("pods = %+v", pods)
	}
	if p := pods.Items[0]; p.Name != "web-1" || p.CPUMillicores != 250 || p.CPULimitMillicores != 500 || p.MemoryMiB != 128 || p.MemoryLimitMiB != 256 {
		t.Errorf("web-1 usage = %+v", p)
	}

	text, isErr = toolText(t, session, "get_usage", map[string]any{"context": "test", "scope": "node"})
	if isErr {
		t.Fatalf("nodes usage failed: %s", text)
	}
	var nodes struct{ Items []nodeUsageOut }
	if err := json.Unmarshal([]byte(text), &nodes); err != nil {
		t.Fatalf("decode %q: %v", text, err)
	}
	if len(nodes.Items) != 1 || nodes.Items[0].CPUPercent != 25 || nodes.Items[0].MemoryPercent != 25 { // 500m of 2 cores; 1Gi of 4Gi
		t.Errorf("nodes = %+v", nodes.Items)
	}

	// The jq filter reaches the reshaped document, not a wrapper.
	text, _ = toolText(t, session, "get_usage", map[string]any{"context": "test", "scope": "pods", "filter": map[string]any{"jq": ".items[0].name"}})
	if strings.TrimSpace(text) != `"web-1"` {
		t.Errorf("jq over usage = %q", text)
	}

	for _, bad := range []map[string]any{{"scope": "deployments"}, {"scope": "pods", "sort": "disk"}} {
		bad["context"] = "test"
		if text, isErr := toolText(t, session, "get_usage", bad); !isErr {
			t.Errorf("%v should be refused, got %s", bad, text)
		}
	}
}

func TestMCPGetUsage_NoMetricsServer(t *testing.T) {
	s := newTestServerWithMetrics(t, metricsRoundTripper{responses: map[string]string{}})
	enableMCP(s, false)
	session := mcpConnect(t, s)
	text, isErr := toolText(t, session, "get_usage", map[string]any{"context": "test", "scope": "pods"})
	if isErr || !strings.Contains(text, `"available":false`) {
		t.Errorf("want a clean available:false, got isErr=%v %s", isErr, text)
	}
}

func TestHumanizeAges(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	got := string(humanizeAges([]byte(`{"items":[{"name":"a","age":"2026-09-17T12:00:00Z","x":1},{"name":"b","age":"2026-09-20T09:30:00Z"}]}`), now))
	want := `{"items":[{"name":"a","age":"3d","created":"2026-09-17T12:00:00Z","x":1},{"name":"b","age":"2h","created":"2026-09-20T09:30:00Z"}]}`
	if got != want {
		t.Errorf("humanizeAges =\n%s\nwant\n%s", got, want)
	}

	// Plain-text bodies (logs) and already-relative ages are left alone.
	log := `{"age":"2026-09-17T12:00:00Z"} first line` + "\n" + `second`
	if got := string(humanizeAges([]byte(log), now)); got != log {
		t.Errorf("a non-JSON body must be untouched, got %q", got)
	}
	if got := string(humanizeAges([]byte(`{"age":"3d"}`), now)); got != `{"age":"3d"}` {
		t.Errorf("a non-timestamp age must be untouched, got %q", got)
	}
}

func TestCompactAge(t *testing.T) {
	cases := map[time.Duration]string{
		-time.Second: "0s", 0: "0s", 59 * time.Second: "59s", 60 * time.Second: "1m", 59 * time.Minute: "59m",
		time.Hour: "1h", 23 * time.Hour: "23h", 24 * time.Hour: "1d", 364 * 24 * time.Hour: "364d", 365 * 24 * time.Hour: "1y",
	}
	for d, want := range cases {
		if got := compactAge(d); got != want {
			t.Errorf("compactAge(%s) = %q, want %q", d, got, want)
		}
	}
}

// TestMCPListResources_AgeIsRelativeAndKeepsTimestamp is the tool-level check:
// list_resources used to hand back the creation timestamp under the name "age".
func TestMCPListResources_AgeIsRelativeAndKeepsTimestamp(t *testing.T) {
	created := metav1.NewTime(time.Now().Add(-49 * time.Hour))
	s := newTestServer(t, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "cfg", Namespace: "prod", CreationTimestamp: created}})
	enableMCP(s, false)
	session := mcpConnect(t, s)
	text, isErr := toolText(t, session, "list_resources", map[string]any{"context": "test", "resource": "configmaps", "namespace": "prod"})
	if isErr {
		t.Fatalf("list_resources: %s", text)
	}
	if !strings.Contains(text, `"age":"2d"`) || !strings.Contains(text, `"created":"`+created.UTC().Format(time.RFC3339)+`"`) {
		t.Errorf("want age 2d plus the original timestamp as created, got %s", text)
	}
}
