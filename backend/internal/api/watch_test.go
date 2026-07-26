package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/robertobado/netsk8-navigator/backend/internal/kube"
)

func TestHandlePodWatch_WatcherUnavailable(t *testing.T) {
	// fakeManager.PodWatcherFor always errors (no live-watch support in tests,
	// same limitation as podexec.go/podlogs.go) — this just exercises the
	// guard clause that runs before the streaming loop starts.
	s := newTestServer(t)
	rec := doRequest(t, s, "GET", "/api/contexts/test/watch/pods", "")
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rec.Code)
	}
}

func TestWriteSSE(t *testing.T) {
	rec := httptest.NewRecorder()
	writeSSE(rec, kube.Event{Type: "SYNCED"})
	body := rec.Body.String()
	if !strings.HasPrefix(body, "data: ") || !strings.HasSuffix(body, "\n\n") {
		t.Errorf("got %q, want an SSE-framed data line", body)
	}
	if !strings.Contains(body, `"type":"SYNCED"`) {
		t.Errorf("got %q, want the event JSON-encoded", body)
	}
}
