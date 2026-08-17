package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"k8s.io/client-go/kubernetes"

	"github.com/robertobado/netsk8-navigator/backend/internal/config"
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

// errReader always fails, letting a test exercise the io.ReadAll error branch
// in handleStartPortForward — something doRequest's plain string body can
// never trigger.
type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, fmt.Errorf("boom") }

func TestHandleStartPortForward_BodyReadError(t *testing.T) {
	s := newTestServer(t)
	r := httptest.NewRequest("POST", "/api/contexts/test/portforward/ns/web", io.NopCloser(errReader{}))
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, r)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// clientForErrManager wraps a *fakeManager but fails ClientFor, to exercise
// handleStartPortForward's ClientFor error branch — fakeManager's own
// ClientFor never errors, so newTestServer alone can't reach it.
type clientForErrManager struct {
	*fakeManager
}

func (clientForErrManager) ClientFor(string) (kubernetes.Interface, error) {
	return nil, fmt.Errorf("no client for context")
}

func TestHandleStartPortForward_ClientForError(t *testing.T) {
	cfg := config.NewStoreAt(filepath.Join(t.TempDir(), "config.json"))
	s := NewServer(clientForErrManager{newFakeManager()}, cfg, "")
	rec := doRequest(t, s, "POST", "/api/contexts/test/portforward/ns/web", `{"port":8080}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestHandleStartPortForward_MalformedBody(t *testing.T) {
	s := newTestServer(t)
	rec := doRequest(t, s, "POST", "/api/contexts/test/portforward/ns/web", `not json`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// A valid port gets past validation and s.mgr.ClientFor (fakeManager never
// errors there), then fails fast at s.mgr.RESTConfigFor — fakeManager always
// errors there since a real tunnel needs a live kubelet, same limitation the
// package header comment already calls out for the SPDY dial itself.
func TestHandleStartPortForward_RunFailsWithoutLiveCluster(t *testing.T) {
	s := newTestServer(t)
	rec := doRequest(t, s, "POST", "/api/contexts/test/portforward/ns/web", `{"port":8080}`)
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rec.Code)
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

// In DemoMode there's no real kubelet to attach a tunnel to (e.g. behind a
// kwok-simulated cluster), so every port-forward endpoint short-circuits
// with 403 before touching s.pf or the cluster at all.
func TestPortForward_BlockedInDemoMode(t *testing.T) {
	s := newTestServer(t)
	s.DemoMode = true
	s.pf["abc"] = &pfSession{namespace: "prod", pod: "web-1", port: 8080, localPort: 54321, stopCh: make(chan struct{})}

	cases := []struct {
		method, path, body string
	}{
		{"POST", "/api/contexts/test/portforward/ns/web", `{"port":8080}`},
		{"GET", "/api/contexts/test/portforward", ""},
		{"DELETE", "/api/contexts/test/portforward/abc", ""},
	}
	for _, c := range cases {
		rec := doRequest(t, s, c.method, c.path, c.body)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s: status = %d, want 403", c.method, c.path, rec.Code)
		}
	}
	if _, ok := s.pf["abc"]; !ok {
		t.Error("existing session should be untouched when blocked in demo mode")
	}
}
