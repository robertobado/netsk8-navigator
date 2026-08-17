package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ktesting "k8s.io/client-go/testing"
)

// This covers the handler's own branching logic (resolving the workload's
// pods and rejecting an empty result) plus, contrary to what an earlier
// comment here assumed, the full SSE fan-in itself: the fake clientset's
// Pods().GetLogs(...).Stream(ctx) doesn't dial out — it returns a canned
// "fake logs" 200 response synchronously (see
// k8s.io/client-go/kubernetes/typed/core/v1/fake.fakePods.GetLogs), so
// streamPodLogsInto completes immediately with no live kubelet needed.
func TestHandleWorkloadLogs_NoPods(t *testing.T) {
	s := newTestServer(t)
	rec := doRequest(t, s, "GET", "/api/contexts/test/pods-of/deployment/prod/web/logs", "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 (no pods found)", rec.Code)
	}
}

func TestWriteSSEMultiLine(t *testing.T) {
	rec := httptest.NewRecorder()
	writeSSEMultiLine(rec, multiLogLine{Pod: "web-1", Line: "hello"})
	body := rec.Body.String()
	if !strings.HasPrefix(body, "data: ") || !strings.HasSuffix(body, "\n\n") {
		t.Errorf("got %q, want an SSE-framed data line", body)
	}
	if !strings.Contains(body, `"pod":"web-1"`) || !strings.Contains(body, `"line":"hello"`) {
		t.Errorf("got %q", body)
	}
}

// noFlushWriter hides http.Flusher behind the http.ResponseWriter interface
// type (method promotion only sees the interface's declared methods, not
// httptest.ResponseRecorder's concrete Flush), for exercising
// handleWorkloadLogs' "streaming unsupported" guard.
type noFlushWriter struct{ http.ResponseWriter }

func TestHandleWorkloadLogs_NotFlusher(t *testing.T) {
	s := newTestServer(t)
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/contexts/test/pods-of/deployment/prod/web/logs", nil)
	s.Routes().ServeHTTP(noFlushWriter{rec}, r)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestHandleWorkloadLogs_ClientListError(t *testing.T) {
	s := newTestServer(t)
	fakeClient(t, s).PrependReactor("list", "pods", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("boom")
	})
	rec := doRequest(t, s, "GET", "/api/contexts/test/pods-of/deployment/prod/web/logs", "")
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rec.Code)
	}
}

func jobOwnedPod(name string) *corev1.Pod {
	controller := true
	return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name:            name,
		Namespace:       "prod",
		OwnerReferences: []metav1.OwnerReference{{Kind: "Job", Name: "batch", Controller: &controller}},
	}}
}

func TestHandleWorkloadLogs_StreamsSuccessfully(t *testing.T) {
	s := newTestServer(t, jobOwnedPod("batch-0"))
	rec := doRequest(t, s, "GET", "/api/contexts/test/pods-of/job/prod/batch/logs", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.HasPrefix(body, "data: ") {
		t.Errorf("expected SSE-framed output, got %q", body)
	}
	if !strings.Contains(body, `"pod":"batch-0"`) || !strings.Contains(body, `"line":"fake logs"`) {
		t.Errorf("got %q, want the fake clientset's canned log line tagged with its pod", body)
	}
}

func TestHandleWorkloadLogs_TruncatesToMaxPods(t *testing.T) {
	objs := make([]runtime.Object, 0, maxAggregatedLogPods+1)
	for i := 0; i < maxAggregatedLogPods+1; i++ {
		objs = append(objs, jobOwnedPod(fmt.Sprintf("batch-%d", i)))
	}
	s := newTestServer(t, objs...)
	rec := doRequest(t, s, "GET", "/api/contexts/test/pods-of/job/prod/batch/logs", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if got := strings.Count(rec.Body.String(), `"line":`); got != maxAggregatedLogPods {
		t.Errorf("got %d streamed lines, want %d (truncated to maxAggregatedLogPods)", got, maxAggregatedLogPods)
	}
}
