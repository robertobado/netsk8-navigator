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

func pendingReason(ctx context.Context, client *kubernetes.Clientset, pod *corev1.Pod) (reason, message string) {
	// 1) Not scheduled yet (the most common Pending cause).
	for _, c := range pod.Status.Conditions {
		if c.Type == corev1.PodScheduled && c.Status != corev1.ConditionTrue {
			reason = orDefault(c.Reason, "Unschedulable")
			message = c.Message
			if message == "" {
				if _, m := latestWarning(ctx, client, pod); m != "" {
					message = m
				}
			}
			return reason, message
		}
	}
	// 2) A container is stuck waiting (image pull, config, etc.).
	waiting := append(append([]corev1.ContainerStatus{}, pod.Status.InitContainerStatuses...), pod.Status.ContainerStatuses...)
	for _, cs := range waiting {
		if cs.State.Waiting != nil && cs.State.Waiting.Reason != "" {
			return cs.State.Waiting.Reason, cs.State.Waiting.Message
		}
	}
	// 3) Fall back to the latest Warning event.
	if rea, msg := latestWarning(ctx, client, pod); rea != "" {
		return rea, msg
	}
	return "Pending", ""
}

func latestWarning(ctx context.Context, client *kubernetes.Clientset, pod *corev1.Pod) (reason, message string) {
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
