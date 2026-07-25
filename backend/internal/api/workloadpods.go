package api

import (
	"net/http"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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

	kind := r.PathValue("kind")
	ns := r.PathValue("namespace")
	name := r.PathValue("name")

	pods, err := client.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}

	// For deployments, gather the ReplicaSets it owns; pods are matched via those.
	rsOwned := map[string]bool{}
	if kind == "deployment" {
		if rss, err := client.AppsV1().ReplicaSets(ns).List(ctx, metav1.ListOptions{}); err == nil {
			for i := range rss.Items {
				rs := &rss.Items[i]
				for _, o := range rs.OwnerReferences {
					if o.Controller != nil && *o.Controller && o.Kind == "Deployment" && o.Name == name {
						rsOwned[rs.Name] = true
						break
					}
				}
			}
		}
	}
	// A service selects its backing pods by label selector (not ownership).
	var svcSelector map[string]string
	if kind == "service" {
		if svc, err := client.CoreV1().Services(ns).Get(ctx, name, metav1.GetOptions{}); err == nil {
			svcSelector = svc.Spec.Selector
		}
	}
	targetKind := slugToK8sKind[kind]

	out := make([]kube.PodView, 0)
	for i := range pods.Items {
		p := &pods.Items[i]
		matched := false
		switch {
		case kind == "service":
			matched = len(svcSelector) > 0 && labelsMatch(svcSelector, p.Labels)
		case kind == "deployment":
			matched = ownedByRS(p, rsOwned)
		default:
			matched = ownedBy(p, targetKind, name)
		}
		if matched {
			out = append(out, kube.ToPodView(p))
		}
	}
	writeJSON(w, http.StatusOK, out)
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
