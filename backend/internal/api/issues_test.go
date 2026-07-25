package api

import (
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
