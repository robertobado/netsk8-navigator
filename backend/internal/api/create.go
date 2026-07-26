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
	obj, err := decodeCreateYAML(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
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
	ns := resolveCreateNamespace(obj, res.Namespaced)

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
		writeDryRunYAML(w, created)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{
		"status":    "created",
		"kind":      obj.GetKind(),
		"namespace": ns,
		"name":      obj.GetName(),
	})
}

// decodeCreateYAML parses the request body's YAML into an unstructured object
// and validates the minimum fields needed to create it.
func decodeCreateYAML(r *http.Request) (*unstructured.Unstructured, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	var payload struct {
		YAML string `json:"yaml"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	jsonBytes, err := yaml.YAMLToJSON([]byte(payload.YAML))
	if err != nil {
		return nil, err
	}
	obj := &unstructured.Unstructured{}
	if err := obj.UnmarshalJSON(jsonBytes); err != nil {
		return nil, err
	}
	if obj.GetAPIVersion() == "" || obj.GetKind() == "" {
		return nil, fmt.Errorf("apiVersion and kind are required")
	}
	if obj.GetName() == "" {
		return nil, fmt.Errorf("metadata.name is required")
	}
	return obj, nil
}

// resolveCreateNamespace picks the namespace to create into: the object's own
// (defaulting to "default" when unset, matching kubectl's behavior), or none
// at all for a cluster-scoped kind.
func resolveCreateNamespace(obj *unstructured.Unstructured, namespaced bool) string {
	if !namespaced {
		obj.SetNamespace("")
		return ""
	}
	ns := obj.GetNamespace()
	if ns == "" {
		ns = "default"
		obj.SetNamespace(ns)
	}
	return ns
}
