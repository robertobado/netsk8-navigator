package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/robertobado/netsk8-navigator/backend/internal/kube"
)

// handlePodWatch streams pod changes as Server-Sent Events. On connect it emits
// the current snapshot as ADDED events, a SYNCED marker, then live deltas.
// The client keys pods by namespace/name with upsert semantics, so reconnects
// and dropped events self-heal.
func (s *Server) handlePodWatch(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	watcher, err := s.mgr.PodWatcherFor(r.PathValue("ctx"))
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	namespace := r.URL.Query().Get("namespace")

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// Subscribe before snapshotting so no delta is lost in the gap.
	events, unsubscribe := watcher.Subscribe(namespace)
	defer unsubscribe()

	for _, pod := range watcher.Snapshot(namespace) {
		writeSSE(w, kube.Event{Type: "ADDED", Object: &pod})
	}
	writeSSE(w, kube.Event{Type: "SYNCED"})
	flusher.Flush()

	keepalive := time.NewTicker(20 * time.Second)
	defer keepalive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-events:
			writeSSE(w, ev)
			flusher.Flush()
		case <-keepalive.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

func writeSSE(w http.ResponseWriter, ev kube.Event) {
	data, err := json.Marshal(ev)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", data)
}
