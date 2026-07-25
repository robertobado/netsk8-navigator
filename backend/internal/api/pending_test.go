package api

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
)

func TestOrDefault(t *testing.T) {
	if got := orDefault("", "fallback"); got != "fallback" {
		t.Errorf("orDefault(\"\", ...) = %q", got)
	}
	if got := orDefault("Reason", "fallback"); got != "Reason" {
		t.Errorf("orDefault(non-empty, ...) = %q, want the original value", got)
	}
}

func TestPendingReason(t *testing.T) {
	ctx := context.Background()

	t.Run("unschedulable condition wins first", func(t *testing.T) {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "web-1", Namespace: "prod"},
			Status: corev1.PodStatus{
				Conditions: []corev1.PodCondition{{
					Type: corev1.PodScheduled, Status: corev1.ConditionFalse,
					Reason: "Unschedulable", Message: "0/3 nodes available",
				}},
			},
		}
		client := kubernetesfake.NewSimpleClientset()
		reason, message := pendingReason(ctx, client, pod)
		if reason != "Unschedulable" || message != "0/3 nodes available" {
			t.Errorf("got reason=%q message=%q", reason, message)
		}
	})

	t.Run("waiting container reason is used next", func(t *testing.T) {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "web-2", Namespace: "prod"},
			Status: corev1.PodStatus{
				ContainerStatuses: []corev1.ContainerStatus{{
					State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{
						Reason: "ImagePullBackOff", Message: "manifest not found",
					}},
				}},
			},
		}
		client := kubernetesfake.NewSimpleClientset()
		reason, message := pendingReason(ctx, client, pod)
		if reason != "ImagePullBackOff" || message != "manifest not found" {
			t.Errorf("got reason=%q message=%q", reason, message)
		}
	})

	t.Run("falls back to Pending with no signal", func(t *testing.T) {
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "web-3", Namespace: "prod"}}
		client := kubernetesfake.NewSimpleClientset()
		reason, message := pendingReason(ctx, client, pod)
		if reason != "Pending" || message != "" {
			t.Errorf("got reason=%q message=%q", reason, message)
		}
	})
}

func TestLatestWarning(t *testing.T) {
	ctx := context.Background()
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "web-1", Namespace: "prod"}}

	t.Run("no events", func(t *testing.T) {
		client := kubernetesfake.NewSimpleClientset()
		reason, message := latestWarning(ctx, client, pod)
		if reason != "" || message != "" {
			t.Errorf("got reason=%q message=%q, want empty", reason, message)
		}
	})

	t.Run("picks the most recent Warning, ignores Normal", func(t *testing.T) {
		older := metav1.NewTime(metav1.Now().Add(-time.Hour))
		newer := metav1.Now()
		client := kubernetesfake.NewSimpleClientset(
			&corev1.Event{ObjectMeta: metav1.ObjectMeta{Name: "e1", Namespace: "prod"}, Type: corev1.EventTypeNormal, Reason: "Scheduled", LastTimestamp: newer},
			&corev1.Event{ObjectMeta: metav1.ObjectMeta{Name: "e2", Namespace: "prod"}, Type: corev1.EventTypeWarning, Reason: "FailedMount", Message: "old warning", LastTimestamp: older},
			&corev1.Event{ObjectMeta: metav1.ObjectMeta{Name: "e3", Namespace: "prod"}, Type: corev1.EventTypeWarning, Reason: "BackOff", Message: "latest warning", LastTimestamp: newer},
		)
		reason, message := latestWarning(ctx, client, pod)
		if reason != "BackOff" || message != "latest warning" {
			t.Errorf("got reason=%q message=%q, want the newer Warning event", reason, message)
		}
	})
}
