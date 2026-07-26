package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

// handleCreateResource creates a resource from raw YAML. Unlike the manifest
// endpoints, the kind isn't part of the URL — it's read from the manifest's own
// apiVersion/kind, the same way `kubectl apply -f` works, so this one endpoint
// covers any resource the cluster's RESTMapper knows about.
// POST /api/contexts/{ctx}/create, body {"yaml":"..."}
func (s *Server) handleCreateResource(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var payload struct {
		YAML string `json:"yaml"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	jsonBytes, err := yaml.YAMLToJSON([]byte(payload.YAML))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	obj := &unstructured.Unstructured{}
	if err := obj.UnmarshalJSON(jsonBytes); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if obj.GetAPIVersion() == "" || obj.GetKind() == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("apiVersion and kind are required"))
		return
	}
	if obj.GetName() == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("metadata.name is required"))
		return
	}

	ctxName := r.PathValue("ctx")
	res, err := s.mgr.ResolveGVK(ctxName, obj.GroupVersionKind())
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	dyn, err := s.mgr.DynamicFor(ctxName)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	ns := ""
	if res.Namespaced {
		ns = obj.GetNamespace()
		if ns == "" {
			ns = "default"
			obj.SetNamespace(ns)
		}
	} else {
		obj.SetNamespace("")
	}

	ctx, cancel := reqCtx(r)
	defer cancel()

	// ?dryRun=true validates + runs admission/defaulting server-side without
	// persisting, mirroring the same preview handleApplyManifest offers for edits.
	dryRun := r.URL.Query().Get("dryRun") == "true"
	opts := metav1.CreateOptions{}
	if dryRun {
		opts.DryRun = []string{metav1.DryRunAll}
	} else {
		audit(r, "create-resource", "kind", obj.GetKind(), "namespace", ns, "name", obj.GetName())
	}
	created, err := dyn.Resource(res.GVR).Namespace(ns).Create(ctx, obj, opts)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	if dryRun {
		cleanUnstructured(created)
		data, err := yaml.Marshal(created.Object)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"yaml": string(data)})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{
		"status":    "created",
		"kind":      obj.GetKind(),
		"namespace": ns,
		"name":      obj.GetName(),
	})
}
