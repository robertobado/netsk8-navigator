package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"github.com/robertobado/netsk8-navigator/backend/internal/kube"
)

// manifestSlugToResource maps a UI manifest slug to its plural resource name.
// The served group/version is resolved per-cluster by the RESTMapper, so this
// stays correct across Kubernetes versions.
var manifestSlugToResource = map[string]string{
	"pod":                 "pods",
	"deployment":          "deployments",
	"service":             "services",
	"ingress":             "ingresses",
	"configmap":           "configmaps",
	"replicaset":          "replicasets",
	"statefulset":         "statefulsets",
	"daemonset":           "daemonsets",
	"job":                 "jobs",
	"cronjob":             "cronjobs",
	"node":                "nodes",
	"namespace":           "namespaces",
	"secret":              "secrets",
	"pvc":                 "persistentvolumeclaims",
	"pv":                  "persistentvolumes",
	"storageclass":        "storageclasses",
	"hpa":                 "horizontalpodautoscalers",
	"endpointslice":       "endpointslices",
	"networkpolicy":       "networkpolicies",
	"ingressclass":        "ingressclasses",
	"serviceaccount":      "serviceaccounts",
	"role":                "roles",
	"clusterrole":         "clusterroles",
	"rolebinding":         "rolebindings",
	"clusterrolebinding":  "clusterrolebindings",
	"resourcequota":       "resourcequotas",
	"limitrange":          "limitranges",
	"poddisruptionbudget": "poddisruptionbudgets",
	"priorityclass":       "priorityclasses",
	"runtimeclass":        "runtimeclasses",
}

// handleGetManifest returns a resource's manifest as YAML, cleaned of the noisy
// server-managed fields. GET /api/contexts/{ctx}/manifest/{kind}/{namespace}/{name}
func (s *Server) handleGetManifest(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := reqCtx(r)
	defer cancel()

	obj, err := s.getUnstructured(ctx, r.PathValue("ctx"), r.PathValue("kind"), r.PathValue("namespace"), r.PathValue("name"))
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	cleanUnstructured(obj)
	data, err := yaml.Marshal(obj.Object)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"yaml": string(data)})
}

// handleApplyManifest updates a resource from edited YAML. This mutates the live
// cluster, so the frontend gates it behind an explicit confirm.
// PUT body: {"yaml":"..."}
func (s *Server) handleApplyManifest(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	if kind == "pod" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("pods are largely immutable; edit the owning workload instead"))
		return
	}
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

	ctx, cancel := reqCtx(r)
	defer cancel()
	ns := ""
	if res.Namespaced {
		ns = r.PathValue("namespace")
	}

	// ?dryRun=true validates + runs admission/defaulting server-side without
	// persisting, so the frontend can preview what applying would actually do
	// (and surface validation errors) before committing for real.
	dryRun := r.URL.Query().Get("dryRun") == "true"
	opts := metav1.UpdateOptions{}
	if dryRun {
		opts.DryRun = []string{metav1.DryRunAll}
	} else {
		audit(r, "apply-manifest", "kind", kind, "namespace", ns, "name", r.PathValue("name"))
	}
	updated, err := dyn.Resource(res.GVR).Namespace(ns).Update(ctx, obj, opts)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	if dryRun {
		cleanUnstructured(updated)
		data, err := yaml.Marshal(updated.Object)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"yaml": string(data)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "applied"})
}

// getUnstructured fetches any resource (typed kind or CRD) by manifest slug.
func (s *Server) getUnstructured(ctx context.Context, contextName, kind, ns, name string) (*unstructured.Unstructured, error) {
	res, err := s.resolveSlug(contextName, kind)
	if err != nil {
		return nil, err
	}
	dyn, err := s.mgr.DynamicFor(contextName)
	if err != nil {
		return nil, err
	}
	if !res.Namespaced || ns == "-" {
		ns = ""
	}
	return dyn.Resource(res.GVR).Namespace(ns).Get(ctx, name, metav1.GetOptions{})
}

func (s *Server) resolveSlug(contextName, slug string) (kube.Resource, error) {
	plural, ok := manifestSlugToResource[slug]
	if !ok {
		return kube.Resource{}, fmt.Errorf("unsupported kind %q", slug)
	}
	return s.mgr.ResolveResource(contextName, plural)
}

// cleanUnstructured strips server-managed noise so the YAML reads cleanly.
func cleanUnstructured(obj *unstructured.Unstructured) {
	unstructured.RemoveNestedField(obj.Object, "metadata", "managedFields")
	ann, _, _ := unstructured.NestedStringMap(obj.Object, "metadata", "annotations")
	if ann != nil {
		delete(ann, "kubectl.kubernetes.io/last-applied-configuration")
		if len(ann) == 0 {
			unstructured.RemoveNestedField(obj.Object, "metadata", "annotations")
		} else {
			_ = unstructured.SetNestedStringMap(obj.Object, ann, "metadata", "annotations")
		}
	}
}
