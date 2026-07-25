package api

import (
	"encoding/json"
	"io"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
)

var upgrader = websocket.Upgrader{
	// Dev: accept any origin. Restrict before any real deployment.
	CheckOrigin: func(*http.Request) bool { return true },
}

// handlePodExec bridges a browser terminal (xterm.js over WebSocket) to a shell
// running inside a pod container via client-go's SPDY executor.
// Protocol client->server (JSON text): {"type":"stdin","data":".."} |
// {"type":"resize","cols":N,"rows":M}. Server->client: raw terminal bytes.
func (s *Server) handlePodExec(w http.ResponseWriter, r *http.Request) {
	client, err := s.mgr.ClientFor(r.PathValue("ctx"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	restCfg, err := s.mgr.RESTConfigFor(r.PathValue("ctx"))
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	// Prefer bash, fall back to sh, with a sane TERM. Note: `exec bash || exec sh`
	// does NOT work — when bash is missing, `exec` makes the non-interactive shell
	// exit 127 before the `||` runs. So only exec bash once we've confirmed it exists.
	cmd := []string{"/bin/sh", "-c", "export TERM=xterm-256color; if command -v bash >/dev/null 2>&1; then exec bash; fi; exec sh"}

	req := client.CoreV1().RESTClient().Post().
		Resource("pods").Name(r.PathValue("name")).Namespace(r.PathValue("namespace")).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: r.URL.Query().Get("container"),
			Command:   cmd,
			Stdin:     true,
			Stdout:    true,
			Stderr:    true,
			TTY:       true,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(restCfg, "POST", req.URL())
	if err != nil {
		_ = conn.WriteMessage(websocket.TextMessage, []byte("\r\nexec setup error: "+err.Error()+"\r\n"))
		return
	}

	h := newWSHandler(conn)
	err = exec.StreamWithContext(r.Context(), remotecommand.StreamOptions{
		Stdin:             h,
		Stdout:            h,
		Stderr:            h,
		Tty:               true,
		TerminalSizeQueue: h,
	})
	if err != nil {
		_ = conn.WriteMessage(websocket.TextMessage, []byte("\r\n["+err.Error()+"]\r\n"))
	}
}

// wsHandler adapts a WebSocket connection to the io.Reader (stdin),
// io.Writer (stdout/stderr) and remotecommand.TerminalSizeQueue interfaces.
type wsHandler struct {
	conn  *websocket.Conn
	stdin chan []byte
	sizes chan remotecommand.TerminalSize
	buf   []byte
	mu    sync.Mutex // serializes conn writes (gorilla forbids concurrent writes)
}

func newWSHandler(conn *websocket.Conn) *wsHandler {
	h := &wsHandler{
		conn:  conn,
		stdin: make(chan []byte, 32),
		sizes: make(chan remotecommand.TerminalSize, 4),
	}
	h.sizes <- remotecommand.TerminalSize{Width: 80, Height: 24} // seed before client resize
	go h.readLoop()
	return h
}

func (h *wsHandler) readLoop() {
	defer close(h.stdin)
	for {
		_, data, err := h.conn.ReadMessage()
		if err != nil {
			return
		}
		var msg struct {
			Type string `json:"type"`
			Data string `json:"data"`
			Cols uint16 `json:"cols"`
			Rows uint16 `json:"rows"`
		}
		if json.Unmarshal(data, &msg) != nil {
			continue
		}
		switch msg.Type {
		case "stdin":
			h.stdin <- []byte(msg.Data)
		case "resize":
			select {
			case h.sizes <- remotecommand.TerminalSize{Width: msg.Cols, Height: msg.Rows}:
			default:
			}
		}
	}
}

func (h *wsHandler) Read(p []byte) (int, error) {
	if len(h.buf) == 0 {
		b, ok := <-h.stdin
		if !ok {
			return 0, io.EOF
		}
		h.buf = b
	}
	n := copy(p, h.buf)
	h.buf = h.buf[n:]
	return n, nil
}

func (h *wsHandler) Write(p []byte) (int, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.conn.WriteMessage(websocket.TextMessage, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (h *wsHandler) Next() *remotecommand.TerminalSize {
	s, ok := <-h.sizes
	if !ok {
		return nil
	}
	return &s
}
