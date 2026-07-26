package api

import (
	"encoding/json"
	"net/http"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestHandleCordonNode(t *testing.T) {
	s := newTestServer(t, &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}})

	rec := doRequest(t, s, "POST", "/api/contexts/test/cordon/node-1", `{"cordon":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "cordoned" {
		t.Errorf("status = %q, want %q", body["status"], "cordoned")
	}

	detail := doRequest(t, s, "GET", "/api/contexts/test/detail/node/-/node-1", "")
	var d resourceDetail
	if err := json.Unmarshal(detail.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	if d.Schedulable == nil || *d.Schedulable {
		t.Errorf("Schedulable = %v, want false after cordon", d.Schedulable)
	}
}

func TestHandleCordonNode_Uncordon(t *testing.T) {
	s := newTestServer(t, &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
		Spec:       corev1.NodeSpec{Unschedulable: true},
	})

	rec := doRequest(t, s, "POST", "/api/contexts/test/cordon/node-1", `{"cordon":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "uncordoned" {
		t.Errorf("status = %q, want %q", body["status"], "uncordoned")
	}

	detail := doRequest(t, s, "GET", "/api/contexts/test/detail/node/-/node-1", "")
	var d resourceDetail
	if err := json.Unmarshal(detail.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	if d.Schedulable == nil || !*d.Schedulable {
		t.Errorf("Schedulable = %v, want true after uncordon", d.Schedulable)
	}
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
