package api

import (
	"fmt"
	"net/http"
	"sync"
	"time"
)

// appEvents is a tiny in-process pub/sub that lets native Go code (a Wails
// menu callback) signal the frontend without depending on Wails' own JS
// bridge (window.wails/window.runtime). That bridge is never present in this
// app: cmd/desktop/main.go's window navigates away from Wails' asset server
// to this server's own real HTTP origin immediately at startup (see its
// bootstrapRedirect comment, kept for SSE/WebSocket to work like a plain
// browser), and Wails only injects the bridge into pages it serves itself.
// BroadcastAppEvent is a plain same-process function call — no HTTP, no JS —
// and handleAppEvents fans it out to the frontend over SSE, the same
// mechanism the pod watch stream (watch.go) already relies on.
type appEvents struct {
	mu   sync.Mutex
	subs map[chan string]struct{}
}

func (a *appEvents) subscribe() (chan string, func()) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.subs == nil {
		a.subs = make(map[chan string]struct{})
	}
	ch := make(chan string, 4)
	a.subs[ch] = struct{}{}
	return ch, func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		if _, ok := a.subs[ch]; ok {
			delete(a.subs, ch)
			close(ch)
		}
	}
}

// BroadcastAppEvent notifies every connected frontend of a named event (e.g.
// "show-about"). In practice there's exactly one subscriber — this app's
// single window — but it fans out to however many are connected. Never
// blocks: a subscriber that isn't keeping up just misses the event rather
// than stalling the caller, which for the intended use (a menu callback on
// the app's main thread) must never hang.
func (s *Server) BroadcastAppEvent(name string) {
	s.appEv.mu.Lock()
	defer s.appEv.mu.Unlock()
	for ch := range s.appEv.subs {
		select {
		case ch <- name:
		default:
		}
	}
}

// handleAppEvents streams BroadcastAppEvent calls to the frontend as SSE. In
// the plain server/browser binary nothing ever calls BroadcastAppEvent, so
// this just idles (keepalive pings only) until the client disconnects —
// harmless, and consistent with the other long-lived SSE routes.
func (s *Server) handleAppEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ch, unsubscribe := s.appEv.subscribe()
	defer unsubscribe()
	flusher.Flush() // opens the stream now, so the client's EventSource.onopen fires promptly

	keepalive := time.NewTicker(20 * time.Second)
	defer keepalive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case name := <-ch:
			_, _ = fmt.Fprintf(w, "data: %s\n\n", name)
			flusher.Flush()
		case <-keepalive.C:
			_, _ = fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}
