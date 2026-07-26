package api

import (
	"context"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/robertobado/netsk8-navigator/backend/internal/kube"
)

// slugToK8sKind maps our manifest slug to the controller Kind found in a pod's
// ownerReferences (Deployment is handled specially via its ReplicaSets).
var slugToK8sKind = map[string]string{
	"replicaset":  "ReplicaSet",
	"statefulset": "StatefulSet",
	"daemonset":   "DaemonSet",
	"job":         "Job",
}

// handleWorkloadPods: GET /api/contexts/{ctx}/pods-of/{kind}/{namespace}/{name}
// Lists the pods owned by a workload (Deployment resolves through its ReplicaSets).
func (s *Server) handleWorkloadPods(w http.ResponseWriter, r *http.Request) {
	client, err := s.mgr.ClientFor(r.PathValue("ctx"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := reqCtx(r)
	defer cancel()

	pods, err := resolveWorkloadPods(ctx, client, r.PathValue("kind"), r.PathValue("namespace"), r.PathValue("name"))
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	out := make([]kube.PodView, 0, len(pods))
	for i := range pods {
		out = append(out, kube.ToPodView(&pods[i]))
	}
	writeJSON(w, http.StatusOK, out)
}

// resolveWorkloadPods lists every pod belonging to a workload — shared by
// handleWorkloadPods and the aggregated multi-pod log streamer.
func resolveWorkloadPods(ctx context.Context, client kubernetes.Interface, kind, ns, name string) ([]corev1.Pod, error) {
	pods, err := client.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	// For deployments, gather the ReplicaSets it owns; pods are matched via those.
	rsOwned := map[string]bool{}
	if kind == "deployment" {
		rsOwned = deploymentReplicaSetNames(ctx, client, ns, name)
	}
	// A service selects its backing pods by label selector (not ownership).
	var svcSelector map[string]string
	if kind == "service" {
		svcSelector = serviceSelector(ctx, client, ns, name)
	}
	targetKind := slugToK8sKind[kind]

	out := make([]corev1.Pod, 0, len(pods.Items))
	for i := range pods.Items {
		p := &pods.Items[i]
		if workloadPodMatches(p, kind, name, targetKind, rsOwned, svcSelector) {
			out = append(out, *p)
		}
	}
	return out, nil
}

// workloadPodMatches applies the right matching rule for the workload kind:
// label selector for Services, ReplicaSet ownership for Deployments (which
// don't own pods directly), else a direct ownerReference check.
func workloadPodMatches(p *corev1.Pod, kind, name, targetKind string, rsOwned map[string]bool, svcSelector map[string]string) bool {
	switch kind {
	case "service":
		return len(svcSelector) > 0 && labelsMatch(svcSelector, p.Labels)
	case "deployment":
		return ownedByRS(p, rsOwned)
	default:
		return ownedBy(p, targetKind, name)
	}
}

// deploymentReplicaSetNames returns the names of every ReplicaSet a Deployment
// owns — pods are matched through those, since Deployments don't own pods directly.
func deploymentReplicaSetNames(ctx context.Context, client kubernetes.Interface, ns, name string) map[string]bool {
	rsOwned := map[string]bool{}
	rss, err := client.AppsV1().ReplicaSets(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return rsOwned
	}
	for i := range rss.Items {
		rs := &rss.Items[i]
		for _, o := range rs.OwnerReferences {
			if o.Controller != nil && *o.Controller && o.Kind == "Deployment" && o.Name == name {
				rsOwned[rs.Name] = true
				break
			}
		}
	}
	return rsOwned
}

// serviceSelector returns a Service's pod label selector.
func serviceSelector(ctx context.Context, client kubernetes.Interface, ns, name string) map[string]string {
	svc, err := client.CoreV1().Services(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil
	}
	return svc.Spec.Selector
}

func ownedByRS(p *corev1.Pod, rsOwned map[string]bool) bool {
	for _, o := range p.OwnerReferences {
		if o.Controller != nil && *o.Controller && o.Kind == "ReplicaSet" && rsOwned[o.Name] {
			return true
		}
	}
	return false
}

func ownedBy(p *corev1.Pod, kind, name string) bool {
	if kind == "" {
		return false
	}
	for _, o := range p.OwnerReferences {
		if o.Controller != nil && *o.Controller && o.Kind == kind && o.Name == name {
			return true
		}
	}
	return false
}
