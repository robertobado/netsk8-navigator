package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
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
	defer func() { _ = stream.Close() }()

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

// fetchBoundedPodLogs returns a container's most recent tailLines log lines
// as a single string — used by the MCP get_logs tool, which (unlike the SSE
// handler above) needs a bounded, non-streaming read: replaying a
// Follow:true request through an in-process httptest.ResponseRecorder would
// simply hang forever, since a recorder has no way to signal "stop
// following" the way a real client disconnect does. tailLines is clamped to
// a sane range, and the read itself is capped so a chatty container can't
// blow up a tool result.
func (s *Server) fetchBoundedPodLogs(ctx context.Context, clusterCtx, namespace, name, container string, tailLines int64) (string, error) {
	client, err := s.mgr.ClientFor(clusterCtx)
	if err != nil {
		return "", err
	}
	if tailLines <= 0 || tailLines > 2000 {
		tailLines = 200
	}
	opts := &corev1.PodLogOptions{
		Follow:     false,
		Container:  container,
		TailLines:  &tailLines,
		Timestamps: true,
	}
	req := client.CoreV1().Pods(namespace).GetLogs(name, opts)
	stream, err := req.Stream(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = stream.Close() }()

	const maxBytes = 512 * 1024
	b, err := io.ReadAll(io.LimitReader(stream, maxBytes))
	if err != nil {
		return "", err
	}
	return string(b), nil
}
