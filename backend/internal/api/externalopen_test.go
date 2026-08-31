package api

import (
	"net/http"
	"testing"
)

func TestHandleOpenExternal_NoOpenerWired(t *testing.T) {
	s := newTestServer(t) // SetExternalOpener never called — plain server/browser build
	rec := doRequest(t, s, "POST", "/api/open-external", `{"url":"https://example.com"}`)
	if rec.Code != http.StatusNotImplemented {
		t.Errorf("status = %d, want 501", rec.Code)
	}
}

func TestHandleOpenExternal_CallsOpener(t *testing.T) {
	s := newTestServer(t)
	var got string
	s.SetExternalOpener(func(url string) { got = url })

	rec := doRequest(t, s, "POST", "/api/open-external", `{"url":"https://example.com/releases"}`)

	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", rec.Code)
	}
	if got != "https://example.com/releases" {
		t.Errorf("opener called with %q, want the request's url", got)
	}
}

func TestHandleOpenExternal_RejectsNonHTTPScheme(t *testing.T) {
	s := newTestServer(t)
	called := false
	s.SetExternalOpener(func(string) { called = true })

	rec := doRequest(t, s, "POST", "/api/open-external", `{"url":"file:///etc/passwd"}`)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	if called {
		t.Error("opener was called for a non-http(s) URL")
	}
}

func TestHandleOpenExternal_RejectsMalformedBody(t *testing.T) {
	s := newTestServer(t)
	s.SetExternalOpener(func(string) {})

	rec := doRequest(t, s, "POST", "/api/open-external", `not json`)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}
