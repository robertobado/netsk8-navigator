package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/robertobado/netsk8-navigator/backend/internal/config"
	"github.com/robertobado/netsk8-navigator/backend/internal/kube"
)

// TestMain disables the reflector's WatchListClient feature (beta,
// default-on since client-go 1.35): with it on, a SharedIndexInformer's
// initial sync issues a single "watch with sendInitialEvents=true" request
// terminated by a bookmark event instead of a plain List then Watch. The fake
// API server in TestHandlePodWatch_StreamsSnapshotThenLiveEvents below only
// speaks the classic list-then-watch protocol, so the feature gate must be
// off before any reflector starts — it's read once (sync.Once) from this env
// var, so it has to happen before m.Run(), not inside an individual test.
func TestMain(m *testing.M) {
	_ = os.Setenv("KUBE_FEATURE_WatchListClient", "false")
	os.Exit(m.Run())
}

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

// fakePodAPIServer is a minimal stand-in for the Kubernetes API server's pod
// list+watch endpoints — just enough for a real *kube.Manager (built via
// kube.NewManager against a kubeconfig pointed at this server) to run a real
// PodWatcher against, so handlePodWatch's streaming body (snapshot, SYNCED,
// live deltas) can be exercised without a live cluster. fakeManager's own
// PodWatcherFor always errors (see testutil_test.go), and that's not
// something this test file can change, so the real *kube.PodWatcher built
// here is plugged in via a small wrapper manager below instead.
type fakePodAPIServer struct {
	mu     sync.Mutex
	events chan podWatchEvent
}

type podWatchEvent struct {
	eventType string
	pod       corev1.Pod
}

func newFakePodAPIServer(t *testing.T, initial ...corev1.Pod) (*httptest.Server, *fakePodAPIServer) {
	t.Helper()
	f := &fakePodAPIServer{events: make(chan podWatchEvent, 8)}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/pods", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("watch") == "true" {
			flusher, ok := w.(http.Flusher)
			if !ok {
				t.Fatal("ResponseWriter must be a Flusher for a watch stream")
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			flusher.Flush()
			enc := json.NewEncoder(w)
			for {
				select {
				case ev := <-f.events:
					ev.pod.TypeMeta = metav1.TypeMeta{Kind: "Pod", APIVersion: "v1"}
					raw, err := json.Marshal(ev.pod)
					if err != nil {
						return
					}
					we := metav1.WatchEvent{Type: ev.eventType, Object: runtime.RawExtension{Raw: raw}}
					if err := enc.Encode(we); err != nil {
						return
					}
					flusher.Flush()
				case <-r.Context().Done():
					return
				}
			}
		}
		list := corev1.PodList{
			TypeMeta: metav1.TypeMeta{Kind: "PodList", APIVersion: "v1"},
			ListMeta: metav1.ListMeta{ResourceVersion: "1"},
			Items:    initial,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(list)
	})
	srv := httptest.NewServer(mux)
	// The watch handler above blocks in StateActive for as long as the
	// PodWatcher built on top of it keeps running — which, since
	// kube.PodWatcher exposes no exported stop/close method, is for the rest
	// of the test binary's life once handed to another package. Plain
	// srv.Close() only force-closes idle/new connections and otherwise waits
	// up to 5s for active ones, so it would hang; force-closing first lets
	// the server-side request context observe Done() and the handler return.
	t.Cleanup(func() {
		srv.CloseClientConnections()
		srv.Close()
	})
	return srv, f
}

func (f *fakePodAPIServer) send(eventType string, p corev1.Pod) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events <- podWatchEvent{eventType: eventType, pod: p}
}

// realPodWatcherFor builds a *kube.Manager pointed at srv via a temp
// kubeconfig (the only exported way to get a live kube.Manager, since its
// internal fields aren't reachable from this package) and returns a real,
// running *kube.PodWatcher for it.
func realPodWatcherFor(t *testing.T, srv *httptest.Server) *kube.PodWatcher {
	t.Helper()
	kubeconfig := fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- name: c
  cluster:
    server: %s
contexts:
- name: test
  context:
    cluster: c
current-context: test
`, srv.URL)
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte(kubeconfig), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KUBECONFIG", path)

	mgr, err := kube.NewManager()
	if err != nil {
		t.Fatalf("kube.NewManager() error: %v", err)
	}
	w, err := mgr.PodWatcherFor("test")
	if err != nil {
		t.Fatalf("PodWatcherFor() error: %v", err)
	}
	return w
}

// podWatcherManager wraps a *fakeManager but serves a real, pre-built
// *kube.PodWatcher, letting handlePodWatch's streaming body run against
// live informer events instead of stopping at fakeManager's "not supported
// in tests" guard.
type podWatcherManager struct {
	*fakeManager
	watcher *kube.PodWatcher
}

func (m podWatcherManager) PodWatcherFor(string) (*kube.PodWatcher, error) {
	return m.watcher, nil
}

func TestHandlePodWatch_StreamsSnapshotThenLiveEvents(t *testing.T) {
	srv, api := newFakePodAPIServer(t, corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "existing", Namespace: "prod"}})
	watcher := realPodWatcherFor(t, srv)

	cfg := config.NewStoreAt(filepath.Join(t.TempDir(), "config.json"))
	s := NewServer(podWatcherManager{fakeManager: newFakeManager(), watcher: watcher}, cfg, "")

	ctx, cancel := context.WithCancel(context.Background())
	r := httptest.NewRequest("GET", "/api/contexts/test/watch/pods", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	go func() {
		time.Sleep(100 * time.Millisecond)
		api.send("ADDED", corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "fresh", Namespace: "prod"}})
		time.Sleep(300 * time.Millisecond)
		cancel()
	}()
	s.Routes().ServeHTTP(rec, r)

	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"type":"ADDED","object":{"name":"existing","namespace":"prod"`) {
		t.Errorf("body = %q, want the initial snapshot emitted as ADDED", body)
	}
	if !strings.Contains(body, `"type":"SYNCED"`) {
		t.Errorf("body = %q, want a SYNCED marker after the snapshot", body)
	}
	if !strings.Contains(body, `"name":"fresh"`) {
		t.Errorf("body = %q, want the live event delivered after SYNCED", body)
	}
	if strings.Index(body, `"type":"SYNCED"`) > strings.Index(body, `"name":"fresh"`) {
		t.Errorf("body = %q, want SYNCED before the live event", body)
	}
}

func TestHandlePodWatch_NamespaceFilter(t *testing.T) {
	srv, _ := newFakePodAPIServer(t,
		corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "in-prod", Namespace: "prod"}},
		corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "in-staging", Namespace: "staging"}},
	)
	watcher := realPodWatcherFor(t, srv)

	cfg := config.NewStoreAt(filepath.Join(t.TempDir(), "config.json"))
	s := NewServer(podWatcherManager{fakeManager: newFakeManager(), watcher: watcher}, cfg, "")

	ctx, cancel := context.WithCancel(context.Background())
	r := httptest.NewRequest("GET", "/api/contexts/test/watch/pods?namespace=prod", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	go func() {
		time.Sleep(150 * time.Millisecond)
		cancel()
	}()
	s.Routes().ServeHTTP(rec, r)

	body := rec.Body.String()
	if !strings.Contains(body, `"name":"in-prod"`) {
		t.Errorf("body = %q, want the prod pod in the namespace-scoped snapshot", body)
	}
	if strings.Contains(body, `"name":"in-staging"`) {
		t.Errorf("body = %q, want the staging pod excluded by the namespace filter", body)
	}
}

func TestHandlePodWatch_NotFlusher(t *testing.T) {
	s := newTestServer(t)
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/contexts/test/watch/pods", nil)
	s.Routes().ServeHTTP(noFlushWriter{rec}, r)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}
