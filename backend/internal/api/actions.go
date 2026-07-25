package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// scalableKinds are the manifest slugs whose spec has a plain `replicas`
// field settable this way (no scale subresource needed).
var scalableKinds = map[string]bool{"deployment": true, "statefulset": true, "replicaset": true}

// restartableKinds are the manifest slugs whose pod template restarts on a
// `kubectl rollout restart`-style annotation bump.
var restartableKinds = map[string]bool{"deployment": true, "statefulset": true, "daemonset": true}

// handleDeleteResource deletes any resource by manifest slug — the same
// generic addressing GET/PUT already use, so it works for every kind in
// manifestSlugToResource with no per-kind code.
// DELETE /api/contexts/{ctx}/manifest/{kind}/{namespace}/{name}
func (s *Server) handleDeleteResource(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	res, err := s.resolveSlug(r.PathValue("ctx"), kind)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	dyn, err := s.mgr.DynamicFor(r.PathValue("ctx"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	ns := ""
	if res.Namespaced {
		ns = r.PathValue("namespace")
	}

	ctx, cancel := reqCtx(r)
	defer cancel()
	audit(r, "delete-resource", "kind", kind, "namespace", ns, "name", r.PathValue("name"))
	if err := dyn.Resource(res.GVR).Namespace(ns).Delete(ctx, r.PathValue("name"), metav1.DeleteOptions{}); err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handleScaleResource sets spec.replicas on a Deployment/StatefulSet/ReplicaSet.
// PUT /api/contexts/{ctx}/scale/{kind}/{namespace}/{name}, body {"replicas": N}
func (s *Server) handleScaleResource(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	if !scalableKinds[kind] {
		writeError(w, http.StatusBadRequest, fmt.Errorf("kind %q cannot be scaled", kind))
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var payload struct {
		Replicas *int32 `json:"replicas"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if payload.Replicas == nil || *payload.Replicas < 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("replicas must be a non-negative integer"))
		return
	}

	ctx, cancel := reqCtx(r)
	defer cancel()
	obj, err := s.getUnstructured(ctx, r.PathValue("ctx"), kind, r.PathValue("namespace"), r.PathValue("name"))
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	if err := unstructured.SetNestedField(obj.Object, int64(*payload.Replicas), "spec", "replicas"); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	dyn, err := s.mgr.DynamicFor(r.PathValue("ctx"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	res, err := s.resolveSlug(r.PathValue("ctx"), kind)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	ns := ""
	if res.Namespaced {
		ns = r.PathValue("namespace")
	}
	audit(r, "scale", "kind", kind, "namespace", ns, "name", r.PathValue("name"), "replicas", fmt.Sprintf("%d", *payload.Replicas))
	if _, err := dyn.Resource(res.GVR).Namespace(ns).Update(ctx, obj, metav1.UpdateOptions{}); err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "scaled"})
}

// handleRestartRollout bumps the pod template's restartedAt annotation —
// the same mechanism `kubectl rollout restart` uses to trigger a rolling
// update without changing the workload's actual spec.
// POST /api/contexts/{ctx}/rollout-restart/{kind}/{namespace}/{name}
func (s *Server) handleRestartRollout(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	if !restartableKinds[kind] {
		writeError(w, http.StatusBadRequest, fmt.Errorf("kind %q cannot be restarted", kind))
		return
	}

	ctx, cancel := reqCtx(r)
	defer cancel()
	obj, err := s.getUnstructured(ctx, r.PathValue("ctx"), kind, r.PathValue("namespace"), r.PathValue("name"))
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	annotations, _, _ := unstructured.NestedStringMap(obj.Object, "spec", "template", "metadata", "annotations")
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations["kubectl.kubernetes.io/restartedAt"] = time.Now().UTC().Format(time.RFC3339)
	if err := unstructured.SetNestedStringMap(obj.Object, annotations, "spec", "template", "metadata", "annotations"); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	dyn, err := s.mgr.DynamicFor(r.PathValue("ctx"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	res, err := s.resolveSlug(r.PathValue("ctx"), kind)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	ns := ""
	if res.Namespaced {
		ns = r.PathValue("namespace")
	}
	audit(r, "rollout-restart", "kind", kind, "namespace", ns, "name", r.PathValue("name"))
	if _, err := dyn.Resource(res.GVR).Namespace(ns).Update(ctx, obj, metav1.UpdateOptions{}); err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "restarted"})
}
