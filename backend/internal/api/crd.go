package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/yaml"
)

// routeCandidate is a known ingress/gateway CRD we surface under "Network" when the
// cluster serves it. Keyed by "<group>/<plural>"; the version comes from discovery.
type routeCandidate struct {
	label string // nav label
	order int    // display order within the group family
}

var routeCandidates = map[string]routeCandidate{
	// Gateway API (Envoy Gateway, Istio, Contour, ...)
	"gateway.networking.k8s.io/gateways":   {"Gateways", 1},
	"gateway.networking.k8s.io/httproutes": {"HTTPRoutes", 2},
	"gateway.networking.k8s.io/grpcroutes": {"GRPCRoutes", 3},
	"gateway.networking.k8s.io/tcproutes":  {"TCPRoutes", 4},
	"gateway.networking.k8s.io/tlsroutes":  {"TLSRoutes", 5},
	// Traefik
	"traefik.io/ingressroutes":          {"IngressRoutes", 10},
	"traefik.io/ingressroutetcps":       {"IngressRoute TCP", 11},
	"traefik.containo.us/ingressroutes": {"IngressRoutes", 12},
	// Istio
	"networking.istio.io/virtualservices": {"VirtualServices", 20},
	"networking.istio.io/gateways":        {"Istio Gateways", 21},
	// Contour
	"projectcontour.io/httpproxies": {"HTTPProxies", 30},
}

type routeKind struct {
	Group      string `json:"group"`
	Version    string `json:"version"`
	Resource   string `json:"resource"`
	Kind       string `json:"kind"`
	Namespaced bool   `json:"namespaced"`
	Label      string `json:"label"`
	Order      int    `json:"order"`
}

// handleRouteKinds: GET /api/contexts/{ctx}/routekinds
// Which known route CRDs the cluster actually serves (via discovery).
func (s *Server) handleRouteKinds(w http.ResponseWriter, r *http.Request) {
	client, err := s.mgr.ClientFor(r.PathValue("ctx"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	lists, err := client.Discovery().ServerPreferredResources()
	// Partial discovery errors (e.g. an unavailable aggregated API) are non-fatal.
	if err != nil && len(lists) == 0 {
		writeJSON(w, http.StatusOK, []routeKind{})
		return
	}

	out := []routeKind{}
	for _, l := range lists {
		gv, perr := schema.ParseGroupVersion(l.GroupVersion)
		if perr != nil {
			continue
		}
		for _, res := range l.APIResources {
			if strings.Contains(res.Name, "/") {
				continue // skip subresources
			}
			cand, ok := routeCandidates[gv.Group+"/"+res.Name]
			if !ok {
				continue
			}
			out = append(out, routeKind{
				Group: gv.Group, Version: gv.Version, Resource: res.Name,
				Kind: res.Kind, Namespaced: res.Namespaced, Label: cand.label, Order: cand.order,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Order < out[j].Order })
	writeJSON(w, http.StatusOK, out)
}

type crdKind struct {
	Group      string `json:"group"`
	Version    string `json:"version"`
	Resource   string `json:"resource"`
	Kind       string `json:"kind"`
	Namespaced bool   `json:"namespaced"`
	Label      string `json:"label"`
}

// handleCRDKinds: GET /api/contexts/{ctx}/crdkinds
// Every CustomResourceDefinition the cluster has registered — no allowlist,
// unlike handleRouteKinds (which stays as the curated "Network" subset this
// complements). Overlap between the two is harmless.
func (s *Server) handleCRDKinds(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := reqCtx(r)
	defer cancel()

	crds, err := s.mgr.CRDsFor(ctx, r.PathValue("ctx"))
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	out := make([]crdKind, 0, len(crds))
	for _, c := range crds {
		version := storageVersion(c.Spec.Versions)
		if version == "" {
			continue // no served version — nothing to browse
		}
		names := c.Status.AcceptedNames // more authoritative than Spec.Names post-admission
		if names.Kind == "" {
			names = c.Spec.Names
		}
		out = append(out, crdKind{
			Group: c.Spec.Group, Version: version, Resource: names.Plural,
			Kind: names.Kind, Namespaced: c.Spec.Scope == apiextensionsv1.NamespaceScoped,
			Label: names.Kind,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Group != out[j].Group {
			return out[i].Group < out[j].Group
		}
		return out[i].Kind < out[j].Kind
	})
	writeJSON(w, http.StatusOK, out)
}

// storageVersion picks the version CRD instances are actually stored/read as.
func storageVersion(versions []apiextensionsv1.CustomResourceDefinitionVersion) string {
	for _, v := range versions {
		if v.Storage {
			return v.Name
		}
	}
	for _, v := range versions {
		if v.Served { // defensive fallback — a well-formed CRD always has a storage version
			return v.Name
		}
	}
	return ""
}

type crdItem struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Age       string `json:"age"`
	Hosts     string `json:"hosts"`
	Refs      string `json:"refs"`
}

func (s *Server) dynFor(ctx string) (dynamic.Interface, error) {
	return s.mgr.DynamicFor(ctx)
}

// handleCRDList: GET /api/contexts/{ctx}/crd/{group}/{version}/{resource}?namespace=
func (s *Server) handleCRDList(w http.ResponseWriter, r *http.Request) {
	dyn, err := s.dynFor(r.PathValue("ctx"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := reqCtx(r)
	defer cancel()

	gvr := schema.GroupVersionResource{Group: r.PathValue("group"), Version: r.PathValue("version"), Resource: r.PathValue("resource")}
	ns := r.URL.Query().Get("namespace")
	list, err := dyn.Resource(gvr).Namespace(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	out := make([]crdItem, 0, len(list.Items))
	for i := range list.Items {
		it := &list.Items[i]
		out = append(out, crdItem{
			Name:      it.GetName(),
			Namespace: it.GetNamespace(),
			Age:       age(it.GetCreationTimestamp()),
			Hosts:     strings.Join(extractHosts(it.Object), ", "),
			Refs:      strings.Join(extractRefs(it.Object), ", "),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	writeJSON(w, http.StatusOK, out)
}

// handleCRDManifest: GET /api/contexts/{ctx}/crd/{group}/{version}/{resource}/{namespace}/{name}/manifest
func (s *Server) handleCRDManifest(w http.ResponseWriter, r *http.Request) {
	dyn, err := s.dynFor(r.PathValue("ctx"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := reqCtx(r)
	defer cancel()

	gvr := schema.GroupVersionResource{Group: r.PathValue("group"), Version: r.PathValue("version"), Resource: r.PathValue("resource")}
	ns := r.PathValue("namespace")
	if ns == "-" {
		ns = ""
	}
	obj, err := dyn.Resource(gvr).Namespace(ns).Get(ctx, r.PathValue("name"), metav1.GetOptions{})
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	unstructured.RemoveNestedField(obj.Object, "metadata", "managedFields")
	y, err := yaml.Marshal(obj.Object)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"yaml": string(y)})
}

// handleCRDApply: PUT /api/contexts/{ctx}/crd/{group}/{version}/{resource}/{namespace}/{name}
// Mirrors handleApplyManifest, but addresses the resource by GVR straight from
// the URL (like handleCRDManifest) instead of a manifest slug — this is what
// makes arbitrary/unmapped CRDs editable, not just the catalog/route ones.
func (s *Server) handleCRDApply(w http.ResponseWriter, r *http.Request) {
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

	dyn, err := s.dynFor(r.PathValue("ctx"))
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
	gvr := schema.GroupVersionResource{Group: r.PathValue("group"), Version: r.PathValue("version"), Resource: r.PathValue("resource")}
	ns := r.PathValue("namespace")
	if ns == "-" {
		ns = ""
	}

	dryRun := r.URL.Query().Get("dryRun") == "true"
	opts := metav1.UpdateOptions{}
	if dryRun {
		opts.DryRun = []string{metav1.DryRunAll}
	} else {
		audit(r, "crd-apply", "resource", gvr.String(), "namespace", ns, "name", r.PathValue("name"))
	}
	updated, err := dyn.Resource(gvr).Namespace(ns).Update(ctx, obj, opts)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	if dryRun {
		writeDryRunYAML(w, updated)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "applied"})
}

// handleCRDDelete: DELETE /api/contexts/{ctx}/crd/{group}/{version}/{resource}/{namespace}/{name}
// Mirrors handleDeleteResource, same GVR-from-URL approach as handleCRDApply.
func (s *Server) handleCRDDelete(w http.ResponseWriter, r *http.Request) {
	dyn, err := s.dynFor(r.PathValue("ctx"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := reqCtx(r)
	defer cancel()

	gvr := schema.GroupVersionResource{Group: r.PathValue("group"), Version: r.PathValue("version"), Resource: r.PathValue("resource")}
	ns := r.PathValue("namespace")
	if ns == "-" {
		ns = ""
	}
	audit(r, "crd-delete", "resource", gvr.String(), "namespace", ns, "name", r.PathValue("name"))
	if err := dyn.Resource(gvr).Namespace(ns).Delete(ctx, r.PathValue("name"), metav1.DeleteOptions{}); err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handleCRDDetail: GET /api/contexts/{ctx}/crd/{group}/{version}/{resource}/{namespace}/{name}/detail
func (s *Server) handleCRDDetail(w http.ResponseWriter, r *http.Request) {
	dyn, err := s.dynFor(r.PathValue("ctx"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := reqCtx(r)
	defer cancel()

	gvr := schema.GroupVersionResource{Group: r.PathValue("group"), Version: r.PathValue("version"), Resource: r.PathValue("resource")}
	ns := r.PathValue("namespace")
	if ns == "-" {
		ns = ""
	}
	obj, err := dyn.Resource(gvr).Namespace(ns).Get(ctx, r.PathValue("name"), metav1.GetOptions{})
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, crdDetail(obj))
}

func crdDetail(u *unstructured.Unstructured) *resourceDetail {
	d := &resourceDetail{
		Kind: u.GetKind(), Name: u.GetName(), Namespace: u.GetNamespace(),
		Age: age(u.GetCreationTimestamp()), Labels: u.GetLabels(),
	}
	switch u.GetKind() {
	case "HTTPRoute", "GRPCRoute", "TCPRoute", "TLSRoute":
		routeCRDDetail(d, u.Object)
	case "Gateway":
		gatewayCRDDetail(d, u.Object)
	default:
		genericSpecSection(d, u.Object)
	}
	crdConditions(d, u.Object)
	return d
}

// routeCRDDetail renders Gateway API *Route resources: hostnames, parent gateways, rules.
func routeCRDDetail(d *resourceDetail, obj map[string]any) {
	d.Hosts = extractHosts(obj)
	if refs := nestedRefs(obj, "spec", "parentRefs"); len(refs) > 0 {
		items := []kv{}
		for _, ref := range refs {
			ns, _ := ref["namespace"].(string)
			sec, _ := ref["sectionName"].(string)
			val := ns
			if sec != "" {
				val = strings.TrimSpace(val + " · " + sec)
			}
			name, _ := ref["name"].(string)
			items = append(items, kv{Label: name, Value: val})
		}
		d.Sections = append(d.Sections, section{Title: "Gateways", Items: items})
	}
	rules, _, _ := unstructured.NestedSlice(obj, "spec", "rules")
	for i, r := range rules {
		rule, ok := r.(map[string]any)
		if !ok {
			continue
		}
		items := []kv{}
		for _, m := range sliceOf(rule, "matches") {
			items = append(items, kv{Label: "Match", Value: matchSummary(m)})
		}
		for _, b := range sliceOf(rule, "backendRefs") {
			items = append(items, kv{Label: "Backend", Value: backendSummary(b)})
		}
		d.Sections = append(d.Sections, section{Title: fmt.Sprintf("Rule %d", i+1), Items: items})
	}
}

func gatewayCRDDetail(d *resourceDetail, obj map[string]any) {
	if gc, ok, _ := unstructured.NestedString(obj, "spec", "gatewayClassName"); ok && gc != "" {
		d.Status = append(d.Status, chip{Label: "Class", Value: gc, Tone: "muted"})
	}
	listeners, _, _ := unstructured.NestedSlice(obj, "spec", "listeners")
	if items := gatewayListenerItems(listeners); len(items) > 0 {
		d.Sections = append(d.Sections, section{Title: "Listeners", Items: items})
	}
	if addrs := gatewayAddresses(obj); len(addrs) > 0 {
		d.Sections = append(d.Sections, section{Title: "Addresses", Items: []kv{{Label: "Address", Value: strings.Join(addrs, ", ")}}})
	}
}

// gatewayListenerItems summarizes each listener as "protocol/port · hostname".
func gatewayListenerItems(listeners []any) []kv {
	items := []kv{}
	for _, l := range listeners {
		lis, ok := l.(map[string]any)
		if !ok {
			continue
		}
		name, _ := lis["name"].(string)
		proto, _ := lis["protocol"].(string)
		port := asInt(lis["port"])
		host, _ := lis["hostname"].(string)
		val := fmt.Sprintf("%s/%d", proto, port)
		if host != "" {
			val += " · " + host
		}
		items = append(items, kv{Label: name, Value: val})
	}
	return items
}

// gatewayAddresses collects the Gateway's assigned addresses from status.
func gatewayAddresses(obj map[string]any) []string {
	addrs := nestedRefs(obj, "status", "addresses")
	av := []string{}
	for _, a := range addrs {
		if v, ok := a["value"].(string); ok {
			av = append(av, v)
		}
	}
	return av
}

// genericSpecSection lists top-level spec fields as a readable overview, with
// no prior knowledge of the CRD's schema. Scalars and simple arrays render
// flat; an object whose own fields are all simple gets a nested mini-grid;
// anything nested deeper (nested objects, arrays of objects) falls back to a
// read-only YAML block — so nothing ever collapses into a bare "{N fields}"
// or "N items" placeholder with no way to see what's inside.
func genericSpecSection(d *resourceDetail, obj map[string]any) {
	spec, ok, _ := unstructured.NestedMap(obj, "spec")
	if !ok {
		return
	}
	if items := fieldRows(spec); len(items) > 0 {
		d.Sections = append(d.Sections, section{Title: "Spec", Items: items})
	}
}

// fieldRows builds one row per key of m, sorted alphabetically.
func fieldRows(m map[string]any) []kv {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	items := make([]kv, 0, len(keys))
	for _, k := range keys {
		items = append(items, fieldRow(k, m[k]))
	}
	return items
}

// fieldRow classifies a single field's value: a scalar/simple-array stays a
// flat row; an object whose own fields are all simple becomes a nested
// mini-grid; anything nested deeper falls back to a YAML code block.
func fieldRow(label string, v any) kv {
	switch t := v.(type) {
	case map[string]any:
		if len(t) == 0 {
			return kv{Label: label, Value: "{}"}
		}
		if allSimple(t) {
			return kv{Label: label, Grid: fieldRows(t)}
		}
		return kv{Label: label, Code: toYAML(t)}
	case []any:
		if len(t) == 0 {
			return kv{Label: label, Value: "[]"}
		}
		if isSimpleArray(t) {
			return kv{Label: label, Chips: chipsOf(t)}
		}
		return kv{Label: label, Code: toYAML(t)}
	default:
		return kv{Label: label, Value: valueSummary(v)}
	}
}

// isScalar reports whether v is a leaf JSON value (string/bool/number/nil).
func isScalar(v any) bool {
	switch v.(type) {
	case nil, string, bool, int64, float64:
		return true
	default:
		return false
	}
}

func isSimpleArray(v []any) bool {
	for _, e := range v {
		if !isScalar(e) {
			return false
		}
	}
	return true
}

// allSimple reports whether every value in m is itself simple (a scalar or a
// simple array) — i.e. m is safe to render as a flat mini-grid with no
// further nesting.
func allSimple(m map[string]any) bool {
	for _, v := range m {
		switch t := v.(type) {
		case map[string]any:
			return false
		case []any:
			if !isSimpleArray(t) {
				return false
			}
		}
	}
	return true
}

func chipsOf(v []any) []string {
	chips := make([]string, 0, len(v))
	for _, e := range v {
		chips = append(chips, valueSummary(e))
	}
	return chips
}

// toYAML renders v as compact, read-only YAML for display in the UI.
func toYAML(v any) string {
	b, err := yaml.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return strings.TrimRight(string(b), "\n")
}

func crdConditions(d *resourceDetail, obj map[string]any) {
	conds, _, _ := unstructured.NestedSlice(obj, "status", "conditions")
	for _, c := range conds {
		cm, ok := c.(map[string]any)
		if !ok {
			continue
		}
		t, _ := cm["type"].(string)
		st, _ := cm["status"].(string)
		tone := "muted"
		switch st {
		case "True":
			tone = "ok"
		case "False":
			tone = "err"
		}
		d.Conditions = append(d.Conditions, chip{Label: t, Value: st, Tone: tone})
	}
}

// --- unstructured helpers ---

func nestedRefs(obj map[string]any, path ...string) []map[string]any {
	slice, _, _ := unstructured.NestedSlice(obj, path...)
	out := []map[string]any{}
	for _, e := range slice {
		if m, ok := e.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func sliceOf(m map[string]any, key string) []map[string]any {
	out := []map[string]any{}
	if s, ok := m[key].([]any); ok {
		for _, e := range s {
			if em, ok := e.(map[string]any); ok {
				out = append(out, em)
			}
		}
	}
	return out
}

func matchSummary(m map[string]any) string {
	if p, ok := m["path"].(map[string]any); ok {
		t, _ := p["type"].(string)
		v, _ := p["value"].(string)
		if method, ok := m["method"].(string); ok && method != "" {
			return fmt.Sprintf("%s %s %s", method, t, v)
		}
		return strings.TrimSpace(fmt.Sprintf("%s %s", t, v))
	}
	return valueSummary(m)
}

func backendSummary(b map[string]any) string {
	name, _ := b["name"].(string)
	if port := asInt(b["port"]); port > 0 {
		return fmt.Sprintf("%s:%d", name, port)
	}
	return name
}

func valueSummary(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case bool:
		return fmt.Sprintf("%t", t)
	case int64:
		return fmt.Sprintf("%d", t)
	case float64:
		return fmt.Sprintf("%g", t)
	case []any:
		return fmt.Sprintf("%d items", len(t))
	case map[string]any:
		return fmt.Sprintf("{%d fields}", len(t))
	}
	return "—"
}

func asInt(v any) int64 {
	switch t := v.(type) {
	case int64:
		return t
	case float64:
		return int64(t)
	}
	return 0
}

// extractHosts pulls a hostnames/hosts list from common CRD spec shapes.
func extractHosts(obj map[string]any) []string {
	for _, path := range [][]string{{"spec", "hostnames"}, {"spec", "hosts"}} {
		if v, ok, _ := unstructured.NestedStringSlice(obj, path...); ok && len(v) > 0 {
			return v
		}
	}
	// Contour: spec.virtualhost.fqdn
	if fqdn, ok, _ := unstructured.NestedString(obj, "spec", "virtualhost", "fqdn"); ok && fqdn != "" {
		return []string{fqdn}
	}
	// single host
	if h, ok, _ := unstructured.NestedString(obj, "spec", "host"); ok && h != "" {
		return []string{h}
	}
	return nil
}

// extractRefs pulls the parent/gateway names a route attaches to.
func extractRefs(obj map[string]any) []string {
	for _, path := range [][]string{{"spec", "parentRefs"}, {"spec", "gateways"}} {
		slice, ok, _ := unstructured.NestedSlice(obj, path...)
		if !ok {
			continue
		}
		if names := refNamesFromSlice(slice); len(names) > 0 {
			return names
		}
	}
	return nil
}

// refNamesFromSlice extracts ref names from either shape: {name: ...} objects
// (Gateway API parentRefs) or bare strings (Istio gateways).
func refNamesFromSlice(slice []any) []string {
	names := []string{}
	for _, e := range slice {
		switch v := e.(type) {
		case map[string]any: // parentRefs → {name: ...}
			if n, has := v["name"].(string); has {
				names = append(names, n)
			}
		case string: // istio gateways → ["name", ...]
			names = append(names, v)
		}
	}
	return names
}
