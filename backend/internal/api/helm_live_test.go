package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage"
	"helm.sh/helm/v3/pkg/storage/driver"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/robertobado/netsk8-navigator/backend/internal/config"
	"github.com/robertobado/netsk8-navigator/backend/internal/kube"
)

// The rest of this file builds a *second*, independent test harness
// (deliberately not touching testutil_test.go's fakeManager/newTestServer) so
// that helm_releases.go's actual 200-success bodies become reachable, not
// just the RESTConfigFor-fails 502 helm_install_test.go/helm_releases_test.go
// already cover.
//
// fakeManager.RESTConfigFor always errors by design — Helm's "secret" storage
// driver only dials out lazily on first real use (see the package doc comment
// in helm_releases_test.go), so every handler reaches that failure fast, but
// never past it. To get past it without a live cluster, newHelmSecretBridge
// below serves just enough of the Kubernetes Secrets REST API — backed by an
// in-process kubernetesfake.Clientset, no real cluster — for Helm's storage
// driver to round-trip for real. That's sufficient for every helm_releases.go
// handler (status/manifest/history/list/rollback/uninstall) as long as the
// release's own manifest is empty: a non-empty manifest would additionally
// need Helm's KubeClient to create/patch/delete arbitrary GVKs against the
// cluster, which is out of scope here.

// liveHelmGetter adapts a fixed bridge URL to the RESTClientGetter interface
// Helm's SDK needs — the same role helmRESTClientGetter plays in production,
// standing in for it here since that type is tied to clusterManager.
type liveHelmGetter struct{ bridgeURL string }

func (g *liveHelmGetter) ToRESTConfig() (*rest.Config, error) {
	// ContentType must be forced to JSON: client-go's typed clients default to
	// protobuf for built-in types, which newHelmSecretBridge doesn't speak.
	return &rest.Config{Host: g.bridgeURL, ContentConfig: rest.ContentConfig{ContentType: "application/json"}}, nil
}
func (g *liveHelmGetter) ToDiscoveryClient() (discovery.CachedDiscoveryInterface, error) {
	return nil, fmt.Errorf("liveHelmGetter: discovery not supported")
}
func (g *liveHelmGetter) ToRESTMapper() (apimeta.RESTMapper, error) {
	return nil, fmt.Errorf("liveHelmGetter: REST mapper not supported")
}
func (g *liveHelmGetter) ToRawKubeConfigLoader() clientcmd.ClientConfig {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{})
}

var _ genericclioptions.RESTClientGetter = (*liveHelmGetter)(nil)

// newHelmSecretBridge serves the slice of the core/v1 Secrets REST API
// Helm's storage driver actually calls (list/get/create/update/delete, plus
// /version for the reachability check), translating each request onto an
// in-process kubernetesfake.Clientset rather than a real API server.
func newHelmSecretBridge(t *testing.T, client kubernetes.Interface) *httptest.Server {
	t.Helper()
	statusNotFound := metav1.Status{
		TypeMeta: metav1.TypeMeta{Kind: "Status", APIVersion: "v1"},
		Status:   metav1.StatusFailure, Reason: metav1.StatusReasonNotFound, Code: http.StatusNotFound,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"major":"1","minor":"29","gitVersion":"v1.29.0"}`))
	})
	// Cluster-scoped list — hit when Helm's "secret" driver is Init'd with an
	// empty namespace (handleHelmReleases' AllNamespaces case).
	mux.HandleFunc("/api/v1/secrets", func(w http.ResponseWriter, r *http.Request) {
		list, err := client.CoreV1().Secrets("").List(r.Context(), metav1.ListOptions{LabelSelector: r.URL.Query().Get("labelSelector")})
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		writeJSONSecretList(w, list)
	})
	nsh := &namespacedSecretsHandler{client: client, statusNotFound: statusNotFound}
	mux.HandleFunc("/api/v1/namespaces/", nsh.serveHTTP)

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// namespacedSecretsHandler serves the .../namespaces/{ns}/secrets[/{name}]
// shapes of newHelmSecretBridge — split out to a method-per-verb so each verb
// reads as its own small function rather than one large switch.
type namespacedSecretsHandler struct {
	client         kubernetes.Interface
	statusNotFound metav1.Status
}

func (h *namespacedSecretsHandler) serveHTTP(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/namespaces/"), "/")
	if len(parts) < 2 || parts[1] != "secrets" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	ns, ctx := parts[0], r.Context()

	switch {
	case len(parts) == 2 && r.Method == http.MethodGet:
		h.list(w, r, ctx, ns)
	case len(parts) == 2 && r.Method == http.MethodPost:
		h.create(w, r, ctx, ns)
	case len(parts) == 3 && r.Method == http.MethodGet:
		h.get(w, ctx, ns, parts[2])
	case len(parts) == 3 && r.Method == http.MethodPut:
		h.update(w, r, ctx, ns)
	case len(parts) == 3 && r.Method == http.MethodDelete:
		h.delete(w, ctx, ns, parts[2])
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (h *namespacedSecretsHandler) list(w http.ResponseWriter, r *http.Request, ctx context.Context, ns string) {
	list, err := h.client.CoreV1().Secrets(ns).List(ctx, metav1.ListOptions{LabelSelector: r.URL.Query().Get("labelSelector")})
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	writeJSONSecretList(w, list)
}

func (h *namespacedSecretsHandler) create(w http.ResponseWriter, r *http.Request, ctx context.Context, ns string) {
	sec, err := decodeSecret(r)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	sec.Namespace = ns
	created, err := h.client.CoreV1().Secrets(ns).Create(ctx, sec, metav1.CreateOptions{})
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	// Content-Type must be set (inside writeJSONSecret) before any
	// WriteHeader call — net/http silently drops headers set after the
	// status line is written, so this relies on the 200 default rather
	// than an explicit 201.
	writeJSONSecret(w, created)
}

func (h *namespacedSecretsHandler) get(w http.ResponseWriter, ctx context.Context, ns, name string) {
	sec, err := h.client.CoreV1().Secrets(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(h.statusNotFound)
		return
	}
	writeJSONSecret(w, sec)
}

func (h *namespacedSecretsHandler) update(w http.ResponseWriter, r *http.Request, ctx context.Context, ns string) {
	sec, err := decodeSecret(r)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	sec.Namespace = ns
	// Helm's driver builds a fresh object with no resourceVersion; carry over
	// whatever the fake tracker currently has so Update doesn't look like a
	// stale write.
	if existing, err := h.client.CoreV1().Secrets(ns).Get(ctx, sec.Name, metav1.GetOptions{}); err == nil {
		sec.ResourceVersion = existing.ResourceVersion
	}
	updated, err := h.client.CoreV1().Secrets(ns).Update(ctx, sec, metav1.UpdateOptions{})
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	writeJSONSecret(w, updated)
}

func (h *namespacedSecretsHandler) delete(w http.ResponseWriter, ctx context.Context, ns, name string) {
	if err := h.client.CoreV1().Secrets(ns).Delete(ctx, name, metav1.DeleteOptions{}); err != nil {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(h.statusNotFound)
		return
	}
	_ = json.NewEncoder(w).Encode(metav1.Status{TypeMeta: metav1.TypeMeta{Kind: "Status", APIVersion: "v1"}, Status: metav1.StatusSuccess})
}

func decodeSecret(r *http.Request) (*corev1.Secret, error) {
	var sec corev1.Secret
	if err := json.NewDecoder(r.Body).Decode(&sec); err != nil {
		return nil, err
	}
	return &sec, nil
}

func writeJSONSecret(w http.ResponseWriter, sec *corev1.Secret) {
	sec.TypeMeta = metav1.TypeMeta{Kind: "Secret", APIVersion: "v1"}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(sec)
}

func writeJSONSecretList(w http.ResponseWriter, list *corev1.SecretList) {
	list.TypeMeta = metav1.TypeMeta{Kind: "SecretList", APIVersion: "v1"}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(list)
}

// liveHelmManager is a clusterManager whose RESTConfigFor genuinely resolves
// (to a newHelmSecretBridge), unlike testutil_test.go's fakeManager. Every
// other method is a stub — helm_releases.go's handlers never call them.
type liveHelmManager struct {
	client    kubernetes.Interface
	bridgeURL string
}

func (m *liveHelmManager) Contexts() []kube.ContextInfo {
	return []kube.ContextInfo{{Name: "test", Cluster: "test", Current: true}}
}
func (m *liveHelmManager) ConfigPath() string                             { return "/fake/kubeconfig" }
func (m *liveHelmManager) ClientFor(string) (kubernetes.Interface, error) { return m.client, nil }
func (m *liveHelmManager) DynamicFor(string) (dynamic.Interface, error) {
	return nil, fmt.Errorf("liveHelmManager: dynamic client not supported")
}
func (m *liveHelmManager) ResolveResource(_, resource string) (kube.Resource, error) {
	return kube.Resource{}, fmt.Errorf("liveHelmManager: no GVR registered for %q", resource)
}
func (m *liveHelmManager) ResolveGVK(_ string, gvk schema.GroupVersionKind) (kube.Resource, error) {
	return kube.Resource{}, fmt.Errorf("liveHelmManager: no GVR registered for kind %q", gvk.Kind)
}
func (m *liveHelmManager) CRDsFor(context.Context, string) ([]apiextensionsv1.CustomResourceDefinition, error) {
	return nil, nil
}
func (m *liveHelmManager) RESTConfigFor(string) (*rest.Config, error) {
	return (&liveHelmGetter{bridgeURL: m.bridgeURL}).ToRESTConfig()
}
func (m *liveHelmManager) RESTMapperFor(string) (apimeta.RESTMapper, error) {
	return nil, fmt.Errorf("liveHelmManager: REST mapper not supported")
}
func (m *liveHelmManager) PodWatcherFor(string) (*kube.PodWatcher, error) {
	return nil, fmt.Errorf("liveHelmManager: pod watcher not supported")
}
func (m *liveHelmManager) ExecInfoFor(string) (command, profile string, ok bool) {
	return "", "", false
}

var _ clusterManager = (*liveHelmManager)(nil)

// newHelmLiveServer builds a Server that can drive Helm release actions to a
// genuine 200, backed by an in-process fake Kubernetes clientset behind
// newHelmSecretBridge instead of a live cluster.
func newHelmLiveServer(t *testing.T) (*Server, kubernetes.Interface) {
	t.Helper()
	client := kubernetesfake.NewSimpleClientset()
	bridge := newHelmSecretBridge(t, client)
	mgr := &liveHelmManager{client: client, bridgeURL: bridge.URL}
	cfg := config.NewStoreAt(filepath.Join(t.TempDir(), "config.json"))
	return NewServer(mgr, cfg, ""), client
}

// seedLiveHelmRelease stores rel through Helm's own storage/driver code path
// (not a hand-rolled Secret, unlike seedHelmRelease in helm_releases_test.go)
// so the object's name/labels exactly match what action.Get/History/Rollback/
// Uninstall look up by. rel.Manifest must stay empty — see the file doc
// comment on why a non-empty manifest is out of scope for this harness.
func seedLiveHelmRelease(t *testing.T, client kubernetes.Interface, rel *release.Release) {
	t.Helper()
	store := storage.Init(driver.NewSecrets(client.CoreV1().Secrets(rel.Namespace)))
	if err := store.Create(rel); err != nil {
		t.Fatal(err)
	}
}

func TestHandleHelmReleaseStatus_Success(t *testing.T) {
	s, client := newHelmLiveServer(t)
	seedLiveHelmRelease(t, client, &release.Release{
		Name: "web", Namespace: "prod", Version: 1,
		Info:   &release.Info{Status: release.StatusDeployed, Notes: "install notes"},
		Chart:  &chart.Chart{Metadata: &chart.Metadata{Name: "nginx", Version: "1.2.3", AppVersion: "1.25.0"}},
		Config: map[string]any{"replicaCount": 2},
	})

	rec := doRequest(t, s, "GET", "/api/contexts/test/helm/releases/prod/web", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var detail helmReleaseDetail
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Name != "web" || detail.Namespace != "prod" || detail.Chart != "nginx-1.2.3" {
		t.Errorf("got %+v", detail)
	}
	if detail.Notes != "install notes" {
		t.Errorf("notes = %q", detail.Notes)
	}
	if !strings.Contains(detail.Values, "replicaCount: 2") {
		t.Errorf("values = %q, want it to contain the supplied config", detail.Values)
	}
}

func TestHandleHelmReleaseStatus_NotFound(t *testing.T) {
	s, _ := newHelmLiveServer(t)
	rec := doRequest(t, s, "GET", "/api/contexts/test/helm/releases/prod/missing", "")
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502 (Helm's own \"release not found\", surfaced the same as any other Get.Run failure)", rec.Code)
	}
}

func TestHandleHelmReleaseManifest_Success(t *testing.T) {
	s, client := newHelmLiveServer(t)
	const manifest = "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm\n"
	seedLiveHelmRelease(t, client, &release.Release{
		Name: "web", Namespace: "prod", Version: 1,
		Info:     &release.Info{Status: release.StatusDeployed},
		Chart:    &chart.Chart{Metadata: &chart.Metadata{Name: "nginx", Version: "1.0.0"}},
		Manifest: manifest,
	})

	rec := doRequest(t, s, "GET", "/api/contexts/test/helm/releases/prod/web/manifest", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["yaml"] != manifest {
		t.Errorf("yaml = %q, want %q", body["yaml"], manifest)
	}
}

func TestHandleHelmReleaseHistory_Success(t *testing.T) {
	s, client := newHelmLiveServer(t)
	for v, status := range map[int]release.Status{1: release.StatusSuperseded, 2: release.StatusDeployed} {
		seedLiveHelmRelease(t, client, &release.Release{
			Name: "web", Namespace: "prod", Version: v,
			Info:  &release.Info{Status: status},
			Chart: &chart.Chart{Metadata: &chart.Metadata{Name: "nginx", Version: "1.0.0"}},
		})
	}

	rec := doRequest(t, s, "GET", "/api/contexts/test/helm/releases/prod/web/history", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var revs []helmReleaseView
	if err := json.Unmarshal(rec.Body.Bytes(), &revs); err != nil {
		t.Fatal(err)
	}
	if len(revs) != 2 {
		t.Fatalf("got %d revisions, want 2: %+v", len(revs), revs)
	}
	if revs[0].Revision != 2 || revs[1].Revision != 1 {
		t.Errorf("expected most-recent-first ordering, got revisions %d, %d", revs[0].Revision, revs[1].Revision)
	}
}

func TestHandleHelmReleases_Success(t *testing.T) {
	s, client := newHelmLiveServer(t)
	seedLiveHelmRelease(t, client, &release.Release{
		Name: "web", Namespace: "prod", Version: 1,
		Info: &release.Info{Status: release.StatusDeployed}, Chart: &chart.Chart{Metadata: &chart.Metadata{Name: "nginx", Version: "1.0.0"}},
	})
	seedLiveHelmRelease(t, client, &release.Release{
		Name: "cache", Namespace: "staging", Version: 1,
		Info: &release.Info{Status: release.StatusDeployed}, Chart: &chart.Chart{Metadata: &chart.Metadata{Name: "redis", Version: "2.0.0"}},
	})

	t.Run("all namespaces", func(t *testing.T) {
		rec := doRequest(t, s, "GET", "/api/contexts/test/helm/releases", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
		}
		var releases []helmReleaseView
		if err := json.Unmarshal(rec.Body.Bytes(), &releases); err != nil {
			t.Fatal(err)
		}
		if len(releases) != 2 {
			t.Errorf("got %d releases, want 2: %+v", len(releases), releases)
		}
	})

	t.Run("scoped to one namespace", func(t *testing.T) {
		rec := doRequest(t, s, "GET", "/api/contexts/test/helm/releases?namespace=prod", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
		}
		var releases []helmReleaseView
		if err := json.Unmarshal(rec.Body.Bytes(), &releases); err != nil {
			t.Fatal(err)
		}
		if len(releases) != 1 || releases[0].Name != "web" {
			t.Errorf("got %+v, want just \"web\"", releases)
		}
	})
}

func TestHandleHelmReleaseRollback_Success(t *testing.T) {
	s, client := newHelmLiveServer(t)
	seedLiveHelmRelease(t, client, &release.Release{
		Name: "web", Namespace: "prod", Version: 1,
		Info: &release.Info{Status: release.StatusDeployed}, Chart: &chart.Chart{Metadata: &chart.Metadata{Name: "nginx", Version: "1.0.0"}},
	})

	rec := doRequest(t, s, "POST", "/api/contexts/test/helm/releases/prod/web/rollback", `{"revision":1}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "rolled-back" {
		t.Errorf("status field = %q", body["status"])
	}
}

func TestHandleHelmReleaseUninstall_Success(t *testing.T) {
	s, client := newHelmLiveServer(t)
	seedLiveHelmRelease(t, client, &release.Release{
		Name: "web", Namespace: "prod", Version: 1,
		Info: &release.Info{Status: release.StatusDeployed}, Chart: &chart.Chart{Metadata: &chart.Metadata{Name: "nginx", Version: "1.0.0"}},
	})

	rec := doRequest(t, s, "DELETE", "/api/contexts/test/helm/releases/prod/web", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "uninstalled" {
		t.Errorf("status field = %q", body["status"])
	}

	// Confirm the release is actually gone, not just a 200 mask.
	rec2 := doRequest(t, s, "GET", "/api/contexts/test/helm/releases/prod/web", "")
	if rec2.Code != http.StatusBadGateway {
		t.Errorf("status after uninstall = %d, want 502 (release no longer found)", rec2.Code)
	}
}
