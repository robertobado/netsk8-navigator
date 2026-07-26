package api

import (
	"net/http"
	"testing"
)

// The real exec path needs a live SPDY/websocket upgrade against an actual
// kubelet, so it isn't otherwise covered by these hermetic tests. DemoMode's
// short-circuit, though, returns before any of that — fully testable.
func TestHandlePodExec_BlockedInDemoMode(t *testing.T) {
	s := newTestServer(t)
	s.DemoMode = true
	rec := doRequest(t, s, "GET", "/api/contexts/test/pods/ns/web/exec", "")
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}
