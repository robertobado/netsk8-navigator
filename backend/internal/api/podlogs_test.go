package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ktesting "k8s.io/client-go/testing"

	"github.com/robertobado/netsk8-navigator/backend/internal/config"
)

func testPodForLogs(name string) *corev1.Pod {
	return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "prod"}}
}

// getLogsErrorReactor fails only the GetLogs subresource call, the same way
// the fake clientset would surface a kubelet that refused the log request —
// leaving plain pod Get/List actions (used elsewhere) unaffected.
func getLogsErrorReactor(err error) ktesting.ReactionFunc {
	return func(action ktesting.Action) (bool, runtime.Object, error) {
		if action.GetVerb() != "get" || action.GetSubresource() != "log" {
			return false, nil, nil
		}
		return true, nil, err
	}
}

func TestHandlePodLogs_NotFlusher(t *testing.T) {
	s := newTestServer(t, testPodForLogs("web-1"))
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/contexts/test/pods/prod/web-1/logs", nil)
	s.Routes().ServeHTTP(noFlushWriter{rec}, r)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

// fakeManager's own ClientFor never errors, so reuse clientForErrManager
// (defined in portforward_test.go) to reach handlePodLogs' ClientFor branch.
func TestHandlePodLogs_ClientForError(t *testing.T) {
	cfg := config.NewStoreAt(filepath.Join(t.TempDir(), "config.json"))
	s := NewServer(clientForErrManager{newFakeManager()}, cfg, "")
	rec := doRequest(t, s, "GET", "/api/contexts/test/pods/prod/web-1/logs", "")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestHandlePodLogs_StreamError(t *testing.T) {
	s := newTestServer(t, testPodForLogs("web-1"))
	fakeClient(t, s).PrependReactor("get", "pods", getLogsErrorReactor(errors.New("boom")))
	rec := doRequest(t, s, "GET", "/api/contexts/test/pods/prod/web-1/logs", "")
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rec.Code)
	}
}

func TestHandlePodLogs_StreamsSuccessfully(t *testing.T) {
	s := newTestServer(t, testPodForLogs("web-1"))
	rec := doRequest(t, s, "GET", "/api/contexts/test/pods/prod/web-1/logs?container=app&tail=50", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.HasPrefix(body, "data: ") || !strings.HasSuffix(body, "\n\n") {
		t.Errorf("expected SSE-framed output, got %q", body)
	}
	if !strings.Contains(body, `"line":"fake logs"`) {
		t.Errorf("got %q, want the fake clientset's canned log line", body)
	}
	for _, h := range []struct{ key, want string }{
		{"Content-Type", "text/event-stream"},
		{"Cache-Control", "no-cache"},
		{"Connection", "keep-alive"},
		{"X-Accel-Buffering", "no"},
	} {
		if got := rec.Header().Get(h.key); got != h.want {
			t.Errorf("header %s = %q, want %q", h.key, got, h.want)
		}
	}
}

func TestWriteSSEData(t *testing.T) {
	rec := httptest.NewRecorder()
	writeSSEData(rec, []byte("hello"))
	body := rec.Body.String()
	if !strings.HasPrefix(body, "data: ") || !strings.HasSuffix(body, "\n\n") {
		t.Errorf("got %q, want an SSE-framed data line", body)
	}
	if !strings.Contains(body, `"line":"hello"`) {
		t.Errorf("got %q", body)
	}
}

func TestFetchBoundedLogs_ClientForError(t *testing.T) {
	cfg := config.NewStoreAt(filepath.Join(t.TempDir(), "config.json"))
	s := NewServer(clientForErrManager{newFakeManager()}, cfg, "")
	if _, err := s.fetchBoundedLogs(context.Background(), logQuery{Context: "test", Namespace: "prod", Name: "web-1", TailLines: 100}); err == nil {
		t.Error("want an error for an unknown context")
	}
}

func TestFetchBoundedLogs_StreamError(t *testing.T) {
	s := newTestServer(t, testPodForLogs("web-1"))
	fakeClient(t, s).PrependReactor("get", "pods", getLogsErrorReactor(errors.New("boom")))
	if _, err := s.fetchBoundedLogs(context.Background(), logQuery{Context: "test", Namespace: "prod", Name: "web-1", TailLines: 100}); err == nil {
		t.Error("want an error when the log stream fails")
	}
}

func TestFetchBoundedLogs_ClampsTailLines(t *testing.T) {
	s := newTestServer(t, testPodForLogs("web-1"))
	cases := []int64{0, -1, 5000}
	for _, tail := range cases {
		got, err := s.fetchBoundedLogs(context.Background(), logQuery{Context: "test", Namespace: "prod", Name: "web-1", Container: "app", TailLines: tail})
		if err != nil {
			t.Fatalf("tailLines=%d: unexpected error %v", tail, err)
		}
		if got != "fake logs" {
			t.Errorf("tailLines=%d: got %q, want the fake clientset's canned log line", tail, got)
		}
	}
}

func TestParseLogSince(t *testing.T) {
	secs := func(s string) int64 {
		t.Helper()
		v, at, err := parseLogSince(s)
		if err != nil || v == nil || at != nil {
			t.Fatalf("parseLogSince(%q) = %v, %v, %v", s, v, at, err)
		}
		return *v
	}
	for in, want := range map[string]int64{"30m": 1800, "2h": 7200, "1d": 86400, "1.5h": 5400, "45s": 45, " 10m ": 600, "1h30m": 5400} {
		if got := secs(in); got != want {
			t.Errorf("since %q = %d s, want %d", in, got, want)
		}
	}

	v, at, err := parseLogSince("2026-09-20T06:00:00Z")
	if err != nil || v != nil || at == nil || !at.Time.Equal(time.Date(2026, 9, 20, 6, 0, 0, 0, time.UTC)) {
		t.Errorf("an RFC3339 time should become SinceTime, got %v %v %v", v, at, err)
	}
	if v, at, err := parseLogSince(""); v != nil || at != nil || err != nil {
		t.Errorf("empty since is no window, got %v %v %v", v, at, err)
	}
	for _, bad := range []string{"soon", "-5m", "0s", "1x", "yesterday"} {
		if _, _, err := parseLogSince(bad); err == nil {
			t.Errorf("since %q should be rejected", bad)
		}
	}
}

func TestLogQueryOptions(t *testing.T) {
	tail := func(q logQuery) int64 {
		t.Helper()
		o, err := q.options()
		if err != nil {
			t.Fatal(err)
		}
		if !o.Timestamps || o.Follow {
			t.Errorf("logs must be timestamped and non-following: %+v", o)
		}
		return *o.TailLines
	}
	cases := []struct {
		q    logQuery
		want int64
	}{
		{logQuery{}, 200},
		{logQuery{TailLines: -3}, 200},
		{logQuery{TailLines: 5000}, 200},
		{logQuery{TailLines: 50}, 50},
		{logQuery{Since: "30m"}, 2000}, // a window with no cap means "everything in it"
		{logQuery{Since: "30m", TailLines: 50}, 50},
	}
	for _, c := range cases {
		if got := tail(c.q); got != c.want {
			t.Errorf("%+v => tail %d, want %d", c.q, got, c.want)
		}
	}
	o, _ := logQuery{Since: "10m", Previous: true}.options()
	if !o.Previous || o.SinceSeconds == nil || *o.SinceSeconds != 600 {
		t.Errorf("previous/since must reach the API options, got %+v", o)
	}
	if _, err := (logQuery{Since: "nope"}).options(); err == nil {
		t.Error("a bad since must be an error")
	}
}

func TestMergeStampedLines(t *testing.T) {
	a := stampLines(logTarget{"web-1", "app"}, "2026-09-20T06:00:01.5Z one\n2026-09-20T06:00:04Z four\n", false)
	b := stampLines(logTarget{"web-2", "app"}, "2026-09-20T06:00:02Z two\ncontinuation without a timestamp\n2026-09-20T06:00:03.25Z three\n", false)
	got := mergeStampedLines(append(a, b...), []string{"[note] x"})
	want := strings.Join([]string{
		"[web-1/app] 2026-09-20T06:00:01.5Z one",
		"[web-2/app] 2026-09-20T06:00:02Z two",
		"[web-2/app] continuation without a timestamp", // stays right behind its parent line
		"[web-2/app] 2026-09-20T06:00:03.25Z three",
		"[web-1/app] 2026-09-20T06:00:04Z four",
		"[note] x", "",
	}, "\n")
	if got != want {
		t.Errorf("merged =\n%s\nwant\n%s", got, want)
	}
}

var isController = true

func ownedPod(name string, containers ...string) *corev1.Pod {
	p := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name: name, Namespace: "prod",
		OwnerReferences: []metav1.OwnerReference{{Kind: "StatefulSet", Name: "db", Controller: &isController}},
	}}
	for _, c := range containers {
		p.Spec.Containers = append(p.Spec.Containers, corev1.Container{Name: c})
	}
	return p
}

func TestFetchBoundedLogs_Targets(t *testing.T) {
	s := newTestServer(t,
		ownedPod("db-1", "pg"),
		ownedPod("db-0", "pg", "exporter"),
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "solo", Namespace: "prod"}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "a"}, {Name: "b"}}}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "single", Namespace: "prod"}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "only"}}}},
	)
	read := func(q logQuery) (string, error) {
		q.Context, q.Namespace = "test", "prod"
		return s.fetchBoundedLogs(context.Background(), q)
	}

	// One container reads as plain text — no prefix — as it always did.
	if got, err := read(logQuery{Name: "single"}); err != nil || got != "fake logs" {
		t.Errorf("single-container pod = %q, %v", got, err)
	}
	// A multi-container pod with no container named reads every container
	// (kubectl would refuse and make the caller guess a name).
	got, err := read(logQuery{Name: "solo"})
	if err != nil || got != "[solo/a] fake logs\n[solo/b] fake logs\n" {
		t.Errorf("multi-container pod = %q, %v", got, err)
	}
	// A whole workload, pods in name order, every container; "sts" is accepted.
	got, err = read(logQuery{Kind: "sts", Name: "db"})
	if err != nil || got != "[db-0/pg] fake logs\n[db-0/exporter] fake logs\n[db-1/pg] fake logs\n" {
		t.Errorf("statefulset logs = %q, %v", got, err)
	}
	// A named container narrows to the pods that have it.
	got, err = read(logQuery{Kind: "statefulset", Name: "db", Container: "exporter"})
	if err != nil || got != "fake logs" { // one target left => plain text
		t.Errorf("container filter = %q, %v", got, err)
	}

	for name, q := range map[string]logQuery{
		"unsupported kind":      {Kind: "configmap", Name: "x"},
		"workload with no pods": {Kind: "statefulset", Name: "ghost"},
		"container no pod has":  {Kind: "statefulset", Name: "db", Container: "nope"},
		"invalid since":         {Name: "single", Since: "yesterday"},
	} {
		if _, err := read(q); err == nil {
			t.Errorf("%s should be an error", name)
		}
	}
	if _, err := read(logQuery{Kind: "configmap", Name: "x"}); err == nil || !strings.Contains(err.Error(), "deployment") {
		t.Errorf("the unsupported-kind error should list the valid kinds, got %v", err)
	}
}

func TestFetchBoundedLogs_OptionsReachTheAPI(t *testing.T) {
	s := newTestServer(t, testPodForLogs("web-1"))
	var got *corev1.PodLogOptions
	fakeClient(t, s).PrependReactor("get", "pods", func(a ktesting.Action) (bool, runtime.Object, error) {
		if ga, ok := a.(ktesting.GenericActionImpl); ok && a.GetSubresource() == "log" {
			got, _ = ga.Value.(*corev1.PodLogOptions)
		}
		return false, nil, nil
	})
	_, err := s.fetchBoundedLogs(context.Background(), logQuery{Context: "test", Namespace: "prod", Name: "web-1", Container: "app", Since: "30m", Previous: true})
	if err != nil || got == nil {
		t.Fatalf("err=%v opts=%v", err, got)
	}
	if !got.Previous || got.Container != "app" || got.SinceSeconds == nil || *got.SinceSeconds != 1800 || *got.TailLines != 2000 {
		t.Errorf("options = %+v", got)
	}
}

func TestFetchBoundedLogs_PartialFailureAndCap(t *testing.T) {
	t.Run("a failing container is skipped with a note when others succeed", func(t *testing.T) {
		s := newTestServer(t, ownedPod("db-0", "pg", "exporter"))
		fakeClient(t, s).PrependReactor("get", "pods", func(a ktesting.Action) (bool, runtime.Object, error) {
			if ga, ok := a.(ktesting.GenericActionImpl); ok && a.GetSubresource() == "log" {
				if o, _ := ga.Value.(*corev1.PodLogOptions); o != nil && o.Container == "exporter" {
					return true, nil, errors.New("previous terminated container not found")
				}
			}
			return false, nil, nil
		})
		got, err := s.fetchBoundedLogs(context.Background(), logQuery{Context: "test", Namespace: "prod", Kind: "statefulset", Name: "db"})
		if err != nil || !strings.Contains(got, "[db-0/pg] fake logs") || !strings.Contains(got, "[note] skipped db-0/exporter:") || !strings.Contains(got, "previous terminated container not found") {
			t.Errorf("got %q, %v", got, err)
		}
	})

	t.Run("every container failing is an error", func(t *testing.T) {
		s := newTestServer(t, ownedPod("db-0", "pg", "exporter"))
		fakeClient(t, s).PrependReactor("get", "pods", getLogsErrorReactor(errors.New("boom")))
		if _, err := s.fetchBoundedLogs(context.Background(), logQuery{Context: "test", Namespace: "prod", Kind: "statefulset", Name: "db"}); err == nil || !strings.Contains(err.Error(), "boom") {
			t.Errorf("want the underlying error, got %v", err)
		}
	})

	t.Run("more than maxLogStreams containers are capped with a note", func(t *testing.T) {
		var objs []runtime.Object
		for i := 0; i < maxLogStreams+5; i++ {
			objs = append(objs, ownedPod(fmt.Sprintf("db-%02d", i), "pg"))
		}
		s := newTestServer(t, objs...)
		got, err := s.fetchBoundedLogs(context.Background(), logQuery{Context: "test", Namespace: "prod", Kind: "statefulset", Name: "db"})
		if err != nil {
			t.Fatal(err)
		}
		if n := strings.Count(got, "fake logs"); n != maxLogStreams || !strings.Contains(got, "25 containers match; showing the first 20") {
			t.Errorf("%d streams, output tail: %.200s", n, got[max(0, len(got)-200):])
		}
	})
}

// End to end through the tool: the whole reason for the feature — one call for
// `kubectl logs sts/db | grep -i … | tail`.
func TestMCPGetLogs_WorkloadWithFilters(t *testing.T) {
	s := newTestServer(t, ownedPod("db-0", "pg"), ownedPod("db-1", "pg"))
	enableMCP(s, false)
	session := mcpConnect(t, s)
	call := func(extra map[string]any) (string, bool) {
		args := map[string]any{"context": "test", "namespace": "prod", "kind": "statefulset", "name": "db"}
		for k, v := range extra {
			args[k] = v
		}
		return toolText(t, session, "get_logs", args)
	}

	if got, isErr := call(nil); isErr || got != "[db-0/pg] fake logs\n[db-1/pg] fake logs\n" {
		t.Errorf("workload logs = %q (err %v)", got, isErr)
	}
	if got, _ := call(map[string]any{"filter": map[string]any{"grep": "FAKE", "ignoreCase": true, "count": true}}); got != "2\n" {
		t.Errorf("grep -ic across replicas = %q, want 2", got)
	}
	if got, _ := call(map[string]any{"filter": map[string]any{"grep": "db-1"}}); got != "[db-1/pg] fake logs\n" {
		t.Errorf("the [pod/container] prefix is greppable, got %q", got)
	}
	if got, _ := call(map[string]any{"since": "30m", "previous": true, "filter": map[string]any{"maxLineLength": 8}}); got != "[db-0/pg…\n[db-1/pg…\n" {
		t.Errorf("since/previous/maxLineLength = %q", got)
	}
	if text, isErr := call(map[string]any{"since": "someday"}); !isErr || !strings.Contains(text, "since must be") {
		t.Errorf("a bad since is a tool error, got isErr=%v %s", isErr, text)
	}
}

func TestHideTimestamps(t *testing.T) {
	if got := stripLogTimestamps("2026-09-20T06:00:01.5Z one\nno stamp here\n2026-09-20T06:00:02Z two\n"); got != "one\nno stamp here\ntwo\n" {
		t.Errorf("stripLogTimestamps = %q", got)
	}

	// Hidden timestamps still order a merged stream.
	a := stampLines(logTarget{"a", "c"}, "2026-09-20T06:00:03Z late\n", true)
	b := stampLines(logTarget{"b", "c"}, "2026-09-20T06:00:01Z early\n", true)
	if got := mergeStampedLines(append(a, b...), nil); got != "[b/c] early\n[a/c] late\n" {
		t.Errorf("merged with hidden timestamps = %q", got)
	}

	// Through the fetch (the fake serves a line with no timestamp, so it's untouched).
	s := newTestServer(t, testPodForLogs("web-1"))
	if got, err := s.fetchBoundedLogs(context.Background(), logQuery{Context: "test", Namespace: "prod", Name: "web-1", Container: "app", HideTimestamps: true}); err != nil || got != "fake logs" {
		t.Errorf("got %q, %v", got, err)
	}
}
