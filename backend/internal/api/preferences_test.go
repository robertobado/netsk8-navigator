package api

import (
	"net/http"
	"testing"
)

func TestHandleAppPrefs_RoundTrip(t *testing.T) {
	s := newTestServer(t)

	rec := doRequest(t, s, "GET", "/api/preferences", "")
	if rec.Code != http.StatusOK || rec.Body.String() != "{}" {
		t.Fatalf("initial GET = %d %s, want 200 {}", rec.Code, rec.Body.String())
	}

	rec = doRequest(t, s, "PUT", "/api/preferences", `{"language":"en"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != `{"language":"en"}` {
		t.Errorf("PUT echoed body = %s", rec.Body.String())
	}

	rec = doRequest(t, s, "GET", "/api/preferences", "")
	if rec.Body.String() != `{"language":"en"}` {
		t.Errorf("GET after PUT = %s, want it to persist", rec.Body.String())
	}
}

func TestHandleAppPrefs_InvalidJSON(t *testing.T) {
	s := newTestServer(t)
	rec := doRequest(t, s, "PUT", "/api/preferences", `{not json`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestHandleClusterPrefs_RoundTrip(t *testing.T) {
	s := newTestServer(t)

	rec := doRequest(t, s, "GET", "/api/contexts/prod/preferences", "")
	if rec.Code != http.StatusOK || rec.Body.String() != "{}" {
		t.Fatalf("initial GET = %d %s, want 200 {}", rec.Code, rec.Body.String())
	}

	rec = doRequest(t, s, "PUT", "/api/contexts/prod/preferences", `{"namespace":"default"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body=%s", rec.Code, rec.Body.String())
	}

	rec = doRequest(t, s, "GET", "/api/contexts/prod/preferences", "")
	if rec.Body.String() != `{"namespace":"default"}` {
		t.Errorf("GET after PUT = %s", rec.Body.String())
	}

	// A different context's prefs must stay isolated.
	rec = doRequest(t, s, "GET", "/api/contexts/staging/preferences", "")
	if rec.Body.String() != "{}" {
		t.Errorf("unrelated context GET = %s, want {}", rec.Body.String())
	}
}

func TestHandleClusterPrefs_InvalidJSON(t *testing.T) {
	s := newTestServer(t)
	rec := doRequest(t, s, "PUT", "/api/contexts/prod/preferences", `not json at all`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}
