package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// scalableKinds are the manifest slugs whose spec has a plain `replicas`
// field settable this way (no scale subresource needed).
var scalableKinds = map[string]bool{"deployment": true, "statefulset": true, "replicaset": true}

// restartableKinds are the manifest slugs whose pod template restarts on a
// `kubectl rollout restart`-style annotation bump.
var restartableKinds = map[string]bool{"deployment": true, "statefulset": true, "daemonset": true}

// deleteQueryOptions parses the popular kubectl-delete switches from the
// request's query string:
//   - cascade=background|foreground|orphan (default background, matching
//     kubectl's own default — orphan leaves dependents like a Deployment's
//     ReplicaSets/Pods behind instead of deleting them too)
//   - gracePeriodSeconds=<N> (defaults to the resource's own
//     terminationGracePeriodSeconds when omitted)
//   - force=true (immediate deletion, equivalent to kubectl's
//     --force --grace-period=0; overrides gracePeriodSeconds)
//   - dryRun=true (server-side validation only, nothing is actually deleted)
//
// ignoreNotFound is returned separately since it changes error handling
// after the call, not the DeleteOptions passed into it.
func deleteQueryOptions(r *http.Request) (opts metav1.DeleteOptions, ignoreNotFound bool, err error) {
	q := r.URL.Query()

	switch cascade := q.Get("cascade"); cascade {
	case "", "background":
		policy := metav1.DeletePropagationBackground
		opts.PropagationPolicy = &policy
	case "foreground":
		policy := metav1.DeletePropagationForeground
		opts.PropagationPolicy = &policy
	case "orphan":
		policy := metav1.DeletePropagationOrphan
		opts.PropagationPolicy = &policy
	default:
		return opts, false, fmt.Errorf("cascade must be background, foreground, or orphan, got %q", cascade)
	}

	if gp := q.Get("gracePeriodSeconds"); gp != "" {
		seconds, perr := strconv.ParseInt(gp, 10, 64)
		if perr != nil {
			return opts, false, fmt.Errorf("gracePeriodSeconds must be an integer: %w", perr)
		}
		opts.GracePeriodSeconds = &seconds
	}
	if q.Get("force") == "true" {
		immediate := int64(0)
		opts.GracePeriodSeconds = &immediate
	}
	if q.Get("dryRun") == "true" {
		opts.DryRun = []string{metav1.DryRunAll}
	}
	return opts, q.Get("ignoreNotFound") == "true", nil
}

// handleDeleteResource deletes any resource by manifest slug — the same
// generic addressing GET/PUT already use, so it works for every kind in
// manifestSlugToResource with no per-kind code.
// DELETE /api/contexts/{ctx}/manifest/{kind}/{namespace}/{name}
// Query: cascade, gracePeriodSeconds, force, dryRun, ignoreNotFound — see
// deleteQueryOptions.
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
	opts, ignoreNotFound, err := deleteQueryOptions(r)
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
	if err := dyn.Resource(res.GVR).Namespace(ns).Delete(ctx, r.PathValue("name"), opts); err != nil {
		if ignoreNotFound && apierrors.IsNotFound(err) {
			writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "note": "already gone (ignoreNotFound)"})
			return
		}
		writeError(w, http.StatusBadGateway, err)
		return
	}
	status := "deleted"
	if len(opts.DryRun) > 0 {
		status = "would-delete"
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": status})
}

// handleScaleResource sets spec.replicas on a Deployment/StatefulSet/ReplicaSet.
// PUT /api/contexts/{ctx}/scale/{kind}/{namespace}/{name}, body {"replicas": N}
func (s *Server) handleScaleResource(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	if !scalableKinds[kind] {
		writeError(w, http.StatusBadRequest, fmt.Errorf("kind %q cannot be scaled (scalable kinds: %s)", kind, strings.Join(sortedKeys(scalableKinds), ", ")))
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

	// ?dryRun=true validates server-side without persisting — same knob
	// handleApplyManifest offers, useful for previewing a scale before it runs.
	dryRun := r.URL.Query().Get("dryRun") == "true"
	opts := metav1.UpdateOptions{}
	if dryRun {
		opts.DryRun = []string{metav1.DryRunAll}
	} else {
		audit(r, "scale", "kind", kind, "namespace", ns, "name", r.PathValue("name"), "replicas", fmt.Sprintf("%d", *payload.Replicas))
	}
	updated, err := dyn.Resource(res.GVR).Namespace(ns).Update(ctx, obj, opts)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	if dryRun {
		writeDryRunYAML(w, updated)
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
		writeError(w, http.StatusBadRequest, fmt.Errorf("kind %q cannot be restarted (restartable kinds: %s)", kind, strings.Join(sortedKeys(restartableKinds), ", ")))
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

	dryRun := r.URL.Query().Get("dryRun") == "true"
	opts := metav1.UpdateOptions{}
	if dryRun {
		opts.DryRun = []string{metav1.DryRunAll}
	} else {
		audit(r, "rollout-restart", "kind", kind, "namespace", ns, "name", r.PathValue("name"))
	}
	updated, err := dyn.Resource(res.GVR).Namespace(ns).Update(ctx, obj, opts)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	if dryRun {
		writeDryRunYAML(w, updated)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "restarted"})
}
