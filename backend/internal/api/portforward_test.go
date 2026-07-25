package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

// The real dial path needs a live SPDY upgrade against an actual kubelet —
// same limitation podexec.go has (no test file of its own, for the same
// reason). What IS testable without a cluster: request validation, and the
// session bookkeeping (list/stop) against a map seeded directly.

func TestHandleStartPortForward_RejectsInvalidPort(t *testing.T) {
	s := newTestServer(t)
	for _, body := range []string{`{"port":0}`, `{"port":-1}`, `{"port":70000}`, `{}`} {
		rec := doRequest(t, s, "POST", "/api/contexts/test/portforward/ns/web", body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("body=%s: status = %d, want 400", body, rec.Code)
		}
	}
}

func TestHandleListPortForwards(t *testing.T) {
	s := newTestServer(t)
	s.pf["abc"] = &pfSession{namespace: "prod", pod: "web-1", port: 8080, localPort: 54321, stopCh: make(chan struct{})}

	rec := doRequest(t, s, "GET", "/api/contexts/test/portforward", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var out []portForwardView
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].ID != "abc" || out[0].LocalPort != 54321 || out[0].Pod != "web-1" {
		t.Errorf("got %+v", out)
	}
}

func TestHandleStopPortForward(t *testing.T) {
	s := newTestServer(t)
	stopCh := make(chan struct{})
	s.pf["abc"] = &pfSession{namespace: "prod", pod: "web-1", port: 8080, localPort: 54321, stopCh: stopCh}

	rec := doRequest(t, s, "DELETE", "/api/contexts/test/portforward/abc", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if _, ok := s.pf["abc"]; ok {
		t.Error("session should have been removed from the map")
	}
	select {
	case <-stopCh:
	default:
		t.Error("stopCh should have been closed")
	}
}

func TestHandleStopPortForward_UnknownID(t *testing.T) {
	s := newTestServer(t)
	rec := doRequest(t, s, "DELETE", "/api/contexts/test/portforward/nope", "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}
