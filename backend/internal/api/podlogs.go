package api

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"

	corev1 "k8s.io/api/core/v1"
)

// writeSSEData sends a single log line as an SSE event, JSON-encoded so any
// special characters survive intact.
func writeSSEData(w http.ResponseWriter, line []byte) {
	data, err := json.Marshal(map[string]string{"line": string(line)})
	if err != nil {
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", data)
}

// handlePodLogs streams a pod container's logs as SSE, following new lines.
// GET /api/contexts/{ctx}/pods/{namespace}/{name}/logs?container=&tail=
func (s *Server) handlePodLogs(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	client, err := s.mgr.ClientFor(r.PathValue("ctx"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	tail := int64(1000)
	opts := &corev1.PodLogOptions{
		Follow:     true,
		Container:  r.URL.Query().Get("container"),
		TailLines:  &tail,
		Timestamps: true, // kubelet prepends RFC3339Nano; the UI parses it into a column
	}

	req := client.CoreV1().Pods(r.PathValue("namespace")).GetLogs(r.PathValue("name"), opts)
	stream, err := req.Stream(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	defer stream.Close()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	scanner := bufio.NewScanner(stream)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		writeSSEData(w, scanner.Bytes())
		flusher.Flush()
		if r.Context().Err() != nil {
			return
		}
	}
}
