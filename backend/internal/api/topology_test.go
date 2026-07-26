package api

import (
	"encoding/json"
	"net/http"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestLabelsMatch(t *testing.T) {
	labels := map[string]string{"app": "web", "tier": "frontend"}
	if !labelsMatch(map[string]string{"app": "web"}, labels) {
		t.Error("a subset selector should match")
	}
	if labelsMatch(map[string]string{"app": "other"}, labels) {
		t.Error("a mismatched value should not match")
	}
	if labelsMatch(map[string]string{"missing": "key"}, labels) {
		t.Error("a selector key absent from labels should not match")
	}
}

func TestLinkToPods(t *testing.T) {
	pods := &corev1.PodList{Items: []corev1.Pod{
		{ObjectMeta: metav1.ObjectMeta{Name: "web-1", Labels: map[string]string{"app": "web"}}},
		{ObjectMeta: metav1.ObjectMeta{Name: "web-2", Labels: map[string]string{"app": "web"}}},
		{ObjectMeta: metav1.ObjectMeta{Name: "other-1", Labels: map[string]string{"app": "other"}}},
	}}

	t.Run("links only matching pods", func(t *testing.T) {
		g := &topoGraph{}
		linkToPods(g, "deployment/web", map[string]string{"app": "web"}, pods)
		if len(g.Edges) != 2 {
			t.Fatalf("got %d edges, want 2", len(g.Edges))
		}
		if g.Edges[0].Target != "pod/web-1" || g.Edges[1].Target != "pod/web-2" {
			t.Errorf("got %+v", g.Edges)
		}
	})

	t.Run("empty selector links nothing", func(t *testing.T) {
		g := &topoGraph{}
		linkToPods(g, "service/mystery", map[string]string{}, pods)
		if len(g.Edges) != 0 {
			t.Errorf("got %d edges, want 0 for an empty selector", len(g.Edges))
		}
	})
}

func TestHandleTopology_RequiresNamespace(t *testing.T) {
	s := newTestServer(t)
	rec := doRequest(t, s, "GET", "/api/contexts/test/topology", "")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (namespace required)", rec.Code)
	}
}

func TestHandleTopology(t *testing.T) {
	s := newTestServer(t,
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "web-1", Namespace: "prod", Labels: map[string]string{"app": "web"}}},
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "prod"},
			Spec: appsv1.DeploymentSpec{
				Replicas: replicas(1),
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "web"}},
			},
			Status: appsv1.DeploymentStatus{ReadyReplicas: 1},
		},
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "web-svc", Namespace: "prod"},
			Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": "web"}, Type: corev1.ServiceTypeClusterIP},
		},
	)
	rec := doRequest(t, s, "GET", "/api/contexts/test/topology?namespace=prod", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var g topoGraph
	if err := json.Unmarshal(rec.Body.Bytes(), &g); err != nil {
		t.Fatal(err)
	}
	if len(g.Nodes) != 3 {
		t.Fatalf("got %d nodes, want 3 (pod+deployment+service): %+v", len(g.Nodes), g.Nodes)
	}
	if len(g.Edges) != 2 {
		t.Fatalf("got %d edges, want 2 (deployment->pod, service->pod): %+v", len(g.Edges), g.Edges)
	}
}
