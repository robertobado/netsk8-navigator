package api

import (
	"encoding/json"
	"io"
	"net/http"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// handleCordonNode sets/clears spec.unschedulable on a node — the same field
// `kubectl cordon`/`kubectl uncordon` toggle, blocking (or allowing again) new
// pods from being scheduled there without evicting anything already running.
// Goes through the dynamic client, same as the other mutating actions in
// actions.go, so it exercises the exact same read-modify-write path the rest
// of the UI's edits already use.
// POST /api/contexts/{ctx}/cordon/{name}, body {"cordon": true|false}
func (s *Server) handleCordonNode(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<10))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var payload struct {
		Cordon bool `json:"cordon"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	ctxName := r.PathValue("ctx")
	name := r.PathValue("name")
	ctx, cancel := reqCtx(r)
	defer cancel()

	obj, err := s.getUnstructured(ctx, ctxName, "node", "", name)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	if err := unstructured.SetNestedField(obj.Object, payload.Cordon, "spec", "unschedulable"); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	dyn, err := s.mgr.DynamicFor(ctxName)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	res, err := s.resolveSlug(ctxName, "node")
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}

	action := "uncordon-node"
	if payload.Cordon {
		action = "cordon-node"
	}
	audit(r, action, "name", name)
	if _, err := dyn.Resource(res.GVR).Namespace("").Update(ctx, obj, metav1.UpdateOptions{}); err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	status := "uncordoned"
	if payload.Cordon {
		status = "cordoned"
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": status})
}
