package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

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

func TestFetchBoundedPodLogs_ClientForError(t *testing.T) {
	cfg := config.NewStoreAt(filepath.Join(t.TempDir(), "config.json"))
	s := NewServer(clientForErrManager{newFakeManager()}, cfg, "")
	if _, err := s.fetchBoundedPodLogs(context.Background(), "test", "prod", "web-1", "", 100); err == nil {
		t.Error("want an error for an unknown context")
	}
}

func TestFetchBoundedPodLogs_StreamError(t *testing.T) {
	s := newTestServer(t, testPodForLogs("web-1"))
	fakeClient(t, s).PrependReactor("get", "pods", getLogsErrorReactor(errors.New("boom")))
	if _, err := s.fetchBoundedPodLogs(context.Background(), "test", "prod", "web-1", "", 100); err == nil {
		t.Error("want an error when the log stream fails")
	}
}

func TestFetchBoundedPodLogs_ClampsTailLines(t *testing.T) {
	s := newTestServer(t, testPodForLogs("web-1"))
	cases := []int64{0, -1, 5000}
	for _, tail := range cases {
		got, err := s.fetchBoundedPodLogs(context.Background(), "test", "prod", "web-1", "app", tail)
		if err != nil {
			t.Fatalf("tailLines=%d: unexpected error %v", tail, err)
		}
		if got != "fake logs" {
			t.Errorf("tailLines=%d: got %q, want the fake clientset's canned log line", tail, got)
		}
	}
}
