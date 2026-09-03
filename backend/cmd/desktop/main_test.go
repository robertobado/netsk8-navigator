package main

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robertobado/netsk8-navigator/backend/internal/config"
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

// freePort grabs an OS-assigned loopback port and immediately releases it, so
// the caller can hand it to code under test as a "known free" port. Standard
// Go net-test idiom; the tiny reuse window is acceptable here.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("probe listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}

func newTestStore(t *testing.T) *config.Store {
	t.Helper()
	return config.NewStoreAt(filepath.Join(t.TempDir(), "config.json"))
}

// startServer isn't parameterized the way backend/main.go's buildMux is (it
// calls mustInit() and web.Handler() internally, the latter log.Fatal-ing
// when no embedded frontend build exists — true in the CI backend job, which
// never runs `pnpm build` first), so only startServer's own socket-serving
// logic is safely testable in isolation, against a fake handler instead of
// the real buildMux() output.
func TestStartServer(t *testing.T) {
	// Seed a persisted port so startServer binds a deterministic, known-free
	// one instead of racing the real preferredDesktopPort.
	cfg := newTestStore(t)
	if err := cfg.SetDesktopPort(freePort(t)); err != nil {
		t.Fatalf("seed port: %v", err)
	}

	called := false
	addr := startServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}), cfg)
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

// A stable port keeps the desktop window's origin (and its localStorage —
// selected context, table sort order) constant across launches.
func TestListenPreferringBindsAndPersistsFirstFreePort(t *testing.T) {
	cfg := newTestStore(t)
	want := freePort(t)

	ln := listenPreferring(cfg, []int{want})
	defer func() { _ = ln.Close() }()

	if got := ln.Addr().(*net.TCPAddr).Port; got != want {
		t.Fatalf("bound port %d, want %d", got, want)
	}
	if got := cfg.DesktopPort(); got != want {
		t.Fatalf("persisted port %d, want %d — next launch must reuse it", got, want)
	}
}

func TestListenPreferringSkipsTakenPortForNextCandidate(t *testing.T) {
	cfg := newTestStore(t)

	blocker, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("blocker listen: %v", err)
	}
	defer func() { _ = blocker.Close() }()
	taken := blocker.Addr().(*net.TCPAddr).Port
	want := freePort(t)

	ln := listenPreferring(cfg, []int{taken, want})
	defer func() { _ = ln.Close() }()

	if got := ln.Addr().(*net.TCPAddr).Port; got != want {
		t.Fatalf("bound port %d, want the second candidate %d", got, want)
	}
}

func TestListenPreferringFallsBackWithoutPersistingWhenAllTaken(t *testing.T) {
	cfg := newTestStore(t)

	var taken []int
	for range 2 {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("blocker listen: %v", err)
		}
		defer func() { _ = ln.Close() }()
		taken = append(taken, ln.Addr().(*net.TCPAddr).Port)
	}

	ln := listenPreferring(cfg, taken)
	defer func() { _ = ln.Close() }()

	got := ln.Addr().(*net.TCPAddr).Port
	for _, p := range taken {
		if got == p {
			t.Fatalf("bound an occupied port %d", got)
		}
	}
	if cfg.DesktopPort() != 0 {
		t.Fatalf("persisted port %d on the ephemeral fallback, want it left unset", cfg.DesktopPort())
	}
}
