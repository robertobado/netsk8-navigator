package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHandleAppEvents_StreamsBroadcastEvent(t *testing.T) {
	s := newTestServer(t)

	ctx, cancel := context.WithCancel(context.Background())
	r := httptest.NewRequest("GET", "/api/app-events", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	go func() {
		// Give handleAppEvents time to subscribe before broadcasting, same
		// as TestHandlePodWatch_StreamsSnapshotThenLiveEvents does for its
		// live delta.
		time.Sleep(100 * time.Millisecond)
		s.BroadcastAppEvent("show-about")
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	s.Routes().ServeHTTP(rec, r)

	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	if body := rec.Body.String(); !strings.Contains(body, "data: show-about\n\n") {
		t.Errorf("body = %q, want the broadcast event framed as SSE", body)
	}
}

func TestHandleAppEvents_NotFlusher(t *testing.T) {
	s := newTestServer(t)
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/app-events", nil)
	s.Routes().ServeHTTP(noFlushWriter{rec}, r)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

// A BroadcastAppEvent call with nobody subscribed (or a subscriber that
// isn't draining its channel) must never block the caller — a Wails menu
// callback on the app's main thread.
func TestBroadcastAppEvent_NoSubscribersNeverBlocks(t *testing.T) {
	s := newTestServer(t)
	done := make(chan struct{})
	go func() {
		s.BroadcastAppEvent("show-about")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("BroadcastAppEvent blocked with no subscribers")
	}
}
