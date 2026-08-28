package api

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/robertobado/netsk8-navigator/backend/internal/config"
	"github.com/robertobado/netsk8-navigator/backend/internal/kube"
	"github.com/robertobado/netsk8-navigator/backend/internal/kubeconfig"
)

// newRealKubeconfigTestServer builds a Server wired to a REAL kube.Manager
// and kubeconfig.Editor pointed at the same temp kubeconfig file — unlike
// newTestServer's fakeManager, this is what lets these tests exercise the
// actual write-then-reload path end to end, which is the one guarantee that
// matters most here (see TestKubeconfigWrite_ReflectedImmediatelyInContexts).
func newRealKubeconfigTestServer(t *testing.T) *Server {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config")
	cfg := clientcmdapi.NewConfig()
	cfg.Clusters["cluster-a"] = &clientcmdapi.Cluster{Server: "https://a.example.com"}
	cfg.AuthInfos["user-a"] = &clientcmdapi.AuthInfo{Token: "s3cr3t"}
	cfg.Contexts["ctx-a"] = &clientcmdapi.Context{Cluster: "cluster-a", AuthInfo: "user-a", Namespace: "default"}
	cfg.CurrentContext = "ctx-a"
	if err := clientcmd.WriteToFile(*cfg, path); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KUBECONFIG", path)

	mgr, err := kube.NewManager()
	if err != nil {
		t.Fatal(err)
	}
	ed, err := kubeconfig.NewEditor()
	if err != nil {
		t.Fatal(err)
	}
	store := config.NewStoreAt(filepath.Join(t.TempDir(), "prefs.json"))
	srv := NewServer(mgr, store, "")
	srv.SetKubeconfigEditor(ed)
	return srv
}

func TestKubeconfig_UnavailableWhenNoEditor(t *testing.T) {
	s := newTestServer(t) // fakeManager, no SetKubeconfigEditor call
	for _, tc := range []struct {
		method, path, body string
	}{
		{"GET", "/api/kubeconfig", ""},
		{"PUT", "/api/kubeconfig/current-context", `{"name":"x"}`},
		{"POST", "/api/kubeconfig/contexts", `{}`},
		{"DELETE", "/api/kubeconfig/contexts/x", ""},
	} {
		rec := doRequest(t, s, tc.method, tc.path, tc.body)
		if rec.Code != http.StatusNotImplemented {
			t.Errorf("%s %s with no editor = %d, want 501", tc.method, tc.path, rec.Code)
		}
	}
}

func TestKubeconfigView_HTTP(t *testing.T) {
	s := newRealKubeconfigTestServer(t)
	rec := doRequest(t, s, "GET", "/api/kubeconfig", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/kubeconfig = %d: %s", rec.Code, rec.Body.String())
	}
	var view kubeconfig.View
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.CurrentContext != "ctx-a" {
		t.Errorf("currentContext = %q, want ctx-a", view.CurrentContext)
	}
	if len(view.Users) != 1 || !view.Users[0].HasToken {
		t.Errorf("users = %+v, want one user with HasToken", view.Users)
	}
	if strings.Contains(rec.Body.String(), "s3cr3t") {
		t.Error("GET /api/kubeconfig leaked the raw token")
	}
}

func TestKubeconfigSetCurrentContext_HTTP(t *testing.T) {
	s := newRealKubeconfigTestServer(t)
	rec := doRequest(t, s, "POST", "/api/kubeconfig/contexts", `{"name":"ctx-b","cluster":"cluster-a","user":"user-a","namespace":"ns-b"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create context = %d: %s", rec.Code, rec.Body.String())
	}
	rec = doRequest(t, s, "PUT", "/api/kubeconfig/current-context", `{"name":"ctx-b"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("set-current-context = %d: %s", rec.Code, rec.Body.String())
	}
	rec = doRequest(t, s, "PUT", "/api/kubeconfig/current-context", `{"name":"no-such-context"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("set-current-context for unknown name = %d, want 400", rec.Code)
	}
}

func TestKubeconfigDeleteContext_HTTP(t *testing.T) {
	s := newRealKubeconfigTestServer(t)
	rec := doRequest(t, s, "DELETE", "/api/kubeconfig/contexts/ctx-a", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete context = %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		OrphanedCluster string `json:"orphanedCluster"`
		OrphanedUser    string `json:"orphanedUser"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.OrphanedCluster != "cluster-a" || body.OrphanedUser != "user-a" {
		t.Errorf("orphans = %+v, want cluster-a/user-a", body)
	}
}

func TestKubeconfigRevealSecret_HTTP(t *testing.T) {
	s := newRealKubeconfigTestServer(t)

	rec := doRequest(t, s, "GET", "/api/kubeconfig/users/user-a/reveal?field=token", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("reveal token = %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Value != "s3cr3t" {
		t.Errorf("revealed value = %q, want s3cr3t", body.Value)
	}

	rec = doRequest(t, s, "GET", "/api/kubeconfig/users/user-a/reveal?field=not-a-real-field", "")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("reveal with invalid field = %d, want 400", rec.Code)
	}
}

func TestKubeconfigImportPreviewAndCommit_HTTP(t *testing.T) {
	s := newRealKubeconfigTestServer(t)

	incoming := clientcmdapi.NewConfig()
	incoming.Clusters["cluster-new"] = &clientcmdapi.Cluster{Server: "https://new.example.com"}
	incoming.AuthInfos["user-new"] = &clientcmdapi.AuthInfo{Token: "t"}
	incoming.Contexts["ctx-new"] = &clientcmdapi.Context{Cluster: "cluster-new", AuthInfo: "user-new"}
	raw, err := clientcmd.Write(*incoming)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(map[string]string{"yaml": string(raw)})
	if err != nil {
		t.Fatal(err)
	}

	rec := doRequest(t, s, "POST", "/api/kubeconfig/import/preview", string(payload))
	if rec.Code != http.StatusOK {
		t.Fatalf("import preview = %d: %s", rec.Code, rec.Body.String())
	}
	var preview kubeconfig.ImportPreview
	if err := json.Unmarshal(rec.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	if len(preview.AddedContexts) != 1 || preview.AddedContexts[0] != "ctx-new" {
		t.Fatalf("preview.AddedContexts = %v, want [ctx-new]", preview.AddedContexts)
	}

	commitPayload, err := json.Marshal(map[string]any{"yaml": string(raw), "overwrite": []string{}})
	if err != nil {
		t.Fatal(err)
	}
	rec = doRequest(t, s, "POST", "/api/kubeconfig/import/commit", string(commitPayload))
	if rec.Code != http.StatusOK {
		t.Fatalf("import commit = %d: %s", rec.Code, rec.Body.String())
	}

	rec = doRequest(t, s, "GET", "/api/contexts", "")
	if !strings.Contains(rec.Body.String(), "ctx-new") {
		t.Fatalf("GET /api/contexts after import doesn't show ctx-new: %s", rec.Body.String())
	}
}

// TestKubeconfigWrite_ReflectedImmediatelyInContexts is the single most
// important test in this file: it proves the Manager.Reload() invalidation
// wired into every mutating handler actually works end to end, against a
// real *kube.Manager rather than the fakeManager every other handler test
// uses — a mock here would just assert the mock's own behavior and prove
// nothing about the real caching/invalidation logic in kube.Manager.
func TestKubeconfigWrite_ReflectedImmediatelyInContexts(t *testing.T) {
	s := newRealKubeconfigTestServer(t)

	rec := doRequest(t, s, "GET", "/api/contexts", "")
	if strings.Contains(rec.Body.String(), "ctx-new") {
		t.Fatalf("ctx-new unexpectedly present before creation: %s", rec.Body.String())
	}

	rec = doRequest(t, s, "POST", "/api/kubeconfig/contexts", `{"name":"ctx-new","cluster":"cluster-a","user":"user-a","namespace":"other"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /api/kubeconfig/contexts = %d: %s", rec.Code, rec.Body.String())
	}

	// No restart, no cache-clearing call from the test itself — the live
	// Manager must already reflect the write.
	rec = doRequest(t, s, "GET", "/api/contexts", "")
	if !strings.Contains(rec.Body.String(), "ctx-new") {
		t.Fatalf("GET /api/contexts after create doesn't show ctx-new (Reload() didn't take effect): %s", rec.Body.String())
	}

	rec = doRequest(t, s, "DELETE", "/api/kubeconfig/contexts/ctx-new", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete ctx-new = %d: %s", rec.Code, rec.Body.String())
	}
	rec = doRequest(t, s, "GET", "/api/contexts", "")
	if strings.Contains(rec.Body.String(), "ctx-new") {
		t.Fatalf("GET /api/contexts after delete still shows ctx-new: %s", rec.Body.String())
	}
}
