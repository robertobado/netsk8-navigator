package api

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"unicode/utf8"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	nodev1 "k8s.io/api/node/v1"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/robertobado/netsk8-navigator/backend/internal/kube"
)

// Structured, UI-friendly detail for a resource — a nicer representation than
// dumping the raw YAML. One flexible shape covers nodes and workload/sets.
type kv struct {
	Label string `json:"label"`
	Value string `json:"value"`
}
type chip struct {
	Label string `json:"label"`
	Value string `json:"value"`
	Tone  string `json:"tone"` // ok | warn | err | muted
}
type section struct {
	Title string `json:"title"`
	Items []kv   `json:"items"`
}

// detailRef is a clickable cross-link to another resource's detail.
type detailRef struct {
	Group     string `json:"group"` // section header, e.g. "Backends"
	Kind      string `json:"kind"`  // manifest slug (service, ...)
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Note      string `json:"note,omitempty"` // optional secondary line (e.g. an endpoint's IP)
}

// detailBlock is a titled preformatted content block (e.g. a ConfigMap key).
// Masked blocks (Secret values) are hidden by the UI until revealed.
type detailBlock struct {
	Title  string `json:"title"`
	Body   string `json:"body"`
	Masked bool   `json:"masked,omitempty"`
}

// portView is one network port, rendered by the UI as a pill (name · port · proto).
type portView struct {
	Name     string `json:"name,omitempty"`
	Port     string `json:"port"`               // "8080" or "8080 → 8080"
	Protocol string `json:"protocol,omitempty"` // TCP | UDP
	Extra    string `json:"extra,omitempty"`    // e.g. "node 30080"
}

type resourceDetail struct {
	Kind        string            `json:"kind"`
	Name        string            `json:"name"`
	Namespace   string            `json:"namespace"`
	Age         string            `json:"age"`
	OwnerKind   string            `json:"ownerKind"`
	OwnerName   string            `json:"ownerName"`
	Status      []chip            `json:"status"`
	Sections    []section         `json:"sections"`
	Selector    map[string]string `json:"selector"`
	Images      []kv              `json:"images"`
	Conditions  []chip            `json:"conditions"`
	Labels      map[string]string `json:"labels"`
	Refs        []detailRef       `json:"refs"`
	Blocks      []detailBlock     `json:"blocks"`
	Hosts       []string          `json:"hosts"` // route hostnames (linkable when not a wildcard)
	Ports       []portView        `json:"ports"`
	Replicas    *int32            `json:"replicas,omitempty"`    // desired count, for kinds the UI can scale
	Schedulable *bool             `json:"schedulable,omitempty"` // nodes only — false when cordoned
}

// detailFrom adapts a typed detail builder into one that accepts a dynamic
// object, so detail fetching is generic (multi-version) while the rich, typed
// builders below stay unchanged.
func detailFrom[T any](build func(*T) *resourceDetail) func(*unstructured.Unstructured) (*resourceDetail, error) {
	return func(u *unstructured.Unstructured) (*resourceDetail, error) {
		o, err := fromUnstructured[T](u)
		if err != nil {
			return nil, err
		}
		return build(o), nil
	}
}

// detailBuilders maps a manifest slug to its structured-detail builder. Adding a
// resource's rich detail is one entry here plus the builder function.
var detailBuilders = map[string]func(*unstructured.Unstructured) (*resourceDetail, error){
	"pod":                 detailFrom(podDetail),
	"node":                detailFrom(nodeDetail),
	"namespace":           detailFrom(namespaceDetail),
	"secret":              detailFrom(secretDetail),
	"deployment":          detailFrom(deploymentDetail),
	"replicaset":          detailFrom(replicaSetDetail),
	"statefulset":         detailFrom(statefulSetDetail),
	"daemonset":           detailFrom(daemonSetDetail),
	"job":                 detailFrom(jobDetail),
	"cronjob":             detailFrom(cronJobDetail),
	"service":             detailFrom(serviceDetail),
	"ingress":             detailFrom(ingressDetail),
	"configmap":           detailFrom(configMapDetail),
	"pvc":                 detailFrom(pvcDetail),
	"pv":                  detailFrom(pvDetail),
	"storageclass":        detailFrom(storageClassDetail),
	"hpa":                 detailFrom(hpaDetail),
	"endpointslice":       detailFrom(endpointSliceDetail),
	"networkpolicy":       detailFrom(networkPolicyDetail),
	"ingressclass":        detailFrom(ingressClassDetail),
	"serviceaccount":      detailFrom(serviceAccountDetail),
	"role":                detailFrom(roleDetail),
	"clusterrole":         detailFrom(clusterRoleDetail),
	"rolebinding":         detailFrom(roleBindingDetail),
	"clusterrolebinding":  detailFrom(clusterRoleBindingDetail),
	"resourcequota":       detailFrom(resourceQuotaDetail),
	"limitrange":          detailFrom(limitRangeDetail),
	"poddisruptionbudget": detailFrom(pdbDetail),
	"priorityclass":       detailFrom(priorityClassDetail),
	"runtimeclass":        detailFrom(runtimeClassDetail),
}

// handleDetail: GET /api/contexts/{ctx}/detail/{kind}/{namespace}/{name}
func (s *Server) handleDetail(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	build, ok := detailBuilders[kind]
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Errorf("no structured detail for kind %q", kind))
		return
	}
	ctx, cancel := reqCtx(r)
	defer cancel()

	obj, err := s.getUnstructured(ctx, r.PathValue("ctx"), kind, r.PathValue("namespace"), r.PathValue("name"))
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	d, err := build(obj)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	if kind == "secret" {
		// The decoded values are in the response body below (masked only in the
		// UI, until the user clicks reveal) — so the sensitive read happens here,
		// not on some later "reveal" request.
		audit(r, "read-secret", "namespace", r.PathValue("namespace"), "name", r.PathValue("name"))
	}
	if enrich, ok := detailEnrichers[kind]; ok {
		enrich(ctx, s, r.PathValue("ctx"), r.PathValue("namespace"), r.PathValue("name"), d)
	}
	writeJSON(w, http.StatusOK, d)
}

// detailEnrichers augment a built detail with data that isn't on the object
// itself — e.g. which pods mount a PVC (needs a pod list). Keyed by slug.
var detailEnrichers = map[string]func(ctx context.Context, s *Server, contextName, ns, name string, d *resourceDetail){
	"pvc": enrichPVCConsumers,
}

// enrichPVCConsumers adds, for a Bound PVC, a "Mounted" chip and a link to each
// pod that mounts it. A Bound PVC with no mounting pod is normal (e.g. a
// StatefulSet scaled down keeps its retained claims), so we say so explicitly
// rather than silently omitting the section.
func enrichPVCConsumers(ctx context.Context, s *Server, contextName, ns, name string, d *resourceDetail) {
	bound := false
	for _, c := range d.Status {
		if c.Value == string(corev1.ClaimBound) {
			bound = true
			break
		}
	}
	if !bound {
		return
	}
	client, err := s.mgr.ClientFor(contextName)
	if err != nil {
		return
	}
	pods, err := client.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return
	}
	mounted := 0
	for i := range pods.Items {
		p := &pods.Items[i]
		for _, v := range p.Spec.Volumes {
			if v.PersistentVolumeClaim != nil && v.PersistentVolumeClaim.ClaimName == name {
				d.Refs = append(d.Refs, detailRef{Group: "Mounted by", Kind: "pod", Namespace: ns, Name: p.Name})
				mounted++
				break
			}
		}
	}
	tone, val := "muted", "No"
	if mounted > 0 {
		tone, val = "ok", "Yes"
	}
	d.Status = append(d.Status, chip{Label: "Mounted", Value: val, Tone: tone})
}

// --- builders ------------------------------------------------------------

func base(kind string, m metav1.ObjectMeta) *resourceDetail {
	ok, on := ownerOf(m)
	return &resourceDetail{
		Kind: kind, Name: m.Name, Namespace: m.Namespace,
		Age: age(m.CreationTimestamp), OwnerKind: ok, OwnerName: on, Labels: m.Labels,
	}
}

func ownerOf(m metav1.ObjectMeta) (string, string) {
	for _, o := range m.OwnerReferences {
		if o.Controller != nil && *o.Controller {
			return o.Kind, o.Name
		}
	}
	return "", ""
}

func imagesOf(spec corev1.PodSpec) []kv {
	out := []kv{}
	for _, cn := range spec.InitContainers {
		out = append(out, kv{Label: cn.Name + " (init)", Value: cn.Image})
	}
	for _, cn := range spec.Containers {
		out = append(out, kv{Label: cn.Name, Value: cn.Image})
	}
	return out
}

func selectorOf(sel *metav1.LabelSelector) map[string]string {
	if sel == nil {
		return nil
	}
	return sel.MatchLabels
}

func replicaChip(label string, ready, total int32) chip {
	tone := "warn"
	if total == 0 {
		tone = "muted"
	} else if ready >= total {
		tone = "ok"
	}
	return chip{Label: label, Value: fmt.Sprintf("%d/%d", ready, total), Tone: tone}
}

func countChip(label string, n int32, tone string) chip {
	return chip{Label: label, Value: fmt.Sprintf("%d", n), Tone: tone}
}

func deploymentDetail(o *appsv1.Deployment) *resourceDetail {
	desired := int32(1)
	if o.Spec.Replicas != nil {
		desired = *o.Spec.Replicas
	}
	d := base("Deployment", o.ObjectMeta)
	d.Replicas = &desired
	d.Status = []chip{
		replicaChip("Ready", o.Status.ReadyReplicas, desired),
		countChip("Up-to-date", o.Status.UpdatedReplicas, "muted"),
		countChip("Available", o.Status.AvailableReplicas, "muted"),
	}
	d.Selector = selectorOf(o.Spec.Selector)
	d.Images = imagesOf(o.Spec.Template.Spec)
	strat := section{Title: "Strategy", Items: []kv{{Label: "Type", Value: string(o.Spec.Strategy.Type)}}}
	if ru := o.Spec.Strategy.RollingUpdate; ru != nil {
		if ru.MaxSurge != nil {
			strat.Items = append(strat.Items, kv{Label: "Max surge", Value: ru.MaxSurge.String()})
		}
		if ru.MaxUnavailable != nil {
			strat.Items = append(strat.Items, kv{Label: "Max unavailable", Value: ru.MaxUnavailable.String()})
		}
	}
	d.Sections = []section{strat}
	d.Conditions = workloadConditions(o.Status.Conditions)
	return d
}

func workloadConditions(conds []appsv1.DeploymentCondition) []chip {
	out := []chip{}
	for _, c := range conds {
		tone := "muted"
		switch c.Status {
		case corev1.ConditionTrue:
			tone = "ok"
		case corev1.ConditionFalse:
			tone = "err"
		}
		out = append(out, chip{Label: string(c.Type), Value: string(c.Status), Tone: tone})
	}
	return out
}

func replicaSetDetail(o *appsv1.ReplicaSet) *resourceDetail {
	desired := int32(1)
	if o.Spec.Replicas != nil {
		desired = *o.Spec.Replicas
	}
	d := base("ReplicaSet", o.ObjectMeta)
	d.Replicas = &desired
	d.Status = []chip{
		replicaChip("Ready", o.Status.ReadyReplicas, desired),
		countChip("Current", o.Status.Replicas, "muted"),
		countChip("Available", o.Status.AvailableReplicas, "muted"),
	}
	d.Selector = selectorOf(o.Spec.Selector)
	d.Images = imagesOf(o.Spec.Template.Spec)
	return d
}

func statefulSetDetail(o *appsv1.StatefulSet) *resourceDetail {
	desired := int32(1)
	if o.Spec.Replicas != nil {
		desired = *o.Spec.Replicas
	}
	d := base("StatefulSet", o.ObjectMeta)
	d.Replicas = &desired
	d.Status = []chip{
		replicaChip("Ready", o.Status.ReadyReplicas, desired),
		countChip("Current", o.Status.CurrentReplicas, "muted"),
		countChip("Updated", o.Status.UpdatedReplicas, "muted"),
	}
	d.Selector = selectorOf(o.Spec.Selector)
	d.Images = imagesOf(o.Spec.Template.Spec)
	d.Sections = []section{{Title: "Configuration", Items: []kv{
		{Label: "Service", Value: o.Spec.ServiceName},
		{Label: "Update strategy", Value: string(o.Spec.UpdateStrategy.Type)},
		{Label: "Pod management", Value: string(o.Spec.PodManagementPolicy)},
	}}}
	return d
}

func daemonSetDetail(o *appsv1.DaemonSet) *resourceDetail {
	d := base("DaemonSet", o.ObjectMeta)
	d.Status = []chip{
		replicaChip("Ready", o.Status.NumberReady, o.Status.DesiredNumberScheduled),
		countChip("Current", o.Status.CurrentNumberScheduled, "muted"),
		countChip("Up-to-date", o.Status.UpdatedNumberScheduled, "muted"),
		countChip("Misscheduled", o.Status.NumberMisscheduled, boolTone(o.Status.NumberMisscheduled == 0)),
	}
	d.Selector = selectorOf(o.Spec.Selector)
	d.Images = imagesOf(o.Spec.Template.Spec)
	d.Sections = []section{{Title: "Configuration", Items: []kv{
		{Label: "Update strategy", Value: string(o.Spec.UpdateStrategy.Type)},
	}}}
	return d
}

func jobDetail(o *batchv1.Job) *resourceDetail {
	d := base("Job", o.ObjectMeta)
	completions := int32(1)
	if o.Spec.Completions != nil {
		completions = *o.Spec.Completions
	}
	d.Status = []chip{
		replicaChip("Completions", o.Status.Succeeded, completions),
		countChip("Active pods", o.Status.Active, "warn"),
		countChip("Failed", o.Status.Failed, boolTone(o.Status.Failed == 0)),
	}
	d.Selector = selectorOf(o.Spec.Selector)
	d.Images = imagesOf(o.Spec.Template.Spec)
	return d
}

func cronJobDetail(o *batchv1.CronJob) *resourceDetail {
	d := base("CronJob", o.ObjectMeta)
	suspended := o.Spec.Suspend != nil && *o.Spec.Suspend
	suspendTone := "ok"
	suspendVal := "Active"
	if suspended {
		suspendTone, suspendVal = "warn", "Suspended"
	}
	d.Status = []chip{
		{Label: "State", Value: suspendVal, Tone: suspendTone},
		countChip("Active jobs", int32(len(o.Status.Active)), "muted"), //nolint:gosec // active job count, always tiny
	}
	last := "—"
	if o.Status.LastScheduleTime != nil {
		last = age(*o.Status.LastScheduleTime)
	}
	d.Sections = []section{{Title: "Scheduling", Items: []kv{
		{Label: "Schedule", Value: o.Spec.Schedule},
		{Label: "Concurrency", Value: string(o.Spec.ConcurrencyPolicy)},
		{Label: "Last run", Value: last},
	}}}
	d.Images = imagesOf(o.Spec.JobTemplate.Spec.Template.Spec)
	return d
}

func serviceDetail(o *corev1.Service) *resourceDetail {
	d := base("Service", o.ObjectMeta)
	external := ""
	if len(o.Status.LoadBalancer.Ingress) > 0 {
		lb := o.Status.LoadBalancer.Ingress[0]
		if external = lb.Hostname; external == "" {
			external = lb.IP
		}
	}
	if external == "" && len(o.Spec.ExternalIPs) > 0 {
		external = o.Spec.ExternalIPs[0]
	}
	d.Status = []chip{
		{Label: "Type", Value: string(o.Spec.Type), Tone: "muted"},
		{Label: "Cluster IP", Value: o.Spec.ClusterIP, Tone: "muted"},
	}
	d.Selector = o.Spec.Selector // matches the backing pods

	for _, p := range o.Spec.Ports {
		pv := portView{Name: p.Name, Port: fmt.Sprintf("%d → %s", p.Port, p.TargetPort.String()), Protocol: string(p.Protocol)}
		if p.NodePort > 0 {
			pv.Extra = fmt.Sprintf("node %d", p.NodePort)
		}
		d.Ports = append(d.Ports, pv)
	}
	cfg := section{Title: "Configuration", Items: []kv{{Label: "Session affinity", Value: string(o.Spec.SessionAffinity)}}}
	if external != "" {
		cfg.Items = append(cfg.Items, kv{Label: "External", Value: external})
	}
	d.Sections = append(d.Sections, cfg)
	return d
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func ingressDetail(o *networkingv1.Ingress) *resourceDetail {
	d := base("Ingress", o.ObjectMeta)
	d.Status = ingressStatusChips(o)

	sections, backends := ingressRuleSections(o)
	d.Sections = append(d.Sections, sections...)
	if db := o.Spec.DefaultBackend; db != nil && db.Service != nil {
		backends = appendUnique(backends, db.Service.Name)
	}
	for _, svc := range backends {
		d.Refs = append(d.Refs, detailRef{Group: "Backends", Kind: "service", Namespace: o.Namespace, Name: svc})
	}

	if tls := ingressTLSSection(o); tls != nil {
		d.Sections = append(d.Sections, *tls)
	}
	return d
}

func ingressStatusChips(o *networkingv1.Ingress) []chip {
	class := ""
	if o.Spec.IngressClassName != nil {
		class = *o.Spec.IngressClassName
	}
	addr := ""
	if len(o.Status.LoadBalancer.Ingress) > 0 {
		lb := o.Status.LoadBalancer.Ingress[0]
		addr = lb.Hostname
		if addr == "" {
			addr = lb.IP
		}
	}
	return []chip{
		{Label: "Class", Value: orDash(class), Tone: "muted"},
		{Label: "Address", Value: orDash(addr), Tone: "muted"},
	}
}

// ingressRuleSections builds one section per host rule and collects every
// backend Service name referenced by a path, deduplicated.
func ingressRuleSections(o *networkingv1.Ingress) (sections []section, backends []string) {
	for _, r := range o.Spec.Rules {
		host := orDash(r.Host)
		if r.Host == "" {
			host = "*"
		}
		items, ruleBackends := ingressRuleItems(r)
		for _, b := range ruleBackends {
			backends = appendUnique(backends, b)
		}
		sections = append(sections, section{Title: host, Items: items})
	}
	return sections, backends
}

// ingressRuleItems lists one item per path in a rule, plus the backend
// Service names those paths reference.
func ingressRuleItems(r networkingv1.IngressRule) (items []kv, backends []string) {
	if r.HTTP == nil {
		return items, backends
	}
	for _, p := range r.HTTP.Paths {
		svc, port := ingressPathBackend(p)
		if svc != "" {
			backends = appendUnique(backends, svc)
		}
		path := p.Path
		if path == "" {
			path = "/"
		}
		items = append(items, kv{Label: path, Value: fmt.Sprintf("%s:%s", svc, port)})
	}
	return items, backends
}

// ingressPathBackend reads the backend Service name/port a path routes to.
func ingressPathBackend(p networkingv1.HTTPIngressPath) (svc, port string) {
	if p.Backend.Service == nil {
		return "", ""
	}
	svc = p.Backend.Service.Name
	if p.Backend.Service.Port.Name != "" {
		port = p.Backend.Service.Port.Name
	} else {
		port = fmt.Sprintf("%d", p.Backend.Service.Port.Number)
	}
	return svc, port
}

func ingressTLSSection(o *networkingv1.Ingress) *section {
	if len(o.Spec.TLS) == 0 {
		return nil
	}
	hosts := []string{}
	for _, t := range o.Spec.TLS {
		hosts = append(hosts, t.Hosts...)
	}
	return &section{Title: "TLS", Items: []kv{{Label: "Hosts", Value: strings.Join(hosts, ", ")}}}
}

// appendUnique appends v to s only if it isn't already present.
func appendUnique(s []string, v string) []string {
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}

func configMapDetail(o *corev1.ConfigMap) *resourceDetail {
	d := base("ConfigMap", o.ObjectMeta)
	d.Status = []chip{countChip("Keys", int32(len(o.Data)+len(o.BinaryData)), "muted")} //nolint:gosec // key count, always tiny

	keys := make([]string, 0, len(o.Data))
	for k := range o.Data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		body := o.Data[k]
		if len(body) > 8000 {
			body = body[:8000] + "\n… (truncated)"
		}
		d.Blocks = append(d.Blocks, detailBlock{Title: k, Body: body})
	}
	if len(o.BinaryData) > 0 {
		bkeys := make([]string, 0, len(o.BinaryData))
		for k := range o.BinaryData {
			bkeys = append(bkeys, k)
		}
		sort.Strings(bkeys)
		items := []kv{}
		for _, k := range bkeys {
			items = append(items, kv{Label: k, Value: fmt.Sprintf("%d bytes (binary)", len(o.BinaryData[k]))})
		}
		d.Sections = append(d.Sections, section{Title: "Binary data", Items: items})
	}
	return d
}

func namespaceDetail(o *corev1.Namespace) *resourceDetail {
	d := base("Namespace", o.ObjectMeta)
	phase := string(o.Status.Phase)
	tone := "ok"
	if phase == string(corev1.NamespaceTerminating) {
		tone = "warn"
	}
	d.Status = []chip{{Label: "Phase", Value: orDash(phase), Tone: tone}}
	if len(o.Spec.Finalizers) > 0 {
		fins := make([]string, 0, len(o.Spec.Finalizers))
		for _, f := range o.Spec.Finalizers {
			fins = append(fins, string(f))
		}
		d.Sections = append(d.Sections, section{Title: "Finalizers", Items: []kv{{Label: "Active finalizers", Value: strings.Join(fins, ", ")}}})
	}
	return d
}

func secretDetail(o *corev1.Secret) *resourceDetail {
	d := base("Secret", o.ObjectMeta)
	d.Status = []chip{
		{Label: "Type", Value: orDash(string(o.Type)), Tone: "muted"},
		countChip("Keys", int32(len(o.Data)), "muted"), //nolint:gosec // key count, always tiny
	}
	keys := make([]string, 0, len(o.Data))
	for k := range o.Data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	// Each value is masked in the UI until explicitly revealed; the body carries
	// the decoded value (or a note when it is binary), never base64.
	for _, k := range keys {
		raw := o.Data[k]
		var body string
		if isPrintable(raw) {
			body = string(raw)
			if len(body) > 8000 {
				body = body[:8000] + "\n… (truncated)"
			}
		} else {
			body = fmt.Sprintf("<binary — %d bytes>", len(raw))
		}
		d.Blocks = append(d.Blocks, detailBlock{Title: fmt.Sprintf("%s (%d bytes)", k, len(raw)), Body: body, Masked: true})
	}
	return d
}

// isPrintable reports whether b is valid UTF-8 without control characters
// (other than the usual whitespace), so it can be shown as text.
func isPrintable(b []byte) bool {
	if !utf8.Valid(b) {
		return false
	}
	for _, r := range string(b) {
		if r < 0x20 && r != '\n' && r != '\r' && r != '\t' {
			return false
		}
	}
	return true
}

func volumePhaseTone(phase string) string {
	switch phase {
	case "Bound", "Available":
		return "ok"
	case "Pending":
		return "warn"
	case "Lost", "Failed":
		return "err"
	case "Released":
		return "warn"
	}
	return "muted"
}

func pvcDetail(o *corev1.PersistentVolumeClaim) *resourceDetail {
	d := base("PersistentVolumeClaim", o.ObjectMeta)
	v := kube.ToPVCView(o)
	d.Status = []chip{
		{Label: "Status", Value: orDash(v.Status), Tone: volumePhaseTone(v.Status)},
		{Label: "Capacity", Value: orDash(v.Capacity), Tone: "muted"},
		{Label: "Modes", Value: orDash(v.AccessModes), Tone: "muted"},
	}
	mode := ""
	if o.Spec.VolumeMode != nil {
		mode = string(*o.Spec.VolumeMode)
	}
	d.Sections = []section{{Title: "Configuration", Items: []kv{
		{Label: "StorageClass", Value: orDash(v.StorageClass)},
		{Label: "Volume mode", Value: orDash(mode)},
		{Label: "Volume", Value: orDash(v.Volume)},
	}}}
	if v.Volume != "" {
		d.Refs = append(d.Refs, detailRef{Group: "Volume", Kind: "pv", Namespace: "", Name: v.Volume})
	}
	return d
}

func pvDetail(o *corev1.PersistentVolume) *resourceDetail {
	d := base("PersistentVolume", o.ObjectMeta)
	v := kube.ToPVView(o)
	d.Status = []chip{
		{Label: "Status", Value: orDash(v.Status), Tone: volumePhaseTone(v.Status)},
		{Label: "Capacity", Value: orDash(v.Capacity), Tone: "muted"},
		{Label: "Modes", Value: orDash(v.AccessModes), Tone: "muted"},
	}
	d.Sections = []section{{Title: "Configuration", Items: []kv{
		{Label: "StorageClass", Value: orDash(v.StorageClass)},
		{Label: "Reclaim policy", Value: orDash(v.Reclaim)},
		{Label: "Source", Value: pvSource(o.Spec.PersistentVolumeSource)},
	}}}
	if o.Spec.ClaimRef != nil {
		d.Refs = append(d.Refs, detailRef{Group: "Claim", Kind: "pvc", Namespace: o.Spec.ClaimRef.Namespace, Name: o.Spec.ClaimRef.Name})
	}
	return d
}

// pvSource names the concrete volume backend (CSI driver, hostPath, NFS, …).
func pvSource(s corev1.PersistentVolumeSource) string {
	switch {
	case s.CSI != nil:
		return "CSI: " + s.CSI.Driver
	case s.HostPath != nil:
		return "HostPath: " + s.HostPath.Path
	case s.NFS != nil:
		return "NFS: " + s.NFS.Server + s.NFS.Path
	case s.Local != nil:
		return "Local: " + s.Local.Path
	case s.AWSElasticBlockStore != nil:
		return "AWS EBS: " + s.AWSElasticBlockStore.VolumeID
	}
	return "—"
}

func storageClassDetail(o *storagev1.StorageClass) *resourceDetail {
	d := base("StorageClass", o.ObjectMeta)
	v := kube.ToStorageClassView(o)
	defTone, defVal := "muted", "No"
	if v.Default {
		defTone, defVal = "ok", "Yes"
	}
	d.Status = []chip{
		{Label: "Provisioner", Value: orDash(v.Provisioner), Tone: "muted"},
		{Label: "Default", Value: defVal, Tone: defTone},
	}
	expansion := "No"
	if o.AllowVolumeExpansion != nil && *o.AllowVolumeExpansion {
		expansion = "Yes"
	}
	d.Sections = []section{{Title: "Configuration", Items: []kv{
		{Label: "Reclaim policy", Value: orDash(v.Reclaim)},
		{Label: "Binding mode", Value: orDash(v.Binding)},
		{Label: "Volume expansion", Value: expansion},
	}}}
	if len(o.Parameters) > 0 {
		keys := make([]string, 0, len(o.Parameters))
		for k := range o.Parameters {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		items := make([]kv, 0, len(keys))
		for _, k := range keys {
			items = append(items, kv{Label: k, Value: o.Parameters[k]})
		}
		d.Sections = append(d.Sections, section{Title: "Parameters", Items: items})
	}
	return d
}

func hpaDetail(o *autoscalingv2.HorizontalPodAutoscaler) *resourceDetail {
	d := base("HorizontalPodAutoscaler", o.ObjectMeta)
	min := int32(1)
	if o.Spec.MinReplicas != nil {
		min = *o.Spec.MinReplicas
	}
	d.Status = []chip{
		replicaChip("Replicas", o.Status.CurrentReplicas, o.Status.DesiredReplicas),
		countChip("Min", min, "muted"),
		countChip("Max", o.Spec.MaxReplicas, "muted"),
	}
	// Metrics: pair each configured target with its current reading. Status
	// metrics aren't guaranteed to match spec order or count, so match by the
	// metric's identity (type + name) rather than by index.
	current := map[string]string{}
	for _, cm := range o.Status.CurrentMetrics {
		k, cur := hpaMetricStatus(cm)
		current[k] = cur
	}
	items := []kv{}
	for _, m := range o.Spec.Metrics {
		k, label, target := hpaMetricSpec(m)
		cur := current[k]
		if cur == "" {
			cur = "—"
		}
		items = append(items, kv{Label: label, Value: fmt.Sprintf("%s / %s", cur, target)})
	}
	if len(items) > 0 {
		d.Sections = append(d.Sections, section{Title: "Metrics (current / target)", Items: items})
	}
	ref := o.Spec.ScaleTargetRef
	slug := strings.ToLower(ref.Kind)
	if _, ok := manifestSlugToResource[slug]; ok {
		d.Refs = append(d.Refs, detailRef{Group: "Target", Kind: slug, Namespace: o.Namespace, Name: ref.Name})
	} else {
		d.Sections = append(d.Sections, section{Title: "Target", Items: []kv{{Label: ref.Kind, Value: ref.Name}}})
	}
	for _, c := range o.Status.Conditions {
		tone := "muted"
		switch c.Status {
		case corev1.ConditionTrue:
			tone = "ok"
		case corev1.ConditionFalse:
			tone = "warn"
		}
		d.Conditions = append(d.Conditions, chip{Label: string(c.Type), Value: string(c.Status), Tone: tone})
	}
	return d
}

// hpaMetricValue renders a target or current reading, checking every value kind
// (utilization %, average value, plain value) — HPAs populate whichever applies.
func hpaMetricTarget(t autoscalingv2.MetricTarget) string {
	switch {
	case t.AverageUtilization != nil:
		return fmt.Sprintf("%d%%", *t.AverageUtilization)
	case t.AverageValue != nil:
		return t.AverageValue.String()
	case t.Value != nil:
		return t.Value.String()
	}
	return "—"
}

func hpaMetricCurrent(v autoscalingv2.MetricValueStatus) string {
	switch {
	case v.AverageUtilization != nil:
		return fmt.Sprintf("%d%%", *v.AverageUtilization)
	case v.AverageValue != nil:
		return v.AverageValue.String()
	case v.Value != nil:
		return v.Value.String()
	}
	return "—"
}

// hpaMetricSpec returns a metric's identity key, display label and configured
// target. The key matches the corresponding status metric (see hpaMetricStatus).
func hpaMetricSpec(m autoscalingv2.MetricSpec) (key, label, target string) {
	switch m.Type {
	case autoscalingv2.ResourceMetricSourceType:
		name := string(m.Resource.Name)
		return "resource/" + name, name, hpaMetricTarget(m.Resource.Target)
	case autoscalingv2.ContainerResourceMetricSourceType:
		name := string(m.ContainerResource.Name)
		return "container/" + m.ContainerResource.Container + "/" + name,
			fmt.Sprintf("%s (%s)", name, m.ContainerResource.Container), hpaMetricTarget(m.ContainerResource.Target)
	case autoscalingv2.PodsMetricSourceType:
		return "pods/" + m.Pods.Metric.Name, m.Pods.Metric.Name, hpaMetricTarget(m.Pods.Target)
	case autoscalingv2.ObjectMetricSourceType:
		return "object/" + m.Object.Metric.Name, m.Object.Metric.Name, hpaMetricTarget(m.Object.Target)
	case autoscalingv2.ExternalMetricSourceType:
		return "external/" + m.External.Metric.Name, m.External.Metric.Name, hpaMetricTarget(m.External.Target)
	}
	return string(m.Type), string(m.Type), "—"
}

// hpaMetricStatus returns a metric's identity key and its current reading.
func hpaMetricStatus(m autoscalingv2.MetricStatus) (key, current string) {
	switch m.Type {
	case autoscalingv2.ResourceMetricSourceType:
		return "resource/" + string(m.Resource.Name), hpaMetricCurrent(m.Resource.Current)
	case autoscalingv2.ContainerResourceMetricSourceType:
		return "container/" + m.ContainerResource.Container + "/" + string(m.ContainerResource.Name), hpaMetricCurrent(m.ContainerResource.Current)
	case autoscalingv2.PodsMetricSourceType:
		return "pods/" + m.Pods.Metric.Name, hpaMetricCurrent(m.Pods.Current)
	case autoscalingv2.ObjectMetricSourceType:
		return "object/" + m.Object.Metric.Name, hpaMetricCurrent(m.Object.Current)
	case autoscalingv2.ExternalMetricSourceType:
		return "external/" + m.External.Metric.Name, hpaMetricCurrent(m.External.Current)
	}
	return string(m.Type), "—"
}

func endpointSliceDetail(o *discoveryv1.EndpointSlice) *resourceDetail {
	d := base("EndpointSlice", o.ObjectMeta)
	v := kube.ToEndpointSliceView(o)
	d.Status = []chip{
		{Label: "Type", Value: orDash(v.AddressType), Tone: "muted"},
		replicaChip("Ready", int32(v.Ready), int32(v.Total)), //nolint:gosec // endpoint counts, always tiny
	}
	if svc := o.Labels["kubernetes.io/service-name"]; svc != "" {
		d.Refs = append(d.Refs, detailRef{Group: "Service", Kind: "service", Namespace: o.Namespace, Name: svc})
	}
	d.Ports = endpointSlicePorts(o)

	// Each endpoint backed by a Pod becomes a clickable ref (with the address as a
	// note); endpoints without a Pod target fall back to a plain listing.
	group := fmt.Sprintf("Endpoints (%d)", len(o.Endpoints))
	refs, orphans := endpointSliceEndpoints(o, group)
	d.Refs = append(d.Refs, refs...)
	if len(orphans) > 0 {
		d.Sections = append(d.Sections, section{Title: group, Items: orphans})
	}
	return d
}

// endpointSlicePorts dedupes the slice's declared ports.
func endpointSlicePorts(o *discoveryv1.EndpointSlice) []portView {
	seen := map[string]bool{}
	ports := []portView{}
	for _, p := range o.Ports {
		pv := portView{}
		if p.Name != nil {
			pv.Name = *p.Name
		}
		if p.Port != nil {
			pv.Port = fmt.Sprintf("%d", *p.Port)
		}
		if p.Protocol != nil {
			pv.Protocol = string(*p.Protocol)
		}
		key := pv.Name + "|" + pv.Port + "|" + pv.Protocol
		if pv.Port != "" && !seen[key] {
			seen[key] = true
			ports = append(ports, pv)
		}
	}
	return ports
}

// endpointSliceEndpoints splits endpoints into Pod-backed clickable refs and
// plain (address, state) listings for anything else.
func endpointSliceEndpoints(o *discoveryv1.EndpointSlice, group string) (refs []detailRef, orphans []kv) {
	for _, e := range o.Endpoints {
		addr := strings.Join(e.Addresses, ", ")
		notReady := e.Conditions.Ready != nil && !*e.Conditions.Ready
		note := addr
		if notReady {
			note += " · not ready"
		}
		if e.TargetRef != nil && e.TargetRef.Kind == "Pod" {
			ns := e.TargetRef.Namespace
			if ns == "" {
				ns = o.Namespace
			}
			refs = append(refs, detailRef{Group: group, Kind: "pod", Namespace: ns, Name: e.TargetRef.Name, Note: note})
			continue
		}
		state := "ready"
		if notReady {
			state = "not ready"
		}
		orphans = append(orphans, kv{Label: addr, Value: state})
	}
	return refs, orphans
}

// npPeers / npPorts summarize a NetworkPolicy rule's peers and ports for display.
func npPeers(peers []networkingv1.NetworkPolicyPeer) string {
	if len(peers) == 0 {
		return "any source/destination"
	}
	parts := make([]string, 0, len(peers))
	for _, p := range peers {
		switch {
		case p.IPBlock != nil:
			parts = append(parts, "ipBlock "+p.IPBlock.CIDR)
		case p.NamespaceSelector != nil && p.PodSelector != nil:
			parts = append(parts, "ns["+kube.FormatSelector(*p.NamespaceSelector)+"] pods["+kube.FormatSelector(*p.PodSelector)+"]")
		case p.NamespaceSelector != nil:
			parts = append(parts, "ns["+kube.FormatSelector(*p.NamespaceSelector)+"]")
		case p.PodSelector != nil:
			parts = append(parts, "pods["+kube.FormatSelector(*p.PodSelector)+"]")
		}
	}
	return strings.Join(parts, "; ")
}

func npPorts(ports []networkingv1.NetworkPolicyPort) string {
	if len(ports) == 0 {
		return "all ports"
	}
	parts := make([]string, 0, len(ports))
	for _, p := range ports {
		s := ""
		if p.Port != nil {
			s = p.Port.String()
		}
		if p.Protocol != nil {
			s += "/" + string(*p.Protocol)
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, ", ")
}

func networkPolicyDetail(o *networkingv1.NetworkPolicy) *resourceDetail {
	d := base("NetworkPolicy", o.ObjectMeta)
	has := func(t networkingv1.PolicyType) string {
		for _, pt := range o.Spec.PolicyTypes {
			if pt == t {
				return "yes"
			}
		}
		return "no"
	}
	d.Status = []chip{
		{Label: "Ingress", Value: has(networkingv1.PolicyTypeIngress), Tone: boolTone(has(networkingv1.PolicyTypeIngress) == "no")},
		{Label: "Egress", Value: has(networkingv1.PolicyTypeEgress), Tone: boolTone(has(networkingv1.PolicyTypeEgress) == "no")},
	}
	d.Selector = o.Spec.PodSelector.MatchLabels // pods this policy applies to
	if len(o.Spec.Ingress) > 0 {
		items := make([]kv, 0, len(o.Spec.Ingress))
		for i, r := range o.Spec.Ingress {
			items = append(items, kv{Label: fmt.Sprintf("Rule %d", i+1), Value: "from " + npPeers(r.From) + " → " + npPorts(r.Ports)})
		}
		d.Sections = append(d.Sections, section{Title: "Ingress (inbound)", Items: items})
	}
	if len(o.Spec.Egress) > 0 {
		items := make([]kv, 0, len(o.Spec.Egress))
		for i, r := range o.Spec.Egress {
			items = append(items, kv{Label: fmt.Sprintf("Rule %d", i+1), Value: "to " + npPeers(r.To) + " → " + npPorts(r.Ports)})
		}
		d.Sections = append(d.Sections, section{Title: "Egress (outbound)", Items: items})
	}
	return d
}

func ingressClassDetail(o *networkingv1.IngressClass) *resourceDetail {
	d := base("IngressClass", o.ObjectMeta)
	def := o.Annotations["ingressclass.kubernetes.io/is-default-class"] == "true"
	defTone, defVal := "muted", "No"
	if def {
		defTone, defVal = "ok", "Yes"
	}
	d.Status = []chip{
		{Label: "Controller", Value: orDash(o.Spec.Controller), Tone: "muted"},
		{Label: "Default", Value: defVal, Tone: defTone},
	}
	if p := o.Spec.Parameters; p != nil {
		items := []kv{{Label: "Kind", Value: p.Kind}, {Label: "Name", Value: p.Name}}
		if p.APIGroup != nil {
			items = append(items, kv{Label: "API group", Value: *p.APIGroup})
		}
		if p.Scope != nil {
			items = append(items, kv{Label: "Scope", Value: *p.Scope})
		}
		d.Sections = append(d.Sections, section{Title: "Parameters", Items: items})
	}
	return d
}

// --- RBAC & governance builders --------------------------------------------

func serviceAccountDetail(o *corev1.ServiceAccount) *resourceDetail {
	d := base("ServiceAccount", o.ObjectMeta)
	automount := "inherits from pod"
	if o.AutomountServiceAccountToken != nil {
		automount = "no"
		if *o.AutomountServiceAccountToken {
			automount = "yes"
		}
	}
	d.Status = []chip{
		countChip("Secrets", int32(len(o.Secrets)), "muted"), //nolint:gosec // secret ref count, always tiny
		{Label: "Automount token", Value: automount, Tone: "muted"},
	}
	for _, s := range o.Secrets {
		d.Refs = append(d.Refs, detailRef{Group: "Secrets", Kind: "secret", Namespace: o.Namespace, Name: s.Name})
	}
	if len(o.ImagePullSecrets) > 0 {
		items := make([]kv, 0, len(o.ImagePullSecrets))
		for _, s := range o.ImagePullSecrets {
			items = append(items, kv{Label: s.Name, Value: "pull secret"})
		}
		d.Sections = append(d.Sections, section{Title: "Image pull secrets", Items: items})
	}
	return d
}

// formatRules renders RBAC PolicyRules as readable "verbs on resources" lines.
func formatRules(rules []rbacv1.PolicyRule) []kv {
	items := make([]kv, 0, len(rules))
	for _, r := range rules {
		verbs := strings.Join(r.Verbs, ",")
		var target string
		switch {
		case len(r.Resources) > 0:
			groups := strings.Join(r.APIGroups, ",")
			if groups == "" {
				groups = "core"
			}
			target = groups + "/" + strings.Join(r.Resources, ",")
		case len(r.NonResourceURLs) > 0:
			target = strings.Join(r.NonResourceURLs, ",")
		}
		items = append(items, kv{Label: verbs, Value: target})
	}
	return items
}

func roleDetail(o *rbacv1.Role) *resourceDetail {
	d := base("Role", o.ObjectMeta)
	d.Status = []chip{countChip("Rules", int32(len(o.Rules)), "muted")} //nolint:gosec // rule count, always tiny
	if len(o.Rules) > 0 {
		d.Sections = append(d.Sections, section{Title: "Rules (verbs → resources)", Items: formatRules(o.Rules)})
	}
	return d
}

func clusterRoleDetail(o *rbacv1.ClusterRole) *resourceDetail {
	d := base("ClusterRole", o.ObjectMeta)
	d.Status = []chip{countChip("Rules", int32(len(o.Rules)), "muted")} //nolint:gosec // rule count, always tiny
	if len(o.Rules) > 0 {
		d.Sections = append(d.Sections, section{Title: "Rules (verbs → resources)", Items: formatRules(o.Rules)})
	}
	return d
}

// bindingDetail is shared by Role/ClusterRoleBinding: a link to the referenced
// role and the list of subjects (ServiceAccounts link; users/groups are text).
func bindingDetail(kind, ns string, m metav1.ObjectMeta, roleRef rbacv1.RoleRef, subjects []rbacv1.Subject) *resourceDetail {
	d := base(kind, m)
	d.Status = []chip{
		{Label: "Role", Value: roleRef.Kind + "/" + roleRef.Name, Tone: "muted"},
		countChip("Subjects", int32(len(subjects)), "muted"), //nolint:gosec // subject count, always tiny
	}
	// Link to the referenced role (Role is namespaced; ClusterRole is not).
	roleSlug := ""
	roleNS := ""
	switch roleRef.Kind {
	case "Role":
		roleSlug, roleNS = "role", ns
	case "ClusterRole":
		roleSlug = "clusterrole"
	}
	if roleSlug != "" {
		d.Refs = append(d.Refs, detailRef{Group: "Role", Kind: roleSlug, Namespace: roleNS, Name: roleRef.Name})
	}
	subs := make([]kv, 0, len(subjects))
	for _, s := range subjects {
		label := s.Kind
		val := s.Name
		if s.Namespace != "" {
			val = s.Namespace + "/" + s.Name
		}
		subs = append(subs, kv{Label: label, Value: val})
		if s.Kind == "ServiceAccount" {
			saNS := s.Namespace
			if saNS == "" {
				saNS = ns
			}
			d.Refs = append(d.Refs, detailRef{Group: "ServiceAccounts", Kind: "serviceaccount", Namespace: saNS, Name: s.Name})
		}
	}
	if len(subs) > 0 {
		d.Sections = append(d.Sections, section{Title: "Subjects", Items: subs})
	}
	return d
}

func roleBindingDetail(o *rbacv1.RoleBinding) *resourceDetail {
	return bindingDetail("RoleBinding", o.Namespace, o.ObjectMeta, o.RoleRef, o.Subjects)
}

func clusterRoleBindingDetail(o *rbacv1.ClusterRoleBinding) *resourceDetail {
	return bindingDetail("ClusterRoleBinding", "", o.ObjectMeta, o.RoleRef, o.Subjects)
}

func resourceQuotaDetail(o *corev1.ResourceQuota) *resourceDetail {
	d := base("ResourceQuota", o.ObjectMeta)
	// Pair each hard limit with its current usage.
	names := make([]string, 0, len(o.Status.Hard))
	for k := range o.Status.Hard {
		names = append(names, string(k))
	}
	sort.Strings(names)
	items := make([]kv, 0, len(names))
	for _, n := range names {
		hard := o.Status.Hard[corev1.ResourceName(n)]
		used := o.Status.Used[corev1.ResourceName(n)]
		items = append(items, kv{Label: n, Value: used.String() + " / " + hard.String()})
	}
	if len(items) > 0 {
		d.Sections = append(d.Sections, section{Title: "Usage / limit", Items: items})
	}
	if len(o.Spec.Scopes) > 0 {
		scopes := make([]string, 0, len(o.Spec.Scopes))
		for _, s := range o.Spec.Scopes {
			scopes = append(scopes, string(s))
		}
		d.Status = []chip{{Label: "Scopes", Value: strings.Join(scopes, ", "), Tone: "muted"}}
	}
	return d
}

func limitRangeDetail(o *corev1.LimitRange) *resourceDetail {
	d := base("LimitRange", o.ObjectMeta)
	for _, l := range o.Spec.Limits {
		items := []kv{}
		add := func(label string, m corev1.ResourceList) {
			for name, q := range m {
				items = append(items, kv{Label: label + " " + string(name), Value: q.String()})
			}
		}
		add("default", l.Default)
		add("defaultRequest", l.DefaultRequest)
		add("min", l.Min)
		add("max", l.Max)
		sort.Slice(items, func(i, j int) bool { return items[i].Label < items[j].Label })
		if len(items) > 0 {
			d.Sections = append(d.Sections, section{Title: string(l.Type), Items: items})
		}
	}
	return d
}

func pdbDetail(o *policyv1.PodDisruptionBudget) *resourceDetail {
	d := base("PodDisruptionBudget", o.ObjectMeta)
	crit := "—"
	if o.Spec.MinAvailable != nil {
		crit = "minAvailable " + o.Spec.MinAvailable.String()
	} else if o.Spec.MaxUnavailable != nil {
		crit = "maxUnavailable " + o.Spec.MaxUnavailable.String()
	}
	d.Status = []chip{
		{Label: "Criteria", Value: crit, Tone: "muted"},
		replicaChip("Healthy", o.Status.CurrentHealthy, o.Status.DesiredHealthy),
		countChip("Disruptions allowed", o.Status.DisruptionsAllowed, boolTone(o.Status.DisruptionsAllowed > 0)),
	}
	d.Selector = selectorOf(o.Spec.Selector)
	d.Sections = append(d.Sections, section{Title: "Status", Items: []kv{
		{Label: "Expected pods", Value: fmt.Sprintf("%d", o.Status.ExpectedPods)},
		{Label: "Current healthy", Value: fmt.Sprintf("%d", o.Status.CurrentHealthy)},
		{Label: "Desired healthy", Value: fmt.Sprintf("%d", o.Status.DesiredHealthy)},
	}})
	return d
}

func priorityClassDetail(o *schedulingv1.PriorityClass) *resourceDetail {
	d := base("PriorityClass", o.ObjectMeta)
	preempt := string(corev1.PreemptLowerPriority)
	if o.PreemptionPolicy != nil {
		preempt = string(*o.PreemptionPolicy)
	}
	defTone, defVal := "muted", "No"
	if o.GlobalDefault {
		defTone, defVal = "ok", "Yes"
	}
	d.Status = []chip{
		{Label: "Value", Value: fmt.Sprintf("%d", o.Value), Tone: "muted"},
		{Label: "Global default", Value: defVal, Tone: defTone},
		{Label: "Preemption", Value: preempt, Tone: "muted"},
	}
	if o.Description != "" {
		d.Sections = append(d.Sections, section{Title: "Description", Items: []kv{{Label: "", Value: o.Description}}})
	}
	return d
}

func runtimeClassDetail(o *nodev1.RuntimeClass) *resourceDetail {
	d := base("RuntimeClass", o.ObjectMeta)
	d.Status = []chip{{Label: "Handler", Value: orDash(o.Handler), Tone: "muted"}}
	if s := runtimeClassSchedulingSection(o); s != nil {
		d.Sections = append(d.Sections, *s)
	}
	if s := runtimeClassOverheadSection(o); s != nil {
		d.Sections = append(d.Sections, *s)
	}
	return d
}

func runtimeClassSchedulingSection(o *nodev1.RuntimeClass) *section {
	if o.Scheduling == nil {
		return nil
	}
	items := []kv{}
	keys := make([]string, 0, len(o.Scheduling.NodeSelector))
	for k := range o.Scheduling.NodeSelector {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		items = append(items, kv{Label: k, Value: o.Scheduling.NodeSelector[k]})
	}
	for _, tol := range o.Scheduling.Tolerations {
		key := tol.Key
		if tol.Value != "" {
			key += "=" + tol.Value
		}
		items = append(items, kv{Label: "toleration " + key, Value: string(tol.Effect)})
	}
	if len(items) == 0 {
		return nil
	}
	return &section{Title: "Scheduling", Items: items}
}

func runtimeClassOverheadSection(o *nodev1.RuntimeClass) *section {
	if o.Overhead == nil || len(o.Overhead.PodFixed) == 0 {
		return nil
	}
	items := []kv{}
	for name, q := range o.Overhead.PodFixed {
		items = append(items, kv{Label: string(name), Value: q.String()})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Label < items[j].Label })
	return &section{Title: "Overhead", Items: items}
}

func podDetail(p *corev1.Pod) *resourceDetail {
	d := base("Pod", p.ObjectMeta)

	ready, total := 0, len(p.Status.ContainerStatuses)
	var restarts int32
	for _, cs := range p.Status.ContainerStatuses {
		if cs.Ready {
			ready++
		}
		restarts += cs.RestartCount
	}
	d.Status = []chip{
		{Label: "Status", Value: kube.PodPhase(p), Tone: phaseTone(kube.PodPhase(p))},
		replicaChip("Ready", int32(ready), int32(total)), //nolint:gosec // container counts, always tiny
		countChip("Restarts", restarts, boolTone(restarts == 0)),
		{Label: "QoS", Value: string(p.Status.QOSClass), Tone: "muted"},
	}
	d.Images = imagesOf(p.Spec)
	for _, c := range p.Spec.Containers {
		for _, cp := range c.Ports {
			d.Ports = append(d.Ports, portView{Name: cp.Name, Port: fmt.Sprintf("%d", cp.ContainerPort), Protocol: string(cp.Protocol), Extra: c.Name})
		}
	}
	d.Sections = append(d.Sections, section{Title: "Info", Items: []kv{
		{Label: "Node", Value: p.Spec.NodeName},
		{Label: "Pod IP", Value: p.Status.PodIP},
		{Label: "Host IP", Value: p.Status.HostIP},
		{Label: "Service account", Value: p.Spec.ServiceAccountName},
		{Label: "Restart policy", Value: string(p.Spec.RestartPolicy)},
	}})

	states := []kv{}
	for _, cs := range p.Status.ContainerStatuses {
		states = append(states, kv{Label: cs.Name, Value: containerState(cs)})
	}
	if len(states) > 0 {
		d.Sections = append(d.Sections, section{Title: "Containers", Items: states})
	}

	for _, c := range p.Status.Conditions {
		tone := "warn"
		if c.Status == corev1.ConditionTrue {
			tone = "ok"
		}
		d.Conditions = append(d.Conditions, chip{Label: string(c.Type), Value: string(c.Status), Tone: tone})
	}
	return d
}

func phaseTone(phase string) string {
	switch phase {
	case "Running", "Succeeded":
		return "ok"
	case "Pending", "ContainerCreating", "Terminating", "PodInitializing":
		return "warn"
	case "Failed", "CrashLoopBackOff", "Error", "Evicted":
		return "err"
	}
	return "muted"
}

func containerState(cs corev1.ContainerStatus) string {
	switch {
	case cs.State.Running != nil:
		return "Running"
	case cs.State.Waiting != nil:
		return "Waiting: " + cs.State.Waiting.Reason
	case cs.State.Terminated != nil:
		return "Terminated: " + cs.State.Terminated.Reason
	}
	return "Unknown"
}

func nodeDetail(n *corev1.Node) *resourceDetail {
	d := base("Node", n.ObjectMeta)
	d.Status = nodeStatusChips(n)
	isSchedulable := !n.Spec.Unschedulable
	d.Schedulable = &isSchedulable

	ni := n.Status.NodeInfo
	d.Sections = append(d.Sections, section{Title: "System", Items: []kv{
		{Label: "Kubelet", Value: ni.KubeletVersion},
		{Label: "Runtime", Value: ni.ContainerRuntimeVersion},
		{Label: "OS", Value: ni.OSImage},
		{Label: "Kernel", Value: ni.KernelVersion},
		{Label: "Architecture", Value: ni.Architecture},
	}})
	d.Sections = append(d.Sections, section{Title: "Infrastructure", Items: []kv{
		{Label: "Instance type", Value: nodeLabel(n, "node.kubernetes.io/instance-type")},
		{Label: "Capacity type", Value: nodeLabel(n, "eks.amazonaws.com/capacityType")},
		{Label: "Zone", Value: nodeLabel(n, "topology.kubernetes.io/zone")},
		{Label: "Region", Value: nodeLabel(n, "topology.kubernetes.io/region")},
	}})
	d.Sections = append(d.Sections, section{Title: "Capacity", Items: []kv{
		{Label: "CPU", Value: n.Status.Capacity.Cpu().String()},
		{Label: "Memory", Value: n.Status.Capacity.Memory().String()},
		{Label: "Pods", Value: n.Status.Capacity.Pods().String()},
	}})
	d.Sections = append(d.Sections, section{Title: "Allocatable", Items: []kv{
		{Label: "CPU", Value: n.Status.Allocatable.Cpu().String()},
		{Label: "Memory", Value: n.Status.Allocatable.Memory().String()},
		{Label: "Pods", Value: n.Status.Allocatable.Pods().String()},
	}})

	internalIP, hostname := nodeAddresses(n)
	d.Sections = append(d.Sections, section{Title: "Network", Items: []kv{
		{Label: "Internal IP", Value: internalIP},
		{Label: "Hostname", Value: hostname},
		{Label: "PodCIDR", Value: n.Spec.PodCIDR},
	}})

	if s := nodeTaintsSection(n); s != nil {
		d.Sections = append(d.Sections, *s)
	}
	d.Conditions = nodeConditionChips(n)
	return d
}

func nodeStatusChips(n *corev1.Node) []chip {
	readyTone, readyVal := "err", "NotReady"
	if nodeReady(n) {
		readyTone, readyVal = "ok", "Ready"
	}
	schedulable, schedTone := "Yes", "ok"
	if n.Spec.Unschedulable {
		schedulable, schedTone = "No", "warn"
	}
	return []chip{
		{Label: "Status", Value: readyVal, Tone: readyTone},
		{Label: "Roles", Value: strings.Join(nodeRoles(n), ", "), Tone: "muted"},
		{Label: "Schedulable", Value: schedulable, Tone: schedTone},
	}
}

func nodeAddresses(n *corev1.Node) (internalIP, hostname string) {
	for _, a := range n.Status.Addresses {
		switch a.Type {
		case corev1.NodeInternalIP:
			internalIP = a.Address
		case corev1.NodeHostName:
			hostname = a.Address
		}
	}
	return internalIP, hostname
}

func nodeTaintsSection(n *corev1.Node) *section {
	if len(n.Spec.Taints) == 0 {
		return nil
	}
	items := []kv{}
	for _, t := range n.Spec.Taints {
		key := t.Key
		if t.Value != "" {
			key += "=" + t.Value
		}
		items = append(items, kv{Label: key, Value: string(t.Effect)})
	}
	return &section{Title: "Taints", Items: items}
}

func nodeConditionChips(n *corev1.Node) []chip {
	conditions := make([]chip, 0, len(n.Status.Conditions))
	for _, c := range n.Status.Conditions {
		tone := "ok"
		if c.Type == corev1.NodeReady {
			if c.Status != corev1.ConditionTrue {
				tone = "err"
			}
		} else if c.Status == corev1.ConditionTrue {
			tone = "warn" // pressure conditions being True is bad
		}
		conditions = append(conditions, chip{Label: string(c.Type), Value: string(c.Status), Tone: tone})
	}
	return conditions
}

func nodeLabel(n *corev1.Node, key string) string {
	if v, ok := n.Labels[key]; ok {
		return v
	}
	return "—"
}

func boolTone(good bool) string {
	if good {
		return "muted"
	}
	return "err"
}
