package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
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

const (
	maxLogStreams     = 20
	maxLogStreamBytes = 512 * 1024
)

// logQuery is one bounded log read for the MCP get_logs tool: a single pod, or
// every pod behind a workload, over an optional time window.
type logQuery struct {
	Context, Namespace, Kind, Name, Container, Since string
	TailLines                                        int64
	Previous                                         bool
	HideTimestamps                                   bool
}

// logTarget is one container of one pod.
type logTarget struct{ pod, container string }

// fetchBoundedLogs returns the most recent log lines for q as a single string.
// Unlike the SSE handler above it needs a bounded, non-streaming read:
// replaying a Follow:true request through an in-process httptest.ResponseRecorder
// would simply hang forever, since a recorder has no way to signal "stop
// following" the way a real client disconnect does.
//
// One container reads as plain text. Several (a Deployment's replicas, a
// multi-container pod) read as one stream: each line is prefixed
// "[pod/container] " and the lines are merged by their timestamp, like
// `kubectl logs --prefix` but chronological. TailLines is per container.
func (s *Server) fetchBoundedLogs(ctx context.Context, q logQuery) (string, error) {
	client, err := s.mgr.ClientFor(q.Context)
	if err != nil {
		return "", err
	}
	opts, err := q.options()
	if err != nil {
		return "", err
	}
	targets, err := resolveLogTargets(ctx, client, q)
	if err != nil {
		return "", err
	}
	var notes []string
	if len(targets) > maxLogStreams {
		notes = append(notes, fmt.Sprintf("[note] %d containers match; showing the first %d — narrow with kind/name/container", len(targets), maxLogStreams))
		targets = targets[:maxLogStreams]
	}

	if len(targets) == 1 {
		text, err := readContainerLogs(ctx, client, q.Namespace, targets[0], opts)
		if q.HideTimestamps {
			text = stripLogTimestamps(text)
		}
		return text, err
	}

	var lines []stampedLine
	var firstErr error
	for _, t := range targets {
		text, err := readContainerLogs(ctx, client, q.Namespace, t, opts)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("%s/%s: %w", t.pod, t.container, err)
			}
			notes = append(notes, fmt.Sprintf("[note] skipped %s/%s: %v", t.pod, t.container, err))
			continue
		}
		lines = append(lines, stampLines(t, text, q.HideTimestamps)...)
	}
	if len(lines) == 0 && firstErr != nil {
		return "", firstErr
	}
	return mergeStampedLines(lines, notes), nil
}

// mergeStampedLines orders lines chronologically (stable, so equal or
// unknown times keep their stream order) and appends the notes last.
func mergeStampedLines(lines []stampedLine, notes []string) string {
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].at.Before(lines[j].at) })
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l.text)
		b.WriteByte('\n')
	}
	for _, n := range notes {
		b.WriteString(n)
		b.WriteByte('\n')
	}
	return b.String()
}

// options builds the PodLogOptions shared by every container of the read.
func (q logQuery) options() (*corev1.PodLogOptions, error) {
	tail := q.TailLines
	if tail <= 0 || tail > 2000 {
		tail = 200
		if q.Since != "" && q.TailLines <= 0 {
			tail = 2000 // a time window with no explicit cap means "everything in it", as kubectl does
		}
	}
	opts := &corev1.PodLogOptions{Follow: false, TailLines: &tail, Timestamps: true, Previous: q.Previous}
	secs, at, err := parseLogSince(q.Since)
	if err != nil {
		return nil, err
	}
	opts.SinceSeconds, opts.SinceTime = secs, at
	return opts, nil
}

// parseLogSince reads a --since style window: a duration ("30m", "2h", "1d") or
// an absolute RFC3339 time.
func parseLogSince(since string) (*int64, *metav1.Time, error) {
	since = strings.TrimSpace(since)
	if since == "" {
		return nil, nil, nil
	}
	if d, err := parseSinceDuration(since); err == nil {
		if d <= 0 {
			return nil, nil, fmt.Errorf("since must be a positive duration, got %q", since)
		}
		secs := int64(d / time.Second)
		if secs == 0 {
			secs = 1
		}
		return &secs, nil, nil
	}
	if t, err := time.Parse(time.RFC3339, since); err == nil {
		mt := metav1.NewTime(t)
		return nil, &mt, nil
	}
	return nil, nil, fmt.Errorf("since must be a duration like 30m, 2h or 1d, or an RFC3339 timestamp, got %q", since)
}

func parseSinceDuration(s string) (time.Duration, error) {
	if days, ok := strings.CutSuffix(s, "d"); ok {
		n, err := strconv.ParseFloat(days, 64)
		if err != nil {
			return 0, err
		}
		return time.Duration(n * float64(24*time.Hour)), nil
	}
	return time.ParseDuration(s)
}

// resolveLogTargets turns a query into the containers to read: the one pod, or
// every pod behind the named workload; the named container in each, or all of
// a pod's containers when none is named.
func resolveLogTargets(ctx context.Context, client kubernetes.Interface, q logQuery) ([]logTarget, error) {
	kind := normalizeKindSlug(q.Kind)
	if kind == "" || kind == "pod" {
		if q.Container != "" {
			return []logTarget{{q.Name, q.Container}}, nil
		}
		pod, err := client.CoreV1().Pods(q.Namespace).Get(ctx, q.Name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		if targets := podLogTargets(pod, ""); len(targets) > 0 {
			return targets, nil
		}
		return []logTarget{{q.Name, ""}}, nil // no containers in the spec (a bare object): let the API pick
	}

	if _, ok := logWorkloadKinds[kind]; !ok {
		return nil, fmt.Errorf("kind %q has no logs of its own; use pod, or one of: %s", q.Kind, strings.Join(sortedKeys(logWorkloadKinds), ", "))
	}
	pods, err := resolveWorkloadPods(ctx, client, kind, q.Namespace, q.Name)
	if err != nil {
		return nil, err
	}
	if len(pods) == 0 {
		return nil, fmt.Errorf("no pods found for %s/%s in namespace %s", kind, q.Name, q.Namespace)
	}
	sort.Slice(pods, func(i, j int) bool { return pods[i].Name < pods[j].Name })
	var targets []logTarget
	for i := range pods {
		targets = append(targets, podLogTargets(&pods[i], q.Container)...)
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("no pod behind %s/%s has a container named %q", kind, q.Name, q.Container)
	}
	return targets, nil
}

// logWorkloadKinds are the kinds whose pods resolveWorkloadPods can find.
var logWorkloadKinds = map[string]bool{"deployment": true, "statefulset": true, "daemonset": true, "replicaset": true, "job": true, "service": true}

// podLogTargets lists a pod's containers (init containers included, so a
// failed init step is readable), or just the named one when it has it.
func podLogTargets(pod *corev1.Pod, container string) []logTarget {
	var out []logTarget
	add := func(cs []corev1.Container) {
		for _, c := range cs {
			if container == "" || c.Name == container {
				out = append(out, logTarget{pod.Name, c.Name})
			}
		}
	}
	add(pod.Spec.InitContainers)
	add(pod.Spec.Containers)
	return out
}

func readContainerLogs(ctx context.Context, client kubernetes.Interface, namespace string, t logTarget, opts *corev1.PodLogOptions) (string, error) {
	o := *opts
	o.Container = t.container
	stream, err := client.CoreV1().Pods(namespace).GetLogs(t.pod, &o).Stream(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = stream.Close() }()
	b, err := io.ReadAll(io.LimitReader(stream, maxLogStreamBytes))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// stampedLine is one log line tagged with its source and parsed timestamp.
type stampedLine struct {
	at   time.Time
	text string
}

// stampLines splits a container's log text into lines prefixed with their
// source. The timestamp the API prepends (Timestamps: true) orders them, and is
// dropped from the text when hide is set; a line without a parsable one (rare)
// inherits the previous line's, so a stray continuation line stays next to its
// parent.
func stampLines(t logTarget, text string, hide bool) []stampedLine {
	prefix := "[" + t.pod + "/" + t.container + "] "
	var out []stampedLine
	var prev time.Time
	for _, l := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		if l == "" {
			continue
		}
		at, rest, ok := splitLogTimestamp(l)
		if !ok {
			at, rest = prev, l
		}
		prev = at
		body := l
		if hide {
			body = rest
		}
		out = append(out, stampedLine{at: at, text: prefix + body})
	}
	return out
}

// splitLogTimestamp separates the RFC3339Nano timestamp the API prepends to a
// log line from the message that follows it.
func splitLogTimestamp(line string) (time.Time, string, bool) {
	ts, rest, found := strings.Cut(line, " ")
	if !found {
		return time.Time{}, line, false
	}
	at, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return time.Time{}, line, false
	}
	return at, rest, true
}

// stripLogTimestamps drops the leading timestamp from every line that has one.
func stripLogTimestamps(text string) string {
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		if _, rest, ok := splitLogTimestamp(l); ok {
			lines[i] = rest
		}
	}
	return strings.Join(lines, "\n")
}
