package api

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

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
	if len(items) > 0 {
		d.Sections = append(d.Sections, section{Title: "Listeners", Items: items})
	}
	if addrs := nestedRefs(obj, "status", "addresses"); len(addrs) > 0 {
		av := []string{}
		for _, a := range addrs {
			if v, ok := a["value"].(string); ok {
				av = append(av, v)
			}
		}
		if len(av) > 0 {
			d.Sections = append(d.Sections, section{Title: "Addresses", Items: []kv{{Label: "Address", Value: strings.Join(av, ", ")}}})
		}
	}
}

// genericSpecSection lists top-level spec fields as a readable overview.
func genericSpecSection(d *resourceDetail, obj map[string]any) {
	spec, ok, _ := unstructured.NestedMap(obj, "spec")
	if !ok {
		return
	}
	keys := make([]string, 0, len(spec))
	for k := range spec {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	items := []kv{}
	for _, k := range keys {
		items = append(items, kv{Label: k, Value: valueSummary(spec[k])})
	}
	if len(items) > 0 {
		d.Sections = append(d.Sections, section{Title: "Spec", Items: items})
	}
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
		if slice, ok, _ := unstructured.NestedSlice(obj, path...); ok {
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
			if len(names) > 0 {
				return names
			}
		}
	}
	return nil
}
