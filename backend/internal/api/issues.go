package api

import (
	"context"
	"net/http"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// issueItem is one unhealthy resource surfaced in an overview carousel.
type issueItem struct {
	Kind       string   `json:"kind"` // pod | node
	Namespace  string   `json:"namespace,omitempty"`
	Name       string   `json:"name"`
	Since      string   `json:"since"` // when it entered the state (RFC3339)
	Reason     string   `json:"reason"`
	Message    string   `json:"message"`
	Containers []string `json:"containers,omitempty"` // pods only, so the drawer can stream logs/exec
}

// handleIssues: GET /api/contexts/{ctx}/issues
// Pending & failed pods plus not-ready nodes, each with reason/detail/since, in a
// single pass over one pod List + one node List (no per-item event lookups).
func (s *Server) handleIssues(w http.ResponseWriter, r *http.Request) {
	client, err := s.mgr.ClientFor(r.PathValue("ctx"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := reqCtx(r)
	defer cancel()

	pending, failed := collectPodIssues(ctx, client)
	nodesNotReady := collectNodeIssues(ctx, client)

	// Most recent first (RFC3339 sorts chronologically as strings).
	sortBySinceDesc(pending)
	sortBySinceDesc(failed)
	sortBySinceDesc(nodesNotReady)

	writeJSON(w, http.StatusOK, map[string]any{
		"pending":       pending,
		"failed":        failed,
		"nodesNotReady": nodesNotReady,
	})
}

// collectPodIssues scans every pod once, splitting Pending/Failed ones
// (skipping terminating pods) into their respective issue lists.
func collectPodIssues(ctx context.Context, client kubernetes.Interface) (pending, failed []issueItem) {
	pending, failed = []issueItem{}, []issueItem{}
	pods, err := client.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return pending, failed
	}
	for i := range pods.Items {
		p := &pods.Items[i]
		if p.DeletionTimestamp != nil {
			continue // terminating, not pending/failed
		}
		switch p.Status.Phase {
		case corev1.PodPending:
			reason, msg := pendingDetail(p)
			pending = append(pending, issueItem{
				Kind: "pod", Namespace: p.Namespace, Name: p.Name,
				Since: rfc3339(p.CreationTimestamp.Time), Reason: reason, Message: msg,
				Containers: containerNames(p),
			})
		case corev1.PodFailed:
			reason, msg, since := failedDetail(p)
			failed = append(failed, issueItem{
				Kind: "pod", Namespace: p.Namespace, Name: p.Name,
				Since: rfc3339(since), Reason: reason, Message: msg,
				Containers: containerNames(p),
			})
		}
	}
	return pending, failed
}

// collectNodeIssues returns one issueItem per node whose Ready condition isn't True.
func collectNodeIssues(ctx context.Context, client kubernetes.Interface) []issueItem {
	nodesNotReady := []issueItem{}
	nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nodesNotReady
	}
	for i := range nodes.Items {
		n := &nodes.Items[i]
		c, ok := notReadyCondition(n)
		if !ok {
			continue
		}
		reason := c.Reason
		if reason == "" {
			reason = "NotReady"
		}
		nodesNotReady = append(nodesNotReady, issueItem{
			Kind: "node", Name: n.Name,
			Since: rfc3339(c.LastTransitionTime.Time), Reason: reason, Message: c.Message,
		})
	}
	return nodesNotReady
}

// notReadyCondition returns the node's NodeReady condition, if it isn't True.
func notReadyCondition(n *corev1.Node) (corev1.NodeCondition, bool) {
	for _, c := range n.Status.Conditions {
		if c.Type == corev1.NodeReady && c.Status != corev1.ConditionTrue {
			return c, true
		}
	}
	return corev1.NodeCondition{}, false
}

func rfc3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func sortBySinceDesc(items []issueItem) {
	sort.Slice(items, func(i, j int) bool { return items[i].Since > items[j].Since })
}

func containerNames(p *corev1.Pod) []string {
	names := make([]string, 0, len(p.Spec.Containers))
	for _, c := range p.Spec.Containers {
		names = append(names, c.Name)
	}
	return names
}

// pendingDetail derives why a pod is Pending from its status alone (no events):
// a container waiting reason, else Unschedulable, else the pod-level reason.
func pendingDetail(p *corev1.Pod) (reason, message string) {
	for _, cs := range p.Status.InitContainerStatuses {
		if w := cs.State.Waiting; w != nil && w.Reason != "" {
			return w.Reason, w.Message
		}
	}
	for _, cs := range p.Status.ContainerStatuses {
		if w := cs.State.Waiting; w != nil && w.Reason != "" {
			return w.Reason, w.Message
		}
	}
	for _, c := range p.Status.Conditions {
		if c.Type == corev1.PodScheduled && c.Status != corev1.ConditionTrue {
			return "Unschedulable", c.Message
		}
	}
	if p.Status.Reason != "" {
		return p.Status.Reason, p.Status.Message
	}
	return "Pending", p.Status.Message
}

// failedDetail derives the failure reason/detail and the time it failed.
func failedDetail(p *corev1.Pod) (reason, message string, since time.Time) {
	reason, message = p.Status.Reason, p.Status.Message
	if reason == "" {
		reason, message = firstTerminationReason(p, message)
	}
	if reason == "" {
		reason = "Failed"
	}
	since = latestTerminationTime(p)
	if since.IsZero() {
		since = p.CreationTimestamp.Time
	}
	return reason, message, since
}

// firstTerminationReason returns the reason of the first container found in a
// terminated state with a non-empty reason, falling back to message when it
// was still empty (a pod-level message from the caller wins otherwise).
func firstTerminationReason(p *corev1.Pod, message string) (reason, msg string) {
	for _, cs := range p.Status.ContainerStatuses {
		t := cs.State.Terminated
		if t == nil || t.Reason == "" {
			continue
		}
		if message == "" {
			message = t.Message
		}
		return t.Reason, message
	}
	return "", message
}

// latestTerminationTime returns the FinishedAt of whichever container
// terminated most recently (the zero time if none did).
func latestTerminationTime(p *corev1.Pod) time.Time {
	var since time.Time
	for _, cs := range p.Status.ContainerStatuses {
		if t := cs.State.Terminated; t != nil && t.FinishedAt.After(since) {
			since = t.FinishedAt.Time
		}
	}
	return since
}
