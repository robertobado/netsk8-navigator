package api

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"github.com/robertobado/netsk8-navigator/backend/internal/kube"
)

// This file backs the inline row expansions for Node, Namespace, ServiceAccount
// and ConfigMap/Secret: joins that are cheaper and cleaner to compute
// server-side (a pod's top-level workload, a namespace's resources by type, an
// SA's bindings, a config object's consumers) than in the client.

// controllerRef returns the Kind and Name of an object's controlling owner.
func controllerRef(refs []metav1.OwnerReference) (kind, name string) {
	for _, o := range refs {
		if o.Controller != nil && *o.Controller {
			return o.Kind, o.Name
		}
	}
	return "", ""
}

// --- Node → workloads running on it ----------------------------------------

// nodeWorkloadGroup is one workload (or the standalone-pods bucket) with the
// pods it currently has scheduled on the queried node.
type nodeWorkloadGroup struct {
	Kind      string         `json:"kind"`      // Deployment | StatefulSet | DaemonSet | Job | Pod (standalone) | …
	Slug      string         `json:"slug"`      // manifest slug for the detail drawer ("" when not openable)
	Namespace string         `json:"namespace"` // empty for the standalone bucket
	Name      string         `json:"name"`      // empty for the standalone bucket
	Pods      []kube.PodView `json:"pods"`
}

// handleNodeWorkloads: GET /api/contexts/{ctx}/node-workloads/{node}
// The pods on a node, grouped by their top-level controller (ReplicaSets are
// resolved to their owning Deployment), for the Node row expansion.
func (s *Server) handleNodeWorkloads(w http.ResponseWriter, r *http.Request) {
	client, err := s.mgr.ClientFor(r.PathValue("ctx"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := reqCtx(r)
	defer cancel()

	pods, err := client.CoreV1().Pods("").List(ctx, metav1.ListOptions{FieldSelector: "spec.nodeName=" + r.PathValue("node")})
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	out := groupPodsByWorkload(pods.Items, replicaSetToDeployment(ctx, client))
	sortNodeGroups(out)
	writeJSON(w, http.StatusOK, out)
}

// replicaSetToDeployment maps "namespace/replicaset" → owning Deployment name,
// so RS-managed pods group under their Deployment rather than the churn-prone RS.
func replicaSetToDeployment(ctx context.Context, client kubernetes.Interface) map[string]string {
	m := map[string]string{}
	rss, err := client.AppsV1().ReplicaSets("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return m
	}
	for i := range rss.Items {
		rs := &rss.Items[i]
		if kind, name := controllerRef(rs.OwnerReferences); kind == "Deployment" {
			m[rs.Namespace+"/"+rs.Name] = name
		}
	}
	return m
}

// workloadOf returns the pod's top-level workload as (Kind, Name); standalone
// pods (no controller) come back as ("Pod", "").
func workloadOf(p *corev1.Pod, rsToDeploy map[string]string) (kind, name string) {
	kind, name = controllerRef(p.OwnerReferences)
	if kind == "ReplicaSet" {
		if dep, ok := rsToDeploy[p.Namespace+"/"+name]; ok {
			return "Deployment", dep
		}
	}
	if kind == "" {
		return "Pod", ""
	}
	return kind, name
}

func groupPodsByWorkload(pods []corev1.Pod, rsToDeploy map[string]string) []nodeWorkloadGroup {
	groups := map[string]*nodeWorkloadGroup{}
	order := []string{}
	for i := range pods {
		p := &pods[i]
		kind, name := workloadOf(p, rsToDeploy)
		ns := p.Namespace
		key := kind + "/" + ns + "/" + name
		if name == "" { // standalone bucket
			key, ns = "__standalone", ""
		}
		g := groups[key]
		if g == nil {
			slug := ""
			if name != "" {
				slug = kindSlug(kind)
			}
			g = &nodeWorkloadGroup{Kind: kind, Slug: slug, Namespace: ns, Name: name}
			groups[key] = g
			order = append(order, key)
		}
		g.Pods = append(g.Pods, kube.ToPodView(p))
	}
	out := make([]nodeWorkloadGroup, 0, len(order))
	for _, k := range order {
		out = append(out, *groups[k])
	}
	return out
}

// sortNodeGroups orders groups by kind then name, with the standalone-pods
// bucket last so it doesn't interrupt the named workloads.
func sortNodeGroups(g []nodeWorkloadGroup) {
	standalone := func(x nodeWorkloadGroup) bool { return x.Kind == "Pod" && x.Name == "" }
	sort.SliceStable(g, func(i, j int) bool {
		if standalone(g[i]) != standalone(g[j]) {
			return standalone(g[j])
		}
		if g[i].Kind != g[j].Kind {
			return g[i].Kind < g[j].Kind
		}
		return g[i].Name < g[j].Name
	})
}

// --- Namespace → its resources grouped by type ------------------------------

type nsNameRef struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}
type namespaceGroup struct {
	Kind  string      `json:"kind"`
	Slug  string      `json:"slug"`
	Items []nsNameRef `json:"items"`
}

// nsResourceTypes is the ordered set of namespaced kinds surfaced by the
// Namespace expansion (workloads → network → config → storage → rbac → policy).
var nsResourceTypes = []struct {
	plural, kind, slug string
}{
	{"pods", "Pod", "pod"},
	{"deployments", "Deployment", "deployment"},
	{"statefulsets", "StatefulSet", "statefulset"},
	{"daemonsets", "DaemonSet", "daemonset"},
	{"replicasets", "ReplicaSet", "replicaset"},
	{"jobs", "Job", "job"},
	{"cronjobs", "CronJob", "cronjob"},
	{"services", "Service", "service"},
	{"ingresses", "Ingress", "ingress"},
	{"endpointslices", "EndpointSlice", "endpointslice"},
	{"networkpolicies", "NetworkPolicy", "networkpolicy"},
	{"configmaps", "ConfigMap", "configmap"},
	{"secrets", "Secret", "secret"},
	{"horizontalpodautoscalers", "HorizontalPodAutoscaler", "hpa"},
	{"persistentvolumeclaims", "PersistentVolumeClaim", "pvc"},
	{"serviceaccounts", "ServiceAccount", "serviceaccount"},
	{"roles", "Role", "role"},
	{"rolebindings", "RoleBinding", "rolebinding"},
	{"resourcequotas", "ResourceQuota", "resourcequota"},
	{"limitranges", "LimitRange", "limitrange"},
	{"poddisruptionbudgets", "PodDisruptionBudget", "poddisruptionbudget"},
}

// handleNamespaceSummary: GET /api/contexts/{ctx}/namespace-summary/{namespace}
// Every namespaced resource in the namespace, grouped by type (non-empty groups
// only), for the Namespace row expansion. Type lists run concurrently.
func (s *Server) handleNamespaceSummary(w http.ResponseWriter, r *http.Request) {
	contextName, ns := r.PathValue("ctx"), r.PathValue("namespace")
	dyn, err := s.mgr.DynamicFor(contextName)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := reqCtx(r)
	defer cancel()

	results := make([]*namespaceGroup, len(nsResourceTypes))
	var wg sync.WaitGroup
	for i, rt := range nsResourceTypes {
		wg.Add(1)
		go func() { // Go 1.22+ per-iteration loop vars — safe to capture i/rt
			defer wg.Done()
			results[i] = s.nsGroup(ctx, dyn, contextName, ns, rt.plural, rt.kind, rt.slug)
		}()
	}
	wg.Wait()

	out := make([]namespaceGroup, 0, len(results)) // preserve nsResourceTypes order
	for _, g := range results {
		if g != nil {
			out = append(out, *g)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// nsGroup lists one resource type in a namespace, returning nil when the kind
// isn't served, the list fails, or it's empty.
func (s *Server) nsGroup(ctx context.Context, dyn dynamic.Interface, contextName, ns, plural, kind, slug string) *namespaceGroup {
	res, err := s.mgr.ResolveResource(contextName, plural)
	if err != nil || !res.Namespaced {
		return nil
	}
	list, err := dyn.Resource(res.GVR).Namespace(ns).List(ctx, metav1.ListOptions{})
	if err != nil || len(list.Items) == 0 {
		return nil
	}
	items := make([]nsNameRef, 0, len(list.Items))
	for j := range list.Items {
		items = append(items, nsNameRef{Name: list.Items[j].GetName(), Namespace: ns})
	}
	sort.Slice(items, func(a, b int) bool { return items[a].Name < items[b].Name })
	return &namespaceGroup{Kind: kind, Slug: slug, Items: items}
}

// --- ServiceAccount → its RoleBindings and the pods that run as it ----------

type bindingRef struct {
	Kind      string `json:"kind"` // RoleBinding | ClusterRoleBinding
	Slug      string `json:"slug"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}
type saUsage struct {
	Bindings    []bindingRef   `json:"bindings"`
	Pods        []kube.PodView `json:"pods"`
	Permissions []kv           `json:"permissions"` // effective, deduped, from every bound Role/ClusterRole
}

// handleServiceAccountUsage: GET /api/contexts/{ctx}/serviceaccount-usage/{namespace}/{name}
// The (Cluster)RoleBindings that grant this SA, the pods running as it, and its
// effective permissions — the union of every bound Role/ClusterRole's rules.
// This is the app's "can this SA do X" answer: a static union of granted
// rules rather than a live SelfSubjectAccessReview/SubjectAccessReview
// simulation, so it works without the backend's own credentials needing
// cluster-admin-level access to the SubjectAccessReview API.
func (s *Server) handleServiceAccountUsage(w http.ResponseWriter, r *http.Request) {
	client, err := s.mgr.ClientFor(r.PathValue("ctx"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := reqCtx(r)
	defer cancel()
	ns, name := r.PathValue("namespace"), r.PathValue("name")

	bindings, rules := bindingsForSA(ctx, client, ns, name)
	writeJSON(w, http.StatusOK, saUsage{
		Bindings:    bindings,
		Pods:        podsUsingSA(ctx, client, ns, name),
		Permissions: formatRules(dedupeRules(rules)),
	})
}

func podsUsingSA(ctx context.Context, client kubernetes.Interface, ns, name string) []kube.PodView {
	out := []kube.PodView{}
	pods, err := client.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return out
	}
	for i := range pods.Items {
		san := pods.Items[i].Spec.ServiceAccountName
		if san == "" {
			san = "default"
		}
		if san == name {
			out = append(out, kube.ToPodView(&pods.Items[i]))
		}
	}
	return out
}

// bindingsForSA returns every (Cluster)RoleBinding naming the SA, plus the
// combined PolicyRules of every Role/ClusterRole those bindings reference.
func bindingsForSA(ctx context.Context, client kubernetes.Interface, ns, name string) ([]bindingRef, []rbacv1.PolicyRule) {
	out := []bindingRef{}
	var rules []rbacv1.PolicyRule
	if rbs, err := client.RbacV1().RoleBindings(ns).List(ctx, metav1.ListOptions{}); err == nil {
		for i := range rbs.Items {
			rb := &rbs.Items[i]
			if !subjectsIncludeSA(rb.Subjects, ns, name) {
				continue
			}
			out = append(out, bindingRef{Kind: "RoleBinding", Slug: "rolebinding", Namespace: ns, Name: rb.Name})
			rules = append(rules, roleRefRules(ctx, client, ns, rb.RoleRef)...)
		}
	}
	if crbs, err := client.RbacV1().ClusterRoleBindings().List(ctx, metav1.ListOptions{}); err == nil {
		for i := range crbs.Items {
			crb := &crbs.Items[i]
			if !subjectsIncludeSA(crb.Subjects, ns, name) {
				continue
			}
			out = append(out, bindingRef{Kind: "ClusterRoleBinding", Slug: "clusterrolebinding", Name: crb.Name})
			rules = append(rules, roleRefRules(ctx, client, "", crb.RoleRef)...)
		}
	}
	return out, rules
}

// roleRefRules resolves a RoleRef (Role or ClusterRole) to its PolicyRules.
// ns only applies to a "Role" ref — a ClusterRole is cluster-scoped.
func roleRefRules(ctx context.Context, client kubernetes.Interface, ns string, ref rbacv1.RoleRef) []rbacv1.PolicyRule {
	switch ref.Kind {
	case "Role":
		if role, err := client.RbacV1().Roles(ns).Get(ctx, ref.Name, metav1.GetOptions{}); err == nil {
			return role.Rules
		}
	case "ClusterRole":
		if role, err := client.RbacV1().ClusterRoles().Get(ctx, ref.Name, metav1.GetOptions{}); err == nil {
			return role.Rules
		}
	}
	return nil
}

// dedupeRules drops rules identical in every field — two bindings can
// reference the same role, which would otherwise duplicate every one of its
// rules in the effective-permissions view.
func dedupeRules(rules []rbacv1.PolicyRule) []rbacv1.PolicyRule {
	seen := map[string]bool{}
	out := make([]rbacv1.PolicyRule, 0, len(rules))
	for _, r := range rules {
		key := strings.Join(r.Verbs, ",") + "|" + strings.Join(r.APIGroups, ",") + "|" + strings.Join(r.Resources, ",") + "|" + strings.Join(r.NonResourceURLs, ",")
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, r)
	}
	return out
}

// subjectsIncludeSA reports whether a binding's subjects name the SA ns/name.
func subjectsIncludeSA(subjects []rbacv1.Subject, ns, name string) bool {
	for _, sub := range subjects {
		if sub.Kind == "ServiceAccount" && sub.Name == name && (sub.Namespace == ns || sub.Namespace == "") {
			return true
		}
	}
	return false
}

// --- ConfigMap / Secret → the pods that consume them ------------------------

// handleConsumers: GET /api/contexts/{ctx}/consumers/{kind}/{namespace}/{name}
// Pods in the namespace that reference the ConfigMap/Secret (volumes, env,
// envFrom, projected sources, and imagePullSecrets for Secrets).
func (s *Server) handleConsumers(w http.ResponseWriter, r *http.Request) {
	client, err := s.mgr.ClientFor(r.PathValue("ctx"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := reqCtx(r)
	defer cancel()
	isCM := r.PathValue("kind") == "configmap"
	ns, name := r.PathValue("namespace"), r.PathValue("name")

	pods, err := client.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	out := make([]kube.PodView, 0)
	for i := range pods.Items {
		if podConsumes(&pods.Items[i], isCM, name) {
			out = append(out, kube.ToPodView(&pods.Items[i]))
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// podConsumes reports whether a pod references the named ConfigMap (isCM) or
// Secret through any of the usual mechanisms.
func podConsumes(p *corev1.Pod, isCM bool, name string) bool {
	return volumesConsume(p.Spec.Volumes, isCM, name) ||
		(!isCM && pullSecretMatches(p.Spec.ImagePullSecrets, name)) ||
		containersConsume(p.Spec.InitContainers, isCM, name) ||
		containersConsume(p.Spec.Containers, isCM, name)
}

func volumesConsume(vols []corev1.Volume, isCM bool, name string) bool {
	for _, v := range vols {
		if isCM && v.ConfigMap != nil && v.ConfigMap.Name == name {
			return true
		}
		if !isCM && v.Secret != nil && v.Secret.SecretName == name {
			return true
		}
		if v.Projected != nil && projectedConsumes(v.Projected.Sources, isCM, name) {
			return true
		}
	}
	return false
}

func projectedConsumes(sources []corev1.VolumeProjection, isCM bool, name string) bool {
	for _, src := range sources {
		if isCM && src.ConfigMap != nil && src.ConfigMap.Name == name {
			return true
		}
		if !isCM && src.Secret != nil && src.Secret.Name == name {
			return true
		}
	}
	return false
}

func pullSecretMatches(refs []corev1.LocalObjectReference, name string) bool {
	for _, s := range refs {
		if s.Name == name {
			return true
		}
	}
	return false
}

func containersConsume(containers []corev1.Container, isCM bool, name string) bool {
	for _, c := range containers {
		if envFromConsumes(c.EnvFrom, isCM, name) || envConsumes(c.Env, isCM, name) {
			return true
		}
	}
	return false
}

func envFromConsumes(sources []corev1.EnvFromSource, isCM bool, name string) bool {
	for _, ef := range sources {
		if isCM && ef.ConfigMapRef != nil && ef.ConfigMapRef.Name == name {
			return true
		}
		if !isCM && ef.SecretRef != nil && ef.SecretRef.Name == name {
			return true
		}
	}
	return false
}

func envConsumes(env []corev1.EnvVar, isCM bool, name string) bool {
	for _, e := range env {
		if e.ValueFrom == nil {
			continue
		}
		if isCM && e.ValueFrom.ConfigMapKeyRef != nil && e.ValueFrom.ConfigMapKeyRef.Name == name {
			return true
		}
		if !isCM && e.ValueFrom.SecretKeyRef != nil && e.ValueFrom.SecretKeyRef.Name == name {
			return true
		}
	}
	return false
}

// kindSlug maps a workload Kind to its manifest slug (for detail links).
func kindSlug(kind string) string {
	switch kind {
	case "Deployment":
		return "deployment"
	case "StatefulSet":
		return "statefulset"
	case "DaemonSet":
		return "daemonset"
	case "ReplicaSet":
		return "replicaset"
	case "Job":
		return "job"
	case "CronJob":
		return "cronjob"
	}
	return ""
}
