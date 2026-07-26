package api

import (
	"context"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// handlePodPending returns, for a Pending pod, since when it has been pending
// and the best-available reason (scheduling condition, container waiting state,
// or the latest Warning event). GET /api/contexts/{ctx}/pods/{ns}/{name}/pending
func (s *Server) handlePodPending(w http.ResponseWriter, r *http.Request) {
	client, err := s.mgr.ClientFor(r.PathValue("ctx"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := reqCtx(r)
	defer cancel()

	ns, name := r.PathValue("namespace"), r.PathValue("name")
	pod, err := client.CoreV1().Pods(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}

	reason, message := pendingReason(ctx, client, pod)
	writeJSON(w, http.StatusOK, map[string]string{
		"since":   age(pod.CreationTimestamp), // Pending since creation
		"reason":  reason,
		"message": message,
	})
}

func pendingReason(ctx context.Context, client kubernetes.Interface, pod *corev1.Pod) (reason, message string) {
	// 1) Not scheduled yet (the most common Pending cause).
	if reason, message, ok := unschedulableReason(ctx, client, pod); ok {
		return reason, message
	}
	// 2) A container is stuck waiting (image pull, config, etc.).
	if reason, message, ok := waitingContainerReason(pod); ok {
		return reason, message
	}
	// 3) Fall back to the latest Warning event.
	if reason, message := latestWarning(ctx, client, pod); reason != "" {
		return reason, message
	}
	return "Pending", ""
}

// unschedulableReason reports the pod's PodScheduled condition when it hasn't
// been scheduled yet, falling back to the latest Warning event for a message
// when the condition itself carries none.
func unschedulableReason(ctx context.Context, client kubernetes.Interface, pod *corev1.Pod) (reason, message string, ok bool) {
	for _, c := range pod.Status.Conditions {
		if c.Type != corev1.PodScheduled || c.Status == corev1.ConditionTrue {
			continue
		}
		reason = orDefault(c.Reason, "Unschedulable")
		message = c.Message
		if message == "" {
			_, message = latestWarning(ctx, client, pod)
		}
		return reason, message, true
	}
	return "", "", false
}

// waitingContainerReason reports the first init/regular container found stuck
// in a Waiting state with a reason (image pull, config, etc.).
func waitingContainerReason(pod *corev1.Pod) (reason, message string, ok bool) {
	waiting := append(append([]corev1.ContainerStatus{}, pod.Status.InitContainerStatuses...), pod.Status.ContainerStatuses...)
	for _, cs := range waiting {
		if cs.State.Waiting != nil && cs.State.Waiting.Reason != "" {
			return cs.State.Waiting.Reason, cs.State.Waiting.Message, true
		}
	}
	return "", "", false
}

func latestWarning(ctx context.Context, client kubernetes.Interface, pod *corev1.Pod) (reason, message string) {
	events, err := client.CoreV1().Events(pod.Namespace).List(ctx, metav1.ListOptions{
		FieldSelector: "involvedObject.name=" + pod.Name,
	})
	if err != nil {
		return "", ""
	}
	var latest *corev1.Event
	for i := range events.Items {
		e := &events.Items[i]
		if e.Type != corev1.EventTypeWarning {
			continue
		}
		if latest == nil || e.LastTimestamp.After(latest.LastTimestamp.Time) {
			latest = e
		}
	}
	if latest == nil {
		return "", ""
	}
	return latest.Reason, latest.Message
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
