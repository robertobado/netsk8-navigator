package kube

import (
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	listersv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
)

// Event is a single change broadcast to SSE subscribers.
type Event struct {
	Type   string  `json:"type"`             // ADDED | MODIFIED | DELETED | SYNCED
	Object *PodView `json:"object,omitempty"` // nil for SYNCED
}

// PodWatcher wraps a shared pod informer for one context and fans its events
// out to any number of SSE subscribers, optionally filtered by namespace.
type PodWatcher struct {
	informer cache.SharedIndexInformer
	lister   listersv1.PodLister
	stop     chan struct{}

	mu   sync.Mutex
	subs map[chan Event]string // subscriber channel -> namespace filter ("" = all)
}

// PodWatcherFor lazily builds and starts a PodWatcher for the given context,
// caching it so multiple browser tabs share one informer/watch connection.
func (m *Manager) PodWatcherFor(contextName string) (*PodWatcher, error) {
	m.mu.Lock()
	if m.watchers == nil {
		m.watchers = make(map[string]*PodWatcher)
	}
	if w, ok := m.watchers[contextName]; ok {
		m.mu.Unlock()
		return w, nil
	}
	m.mu.Unlock()

	client, err := m.ClientFor(contextName)
	if err != nil {
		return nil, err
	}

	factory := informers.NewSharedInformerFactory(client, 30*time.Second)
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
			// On delete the object may be wrapped in a tombstone.
			if tomb, ok := o.(cache.DeletedFinalStateUnknown); ok {
				o = tomb.Obj
			}
			w.broadcast("DELETED", o)
		},
	})

	go w.informer.Run(w.stop)
	cache.WaitForCacheSync(w.stop, w.informer.HasSynced)

	m.mu.Lock()
	m.watchers[contextName] = w
	m.mu.Unlock()
	return w, nil
}

func (w *PodWatcher) broadcast(kind string, obj any) {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return
	}
	view := ToPodView(pod)
	ev := Event{Type: kind, Object: &view}

	w.mu.Lock()
	defer w.mu.Unlock()
	for ch, ns := range w.subs {
		if ns != "" && ns != pod.Namespace {
			continue
		}
		// Non-blocking: a slow client drops an event rather than stalling the
		// informer. The 30s resync re-emits MODIFIED for everything, so the
		// client self-heals.
		select {
		case ch <- ev:
		default:
		}
	}
}

// Subscribe registers a subscriber for the given namespace filter and returns
// its event channel plus an unsubscribe func. Call Snapshot to seed state.
func (w *PodWatcher) Subscribe(namespace string) (<-chan Event, func()) {
	ch := make(chan Event, 512)
	w.mu.Lock()
	w.subs[ch] = namespace
	w.mu.Unlock()

	return ch, func() {
		w.mu.Lock()
		delete(w.subs, ch)
		w.mu.Unlock()
		close(ch)
	}
}

// Snapshot returns the current pods from the informer cache for the namespace
// filter, so a freshly-connected client gets the full picture immediately.
func (w *PodWatcher) Snapshot(namespace string) []PodView {
	var pods []*corev1.Pod
	var err error
	if namespace == "" {
		pods, err = w.lister.List(labels.Everything())
	} else {
		pods, err = w.lister.Pods(namespace).List(labels.Everything())
	}
	if err != nil {
		return nil
	}
	out := make([]PodView, 0, len(pods))
	for _, p := range pods {
		out = append(out, ToPodView(p))
	}
	return out
}
