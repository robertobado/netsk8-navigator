package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPrintUsage(t *testing.T) {
	var buf bytes.Buffer
	printUsage(&buf)
	out := buf.String()
	for _, want := range []string{"--mcp-stdio", "--mcp-allow-write", "mcp install", "--allow-write", "--version", "--help"} {
		if !strings.Contains(out, want) {
			t.Errorf("usage text missing %q:\n%s", want, out)
		}
	}
}

func TestBootstrapRedirect(t *testing.T) {
	h := bootstrapRedirect("http://127.0.0.1:54321/")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `location.replace("http://127.0.0.1:54321/")`) {
		t.Errorf("body = %q, want a client-side redirect to the given URL", body)
	}
}

// startServer isn't parameterized the way backend/main.go's buildMux is (it
// calls mustInit() and web.Handler() internally, the latter log.Fatal-ing
// when no embedded frontend build exists — true in the CI backend job, which
// never runs `pnpm build` first), so only startServer's own socket-serving
// logic is safely testable in isolation, against a fake handler instead of
// the real buildMux() output.
func TestStartServer(t *testing.T) {
	called := false
	addr := startServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	if addr == "" {
		t.Fatal("expected a non-empty listen address")
	}

	resp, err := http.Get("http://" + addr + "/")
	if err != nil {
		t.Fatalf("GET %s: %v", addr, err)
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if !called {
		t.Error("expected the handler passed to startServer to have been invoked")
	}
}
