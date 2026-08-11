package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
)

func TestNodePeak(t *testing.T) {
	cases := []struct {
		name string
		n    nodeUsageItem
		want float64
	}{
		{"cpu more pressured", nodeUsageItem{CPU: gauge{Used: 80, Total: 100}, Memory: gauge{Used: 20, Total: 100}}, 0.8},
		{"memory more pressured", nodeUsageItem{CPU: gauge{Used: 10, Total: 100}, Memory: gauge{Used: 90, Total: 100}}, 0.9},
		{"no ceilings", nodeUsageItem{CPU: gauge{Used: 10}, Memory: gauge{Used: 10}}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := nodePeak(c.n); got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestPickCeiling(t *testing.T) {
	if got := pickCeiling(10, 5); got != 10 {
		t.Errorf("limit set: got %v, want 10", got)
	}
	if got := pickCeiling(0, 5); got != 5 {
		t.Errorf("no limit: got %v, want request (5)", got)
	}
	if got := pickCeiling(0, 0); got != 0 {
		t.Errorf("neither set: got %v, want 0", got)
	}
}

func TestUsageFor_UnknownScope(t *testing.T) {
	client := kubernetesfake.NewSimpleClientset()
	if _, _, err := usageFor(context.Background(), client, "bogus", "", ""); err == nil {
		t.Error("want an error for an unknown scope")
	}
}

// The plain kubernetesfake.Clientset's RESTClient() has no working transport
// configured, so a raw AbsPath().DoRaw() call panics instead of erroring
// cleanly (unlike a real cluster without metrics-server, which just 404s).
// The tests below use newFakeClientWithMetrics (metricsfake_test.go), which
// swaps in a fake RESTClient backed by a canned-response round tripper, to
// exercise hasMetricsServer and everything gated behind it.

var testPod = &corev1.Pod{
	ObjectMeta: metav1.ObjectMeta{Name: "web-1", Namespace: "prod"},
	Spec: corev1.PodSpec{Containers: []corev1.Container{{
		Name: "app",
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m"), corev1.ResourceMemory: resource.MustParse("64Mi")},
			Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m"), corev1.ResourceMemory: resource.MustParse("256Mi")},
		},
	}}},
}

var testNode = &corev1.Node{
	ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
	Status: corev1.NodeStatus{Allocatable: corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("2"),
		corev1.ResourceMemory: resource.MustParse("4Gi"),
	}},
}

const podMetricsJSON = `{"items":[{"metadata":{"name":"web-1","namespace":"prod"},"containers":[{"usage":{"cpu":"250m","memory":"128Mi"}}]}]}`
const nodeMetricsJSON = `{"items":[{"metadata":{"name":"node-1"},"usage":{"cpu":"500m","memory":"1Gi"}}]}`
const singleNodeMetricsJSON = `{"usage":{"cpu":"500m","memory":"1Gi"}}`
const singlePodMetricsJSON = `{"containers":[{"usage":{"cpu":"250m","memory":"128Mi"}}]}`

func TestHasMetricsServer(t *testing.T) {
	t.Run("available", func(t *testing.T) {
		s := newTestServerWithMetrics(t, metricsRoundTripper{responses: map[string]string{metricsAPIPath: "{}"}})
		client, _ := s.mgr.ClientFor("test")
		if !s.hasMetricsServer(context.Background(), client, "test") {
			t.Error("want true when the metrics API root responds")
		}
	})
	t.Run("unavailable, and cached", func(t *testing.T) {
		rt := metricsRoundTripper{responses: map[string]string{}}
		s := newTestServerWithMetrics(t, rt)
		client, _ := s.mgr.ClientFor("test")
		// Called twice to exercise the s.msCache hit path, not just the miss.
		for range 2 {
			if s.hasMetricsServer(context.Background(), client, "test") {
				t.Error("want false when the metrics API 404s")
			}
		}
	})
}

func TestHandlePodsUsage(t *testing.T) {
	rt := metricsRoundTripper{responses: map[string]string{
		metricsAPIPath:           "{}",
		metricsAPIPath + "/pods": podMetricsJSON,
	}}
	s := newTestServerWithMetrics(t, rt, testPod)
	rec := doRequest(t, s, "GET", "/api/contexts/test/podusage", "")
	if rec.Code != 200 {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Available bool                     `json:"available"`
		Items     map[string]podUsageEntry `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !body.Available {
		t.Fatal("want available: true")
	}
	entry, ok := body.Items["prod/web-1"]
	if !ok {
		t.Fatalf("want an entry for prod/web-1, got %+v", body.Items)
	}
	if entry.CPU.Request != 0.1 || entry.CPU.Limit != 0.5 || entry.CPU.Total != 0.5 {
		t.Errorf("cpu gauge = %+v", entry.CPU)
	}
}

func TestHandlePodsUsage_MetricsServerAbsent(t *testing.T) {
	s := newTestServerWithMetrics(t, metricsRoundTripper{responses: map[string]string{}})
	rec := doRequest(t, s, "GET", "/api/contexts/test/podusage", "")
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body["available"] != false {
		t.Errorf("available = %v, want false", body["available"])
	}
}

func TestHandleNodesUsage(t *testing.T) {
	rt := metricsRoundTripper{responses: map[string]string{
		metricsAPIPath:            "{}",
		metricsAPIPath + "/nodes": nodeMetricsJSON,
	}}
	s := newTestServerWithMetrics(t, rt, testNode)
	rec := doRequest(t, s, "GET", "/api/contexts/test/nodeusage", "")
	if rec.Code != 200 {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Available bool            `json:"available"`
		Items     []nodeUsageItem `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !body.Available || len(body.Items) != 1 || body.Items[0].Name != "node-1" {
		t.Fatalf("got %+v", body)
	}
	if body.Items[0].CPU.Used != 0.5 || body.Items[0].CPU.Total != 2 {
		t.Errorf("cpu = %+v", body.Items[0].CPU)
	}
}

func TestHandleUsage_Scopes(t *testing.T) {
	rt := metricsRoundTripper{responses: map[string]string{
		metricsAPIPath:                                 "{}",
		metricsAPIPath + "/nodes/node-1":               singleNodeMetricsJSON,
		metricsAPIPath + "/namespaces/prod/pods/web-1": singlePodMetricsJSON,
		metricsAPIPath + "/nodes":                      nodeMetricsJSON,
	}}
	s := newTestServerWithMetrics(t, rt, testPod, testNode)

	cases := []struct {
		name string
		path string
	}{
		{"node", "/api/contexts/test/usage/node?name=node-1"},
		{"pod", "/api/contexts/test/usage/pod?namespace=prod&name=web-1"},
		{"cluster", "/api/contexts/test/usage/cluster"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := doRequest(t, s, "GET", c.path, "")
			if rec.Code != 200 {
				t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
			}
			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if body["available"] != true {
				t.Errorf("available = %v, want true", body["available"])
			}
		})
	}

	t.Run("unknown scope", func(t *testing.T) {
		rec := doRequest(t, s, "GET", "/api/contexts/test/usage/bogus", "")
		if rec.Code != http.StatusBadGateway {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadGateway)
		}
	})
}

func TestHandleDeploymentsUsage(t *testing.T) {
	rs := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "web-abc123", Namespace: "prod",
			OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: "web", Controller: boolPtr(true)}},
		},
	}
	pod := testPod.DeepCopy()
	pod.OwnerReferences = []metav1.OwnerReference{{Kind: "ReplicaSet", Name: "web-abc123", Controller: boolPtr(true)}}

	rt := metricsRoundTripper{responses: map[string]string{
		metricsAPIPath:           "{}",
		metricsAPIPath + "/pods": podMetricsJSON,
	}}
	s := newTestServerWithMetrics(t, rt, pod, rs)
	rec := doRequest(t, s, "GET", "/api/contexts/test/deploymentusage", "")
	if rec.Code != 200 {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Available bool                     `json:"available"`
		Items     map[string]podUsageEntry `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	entry, ok := body.Items["prod/web"]
	if !ok {
		t.Fatalf("want an aggregated entry for prod/web, got %+v", body.Items)
	}
	if entry.CPU.Used != 0.25 || entry.CPU.Request != 0.1 || entry.CPU.Limit != 0.5 {
		t.Errorf("cpu = %+v", entry.CPU)
	}
}
