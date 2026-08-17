package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

// maxAggregatedLogPods caps how many pods stream concurrently — enough for any
// realistic workload page, without opening an unbounded number of upstream
// kubelet log connections for a huge DaemonSet.
const maxAggregatedLogPods = 12

// multiLogLine is one line from one pod, sent over SSE tagged with its source
// so the UI can color/group/filter by pod.
type multiLogLine struct {
	Pod  string `json:"pod"`
	Line string `json:"line"`
}

func writeSSEMultiLine(w http.ResponseWriter, l multiLogLine) {
	data, err := json.Marshal(l)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
}

// handleWorkloadLogs streams SSE logs from every pod of a workload (up to
// maxAggregatedLogPods), each line tagged with its source pod.
// GET /api/contexts/{ctx}/pods-of/{kind}/{namespace}/{name}/logs?container=
func (s *Server) handleWorkloadLogs(w http.ResponseWriter, r *http.Request) {
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

	ns := r.PathValue("namespace")
	pods, err := resolveWorkloadPods(r.Context(), client, r.PathValue("kind"), ns, r.PathValue("name"))
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	if len(pods) == 0 {
		writeError(w, http.StatusNotFound, fmt.Errorf("no pods found for this workload"))
		return
	}
	if len(pods) > maxAggregatedLogPods {
		pods = pods[:maxAggregatedLogPods]
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	container := r.URL.Query().Get("container")
	// A smaller per-pod tail than the single-pod view keeps N-pods × tail readable.
	tail := int64(200)

	ctx := r.Context()
	lines := make(chan multiLogLine)
	var wg sync.WaitGroup
	for i := range pods {
		wg.Add(1)
		go streamPodLogsInto(ctx, &wg, client, podLogsTarget{namespace: ns, pod: pods[i].Name, container: container}, tail, lines)
	}
	go func() {
		wg.Wait()
		close(lines)
	}()

	for l := range lines {
		writeSSEMultiLine(w, l)
		flusher.Flush()
		if ctx.Err() != nil {
			return
		}
	}
}

// podLogsTarget names the pod/namespace/container streamPodLogsInto follows —
// grouped into one param to keep the function's parameter count in check.
type podLogsTarget struct {
	namespace string
	pod       string
	container string
}

// streamPodLogsInto follows one pod's logs, sending each line into out until
// the stream ends or ctx is cancelled. A pod that fails to stream (e.g. still
// pending) is silently skipped — the others keep flowing.
func streamPodLogsInto(ctx context.Context, wg *sync.WaitGroup, client kubernetes.Interface, target podLogsTarget, tail int64, out chan<- multiLogLine) {
	defer wg.Done()
	opts := &corev1.PodLogOptions{
		Follow:     true,
		Container:  target.container,
		TailLines:  &tail,
		Timestamps: true,
	}
	stream, err := client.CoreV1().Pods(target.namespace).GetLogs(target.pod, opts).Stream(ctx)
	if err != nil {
		return
	}
	defer func() { _ = stream.Close() }()

	scanner := bufio.NewScanner(stream)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		select {
		case out <- multiLogLine{Pod: target.pod, Line: scanner.Text()}:
		case <-ctx.Done():
			return
		}
	}
}
