package main

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/robertobado/netsk8-navigator/backend/internal/api"
	"github.com/robertobado/netsk8-navigator/backend/internal/config"
	"github.com/robertobado/netsk8-navigator/backend/internal/kube"
)

// stubKubeconfig writes a minimal but valid kubeconfig (a context pointing at
// a cluster that's never actually dialed) and points KUBECONFIG at it, so
// kube.NewManager() succeeds without a real cluster or the developer's own
// ~/.kube/config — same trick release.yml's CI job uses for `wails build`'s
// bindings-generation step.
func stubKubeconfig(t *testing.T) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config")
	kubeconfig := "apiVersion: v1\n" +
		"kind: Config\n" +
		"clusters:\n" +
		"- name: test-cluster\n" +
		"  cluster:\n" +
		"    server: https://example.com\n" +
		"contexts:\n" +
		"- name: test-context\n" +
		"  context:\n" +
		"    cluster: test-cluster\n" +
		"current-context: test-context\n"
	if err := os.WriteFile(path, []byte(kubeconfig), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KUBECONFIG", path)
}

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

func TestWithBasicAuth(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	h := withBasicAuth("admin", "s3cret", inner)

	t.Run("no credentials rejected", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
		if rec.Header().Get("WWW-Authenticate") == "" {
			t.Error("expected a WWW-Authenticate header to prompt the browser")
		}
	})

	t.Run("wrong password rejected", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.SetBasicAuth("admin", "wrong")
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("wrong user rejected", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.SetBasicAuth("someone-else", "s3cret")
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("correct credentials pass through", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.SetBasicAuth("admin", "s3cret")
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", rec.Code)
		}
	})
}

func TestWrapWithAuth(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	t.Run("no AUTH_PASSWORD passes through unauthenticated", func(t *testing.T) {
		t.Setenv("AUTH_PASSWORD", "")
		h := wrapWithAuth(inner)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want 200 (no auth configured)", rec.Code)
		}
	})

	t.Run("AUTH_PASSWORD set requires Basic Auth, defaulting the user to admin", func(t *testing.T) {
		t.Setenv("AUTH_PASSWORD", "s3cret")
		t.Setenv("AUTH_USER", "")
		h := wrapWithAuth(inner)

		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("no creds: status = %d, want 401", rec.Code)
		}

		rec2 := httptest.NewRecorder()
		req2 := httptest.NewRequest(http.MethodGet, "/", nil)
		req2.SetBasicAuth("admin", "s3cret")
		h.ServeHTTP(rec2, req2)
		if rec2.Code != http.StatusOK {
			t.Errorf("default user \"admin\": status = %d, want 200", rec2.Code)
		}
	})

	t.Run("AUTH_USER overrides the default admin username", func(t *testing.T) {
		t.Setenv("AUTH_PASSWORD", "s3cret")
		t.Setenv("AUTH_USER", "custom")
		h := wrapWithAuth(inner)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.SetBasicAuth("custom", "s3cret")
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", rec.Code)
		}
	})
}

func TestBuildMux(t *testing.T) {
	stubKubeconfig(t)
	mgr, err := kube.NewManager()
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.NewStoreAt(filepath.Join(t.TempDir(), "config.json"))
	mux := buildMux(api.NewServer(mgr, cfg, ""))

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/contexts", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("GET /api/contexts: status = %d, want 200 (routed under /api/ to srv.Routes())", rec.Code)
	}

	// /mcp is wired in unconditionally, but MCP itself is disabled by
	// default — same 404 TestMCPHandler_DisabledReturns404 documents for
	// api.Server.MCPHandler() directly.
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("{}")))
	if rec2.Code != http.StatusNotFound {
		t.Errorf("POST /mcp: status = %d, want 404 (MCP disabled by default)", rec2.Code)
	}
}

func TestServe_PlainHTTP(t *testing.T) {
	t.Setenv("TLS_CERT", "")
	t.Setenv("TLS_KEY", "")
	// Occupy a port first so ListenAndServe fails fast with "address already
	// in use" instead of actually serving forever.
	blocker, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = blocker.Close() }()

	if err := serve(&http.Server{Addr: blocker.Addr().String(), ReadHeaderTimeout: 10 * time.Second}); err == nil {
		t.Error("expected an error from ListenAndServe against an already-bound address")
	}
}

// TestMustInit_Success exercises mustInit's happy path directly — it's
// otherwise only reached through main(), which this package's tests
// deliberately never call for the default server-start path (it blocks
// forever serving). The error branches (log.Fatalf) aren't reachable from an
// in-process test at all, since they'd terminate the test binary.
func TestMustInit_Success(t *testing.T) {
	stubKubeconfig(t)
	t.Setenv("HOME", t.TempDir()) // keep config.NewStore() off the developer's real prefs file

	mgr, cfg := mustInit()
	if mgr == nil {
		t.Error("expected a non-nil *kube.Manager")
	}
	if cfg == nil {
		t.Error("expected a non-nil *config.Store")
	}
}

// TestMain_VersionAndHelp exercises the arg-dispatch switch in main() for the
// branches that print and return without side effects (no os.Exit, no server
// start) — --mcp-stdio/mcp/unrecognized all either block on stdin or exit the
// process, so they aren't safe to run in-process here.
func TestMain_VersionAndHelp(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	for _, arg := range []string{"--version", "-version", "--help", "-help", "-h"} {
		t.Run(arg, func(t *testing.T) {
			os.Args = []string{"netsk8-navigator", arg}

			r, w, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			origStdout := os.Stdout
			os.Stdout = w
			main()
			_ = w.Close()
			os.Stdout = origStdout

			out, err := io.ReadAll(r)
			if err != nil {
				t.Fatal(err)
			}
			if len(out) == 0 {
				t.Errorf("%s: expected output, got none", arg)
			}
		})
	}
}

func TestServe_TLS(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TLS_CERT", filepath.Join(dir, "missing-cert.pem"))
	t.Setenv("TLS_KEY", filepath.Join(dir, "missing-key.pem"))

	// net.Listen runs before the cert files are loaded (net/http's own
	// ListenAndServeTLS), so an ephemeral port keeps this from blocking —
	// it fails fast once it tries to read the nonexistent cert/key.
	if err := serve(&http.Server{Addr: "127.0.0.1:0", ReadHeaderTimeout: 10 * time.Second}); err == nil {
		t.Error("expected an error loading a nonexistent TLS cert/key pair")
	}
}
