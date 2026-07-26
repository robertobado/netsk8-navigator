package api

import (
	"encoding/json"
	"net/http"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
