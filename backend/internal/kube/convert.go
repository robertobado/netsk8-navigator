package kube

import (
	"fmt"
	"sort"
	"strings"
	"time"

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
)

// formatAge renders a creation time as an RFC3339 string (the client turns it
// into a compact "3d"/"2h" age). Empty for zero timestamps.
func formatAge(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// PodView is the UI-friendly projection of a Pod, shared by the REST list
// endpoint and the SSE watch stream so both speak the same shape.
type PodView struct {
	Name       string   `json:"name"`
	Namespace  string   `json:"namespace"`
	Status     string   `json:"status"`
	Ready      int      `json:"ready"`
	Total      int      `json:"total"`
	Restarts   int32    `json:"restarts"`
	Node       string   `json:"node"`
	IP         string   `json:"ip"`
	Age        string   `json:"age"`
	Containers []string `json:"containers"`
	OwnerKind  string   `json:"ownerKind"` // controller kind (ReplicaSet, StatefulSet, DaemonSet, Job…)
	OwnerName  string   `json:"ownerName"`
	Reason     string   `json:"reason"`     // container waiting reason (ImagePullBackOff, ContainerCreating…) or Unschedulable
	DeletedAt  string   `json:"deletedAt"`  // moment it entered Terminating (RFC3339); empty otherwise
	Finalizers []string `json:"finalizers"` // metadata.finalizers (why a Terminating pod may be stuck)
}

// Key uniquely identifies a pod within a cluster (used for client-side upserts).
func (p PodView) Key() string { return p.Namespace + "/" + p.Name }

// ToPodView projects a Pod into its UI view.
func ToPodView(p *corev1.Pod) PodView {
	ready, total := 0, len(p.Status.ContainerStatuses)
	var restarts int32
	for _, cs := range p.Status.ContainerStatuses {
		if cs.Ready {
			ready++
		}
		restarts += cs.RestartCount
	}
	containers := make([]string, 0, len(p.Spec.Containers))
	for _, c := range p.Spec.Containers {
		containers = append(containers, c.Name)
	}
	ownerKind, ownerName := "", ""
	for _, o := range p.OwnerReferences {
		if o.Controller != nil && *o.Controller {
			ownerKind, ownerName = o.Kind, o.Name
			break
		}
	}
	reason := WaitingReason(p)
	// deletionTimestamp is the *deadline* (request time + grace period), which is in
	// the future. The moment the pod entered Terminating is that minus the grace
	// period — that's what the "time in Terminating" counter should count from.
	deletedAt := ""
	if p.DeletionTimestamp != nil {
		since := p.DeletionTimestamp.Time
		if p.DeletionGracePeriodSeconds != nil {
			since = since.Add(-time.Duration(*p.DeletionGracePeriodSeconds) * time.Second)
		}
		deletedAt = formatAge(since)
	}
	return PodView{
		Name:       p.Name,
		Namespace:  p.Namespace,
		Status:     PodPhase(p),
		Ready:      ready,
		Total:      total,
		Restarts:   restarts,
		Node:       p.Spec.NodeName,
		IP:         p.Status.PodIP,
		Age:        formatAge(p.CreationTimestamp.Time),
		Containers: containers,
		OwnerKind:  ownerKind,
		OwnerName:  ownerName,
		Reason:     reason,
		DeletedAt:  deletedAt,
		Finalizers: p.Finalizers,
	}
}

// WaitingReason returns the kubectl-style reason a pod isn't healthily running:
// a container waiting reason (ImagePullBackOff, ContainerCreating, CrashLoopBackOff…),
// a terminated reason (OOMKilled, Error, Completed…), or "Unschedulable". This is
// reported even when the phase is Running (e.g. a container crash-looping), so the
// UI can flag it. Empty for healthy pods.
func WaitingReason(p *corev1.Pod) string {
	// Waiting containers (init first, then main).
	for _, cs := range p.Status.InitContainerStatuses {
		if cs.State.Waiting != nil && cs.State.Waiting.Reason != "" {
			return cs.State.Waiting.Reason
		}
	}
	for _, cs := range p.Status.ContainerStatuses {
		if cs.State.Waiting != nil && cs.State.Waiting.Reason != "" {
			return cs.State.Waiting.Reason
		}
	}
	// A failed init container that terminated (non-Completed).
	for _, cs := range p.Status.InitContainerStatuses {
		if t := cs.State.Terminated; t != nil && t.Reason != "" && t.Reason != "Completed" {
			return t.Reason
		}
	}
	// A main container that terminated (OOMKilled, Error, Completed, ContainerCannotRun…).
	for _, cs := range p.Status.ContainerStatuses {
		if t := cs.State.Terminated; t != nil && t.Reason != "" {
			return t.Reason
		}
	}
	if PodPhase(p) == "Pending" {
		for _, c := range p.Status.Conditions {
			if c.Type == corev1.PodScheduled && c.Status != corev1.ConditionTrue {
				return "Unschedulable"
			}
		}
	}
	return ""
}

// PodPhase reflects the display status, accounting for terminating pods.
func PodPhase(p *corev1.Pod) string {
	if p.DeletionTimestamp != nil {
		return "Terminating"
	}
	return string(p.Status.Phase)
}

// DeploymentView is the UI projection of a Deployment.
type DeploymentView struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Ready     string `json:"ready"` // "3/3"
	UpToDate  int32  `json:"upToDate"`
	Available int32  `json:"available"`
	Status    string `json:"status"` // Available | Progressing
	Age       string `json:"age"`
}

func ToDeploymentView(d *appsv1.Deployment) DeploymentView {
	desired := int32(1)
	if d.Spec.Replicas != nil {
		desired = *d.Spec.Replicas
	}
	status := "Progressing"
	if d.Status.ReadyReplicas == desired && desired > 0 {
		status = "Available"
	} else if desired == 0 {
		status = "Scaled to 0"
	}
	return DeploymentView{
		Name:      d.Name,
		Namespace: d.Namespace,
		Ready:     fmt.Sprintf("%d/%d", d.Status.ReadyReplicas, desired),
		UpToDate:  d.Status.UpdatedReplicas,
		Available: d.Status.AvailableReplicas,
		Status:    status,
		Age:       formatAge(d.CreationTimestamp.Time),
	}
}

// ServiceView is the UI projection of a Service.
type ServiceView struct {
	Name       string `json:"name"`
	Namespace  string `json:"namespace"`
	Type       string `json:"type"`
	ClusterIP  string `json:"clusterIP"`
	ExternalIP string `json:"externalIP"`
	Ports      string `json:"ports"`
	Age        string `json:"age"`
}

func ToServiceView(s *corev1.Service) ServiceView {
	ports := make([]string, 0, len(s.Spec.Ports))
	for _, p := range s.Spec.Ports {
		if p.NodePort > 0 {
			ports = append(ports, fmt.Sprintf("%d:%d/%s", p.Port, p.NodePort, p.Protocol))
		} else {
			ports = append(ports, fmt.Sprintf("%d/%s", p.Port, p.Protocol))
		}
	}
	external := ""
	if len(s.Status.LoadBalancer.Ingress) > 0 {
		lb := s.Status.LoadBalancer.Ingress[0]
		external = lb.Hostname
		if external == "" {
			external = lb.IP
		}
	}
	if len(s.Spec.ExternalIPs) > 0 && external == "" {
		external = s.Spec.ExternalIPs[0]
	}
	return ServiceView{
		Name:       s.Name,
		Namespace:  s.Namespace,
		Type:       string(s.Spec.Type),
		ClusterIP:  s.Spec.ClusterIP,
		ExternalIP: external,
		Ports:      strings.Join(ports, ", "),
		Age:        formatAge(s.CreationTimestamp.Time),
	}
}

// IngressView is the UI projection of an Ingress.
type IngressView struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Class     string `json:"class"`
	Hosts     string `json:"hosts"`
	Address   string `json:"address"`
	Age       string `json:"age"`
}

func ToIngressView(in *networkingv1.Ingress) IngressView {
	hosts := make([]string, 0, len(in.Spec.Rules))
	for _, r := range in.Spec.Rules {
		if r.Host != "" {
			hosts = append(hosts, r.Host)
		}
	}
	class := ""
	if in.Spec.IngressClassName != nil {
		class = *in.Spec.IngressClassName
	}
	address := ""
	if len(in.Status.LoadBalancer.Ingress) > 0 {
		lb := in.Status.LoadBalancer.Ingress[0]
		address = lb.Hostname
		if address == "" {
			address = lb.IP
		}
	}
	return IngressView{
		Name:      in.Name,
		Namespace: in.Namespace,
		Class:     class,
		Hosts:     strings.Join(hosts, ", "),
		Address:   address,
		Age:       formatAge(in.CreationTimestamp.Time),
	}
}

// ConfigMapView is the UI projection of a ConfigMap.
type ConfigMapView struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Keys      int    `json:"keys"`
	Age       string `json:"age"`
}

func ToConfigMapView(c *corev1.ConfigMap) ConfigMapView {
	return ConfigMapView{
		Name:      c.Name,
		Namespace: c.Namespace,
		Keys:      len(c.Data) + len(c.BinaryData),
		Age:       formatAge(c.CreationTimestamp.Time),
	}
}

// NamespaceView is the UI projection of a Namespace (cluster-scoped).
type NamespaceView struct {
	Name   string `json:"name"`
	Status string `json:"status"` // Active | Terminating
	Age    string `json:"age"`
}

func ToNamespaceView(n *corev1.Namespace) NamespaceView {
	return NamespaceView{
		Name:   n.Name,
		Status: string(n.Status.Phase),
		Age:    formatAge(n.CreationTimestamp.Time),
	}
}

// SecretView is the UI projection of a Secret — never carries the values.
type SecretView struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Type      string `json:"type"`
	Keys      int    `json:"keys"`
	Age       string `json:"age"`
}

func ToSecretView(s *corev1.Secret) SecretView {
	return SecretView{
		Name:      s.Name,
		Namespace: s.Namespace,
		Type:      string(s.Type),
		Keys:      len(s.Data) + len(s.StringData),
		Age:       formatAge(s.CreationTimestamp.Time),
	}
}

// ShortAccessModes renders PV/PVC access modes as the familiar RWO/ROX/RWX/RWOP.
func ShortAccessModes(modes []corev1.PersistentVolumeAccessMode) string {
	short := map[corev1.PersistentVolumeAccessMode]string{
		corev1.ReadWriteOnce:    "RWO",
		corev1.ReadOnlyMany:     "ROX",
		corev1.ReadWriteMany:    "RWX",
		corev1.ReadWriteOncePod: "RWOP",
	}
	out := make([]string, 0, len(modes))
	for _, m := range modes {
		if s, ok := short[m]; ok {
			out = append(out, s)
		} else {
			out = append(out, string(m))
		}
	}
	return strings.Join(out, ",")
}

// PVCMountPoint is one container's mount of the claim (container + path).
type PVCMountPoint struct {
	Container string `json:"container"`
	Path      string `json:"path"`
}

// PVCMount is a pod that mounts the claim, with its per-container mount points.
type PVCMount struct {
	Pod    string          `json:"pod"`
	Mounts []PVCMountPoint `json:"mounts"`
}

// PVCView is the UI projection of a PersistentVolumeClaim. MountedBy is filled
// by the list enricher (a Bound PVC may have no mounting pod — see enrichPVCMounts).
type PVCView struct {
	Name         string     `json:"name"`
	Namespace    string     `json:"namespace"`
	Status       string     `json:"status"` // Bound | Pending | Lost
	Volume       string     `json:"volume"`
	Capacity     string     `json:"capacity"`
	AccessModes  string     `json:"accessModes"`
	StorageClass string     `json:"storageClass"`
	MountedBy    []PVCMount `json:"mountedBy"` // pods (same namespace) mounting the claim
	Age          string     `json:"age"`
}

func ToPVCView(c *corev1.PersistentVolumeClaim) PVCView {
	cap := ""
	if q, ok := c.Status.Capacity[corev1.ResourceStorage]; ok {
		cap = q.String()
	} else if q, ok := c.Spec.Resources.Requests[corev1.ResourceStorage]; ok {
		cap = q.String()
	}
	sc := ""
	if c.Spec.StorageClassName != nil {
		sc = *c.Spec.StorageClassName
	}
	return PVCView{
		Name:         c.Name,
		Namespace:    c.Namespace,
		Status:       string(c.Status.Phase),
		Volume:       c.Spec.VolumeName,
		Capacity:     cap,
		AccessModes:  ShortAccessModes(c.Status.AccessModes),
		StorageClass: sc,
		Age:          formatAge(c.CreationTimestamp.Time),
	}
}

// PVView is the UI projection of a PersistentVolume (cluster-scoped).
type PVView struct {
	Name         string `json:"name"`
	Capacity     string `json:"capacity"`
	AccessModes  string `json:"accessModes"`
	Reclaim      string `json:"reclaim"`
	Status       string `json:"status"` // Available | Bound | Released | Failed
	Claim        string `json:"claim"`
	StorageClass string `json:"storageClass"`
	Age          string `json:"age"`
}

func ToPVView(p *corev1.PersistentVolume) PVView {
	cap := ""
	if q, ok := p.Spec.Capacity[corev1.ResourceStorage]; ok {
		cap = q.String()
	}
	claim := ""
	if p.Spec.ClaimRef != nil {
		claim = p.Spec.ClaimRef.Namespace + "/" + p.Spec.ClaimRef.Name
	}
	return PVView{
		Name:         p.Name,
		Capacity:     cap,
		AccessModes:  ShortAccessModes(p.Spec.AccessModes),
		Reclaim:      string(p.Spec.PersistentVolumeReclaimPolicy),
		Status:       string(p.Status.Phase),
		Claim:        claim,
		StorageClass: p.Spec.StorageClassName,
		Age:          formatAge(p.CreationTimestamp.Time),
	}
}

// StorageClassView is the UI projection of a StorageClass (cluster-scoped).
type StorageClassView struct {
	Name        string `json:"name"`
	Provisioner string `json:"provisioner"`
	Reclaim     string `json:"reclaim"`
	Binding     string `json:"binding"`
	Default     bool   `json:"default"`
	Age         string `json:"age"`
}

func ToStorageClassView(s *storagev1.StorageClass) StorageClassView {
	reclaim := ""
	if s.ReclaimPolicy != nil {
		reclaim = string(*s.ReclaimPolicy)
	}
	binding := ""
	if s.VolumeBindingMode != nil {
		binding = string(*s.VolumeBindingMode)
	}
	def := s.Annotations["storageclass.kubernetes.io/is-default-class"] == "true"
	return StorageClassView{
		Name:        s.Name,
		Provisioner: s.Provisioner,
		Reclaim:     reclaim,
		Binding:     binding,
		Default:     def,
		Age:         formatAge(s.CreationTimestamp.Time),
	}
}

// HPAView is the UI projection of a HorizontalPodAutoscaler.
type HPAView struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Reference string `json:"reference"` // Kind/name it scales
	MinPods   int32  `json:"minPods"`
	MaxPods   int32  `json:"maxPods"`
	Replicas  int32  `json:"replicas"`
	Age       string `json:"age"`
}

func ToHPAView(h *autoscalingv2.HorizontalPodAutoscaler) HPAView {
	min := int32(1)
	if h.Spec.MinReplicas != nil {
		min = *h.Spec.MinReplicas
	}
	return HPAView{
		Name:      h.Name,
		Namespace: h.Namespace,
		Reference: h.Spec.ScaleTargetRef.Kind + "/" + h.Spec.ScaleTargetRef.Name,
		MinPods:   min,
		MaxPods:   h.Spec.MaxReplicas,
		Replicas:  h.Status.CurrentReplicas,
		Age:       formatAge(h.CreationTimestamp.Time),
	}
}

// FormatSelector renders a label selector as "k=v, k=v" (or "all" when empty,
// which for a selector means "matches everything").
func FormatSelector(sel metav1.LabelSelector) string {
	if len(sel.MatchLabels) == 0 && len(sel.MatchExpressions) == 0 {
		return "all"
	}
	parts := make([]string, 0, len(sel.MatchLabels))
	for k, v := range sel.MatchLabels {
		parts = append(parts, k+"="+v)
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

// EndpointSliceView is the UI projection of an EndpointSlice.
type EndpointSliceView struct {
	Name        string `json:"name"`
	Namespace   string `json:"namespace"`
	Service     string `json:"service"`     // owning Service (label kubernetes.io/service-name)
	AddressType string `json:"addressType"` // IPv4 | IPv6 | FQDN
	Ready       int    `json:"ready"`
	Total       int    `json:"total"`
	Ports       string `json:"ports"`
	Age         string `json:"age"`
}

func endpointReady(e discoveryv1.Endpoint) bool {
	// A nil Ready means "unknown" — kube treats those as ready for compatibility.
	return e.Conditions.Ready == nil || *e.Conditions.Ready
}

func ToEndpointSliceView(s *discoveryv1.EndpointSlice) EndpointSliceView {
	ready := 0
	for _, e := range s.Endpoints {
		if endpointReady(e) {
			ready++
		}
	}
	ports := make([]string, 0, len(s.Ports))
	seen := map[string]bool{}
	for _, p := range s.Ports {
		label := ""
		if p.Name != nil && *p.Name != "" {
			label = *p.Name + ":"
		}
		if p.Port != nil {
			label += fmt.Sprintf("%d", *p.Port)
		}
		if p.Protocol != nil {
			label += "/" + string(*p.Protocol)
		}
		if label != "" && !seen[label] {
			seen[label] = true
			ports = append(ports, label)
		}
	}
	return EndpointSliceView{
		Name:        s.Name,
		Namespace:   s.Namespace,
		Service:     s.Labels["kubernetes.io/service-name"],
		AddressType: string(s.AddressType),
		Ready:       ready,
		Total:       len(s.Endpoints),
		Ports:       strings.Join(ports, ", "),
		Age:         formatAge(s.CreationTimestamp.Time),
	}
}

// NetworkPolicyView is the UI projection of a NetworkPolicy.
type NetworkPolicyView struct {
	Name        string `json:"name"`
	Namespace   string `json:"namespace"`
	PodSelector string `json:"podSelector"`
	PolicyTypes string `json:"policyTypes"` // Ingress, Egress
	Age         string `json:"age"`
}

func ToNetworkPolicyView(n *networkingv1.NetworkPolicy) NetworkPolicyView {
	types := make([]string, 0, len(n.Spec.PolicyTypes))
	for _, t := range n.Spec.PolicyTypes {
		types = append(types, string(t))
	}
	return NetworkPolicyView{
		Name:        n.Name,
		Namespace:   n.Namespace,
		PodSelector: FormatSelector(n.Spec.PodSelector),
		PolicyTypes: strings.Join(types, ", "),
		Age:         formatAge(n.CreationTimestamp.Time),
	}
}

// IngressClassView is the UI projection of an IngressClass (cluster-scoped).
type IngressClassView struct {
	Name       string `json:"name"`
	Controller string `json:"controller"`
	Default    bool   `json:"default"`
	Age        string `json:"age"`
}

func ToIngressClassView(c *networkingv1.IngressClass) IngressClassView {
	return IngressClassView{
		Name:       c.Name,
		Controller: c.Spec.Controller,
		Default:    c.Annotations["ingressclass.kubernetes.io/is-default-class"] == "true",
		Age:        formatAge(c.CreationTimestamp.Time),
	}
}

// --- RBAC ------------------------------------------------------------------

// ServiceAccountView is the UI projection of a ServiceAccount.
type ServiceAccountView struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Secrets   int    `json:"secrets"`
	Age       string `json:"age"`
}

func ToServiceAccountView(s *corev1.ServiceAccount) ServiceAccountView {
	return ServiceAccountView{Name: s.Name, Namespace: s.Namespace, Secrets: len(s.Secrets), Age: formatAge(s.CreationTimestamp.Time)}
}

// RoleView is the UI projection of a Role (namespaced) or ClusterRole (cluster).
type RoleView struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Rules     int    `json:"rules"`
	Age       string `json:"age"`
}

func ToRoleView(r *rbacv1.Role) RoleView {
	return RoleView{Name: r.Name, Namespace: r.Namespace, Rules: len(r.Rules), Age: formatAge(r.CreationTimestamp.Time)}
}

func ToClusterRoleView(r *rbacv1.ClusterRole) RoleView {
	return RoleView{Name: r.Name, Rules: len(r.Rules), Age: formatAge(r.CreationTimestamp.Time)}
}

// RoleBindingView is the UI projection of a (Cluster)RoleBinding.
type RoleBindingView struct {
	Name      string   `json:"name"`
	Namespace string   `json:"namespace"`
	Role      string   `json:"role"`     // roleRef Kind/Name
	Subjects  []string `json:"subjects"` // formatted subjects (SA/user/group)
	Age       string   `json:"age"`
}

// formatSubjects renders a binding's subjects generically: ServiceAccounts as
// "ns/name" (defaulting to defaultNS), users/groups as "user:name"/"group:name".
func formatSubjects(subjects []rbacv1.Subject, defaultNS string) []string {
	out := make([]string, 0, len(subjects))
	for _, s := range subjects {
		switch s.Kind {
		case "ServiceAccount":
			ns := s.Namespace
			if ns == "" {
				ns = defaultNS
			}
			if ns != "" {
				out = append(out, ns+"/"+s.Name)
			} else {
				out = append(out, s.Name)
			}
		default:
			out = append(out, strings.ToLower(s.Kind)+":"+s.Name)
		}
	}
	return out
}

func ToRoleBindingView(b *rbacv1.RoleBinding) RoleBindingView {
	return RoleBindingView{
		Name:      b.Name,
		Namespace: b.Namespace,
		Role:      b.RoleRef.Kind + "/" + b.RoleRef.Name,
		Subjects:  formatSubjects(b.Subjects, b.Namespace),
		Age:       formatAge(b.CreationTimestamp.Time),
	}
}

func ToClusterRoleBindingView(b *rbacv1.ClusterRoleBinding) RoleBindingView {
	return RoleBindingView{
		Name:     b.Name,
		Role:     b.RoleRef.Kind + "/" + b.RoleRef.Name,
		Subjects: formatSubjects(b.Subjects, ""),
		Age:      formatAge(b.CreationTimestamp.Time),
	}
}

// --- Governance ------------------------------------------------------------

// ResourceQuotaView / LimitRangeView are minimal in the list — detail carries
// the hard/used and limit breakdowns.
type ResourceQuotaView struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Age       string `json:"age"`
}

func ToResourceQuotaView(q *corev1.ResourceQuota) ResourceQuotaView {
	return ResourceQuotaView{Name: q.Name, Namespace: q.Namespace, Age: formatAge(q.CreationTimestamp.Time)}
}

type LimitRangeView struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Age       string `json:"age"`
}

func ToLimitRangeView(l *corev1.LimitRange) LimitRangeView {
	return LimitRangeView{Name: l.Name, Namespace: l.Namespace, Age: formatAge(l.CreationTimestamp.Time)}
}

// PDBView is the UI projection of a PodDisruptionBudget.
type PDBView struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Criteria  string `json:"criteria"` // "min N" | "max N"
	Current   int32  `json:"current"`  // currently healthy
	Desired   int32  `json:"desired"`  // desired healthy
	Allowed   int32  `json:"allowed"`  // disruptions allowed now
	Age       string `json:"age"`
}

func ToPDBView(p *policyv1.PodDisruptionBudget) PDBView {
	crit := "—"
	if p.Spec.MinAvailable != nil {
		crit = "min " + p.Spec.MinAvailable.String()
	} else if p.Spec.MaxUnavailable != nil {
		crit = "max " + p.Spec.MaxUnavailable.String()
	}
	return PDBView{
		Name:      p.Name,
		Namespace: p.Namespace,
		Criteria:  crit,
		Current:   p.Status.CurrentHealthy,
		Desired:   p.Status.DesiredHealthy,
		Allowed:   p.Status.DisruptionsAllowed,
		Age:       formatAge(p.CreationTimestamp.Time),
	}
}

// PriorityClassView is the UI projection of a PriorityClass (cluster-scoped).
type PriorityClassView struct {
	Name          string `json:"name"`
	Value         int32  `json:"value"`
	GlobalDefault bool   `json:"globalDefault"`
	Preemption    string `json:"preemption"` // PreemptLowerPriority | Never
	Age           string `json:"age"`
}

func ToPriorityClassView(p *schedulingv1.PriorityClass) PriorityClassView {
	preempt := string(corev1.PreemptLowerPriority)
	if p.PreemptionPolicy != nil {
		preempt = string(*p.PreemptionPolicy)
	}
	return PriorityClassView{
		Name:          p.Name,
		Value:         p.Value,
		GlobalDefault: p.GlobalDefault,
		Preemption:    preempt,
		Age:           formatAge(p.CreationTimestamp.Time),
	}
}

// RuntimeClassView is the UI projection of a RuntimeClass (cluster-scoped).
type RuntimeClassView struct {
	Name    string `json:"name"`
	Handler string `json:"handler"`
	Age     string `json:"age"`
}

func ToRuntimeClassView(rc *nodev1.RuntimeClass) RuntimeClassView {
	return RuntimeClassView{
		Name:    rc.Name,
		Handler: rc.Handler,
		Age:     formatAge(rc.CreationTimestamp.Time),
	}
}

func replicasOrDefault(r *int32) int32 {
	if r != nil {
		return *r
	}
	return 1
}

// StatefulSetView / ReplicaSetView are the UI projections of the *Set workloads.
type StatefulSetView struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Ready     string `json:"ready"` // "3/3"
	Service   string `json:"service"`
	Age       string `json:"age"`
}

func ToStatefulSetView(o *appsv1.StatefulSet) StatefulSetView {
	return StatefulSetView{
		Name:      o.Name,
		Namespace: o.Namespace,
		Ready:     fmt.Sprintf("%d/%d", o.Status.ReadyReplicas, replicasOrDefault(o.Spec.Replicas)),
		Service:   o.Spec.ServiceName,
		Age:       formatAge(o.CreationTimestamp.Time),
	}
}

type ReplicaSetView struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Ready     string `json:"ready"`
	OwnerKind string `json:"ownerKind"`
	OwnerName string `json:"ownerName"`
	Revision  string `json:"revision"` // deployment revision (for history grouping)
	Current   bool   `json:"current"`  // active revision (desired > 0, or standalone)
	Age       string `json:"age"`
}

func ToReplicaSetView(o *appsv1.ReplicaSet) ReplicaSetView {
	ok, on := "", ""
	for _, ref := range o.OwnerReferences {
		if ref.Controller != nil && *ref.Controller {
			ok, on = ref.Kind, ref.Name
			break
		}
	}
	desired := replicasOrDefault(o.Spec.Replicas)
	return ReplicaSetView{
		Name:      o.Name,
		Namespace: o.Namespace,
		Ready:     fmt.Sprintf("%d/%d", o.Status.ReadyReplicas, desired),
		OwnerKind: ok,
		OwnerName: on,
		Revision:  o.Annotations["deployment.kubernetes.io/revision"],
		Current:   desired > 0 || ok == "",
		Age:       formatAge(o.CreationTimestamp.Time),
	}
}

// DaemonSetView is the UI projection of a DaemonSet.
type DaemonSetView struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Ready     string `json:"ready"` // numberReady/desired
	UpToDate  int32  `json:"upToDate"`
	Available int32  `json:"available"`
	Age       string `json:"age"`
}

func ToDaemonSetView(o *appsv1.DaemonSet) DaemonSetView {
	return DaemonSetView{
		Name:      o.Name,
		Namespace: o.Namespace,
		Ready:     fmt.Sprintf("%d/%d", o.Status.NumberReady, o.Status.DesiredNumberScheduled),
		UpToDate:  o.Status.UpdatedNumberScheduled,
		Available: o.Status.NumberAvailable,
		Age:       formatAge(o.CreationTimestamp.Time),
	}
}

// JobView is the UI projection of a Job.
type JobView struct {
	Name        string `json:"name"`
	Namespace   string `json:"namespace"`
	Completions string `json:"completions"` // "1/1"
	Status      string `json:"status"`      // Complete | Running | Failed | Suspended
	Age         string `json:"age"`
}

func ToJobView(o *batchv1.Job) JobView {
	completions := replicasOrDefault(o.Spec.Completions)
	status := "Running"
	for _, c := range o.Status.Conditions {
		if c.Status != corev1.ConditionTrue {
			continue
		}
		switch c.Type {
		case batchv1.JobComplete:
			status = "Complete"
		case batchv1.JobFailed:
			status = "Failed"
		case batchv1.JobSuspended:
			status = "Suspended"
		}
	}
	return JobView{
		Name:        o.Name,
		Namespace:   o.Namespace,
		Completions: fmt.Sprintf("%d/%d", o.Status.Succeeded, completions),
		Status:      status,
		Age:         formatAge(o.CreationTimestamp.Time),
	}
}

// CronJobView is the UI projection of a CronJob.
type CronJobView struct {
	Name         string `json:"name"`
	Namespace    string `json:"namespace"`
	Schedule     string `json:"schedule"`
	Suspend      bool   `json:"suspend"`
	Active       int    `json:"active"`
	LastSchedule string `json:"lastSchedule"` // RFC3339 (client renders age) or ""
	Age          string `json:"age"`
}

func ToCronJobView(o *batchv1.CronJob) CronJobView {
	last := ""
	if o.Status.LastScheduleTime != nil {
		last = formatAge(o.Status.LastScheduleTime.Time)
	}
	return CronJobView{
		Name:         o.Name,
		Namespace:    o.Namespace,
		Schedule:     o.Spec.Schedule,
		Suspend:      o.Spec.Suspend != nil && *o.Spec.Suspend,
		Active:       len(o.Status.Active),
		LastSchedule: last,
		Age:          formatAge(o.CreationTimestamp.Time),
	}
}
