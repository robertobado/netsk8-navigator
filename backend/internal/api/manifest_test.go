package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	ktesting "k8s.io/client-go/testing"

	"github.com/robertobado/netsk8-navigator/backend/internal/kube"
)

// countedFailManager wraps *fakeManager but fails DynamicFor starting at its
// dynamicForFailAt'th call (1-indexed; 0 disables) — some handlers call
// DynamicFor more than once per request (fetch via getUnstructured, then
// mutate), so a manager that always errors can only ever reach the first such
// call. fakeManager's own DynamicFor never errors at all.
type countedFailManager struct {
	*fakeManager
	dynamicForCalls  int
	dynamicForFailAt int

	resolveResourceCalls  int
	resolveResourceFailAt int
}

func (m *countedFailManager) DynamicFor(ctx string) (dynamic.Interface, error) {
	m.dynamicForCalls++
	if m.dynamicForFailAt != 0 && m.dynamicForCalls >= m.dynamicForFailAt {
		return nil, fmt.Errorf("dynamic client unavailable")
	}
	return m.fakeManager.DynamicFor(ctx)
}

// resolveResourceFailAt, if set, similarly fails ResolveResource from its
// Nth call onward — e.g. handleScaleResource/handleRestartRollout resolve
// the slug once inside getUnstructured (a fetch) and again directly (before
// the mutating Update), so a manager that always errors can only reach the
// first of those two calls.
func (m *countedFailManager) withResolveResourceFailAt(n int) *countedFailManager {
	m.resolveResourceFailAt = n
	return m
}

func (m *countedFailManager) ResolveResource(contextName, resource string) (kube.Resource, error) {
	m.resolveResourceCalls++
	if m.resolveResourceFailAt != 0 && m.resolveResourceCalls >= m.resolveResourceFailAt {
		return kube.Resource{}, fmt.Errorf("resource unresolvable")
	}
	return m.fakeManager.ResolveResource(contextName, resource)
}

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

func TestHandleGetManifest_DynamicForError(t *testing.T) {
	mgr := &countedFailManager{fakeManager: newFakeManager(), dynamicForFailAt: 1}
	s := NewServer(mgr, testConfigStore(t), "")
	rec := doRequest(t, s, "GET", "/api/contexts/test/manifest/deployment/prod/web", "")
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rec.Code)
	}
}

func TestHandleApplyManifest_BodyReadError(t *testing.T) {
	s := newTestServer(t)
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/contexts/test/manifest/deployment/prod/web", errReader{})
	s.Routes().ServeHTTP(rec, r)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestHandleApplyManifest_MalformedJSON(t *testing.T) {
	s := newTestServer(t)
	rec := doRequest(t, s, "PUT", "/api/contexts/test/manifest/deployment/prod/web", `not json`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestHandleApplyManifest_UnsupportedKind(t *testing.T) {
	s := newTestServer(t)
	rec := doRequest(t, s, "PUT", "/api/contexts/test/manifest/frobnicator/ns/name", `{"yaml":""}`)
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502 (unsupported kind)", rec.Code)
	}
}

func TestHandleApplyManifest_DynamicForError(t *testing.T) {
	mgr := &countedFailManager{fakeManager: newFakeManager(), dynamicForFailAt: 1}
	s := NewServer(mgr, testConfigStore(t), "")
	rec := doRequest(t, s, "PUT", "/api/contexts/test/manifest/deployment/prod/web", `{"yaml":""}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestHandleApplyManifest_MalformedYAML(t *testing.T) {
	s := newTestServer(t)
	body := `{"yaml":"foo: [1,2"}`
	rec := doRequest(t, s, "PUT", "/api/contexts/test/manifest/deployment/prod/web", body)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (malformed YAML)", rec.Code)
	}
}

// TestHandleApplyManifest_NonObjectYAML feeds YAML that parses fine but isn't
// an object (a top-level list instead) — YAMLToJSON succeeds, but
// unstructured.Unstructured.UnmarshalJSON then fails since it always expects
// a JSON object.
func TestHandleApplyManifest_NonObjectYAML(t *testing.T) {
	s := newTestServer(t)
	body := `{"yaml":"- a\n- b\n"}`
	rec := doRequest(t, s, "PUT", "/api/contexts/test/manifest/deployment/prod/web", body)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (non-object YAML)", rec.Code)
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
	dyn := fakeDynamic(t, s)
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
