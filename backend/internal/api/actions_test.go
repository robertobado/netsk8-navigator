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
	"k8s.io/apimachinery/pkg/runtime"
	ktesting "k8s.io/client-go/testing"

	"github.com/robertobado/netsk8-navigator/backend/internal/kube"
)

func TestHandleDeleteResource(t *testing.T) {
	s := newTestServer(t, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "prod"},
		Spec:       appsv1.DeploymentSpec{Replicas: replicas(2)},
	})
	rec := doRequest(t, s, "DELETE", "/api/contexts/test/manifest/deployment/prod/web", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}

	rec2 := doRequest(t, s, "GET", "/api/contexts/test/resources/deployments?namespace=prod", "")
	var out []kube.DeploymentView
	if err := json.Unmarshal(rec2.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Errorf("after delete, got %d deployments, want 0", len(out))
	}
}

func TestHandleDeleteResource_UnknownKind(t *testing.T) {
	s := newTestServer(t)
	rec := doRequest(t, s, "DELETE", "/api/contexts/test/manifest/notaresource/ns/name", "")
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502 (unsupported kind)", rec.Code)
	}
}

func TestHandleScaleResource(t *testing.T) {
	s := newTestServer(t, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "prod"},
		Spec:       appsv1.DeploymentSpec{Replicas: replicas(2)},
	})
	rec := doRequest(t, s, "PUT", "/api/contexts/test/scale/deployment/prod/web", `{"replicas":5}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}

	rec2 := doRequest(t, s, "GET", "/api/contexts/test/resources/deployments?namespace=prod", "")
	var out []kube.DeploymentView
	if err := json.Unmarshal(rec2.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Ready != "0/5" {
		t.Errorf("after scale, got %+v, want replicas=5 reflected", out)
	}
}

func TestHandleScaleResource_RejectsNonScalableKind(t *testing.T) {
	s := newTestServer(t)
	rec := doRequest(t, s, "PUT", "/api/contexts/test/scale/service/prod/web", `{"replicas":3}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (service isn't scalable)", rec.Code)
	}
}

func TestHandleScaleResource_RejectsNegativeReplicas(t *testing.T) {
	s := newTestServer(t, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "prod"},
		Spec:       appsv1.DeploymentSpec{Replicas: replicas(2)},
	})
	rec := doRequest(t, s, "PUT", "/api/contexts/test/scale/deployment/prod/web", `{"replicas":-1}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (negative replicas)", rec.Code)
	}
}

func TestHandleScaleResource_RejectsMissingReplicas(t *testing.T) {
	s := newTestServer(t, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "prod"},
		Spec:       appsv1.DeploymentSpec{Replicas: replicas(2)},
	})
	rec := doRequest(t, s, "PUT", "/api/contexts/test/scale/deployment/prod/web", `{}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (replicas missing)", rec.Code)
	}
}

func TestHandleRestartRollout(t *testing.T) {
	s := newTestServer(t, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "prod"},
		Spec:       appsv1.DeploymentSpec{Replicas: replicas(2)},
	})
	rec := doRequest(t, s, "POST", "/api/contexts/test/rollout-restart/deployment/prod/web", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}

	rec2 := doRequest(t, s, "GET", "/api/contexts/test/manifest/deployment/prod/web", "")
	var out map[string]string
	if err := json.Unmarshal(rec2.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out["yaml"], "kubectl.kubernetes.io/restartedAt") {
		t.Errorf("expected restartedAt annotation in manifest, got:\n%s", out["yaml"])
	}
}

func TestHandleRestartRollout_RejectsNonRestartableKind(t *testing.T) {
	s := newTestServer(t)
	rec := doRequest(t, s, "POST", "/api/contexts/test/rollout-restart/service/prod/web", "")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (service can't be restarted)", rec.Code)
	}
}

func TestHandleDeleteResource_DeleteFails(t *testing.T) {
	s := newTestServer(t, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "prod"},
		Spec:       appsv1.DeploymentSpec{Replicas: replicas(2)},
	})
	dyn := fakeDynamic(t, s)
	dyn.PrependReactor("delete", "deployments", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("etcd unavailable")
	})
	rec := doRequest(t, s, "DELETE", "/api/contexts/test/manifest/deployment/prod/web", "")
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502 (Delete failed)", rec.Code)
	}
}

func TestHandleScaleResource_InvalidJSON(t *testing.T) {
	s := newTestServer(t, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "prod"},
		Spec:       appsv1.DeploymentSpec{Replicas: replicas(2)},
	})
	rec := doRequest(t, s, "PUT", "/api/contexts/test/scale/deployment/prod/web", `not-json`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (malformed JSON body)", rec.Code)
	}
}

func TestHandleScaleResource_ResourceNotFound(t *testing.T) {
	s := newTestServer(t) // no deployment seeded
	rec := doRequest(t, s, "PUT", "/api/contexts/test/scale/deployment/prod/web", `{"replicas":5}`)
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502 (resource doesn't exist)", rec.Code)
	}
}

func TestHandleScaleResource_UpdateFails(t *testing.T) {
	s := newTestServer(t, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "prod"},
		Spec:       appsv1.DeploymentSpec{Replicas: replicas(2)},
	})
	dyn := fakeDynamic(t, s)
	dyn.PrependReactor("update", "deployments", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("conflict")
	})
	rec := doRequest(t, s, "PUT", "/api/contexts/test/scale/deployment/prod/web", `{"replicas":5}`)
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502 (Update failed)", rec.Code)
	}
}

func TestHandleRestartRollout_ResourceNotFound(t *testing.T) {
	s := newTestServer(t) // no deployment seeded
	rec := doRequest(t, s, "POST", "/api/contexts/test/rollout-restart/deployment/prod/web", "")
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502 (resource doesn't exist)", rec.Code)
	}
}

func TestHandleDeleteResource_DynamicForError(t *testing.T) {
	mgr := &countedFailManager{fakeManager: newFakeManager(), dynamicForFailAt: 1}
	s := NewServer(mgr, testConfigStore(t), "")
	rec := doRequest(t, s, "DELETE", "/api/contexts/test/manifest/deployment/prod/web", "")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestHandleScaleResource_BodyReadError(t *testing.T) {
	s := newTestServer(t)
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/contexts/test/scale/deployment/prod/web", errReader{})
	s.Routes().ServeHTTP(rec, r)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// TestHandleScaleResource_DynamicForError needs a resource to exist so
// getUnstructured's own DynamicFor call (the first) succeeds, isolating the
// handler's second DynamicFor call (for the Update) as the one that fails.
func TestHandleScaleResource_DynamicForError(t *testing.T) {
	mgr := &countedFailManager{
		fakeManager: newFakeManager(&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "prod"},
			Spec:       appsv1.DeploymentSpec{Replicas: replicas(2)},
		}),
		dynamicForFailAt: 2,
	}
	s := NewServer(mgr, testConfigStore(t), "")
	rec := doRequest(t, s, "PUT", "/api/contexts/test/scale/deployment/prod/web", `{"replicas":5}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// TestHandleScaleResource_ResolveSlugError isolates handleScaleResource's own
// resolveSlug call (the second — the first is inside getUnstructured) as the
// one that fails.
func TestHandleScaleResource_ResolveSlugError(t *testing.T) {
	mgr := (&countedFailManager{
		fakeManager: newFakeManager(&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "prod"},
			Spec:       appsv1.DeploymentSpec{Replicas: replicas(2)},
		}),
	}).withResolveResourceFailAt(2)
	s := NewServer(mgr, testConfigStore(t), "")
	rec := doRequest(t, s, "PUT", "/api/contexts/test/scale/deployment/prod/web", `{"replicas":5}`)
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rec.Code)
	}
}

func TestHandleRestartRollout_DynamicForError(t *testing.T) {
	mgr := &countedFailManager{
		fakeManager: newFakeManager(&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "prod"},
			Spec:       appsv1.DeploymentSpec{Replicas: replicas(2)},
		}),
		dynamicForFailAt: 2,
	}
	s := NewServer(mgr, testConfigStore(t), "")
	rec := doRequest(t, s, "POST", "/api/contexts/test/rollout-restart/deployment/prod/web", "")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestHandleRestartRollout_ResolveSlugError(t *testing.T) {
	mgr := (&countedFailManager{
		fakeManager: newFakeManager(&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "prod"},
			Spec:       appsv1.DeploymentSpec{Replicas: replicas(2)},
		}),
	}).withResolveResourceFailAt(2)
	s := NewServer(mgr, testConfigStore(t), "")
	rec := doRequest(t, s, "POST", "/api/contexts/test/rollout-restart/deployment/prod/web", "")
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rec.Code)
	}
}

func TestHandleRestartRollout_UpdateFails(t *testing.T) {
	s := newTestServer(t, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "prod"},
		Spec:       appsv1.DeploymentSpec{Replicas: replicas(2)},
	})
	dyn := fakeDynamic(t, s)
	dyn.PrependReactor("update", "deployments", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("conflict")
	})
	rec := doRequest(t, s, "POST", "/api/contexts/test/rollout-restart/deployment/prod/web", "")
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502 (Update failed)", rec.Code)
	}
}

// TestDeleteQueryOptions is the pure-function unit test for every
// kubectl-delete-style switch delete_resource/delete_crd_resource expose:
// cascade (with its default and validation), gracePeriodSeconds, force, and
// dryRun. ignoreNotFound is deliberately excluded from DeleteOptions itself
// (see TestHandleDeleteResource_IgnoreNotFound) since it changes error
// handling after the call, not the options passed into it.
func TestDeleteQueryOptions(t *testing.T) {
	propagation := func(query string) metav1.DeletionPropagation {
		r := httptest.NewRequest(http.MethodDelete, "/x?"+query, nil)
		opts, _, err := deleteQueryOptions(r)
		if err != nil {
			t.Fatalf("deleteQueryOptions(%q): unexpected error: %v", query, err)
		}
		if opts.PropagationPolicy == nil {
			t.Fatalf("deleteQueryOptions(%q): PropagationPolicy is nil", query)
		}
		return *opts.PropagationPolicy
	}

	if got := propagation(""); got != metav1.DeletePropagationBackground {
		t.Errorf("default cascade = %q, want background (kubectl's own default)", got)
	}
	if got := propagation("cascade=background"); got != metav1.DeletePropagationBackground {
		t.Errorf("cascade=background = %q, want background", got)
	}
	if got := propagation("cascade=foreground"); got != metav1.DeletePropagationForeground {
		t.Errorf("cascade=foreground = %q, want foreground", got)
	}
	if got := propagation("cascade=orphan"); got != metav1.DeletePropagationOrphan {
		t.Errorf("cascade=orphan = %q, want orphan", got)
	}

	if _, _, err := deleteQueryOptions(httptest.NewRequest(http.MethodDelete, "/x?cascade=bogus", nil)); err == nil {
		t.Error("cascade=bogus should be rejected, got no error")
	}
	if _, _, err := deleteQueryOptions(httptest.NewRequest(http.MethodDelete, "/x?gracePeriodSeconds=notanumber", nil)); err == nil {
		t.Error("gracePeriodSeconds=notanumber should be rejected, got no error")
	}

	r := httptest.NewRequest(http.MethodDelete, "/x?gracePeriodSeconds=30", nil)
	opts, _, err := deleteQueryOptions(r)
	if err != nil {
		t.Fatalf("gracePeriodSeconds=30: unexpected error: %v", err)
	}
	if opts.GracePeriodSeconds == nil || *opts.GracePeriodSeconds != 30 {
		t.Errorf("GracePeriodSeconds = %v, want 30", opts.GracePeriodSeconds)
	}

	r = httptest.NewRequest(http.MethodDelete, "/x?force=true", nil)
	opts, _, err = deleteQueryOptions(r)
	if err != nil {
		t.Fatalf("force=true: unexpected error: %v", err)
	}
	if opts.GracePeriodSeconds == nil || *opts.GracePeriodSeconds != 0 {
		t.Errorf("force=true GracePeriodSeconds = %v, want 0 (immediate)", opts.GracePeriodSeconds)
	}

	// force wins even if a non-zero gracePeriodSeconds was also given —
	// mirrors kubectl delete --force --grace-period=<n> (n is ignored).
	r = httptest.NewRequest(http.MethodDelete, "/x?gracePeriodSeconds=30&force=true", nil)
	opts, _, err = deleteQueryOptions(r)
	if err != nil {
		t.Fatalf("gracePeriodSeconds=30&force=true: unexpected error: %v", err)
	}
	if opts.GracePeriodSeconds == nil || *opts.GracePeriodSeconds != 0 {
		t.Errorf("force=true should override gracePeriodSeconds, got %v", opts.GracePeriodSeconds)
	}

	r = httptest.NewRequest(http.MethodDelete, "/x?dryRun=true", nil)
	opts, ignoreNotFound, err := deleteQueryOptions(r)
	if err != nil {
		t.Fatalf("dryRun=true: unexpected error: %v", err)
	}
	if len(opts.DryRun) == 0 {
		t.Error("dryRun=true should set DeleteOptions.DryRun")
	}
	if ignoreNotFound {
		t.Error("ignoreNotFound should default to false")
	}

	_, ignoreNotFound, err = deleteQueryOptions(httptest.NewRequest(http.MethodDelete, "/x?ignoreNotFound=true", nil))
	if err != nil {
		t.Fatalf("ignoreNotFound=true: unexpected error: %v", err)
	}
	if !ignoreNotFound {
		t.Error("ignoreNotFound=true should report true")
	}
}

// TestHandleDeleteResource_DryRun proves ?dryRun=true validates the delete
// server-side without actually removing the resource — the same contract
// handleApplyManifest already offers for updates. The fake dynamic client
// (unlike a real API server) doesn't implement dry-run itself — it deletes
// for real regardless of DeleteOptions.DryRun — so a reactor mimics real
// dry-run semantics here, exactly like TestHandleApplyManifest_DryRunDoesNotPersist
// already does for Update.
func TestHandleDeleteResource_DryRun(t *testing.T) {
	s := newTestServer(t, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "prod"},
		Spec:       appsv1.DeploymentSpec{Replicas: replicas(2)},
	})
	dyn := fakeDynamic(t, s)
	dyn.PrependReactor("delete", "deployments", func(action ktesting.Action) (bool, runtime.Object, error) {
		da, ok := action.(ktesting.DeleteActionImpl)
		if !ok || len(da.GetDeleteOptions().DryRun) == 0 {
			return false, nil, nil // not a dry-run — defer to the default reactor, which deletes for real
		}
		return true, nil, nil // dry-run: report success without touching the tracker
	})
	rec := doRequest(t, s, "DELETE", "/api/contexts/test/manifest/deployment/prod/web?dryRun=true", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["status"] != "would-delete" {
		t.Errorf(`status = %q, want "would-delete"`, out["status"])
	}

	rec2 := doRequest(t, s, "GET", "/api/contexts/test/resources/deployments?namespace=prod", "")
	var deps []kube.DeploymentView
	if err := json.Unmarshal(rec2.Body.Bytes(), &deps); err != nil {
		t.Fatal(err)
	}
	if len(deps) != 1 {
		t.Errorf("dryRun delete removed the resource: got %d deployments, want 1", len(deps))
	}
}

// TestHandleDeleteResource_IgnoreNotFound proves ?ignoreNotFound=true turns
// deleting an already-gone resource into a 200, like kubectl delete
// --ignore-not-found — the idempotent-cleanup case agents actually need.
func TestHandleDeleteResource_IgnoreNotFound(t *testing.T) {
	s := newTestServer(t)

	rec := doRequest(t, s, "DELETE", "/api/contexts/test/manifest/deployment/prod/ghost", "")
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("without ignoreNotFound, status = %d, want 502", rec.Code)
	}

	rec2 := doRequest(t, s, "DELETE", "/api/contexts/test/manifest/deployment/prod/ghost?ignoreNotFound=true", "")
	if rec2.Code != http.StatusOK {
		t.Fatalf("with ignoreNotFound, status = %d, body=%s, want 200", rec2.Code, rec2.Body.String())
	}
}

// TestHandleDeleteResource_InvalidCascade proves a bogus cascade value is
// rejected with 400 rather than silently falling back to a default.
func TestHandleDeleteResource_InvalidCascade(t *testing.T) {
	s := newTestServer(t, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "prod"},
		Spec:       appsv1.DeploymentSpec{Replicas: replicas(2)},
	})
	rec := doRequest(t, s, "DELETE", "/api/contexts/test/manifest/deployment/prod/web?cascade=bogus", "")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// dryRunUpdateReactor mimics real dry-run semantics for Update on the fake
// dynamic client, which (unlike a real API server) persists regardless of
// UpdateOptions.DryRun — see TestHandleApplyManifest_DryRunDoesNotPersist.
func dryRunUpdateReactor(t *testing.T, s *Server, resource string) {
	t.Helper()
	fakeDynamic(t, s).PrependReactor("update", resource, func(action ktesting.Action) (bool, runtime.Object, error) {
		ua, ok := action.(ktesting.UpdateActionImpl)
		if !ok || len(ua.GetUpdateOptions().DryRun) == 0 {
			return false, nil, nil
		}
		return true, ua.GetObject(), nil
	})
}

// TestHandleScaleResource_DryRun and TestHandleRestartRollout_DryRun prove
// the same ?dryRun=true contract on the other two Update-based write tools:
// the server validates but the live object is untouched.
func TestHandleScaleResource_DryRun(t *testing.T) {
	s := newTestServer(t, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "prod"},
		Spec:       appsv1.DeploymentSpec{Replicas: replicas(2)},
	})
	dryRunUpdateReactor(t, s, "deployments")
	rec := doRequest(t, s, "PUT", "/api/contexts/test/scale/deployment/prod/web?dryRun=true", `{"replicas":9}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}

	rec2 := doRequest(t, s, "GET", "/api/contexts/test/resources/deployments?namespace=prod", "")
	var out []kube.DeploymentView
	if err := json.Unmarshal(rec2.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Ready != "0/2" {
		t.Errorf("dryRun scale changed the live resource: got %+v, want replicas still 2", out)
	}
}

func TestHandleRestartRollout_DryRun(t *testing.T) {
	s := newTestServer(t, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "prod"},
		Spec:       appsv1.DeploymentSpec{Replicas: replicas(2)},
	})
	dryRunUpdateReactor(t, s, "deployments")
	rec := doRequest(t, s, "POST", "/api/contexts/test/rollout-restart/deployment/prod/web?dryRun=true", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}

	rec2 := doRequest(t, s, "GET", "/api/contexts/test/manifest/deployment/prod/web", "")
	var out map[string]string
	if err := json.Unmarshal(rec2.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out["yaml"], "kubectl.kubernetes.io/restartedAt") {
		t.Errorf("dryRun restart left the restartedAt annotation on the live resource:\n%s", out["yaml"])
	}
}
