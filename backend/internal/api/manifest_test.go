package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	ktesting "k8s.io/client-go/testing"

	"github.com/robertobado/netsk8-navigator/backend/internal/kube"
)

func TestCleanUnstructured(t *testing.T) {
	obj := &unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{
			"name":          "web",
			"managedFields": []any{map[string]any{"manager": "kubectl"}},
			"annotations": map[string]any{
				"kubectl.kubernetes.io/last-applied-configuration": "{...}",
				"custom.io/keep": "yes",
			},
		},
	}}
	cleanUnstructured(obj)

	if _, found, _ := unstructured.NestedFieldNoCopy(obj.Object, "metadata", "managedFields"); found {
		t.Error("managedFields should be removed")
	}
	ann, _, _ := unstructured.NestedStringMap(obj.Object, "metadata", "annotations")
	if _, has := ann["kubectl.kubernetes.io/last-applied-configuration"]; has {
		t.Error("last-applied-configuration annotation should be removed")
	}
	if ann["custom.io/keep"] != "yes" {
		t.Error("unrelated annotations should survive")
	}
}

func TestCleanUnstructured_RemovesEmptyAnnotationsMap(t *testing.T) {
	obj := &unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{
			"annotations": map[string]any{
				"kubectl.kubernetes.io/last-applied-configuration": "{...}",
			},
		},
	}}
	cleanUnstructured(obj)
	if _, found, _ := unstructured.NestedFieldNoCopy(obj.Object, "metadata", "annotations"); found {
		t.Error("annotations map should be removed entirely once emptied")
	}
}

func TestResolveSlug_UnknownSlug(t *testing.T) {
	s := newTestServer(t)
	if _, err := s.resolveSlug("test", "not-a-real-kind"); err == nil {
		t.Error("expected an error for an unknown manifest slug")
	}
}

// The fake dynamic client's tracker doesn't actually understand DryRun (it
// persists unconditionally), unlike a real API server — so this test patches
// that gap with a reactor that mimics real dry-run semantics, purely so we
// can prove handleApplyManifest's own behavior (preview the result, don't
// persist) is correct.
func TestHandleApplyManifest_DryRunDoesNotPersist(t *testing.T) {
	s := newTestServer(t, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "prod"},
		Spec:       appsv1.DeploymentSpec{Replicas: replicas(2)},
	})
	fm, ok := s.mgr.(*fakeManager)
	if !ok {
		t.Fatal("expected a *fakeManager")
	}
	dyn, ok := fm.dynamic.(*dynamicfake.FakeDynamicClient)
	if !ok {
		t.Fatal("expected a *dynamicfake.FakeDynamicClient")
	}
	dyn.PrependReactor("update", "deployments", func(action ktesting.Action) (bool, runtime.Object, error) {
		ua, ok := action.(ktesting.UpdateActionImpl)
		if !ok || len(ua.GetUpdateOptions().DryRun) == 0 {
			return false, nil, nil // not a dry-run — defer to the default reactor, which persists for real
		}
		return true, ua.GetObject(), nil // dry-run: report success without touching the tracker
	})

	body := `{"yaml":"apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: web\n  namespace: prod\nspec:\n  replicas: 9\n"}`
	rec := doRequest(t, s, "PUT", "/api/contexts/test/manifest/deployment/prod/web?dryRun=true", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out["yaml"], "replicas: 9") {
		t.Errorf("dry-run response should preview the requested change, got:\n%s", out["yaml"])
	}

	rec2 := doRequest(t, s, "GET", "/api/contexts/test/resources/deployments?namespace=prod", "")
	var list []kube.DeploymentView
	if err := json.Unmarshal(rec2.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Ready != "0/2" {
		t.Errorf("dry-run must not persist — live deployment should still want 2 replicas, got %+v", list)
	}
}
