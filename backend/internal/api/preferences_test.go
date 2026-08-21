package api

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/robertobado/netsk8-navigator/backend/internal/config"
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

// brokenStoreServer builds a Server whose config.Store can never persist —
// its path sits under a regular file, so save()'s os.MkdirAll always fails —
// to exercise every handler's cfg-write-error branch (500), which a normal
// t.TempDir()-backed store (newTestServer) never hits.
func brokenStoreServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.NewStoreAt(filepath.Join(blocker, "sub", "config.json"))
	return NewServer(newFakeManager(), cfg, "")
}

func TestHandlePutAppPrefs_SaveError(t *testing.T) {
	s := brokenStoreServer(t)
	rec := doRequest(t, s, "PUT", "/api/preferences", `{"language":"en"}`)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestHandlePutClusterPrefs_SaveError(t *testing.T) {
	s := brokenStoreServer(t)
	rec := doRequest(t, s, "PUT", "/api/contexts/prod/preferences", `{"namespace":"default"}`)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestHandleGetMCPToken_Error(t *testing.T) {
	s := brokenStoreServer(t)
	rec := doRequest(t, s, "GET", "/api/mcp/token", "")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestHandleRegenerateMCPToken_Error(t *testing.T) {
	s := brokenStoreServer(t)
	rec := doRequest(t, s, "POST", "/api/mcp/token/regenerate", "")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestHandleGetMCPToken_Success(t *testing.T) {
	s := newTestServer(t)
	rec := doRequest(t, s, "GET", "/api/mcp/token", "")
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleRegenerateMCPToken_Success(t *testing.T) {
	s := newTestServer(t)
	rec := doRequest(t, s, "POST", "/api/mcp/token/regenerate", "")
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
}

// errReader always fails, letting a test exercise readJSONBody's
// io.ReadAll error branch — something doRequest's plain string body can
// never trigger (same stand-in portforward_test.go/cordon_test.go use for
// the same gap on other handlers).
type prefsErrReader struct{}

func (prefsErrReader) Read([]byte) (int, error) { return 0, fmt.Errorf("boom") }

func TestHandlePutAppPrefs_BodyReadError(t *testing.T) {
	s := newTestServer(t)
	r := httptest.NewRequest("PUT", "/api/preferences", io.NopCloser(prefsErrReader{}))
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, r)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}
