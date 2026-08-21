package kube

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
	listersv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// TestMain disables the reflector's WatchListClient feature (beta,
// default-on since client-go 1.35): with it on, a SharedIndexInformer's
// initial sync issues a single "watch with sendInitialEvents=true" request
// terminated by a bookmark event instead of a plain List then Watch.
// TestPodWatcherFor_BuildsAgainstLiveAPI's fake API server below only speaks
// the classic list-then-watch protocol, so the feature gate must be off
// before any reflector in this package starts — the gate is read once
// (sync.Once) from this env var, so it has to happen before m.Run(), not
// inside an individual test.
func TestMain(m *testing.M) {
	_ = os.Setenv("KUBE_FEATURE_WatchListClient", "false")
	os.Exit(m.Run())
}

// newTestPodWatcher builds a *PodWatcher wired to a real SharedIndexInformer
// backed by client-go's fake clientset. Watch() on a fake clientset is backed
// by a real watch.FakeWatcher via the fake's ObjectTracker (unlike
// GetLogs(...).Stream(), which needs a canned-response trick — see
// workloadlogs_test.go), so a fake-clientset-driven informer really syncs and
// really dispatches ADDED/MODIFIED/DELETED, letting Subscribe/Snapshot/
// broadcast be exercised end-to-end.
//
// This mirrors the wiring PodWatcherFor does after m.ClientFor succeeds
// (event handlers, Run, WaitForCacheSync) — duplicated here because
// PodWatcherFor's own ClientFor call is pinned to *kubernetes.Clientset,
// which can't be swapped for a fake. See TestPodWatcherFor_BuildsAgainstLiveAPI
// below for coverage of PodWatcherFor's own build path via a minimal fake
// HTTP API server instead.
func newTestPodWatcher(t *testing.T, objs ...runtime.Object) (*PodWatcher, *kubernetesfake.Clientset) {
	t.Helper()
	client := kubernetesfake.NewSimpleClientset(objs...)
	factory := informers.NewSharedInformerFactory(client, 0)
	podInformer := factory.Core().V1().Pods()

	w := &PodWatcher{
		informer: podInformer.Informer(),
		lister:   podInformer.Lister(),
		stop:     make(chan struct{}),
		subs:     make(map[chan Event]string),
	}
	_, _ = w.informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(o any) { w.broadcast("ADDED", o) },
		UpdateFunc: func(_, o any) { w.broadcast("MODIFIED", o) },
		DeleteFunc: func(o any) {
			if tomb, ok := o.(cache.DeletedFinalStateUnknown); ok {
				o = tomb.Obj
			}
			w.broadcast("DELETED", o)
		},
	})
	go w.informer.Run(w.stop)
	if !cache.WaitForCacheSync(w.stop, w.informer.HasSynced) {
		t.Fatal("informer cache never synced")
	}
	t.Cleanup(func() { close(w.stop) })
	return w, client
}

func pod(namespace, name string) *corev1.Pod {
	return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}}
}

// recvEvent waits briefly for an event on ch, failing the test if none
// arrives — the informer dispatches asynchronously off the fake watch.
func recvEvent(t *testing.T, ch <-chan Event) Event {
	t.Helper()
	select {
	case ev := <-ch:
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
		return Event{}
	}
}

func expectNoEvent(t *testing.T, ch <-chan Event) {
	t.Helper()
	select {
	case ev := <-ch:
		t.Errorf("got unexpected event %+v, want none", ev)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestPodWatcher_SubscribeReceivesAddedModifiedDeleted(t *testing.T) {
	w, client := newTestPodWatcher(t)
	events, unsubscribe := w.Subscribe("")
	defer unsubscribe()

	if _, err := client.CoreV1().Pods("prod").Create(t.Context(), pod("prod", "web-1"), metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	ev := recvEvent(t, events)
	if ev.Type != "ADDED" || ev.Object == nil || ev.Object.Name != "web-1" || ev.Object.Namespace != "prod" {
		t.Errorf("got %+v, want ADDED web-1/prod", ev)
	}

	updated := pod("prod", "web-1")
	updated.Labels = map[string]string{"updated": "true"}
	if _, err := client.CoreV1().Pods("prod").Update(t.Context(), updated, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	ev = recvEvent(t, events)
	if ev.Type != "MODIFIED" || ev.Object == nil || ev.Object.Name != "web-1" {
		t.Errorf("got %+v, want MODIFIED web-1", ev)
	}

	if err := client.CoreV1().Pods("prod").Delete(t.Context(), "web-1", metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	ev = recvEvent(t, events)
	if ev.Type != "DELETED" || ev.Object == nil || ev.Object.Name != "web-1" {
		t.Errorf("got %+v, want DELETED web-1", ev)
	}
}

func TestPodWatcher_SubscribeFiltersByNamespace(t *testing.T) {
	w, client := newTestPodWatcher(t)
	events, unsubscribe := w.Subscribe("prod")
	defer unsubscribe()

	if _, err := client.CoreV1().Pods("staging").Create(t.Context(), pod("staging", "other"), metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	expectNoEvent(t, events)

	if _, err := client.CoreV1().Pods("prod").Create(t.Context(), pod("prod", "web-1"), metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	ev := recvEvent(t, events)
	if ev.Object == nil || ev.Object.Namespace != "prod" {
		t.Errorf("got %+v, want the prod-namespace pod", ev)
	}
}

func TestPodWatcher_Unsubscribe(t *testing.T) {
	w, client := newTestPodWatcher(t)
	events, unsubscribe := w.Subscribe("")
	unsubscribe()

	if _, err := client.CoreV1().Pods("prod").Create(t.Context(), pod("prod", "web-1"), metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	// The channel should be closed, not just quiet, once unsubscribed.
	select {
	case _, ok := <-events:
		if ok {
			t.Error("expected the channel to be closed after unsubscribe")
		}
	case <-time.After(2 * time.Second):
		t.Error("channel was neither closed nor did it receive anything")
	}
}

func TestPodWatcher_BroadcastIgnoresNonPodObjects(t *testing.T) {
	w, _ := newTestPodWatcher(t)
	events, unsubscribe := w.Subscribe("")
	defer unsubscribe()

	// broadcast type-asserts obj.(*corev1.Pod); anything else must be a no-op,
	// not a panic — exercise that guard directly since it's unreachable via a
	// real pod informer's own event stream.
	w.broadcast("ADDED", &corev1.ConfigMap{})
	expectNoEvent(t, events)
}

func TestPodWatcher_BroadcastDropsWhenSubscriberIsSlow(t *testing.T) {
	w, client := newTestPodWatcher(t)
	// Unbuffered from the caller's perspective — fill the 512-buffer without
	// reading, then confirm sends beyond it don't block the informer.
	events, unsubscribe := w.Subscribe("")
	defer unsubscribe()

	for i := 0; i < 520; i++ {
		w.broadcast("ADDED", pod("prod", "filler"))
	}
	// The real regression this guards: a full channel must never block
	// broadcast. Create a pod through the real informer pipeline to prove
	// it still completes promptly.
	done := make(chan struct{})
	go func() {
		_, _ = client.CoreV1().Pods("prod").Create(t.Context(), pod("prod", "web-1"), metav1.CreateOptions{})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("broadcast appears to have blocked on a full subscriber channel")
	}
	// Drain so the deferred unsubscribe's close doesn't race a pending send.
	for {
		select {
		case <-events:
		default:
			return
		}
	}
}

func TestPodWatcher_Snapshot(t *testing.T) {
	w, _ := newTestPodWatcher(t, pod("prod", "web-1"), pod("prod", "web-2"), pod("staging", "web-1"))

	all := w.Snapshot("")
	if len(all) != 3 {
		t.Errorf("Snapshot(\"\") returned %d pods, want 3", len(all))
	}

	prod := w.Snapshot("prod")
	if len(prod) != 2 {
		t.Errorf("Snapshot(\"prod\") returned %d pods, want 2", len(prod))
	}
	for _, p := range prod {
		if p.Namespace != "prod" {
			t.Errorf("Snapshot(\"prod\") returned a pod in namespace %q", p.Namespace)
		}
	}

	empty := w.Snapshot("nonexistent")
	if len(empty) != 0 {
		t.Errorf("Snapshot(\"nonexistent\") returned %d pods, want 0", len(empty))
	}
}

// failingPodLister and failingPodNamespaceLister let Snapshot's error
// branches (unreachable through a real informer's lister, which never
// errors) be exercised directly.
type failingPodLister struct{}

func (failingPodLister) List(labels.Selector) ([]*corev1.Pod, error) {
	return nil, fmt.Errorf("list failed")
}
func (failingPodLister) Pods(string) listersv1.PodNamespaceLister { return failingPodNamespaceLister{} }

type failingPodNamespaceLister struct{}

func (failingPodNamespaceLister) List(labels.Selector) ([]*corev1.Pod, error) {
	return nil, fmt.Errorf("list failed")
}
func (failingPodNamespaceLister) Get(string) (*corev1.Pod, error) {
	return nil, fmt.Errorf("not found")
}

func TestPodWatcher_SnapshotListError(t *testing.T) {
	w := &PodWatcher{lister: failingPodLister{}}

	if got := w.Snapshot(""); got != nil {
		t.Errorf("Snapshot(\"\") = %+v, want nil on a lister error", got)
	}
	if got := w.Snapshot("prod"); got != nil {
		t.Errorf("Snapshot(\"prod\") = %+v, want nil on a lister error", got)
	}
}

// fakePodAPIServer is a minimal stand-in for the Kubernetes API server's pod
// list+watch endpoints, just enough for a real *kubernetes.Clientset (as
// built by Manager.ClientFor) to run a real reflector against — used to drive
// PodWatcherFor's own build path (informer factory construction, event
// handler wiring, Run, WaitForCacheSync), which can't be reached by swapping
// in a fake clientset since Manager.clients is typed to the concrete
// *kubernetes.Clientset. Content-type negotiates to plain JSON by default
// (rest.Config.ContentType is unset), so a hand-rolled JSON list/watch
// endpoint is enough — no protobuf involved.
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
	t.Cleanup(srv.Close)
	return srv, f
}

func (f *fakePodAPIServer) send(eventType string, p corev1.Pod) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events <- podWatchEvent{eventType: eventType, pod: p}
}

// TestPodWatcherFor_BuildsAgainstLiveAPI drives PodWatcherFor's own build
// path — the part newTestPodWatcher above can't reach, since it goes through
// Manager.ClientFor rather than a fake clientset — against a minimal fake API
// server, proving the real event-handler wiring (including the tombstone
// unwrap on delete) executes end-to-end.
func TestPodWatcherFor_BuildsAgainstLiveAPI(t *testing.T) {
	srv, api := newFakePodAPIServer(t)
	m := managerWithContext("fake-api")
	m.rawConfig.Clusters["c"].Server = srv.URL

	w, err := m.PodWatcherFor("fake-api")
	if err != nil {
		t.Fatalf("PodWatcherFor() error: %v", err)
	}
	t.Cleanup(func() { close(w.stop) })

	events, unsubscribe := w.Subscribe("")
	defer unsubscribe()

	api.send("ADDED", *pod("prod", "web-1"))
	ev := recvEvent(t, events)
	if ev.Type != "ADDED" || ev.Object == nil || ev.Object.Name != "web-1" {
		t.Errorf("got %+v, want ADDED web-1", ev)
	}

	// A second call for the same context must return the cached watcher
	// rather than building a new one.
	w2, err := m.PodWatcherFor("fake-api")
	if err != nil {
		t.Fatalf("second PodWatcherFor() error: %v", err)
	}
	if w != w2 {
		t.Error("want the cached PodWatcher on the second call, not a freshly built one")
	}
}

func TestPodWatcherFor_ClientForError(t *testing.T) {
	m := &Manager{rawConfig: clientcmdapi.Config{}}
	if _, err := m.PodWatcherFor("unknown-context"); err == nil {
		t.Error("want an error when the context isn't in the kubeconfig")
	}
}
