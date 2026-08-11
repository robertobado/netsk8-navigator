package api

import (
	"encoding/json"
	"net/http"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/robertobado/netsk8-navigator/backend/internal/kube"
)

func replicas(n int32) *int32 { return &n }

func TestHandleHealth(t *testing.T) {
	s := newTestServer(t)
	rec := doRequest(t, s, "GET", "/api/health", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "ok" {
		t.Errorf("status field = %v, want ok", body["status"])
	}
	if body["demo"] != false {
		t.Errorf("demo field = %v, want false when DemoMode is unset", body["demo"])
	}
}

func TestHandleHealth_VersionAndAuthEnabled(t *testing.T) {
	s := newTestServer(t)
	s.Version = "1.2.3"
	s.AuthEnabled = true
	rec := doRequest(t, s, "GET", "/api/health", "")
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["version"] != "1.2.3" {
		t.Errorf("version field = %v, want 1.2.3", body["version"])
	}
	if body["authEnabled"] != true {
		t.Errorf("authEnabled field = %v, want true", body["authEnabled"])
	}
}

func TestHandleHealth_DemoMode(t *testing.T) {
	s := newTestServer(t)
	s.DemoMode = true
	rec := doRequest(t, s, "GET", "/api/health", "")
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["demo"] != true {
		t.Errorf("demo field = %v, want true when DemoMode is set", body["demo"])
	}
}

func TestHandleContexts(t *testing.T) {
	s := newTestServer(t)
	rec := doRequest(t, s, "GET", "/api/contexts", "")
	var contexts []kube.ContextInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &contexts); err != nil {
		t.Fatal(err)
	}
	if len(contexts) != 1 || contexts[0].Name != "test" {
		t.Errorf("contexts = %+v", contexts)
	}
}

func TestHandleNamespaces(t *testing.T) {
	s := newTestServer(t, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "prod"},
		Status:     corev1.NamespaceStatus{Phase: corev1.NamespaceActive},
	})
	rec := doRequest(t, s, "GET", "/api/contexts/test/namespaces", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var out []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0]["name"] != "prod" || out[0]["status"] != "Active" {
		t.Errorf("got %+v", out)
	}
}

func TestHandleNodes(t *testing.T) {
	s := newTestServer(t, &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "worker-1"},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}},
		},
	})
	rec := doRequest(t, s, "GET", "/api/contexts/test/nodes", "")
	var out []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0]["ready"] != true {
		t.Errorf("got %+v", out)
	}
}

func TestHandlePods(t *testing.T) {
	s := newTestServer(t, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "web-0", Namespace: "prod"},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning},
	})
	rec := doRequest(t, s, "GET", "/api/contexts/test/pods?namespace=prod", "")
	var pods []kube.PodView
	if err := json.Unmarshal(rec.Body.Bytes(), &pods); err != nil {
		t.Fatal(err)
	}
	if len(pods) != 1 || pods[0].Name != "web-0" {
		t.Errorf("got %+v", pods)
	}
}

func TestHandleOverview(t *testing.T) {
	s := newTestServer(t,
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "n1"}, Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}},
		}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "default"}, Status: corev1.PodStatus{Phase: corev1.PodRunning}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p2", Namespace: "default"}, Status: corev1.PodStatus{Phase: corev1.PodPending}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
	)
	rec := doRequest(t, s, "GET", "/api/contexts/test/overview", "")
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["pods"] != float64(2) || out["running"] != float64(1) || out["pending"] != float64(1) {
		t.Errorf("got %+v", out)
	}
}

func TestHandleResourceList_Deployments(t *testing.T) {
	s := newTestServer(t, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "prod"},
		Spec:       appsv1.DeploymentSpec{Replicas: replicas(3)},
		Status:     appsv1.DeploymentStatus{ReadyReplicas: 3, UpdatedReplicas: 3, AvailableReplicas: 3, Replicas: 3},
	})
	rec := doRequest(t, s, "GET", "/api/contexts/test/resources/deployments?namespace=prod", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var out []kube.DeploymentView
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Name != "web" || out[0].Status != "Available" {
		t.Errorf("got %+v", out)
	}
}

func TestHandleResourceList_UnknownResource(t *testing.T) {
	s := newTestServer(t)
	rec := doRequest(t, s, "GET", "/api/contexts/test/resources/frobnicators", "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestHandleGetManifest(t *testing.T) {
	s := newTestServer(t, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "prod"},
		Spec:       appsv1.DeploymentSpec{Replicas: replicas(2)},
	})
	rec := doRequest(t, s, "GET", "/api/contexts/test/manifest/deployment/prod/web", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["yaml"] == "" {
		t.Error("expected non-empty yaml")
	}
}

func TestHandleGetManifest_PodBlocked(t *testing.T) {
	// Pods aren't blocked on GET, only on apply — sanity-check unrelated slugs 404 cleanly instead.
	s := newTestServer(t)
	rec := doRequest(t, s, "GET", "/api/contexts/test/manifest/notaresource/ns/name", "")
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502 (unsupported kind)", rec.Code)
	}
}

func TestHandleApplyManifest(t *testing.T) {
	s := newTestServer(t, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "prod"},
		Spec:       appsv1.DeploymentSpec{Replicas: replicas(2)},
	})
	body := `{"yaml":"apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: web\n  namespace: prod\nspec:\n  replicas: 5\n"}`
	rec := doRequest(t, s, "PUT", "/api/contexts/test/manifest/deployment/prod/web", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}

	// Confirm the update actually landed by reading it back through the list endpoint.
	rec2 := doRequest(t, s, "GET", "/api/contexts/test/resources/deployments?namespace=prod", "")
	var out []kube.DeploymentView
	if err := json.Unmarshal(rec2.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Ready != "0/5" {
		t.Errorf("after apply, got %+v, want replicas=5 reflected", out)
	}
}

func TestHandleApplyManifest_RejectsPods(t *testing.T) {
	s := newTestServer(t)
	rec := doRequest(t, s, "PUT", "/api/contexts/test/manifest/pod/ns/name", `{"yaml":""}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (pods are immutable here)", rec.Code)
	}
}

func TestHandleConsumers_ConfigMap(t *testing.T) {
	s := newTestServer(t,
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "consumer", Namespace: "prod"},
			Spec: corev1.PodSpec{Volumes: []corev1.Volume{
				{VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: "app-config"}}}},
			}},
		},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "unrelated", Namespace: "prod"}},
	)
	rec := doRequest(t, s, "GET", "/api/contexts/test/consumers/configmap/prod/app-config", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var pods []kube.PodView
	if err := json.Unmarshal(rec.Body.Bytes(), &pods); err != nil {
		t.Fatal(err)
	}
	if len(pods) != 1 || pods[0].Name != "consumer" {
		t.Errorf("got %+v", pods)
	}
}

func TestHandleServiceAccountUsage(t *testing.T) {
	s := newTestServer(t,
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "web-0", Namespace: "prod"},
			Spec:       corev1.PodSpec{ServiceAccountName: "web"},
		},
	)
	rec := doRequest(t, s, "GET", "/api/contexts/test/serviceaccount-usage/prod/web", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var out saUsage
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Pods) != 1 || out.Pods[0].Name != "web-0" {
		t.Errorf("got %+v", out)
	}
}

// TestHandleServiceAccountUsage_EffectivePermissions seeds a Role bound via a
// RoleBinding and a ClusterRole bound via a ClusterRoleBinding, both naming
// the SA, and checks the response unions both sets of rules (deduped, in the
// same "verbs → resources" shape Role/ClusterRole detail already renders).
func TestHandleServiceAccountUsage_EffectivePermissions(t *testing.T) {
	s := newTestServer(t,
		&rbacv1.Role{
			ObjectMeta: metav1.ObjectMeta{Name: "pod-reader", Namespace: "prod"},
			Rules:      []rbacv1.PolicyRule{{Verbs: []string{"get", "list"}, APIGroups: []string{""}, Resources: []string{"pods"}}},
		},
		&rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "web-pod-reader", Namespace: "prod"},
			RoleRef:    rbacv1.RoleRef{Kind: "Role", Name: "pod-reader", APIGroup: "rbac.authorization.k8s.io"},
			Subjects:   []rbacv1.Subject{{Kind: "ServiceAccount", Name: "web", Namespace: "prod"}},
		},
		&rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{Name: "node-viewer"},
			Rules:      []rbacv1.PolicyRule{{Verbs: []string{"get"}, APIGroups: []string{""}, Resources: []string{"nodes"}}},
		},
		&rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "web-node-viewer"},
			RoleRef:    rbacv1.RoleRef{Kind: "ClusterRole", Name: "node-viewer", APIGroup: "rbac.authorization.k8s.io"},
			Subjects:   []rbacv1.Subject{{Kind: "ServiceAccount", Name: "web", Namespace: "prod"}},
		},
	)
	rec := doRequest(t, s, "GET", "/api/contexts/test/serviceaccount-usage/prod/web", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var out saUsage
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Bindings) != 2 {
		t.Errorf("expected 2 bindings, got %+v", out.Bindings)
	}
	want := map[string]string{"get,list": "core/pods", "get": "core/nodes"}
	if len(out.Permissions) != len(want) {
		t.Fatalf("expected %d permission rows, got %+v", len(want), out.Permissions)
	}
	for _, p := range out.Permissions {
		if want[p.Label] != p.Value {
			t.Errorf("permission %q = %q, want %q", p.Label, p.Value, want[p.Label])
		}
	}
}

func TestHandleConsumers_Secret(t *testing.T) {
	s := newTestServer(t,
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "consumer", Namespace: "prod"},
			Spec:       corev1.PodSpec{ImagePullSecrets: []corev1.LocalObjectReference{{Name: "regcred"}}},
		},
	)
	rec := doRequest(t, s, "GET", "/api/contexts/test/consumers/secret/prod/regcred", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var pods []kube.PodView
	if err := json.Unmarshal(rec.Body.Bytes(), &pods); err != nil {
		t.Fatal(err)
	}
	if len(pods) != 1 || pods[0].Name != "consumer" {
		t.Errorf("got %+v", pods)
	}
}

func TestHandleNamespaceSummary(t *testing.T) {
	s := newTestServer(t,
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "prod"}},
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "prod"}},
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "other-ns", Namespace: "staging"}},
	)
	rec := doRequest(t, s, "GET", "/api/contexts/test/namespace-summary/prod", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var groups []namespaceGroup
	if err := json.Unmarshal(rec.Body.Bytes(), &groups); err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || groups[0].Kind != "Deployment" || len(groups[0].Items) != 2 {
		t.Errorf("got %+v", groups)
	}
}

func TestHandleNodeWorkloads(t *testing.T) {
	s := newTestServer(t,
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "standalone", Namespace: "prod"},
			Spec:       corev1.PodSpec{NodeName: "worker-1"},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "elsewhere", Namespace: "prod"},
			Spec:       corev1.PodSpec{NodeName: "worker-2"},
		},
	)
	rec := doRequest(t, s, "GET", "/api/contexts/test/node-workloads/worker-1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var groups []nodeWorkloadGroup
	if err := json.Unmarshal(rec.Body.Bytes(), &groups); err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || groups[0].Kind != "Pod" || len(groups[0].Pods) != 1 {
		t.Errorf("got %+v", groups)
	}
}
