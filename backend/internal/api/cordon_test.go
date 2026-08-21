package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dynamic "k8s.io/client-go/dynamic"
	ktesting "k8s.io/client-go/testing"

	"github.com/robertobado/netsk8-navigator/backend/internal/config"
	"github.com/robertobado/netsk8-navigator/backend/internal/kube"
)

// doCordon posts the cordon toggle and returns the decoded response body.
func doCordon(t *testing.T, s *Server, name string, cordon bool) map[string]string {
	t.Helper()
	body := `{"cordon":false}`
	if cordon {
		body = `{"cordon":true}`
	}
	rec := doRequest(t, s, "POST", "/api/contexts/test/cordon/"+name, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// assertNodeSchedulable reads the node's detail and checks its Schedulable flag.
func assertNodeSchedulable(t *testing.T, s *Server, name string, want bool) {
	t.Helper()
	detail := doRequest(t, s, "GET", "/api/contexts/test/detail/node/-/"+name, "")
	var d resourceDetail
	if err := json.Unmarshal(detail.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	if d.Schedulable == nil || *d.Schedulable != want {
		t.Errorf("Schedulable = %v, want %v", d.Schedulable, want)
	}
}

func TestHandleCordonNode(t *testing.T) {
	s := newTestServer(t, &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}})

	out := doCordon(t, s, "node-1", true)
	if out["status"] != "cordoned" {
		t.Errorf("status = %q, want %q", out["status"], "cordoned")
	}
	assertNodeSchedulable(t, s, "node-1", false)
}

func TestHandleCordonNode_Uncordon(t *testing.T) {
	s := newTestServer(t, &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
		Spec:       corev1.NodeSpec{Unschedulable: true},
	})

	out := doCordon(t, s, "node-1", false)
	if out["status"] != "uncordoned" {
		t.Errorf("status = %q, want %q", out["status"], "uncordoned")
	}
	assertNodeSchedulable(t, s, "node-1", true)
}

func TestHandleCordonNode_UnknownNode(t *testing.T) {
	s := newTestServer(t)
	rec := doRequest(t, s, "POST", "/api/contexts/test/cordon/missing", `{"cordon":true}`)
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502 (node not found)", rec.Code)
	}
}

func TestHandleCordonNode_InvalidBody(t *testing.T) {
	s := newTestServer(t, &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}})
	rec := doRequest(t, s, "POST", "/api/contexts/test/cordon/node-1", `not-json`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// errReader always fails, letting a test exercise the io.ReadAll error
// branch — the same stand-in portforward_test.go's errReader uses for the
// same gap on a different handler.
type cordonErrReader struct{}

func (cordonErrReader) Read([]byte) (int, error) { return 0, fmt.Errorf("boom") }

func TestHandleCordonNode_BodyReadError(t *testing.T) {
	s := newTestServer(t, &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}})
	r := httptest.NewRequest("POST", "/api/contexts/test/cordon/node-1", io.NopCloser(cordonErrReader{}))
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, r)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// dynamicForErrOnceManager wraps *fakeManager but fails DynamicFor from its
// Nth call onward — handleCordonNode calls it twice (once inside
// getUnstructured, once directly for the Update), and only the *second*
// call's error branch (lines 48-52) is otherwise unreachable, since a first
// -call failure trips getUnstructured's own (already-covered) error path
// instead.
type dynamicForErrOnceManager struct {
	*fakeManager
	failFrom int
	calls    int
}

func (m *dynamicForErrOnceManager) DynamicFor(ctx string) (dynamic.Interface, error) {
	m.calls++
	if m.calls >= m.failFrom {
		return nil, fmt.Errorf("no dynamic client for context")
	}
	return m.fakeManager.DynamicFor(ctx)
}

func TestHandleCordonNode_SecondDynamicForCallFails(t *testing.T) {
	cfg := config.NewStoreAt(filepath.Join(t.TempDir(), "config.json"))
	mgr := &dynamicForErrOnceManager{fakeManager: newFakeManager(&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}}), failFrom: 2}
	s := NewServer(mgr, cfg, "")
	rec := doRequest(t, s, "POST", "/api/contexts/test/cordon/node-1", `{"cordon":true}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// resolveResourceErrOnceManager is the same shape, for resolveSlug's second
// call (lines 53-56) — the first call happens inside getUnstructured too.
type resolveResourceErrOnceManager struct {
	*fakeManager
	failFrom int
	calls    int
}

func (m *resolveResourceErrOnceManager) ResolveResource(ctx, resource string) (kube.Resource, error) {
	m.calls++
	if m.calls >= m.failFrom {
		return kube.Resource{}, fmt.Errorf("no resource mapping")
	}
	return m.fakeManager.ResolveResource(ctx, resource)
}

func TestHandleCordonNode_SecondResolveSlugCallFails(t *testing.T) {
	cfg := config.NewStoreAt(filepath.Join(t.TempDir(), "config.json"))
	mgr := &resolveResourceErrOnceManager{fakeManager: newFakeManager(&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}}), failFrom: 2}
	s := NewServer(mgr, cfg, "")
	rec := doRequest(t, s, "POST", "/api/contexts/test/cordon/node-1", `{"cordon":true}`)
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rec.Code)
	}
}

// TestHandleCordonNode_SetNestedFieldError covers the SetNestedField error
// branch: seeding the fake dynamic client with a raw unstructured Node whose
// "spec" is a scalar (not a map) makes SetNestedField(obj, ..., "spec",
// "unschedulable") fail, since it can't descend into a non-map to set the
// nested key.
func TestHandleCordonNode_SetNestedFieldError(t *testing.T) {
	node := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "Node",
		"metadata":   map[string]any{"name": "node-1"},
		"spec":       "not-a-map",
	}}
	s := newTestServer(t, node)
	rec := doRequest(t, s, "POST", "/api/contexts/test/cordon/node-1", `{"cordon":true}`)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestHandleCordonNode_UpdateFails(t *testing.T) {
	s := newTestServer(t, &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}})
	dyn := fakeDynamic(t, s)
	dyn.PrependReactor("update", "nodes", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("conflict")
	})
	rec := doRequest(t, s, "POST", "/api/contexts/test/cordon/node-1", `{"cordon":true}`)
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502 (Update failed)", rec.Code)
	}
}
