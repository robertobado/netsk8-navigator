package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Only Deployments are supported here. StatefulSet/DaemonSet track history via
// ControllerRevision (a stored strategic-merge patch, not a plain object copy
// like a ReplicaSet's pod template) — a genuinely different mechanism, left
// as a follow-up (see docs/FEATURE_GAP_ANALYSIS.md).
const revisionAnnotation = "deployment.kubernetes.io/revision"

func revisionOf(annotations map[string]string) int {
	n, _ := strconv.Atoi(annotations[revisionAnnotation])
	return n
}

// deploymentReplicaSets returns every ReplicaSet owned by dep, newest revision first.
func (s *Server) deploymentReplicaSets(r *http.Request, dep *appsv1.Deployment) ([]appsv1.ReplicaSet, error) {
	client, err := s.mgr.ClientFor(r.PathValue("ctx"))
	if err != nil {
		return nil, err
	}
	selector, err := metav1.LabelSelectorAsSelector(dep.Spec.Selector)
	if err != nil {
		return nil, err
	}
	ctx, cancel := reqCtx(r)
	defer cancel()
	list, err := client.AppsV1().ReplicaSets(dep.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return nil, err
	}
	owned := make([]appsv1.ReplicaSet, 0, len(list.Items))
	for _, rs := range list.Items {
		for _, ref := range rs.OwnerReferences {
			if ref.UID == dep.UID {
				owned = append(owned, rs)
				break
			}
		}
	}
	sort.Slice(owned, func(i, j int) bool { return revisionOf(owned[i].Annotations) > revisionOf(owned[j].Annotations) })
	return owned, nil
}

func imagesOfReplicaSet(rs *appsv1.ReplicaSet) []string {
	out := make([]string, 0, len(rs.Spec.Template.Spec.Containers))
	for _, c := range rs.Spec.Template.Spec.Containers {
		out = append(out, c.Image)
	}
	return out
}

type revisionInfo struct {
	Revision  int      `json:"revision"`
	Images    []string `json:"images"`
	CreatedAt string   `json:"createdAt"`
	Current   bool     `json:"current"`
}

// handleRolloutHistory lists a Deployment's revision history via its owned
// ReplicaSets. GET /api/contexts/{ctx}/rollout-history/{kind}/{namespace}/{name}
func (s *Server) handleRolloutHistory(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("kind") != "deployment" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("rollout history is only supported for deployments"))
		return
	}
	client, err := s.mgr.ClientFor(r.PathValue("ctx"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := reqCtx(r)
	defer cancel()
	dep, err := client.AppsV1().Deployments(r.PathValue("namespace")).Get(ctx, r.PathValue("name"), metav1.GetOptions{})
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	rsList, err := s.deploymentReplicaSets(r, dep)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}

	current := revisionOf(dep.Annotations)
	out := make([]revisionInfo, 0, len(rsList))
	for i := range rsList {
		rs := &rsList[i]
		rev := revisionOf(rs.Annotations)
		out = append(out, revisionInfo{
			Revision:  rev,
			Images:    imagesOfReplicaSet(rs),
			CreatedAt: rs.CreationTimestamp.Format(time.RFC3339),
			Current:   rev == current,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleRolloutUndo reverts a Deployment's pod template to a previous
// revision's — the same mechanism `kubectl rollout undo` uses.
// POST /api/contexts/{ctx}/rollout-undo/{kind}/{namespace}/{name}, body {"toRevision": N}
func (s *Server) handleRolloutUndo(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("kind") != "deployment" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("rollout undo is only supported for deployments"))
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<10))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var payload struct {
		ToRevision int `json:"toRevision"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	client, err := s.mgr.ClientFor(r.PathValue("ctx"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := reqCtx(r)
	defer cancel()
	dep, err := client.AppsV1().Deployments(r.PathValue("namespace")).Get(ctx, r.PathValue("name"), metav1.GetOptions{})
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	rsList, err := s.deploymentReplicaSets(r, dep)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}

	var target *appsv1.ReplicaSet
	for i := range rsList {
		if revisionOf(rsList[i].Annotations) == payload.ToRevision {
			target = &rsList[i]
			break
		}
	}
	if target == nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("revision %d not found", payload.ToRevision))
		return
	}

	dep.Spec.Template = target.Spec.Template
	audit(r, "rollout-undo", "namespace", dep.Namespace, "name", dep.Name, "toRevision", fmt.Sprintf("%d", payload.ToRevision))
	if _, err := client.AppsV1().Deployments(dep.Namespace).Update(ctx, dep, metav1.UpdateOptions{}); err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "reverted"})
}
