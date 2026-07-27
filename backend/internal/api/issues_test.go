package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestRfc3339(t *testing.T) {
	if got := rfc3339(time.Time{}); got != "" {
		t.Errorf("zero time = %q, want empty", got)
	}
	tm := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if got := rfc3339(tm); got != "2026-01-02T03:04:05Z" {
		t.Errorf("got %q", got)
	}
}

func TestSortBySinceDesc(t *testing.T) {
	items := []issueItem{
		{Name: "old", Since: "2026-01-01T00:00:00Z"},
		{Name: "newest", Since: "2026-03-01T00:00:00Z"},
		{Name: "middle", Since: "2026-02-01T00:00:00Z"},
	}
	sortBySinceDesc(items)
	if items[0].Name != "newest" || items[1].Name != "middle" || items[2].Name != "old" {
		t.Errorf("order = %v", []string{items[0].Name, items[1].Name, items[2].Name})
	}
}

func TestContainerNames(t *testing.T) {
	p := &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "web"}, {Name: "sidecar"}}}}
	got := containerNames(p)
	if len(got) != 2 || got[0] != "web" || got[1] != "sidecar" {
		t.Errorf("got %v", got)
	}
}

func TestPendingDetail(t *testing.T) {
	t.Run("waiting container reason wins", func(t *testing.T) {
		p := &corev1.Pod{Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{{State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff", Message: "not found"}}}},
		}}
		reason, msg := pendingDetail(p)
		if reason != "ImagePullBackOff" || msg != "not found" {
			t.Errorf("got %q/%q", reason, msg)
		}
	})
	t.Run("unschedulable condition next", func(t *testing.T) {
		p := &corev1.Pod{Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{{Type: corev1.PodScheduled, Status: corev1.ConditionFalse, Message: "insufficient cpu"}},
		}}
		reason, msg := pendingDetail(p)
		if reason != "Unschedulable" || msg != "insufficient cpu" {
			t.Errorf("got %q/%q", reason, msg)
		}
	})
	t.Run("falls back to Pending", func(t *testing.T) {
		reason, _ := pendingDetail(&corev1.Pod{})
		if reason != "Pending" {
			t.Errorf("got %q", reason)
		}
	})
}

func TestFailedDetail(t *testing.T) {
	t.Run("terminated container reason and time", func(t *testing.T) {
		finishedAt := metav1.NewTime(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC))
		p := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{CreationTimestamp: metav1.NewTime(time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC))},
			Status: corev1.PodStatus{
				ContainerStatuses: []corev1.ContainerStatus{{State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
					Reason: "OOMKilled", FinishedAt: finishedAt,
				}}}},
			},
		}
		reason, _, since := failedDetail(p)
		if reason != "OOMKilled" || !since.Equal(finishedAt.Time) {
			t.Errorf("got reason=%q since=%v", reason, since)
		}
	})
	t.Run("no signal falls back to Failed and creation time", func(t *testing.T) {
		created := metav1.NewTime(time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC))
		p := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{CreationTimestamp: created}}
		reason, _, since := failedDetail(p)
		if reason != "Failed" || !since.Equal(created.Time) {
			t.Errorf("got reason=%q since=%v", reason, since)
		}
	})
}

func TestRunningContainerIssue(t *testing.T) {
	t.Run("not-ready waiting container reported", func(t *testing.T) {
		p := &corev1.Pod{Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{{
				Ready: false,
				State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff", Message: "back-off"}},
			}},
		}}
		reason, msg, ok := runningContainerIssue(p)
		if !ok || reason != "CrashLoopBackOff" || msg != "back-off" {
			t.Errorf("got reason=%q msg=%q ok=%v", reason, msg, ok)
		}
	})
	t.Run("all containers ready is not an issue", func(t *testing.T) {
		p := &corev1.Pod{Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{{Ready: true}},
		}}
		if _, _, ok := runningContainerIssue(p); ok {
			t.Error("expected ok=false for a fully ready pod")
		}
	})
	t.Run("not-ready but not waiting (e.g. still starting) is not an issue", func(t *testing.T) {
		p := &corev1.Pod{Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{{Ready: false, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}},
		}}
		if _, _, ok := runningContainerIssue(p); ok {
			t.Error("expected ok=false when not waiting")
		}
	})
}

func TestHandleIssues(t *testing.T) {
	s := newTestServer(t,
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "pending-1", Namespace: "prod"},
			Status:     corev1.PodStatus{Phase: corev1.PodPending},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "failed-1", Namespace: "prod"},
			Status:     corev1.PodStatus{Phase: corev1.PodFailed, Reason: "Evicted"},
		},
		&corev1.Pod{
			// Running pods with a crash-looping container never leave
			// Phase Running — this is the most common real-world failure,
			// and it must still surface as an issue.
			ObjectMeta: metav1.ObjectMeta{Name: "crashlooping-1", Namespace: "prod"},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{{
					Ready: false,
					State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{
						Reason: "CrashLoopBackOff", Message: "back-off restarting failed container",
					}},
				}},
			},
		},
		&corev1.Pod{
			// A genuinely healthy Running pod (all containers Ready) is not an issue.
			ObjectMeta: metav1.ObjectMeta{Name: "healthy-1", Namespace: "prod"},
			Status: corev1.PodStatus{
				Phase:             corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{{Ready: true}},
			},
		},
		&corev1.Pod{
			// Terminating pods are neither pending nor failed — should be skipped.
			ObjectMeta: metav1.ObjectMeta{Name: "terminating-1", Namespace: "prod", DeletionTimestamp: &metav1.Time{Time: time.Now()}},
			Status:     corev1.PodStatus{Phase: corev1.PodPending},
		},
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
			Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionFalse, Reason: "KubeletNotReady"},
			}},
		},
	)
	rec := doRequest(t, s, "GET", "/api/contexts/test/issues", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		Pending       []issueItem `json:"pending"`
		Failed        []issueItem `json:"failed"`
		NodesNotReady []issueItem `json:"nodesNotReady"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Pending) != 1 || out.Pending[0].Name != "pending-1" {
		t.Errorf("pending = %+v", out.Pending)
	}
	if len(out.Failed) != 2 {
		t.Fatalf("failed = %+v", out.Failed)
	}
	byName := map[string]issueItem{}
	for _, i := range out.Failed {
		byName[i.Name] = i
	}
	if byName["failed-1"].Reason != "Evicted" {
		t.Errorf("failed-1 reason = %+v", byName["failed-1"])
	}
	if byName["crashlooping-1"].Reason != "CrashLoopBackOff" {
		t.Errorf("crashlooping-1 = %+v", byName["crashlooping-1"])
	}
	if len(out.NodesNotReady) != 1 || out.NodesNotReady[0].Name != "node-1" || out.NodesNotReady[0].Reason != "KubeletNotReady" {
		t.Errorf("nodesNotReady = %+v", out.NodesNotReady)
	}
}
