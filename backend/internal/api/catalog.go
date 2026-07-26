package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"

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
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/robertobado/netsk8-navigator/backend/internal/kube"
)

// resourceEntry describes a resource surfaced by the generic list endpoint: its
// plural name (resolved to the served GVR at request time, so it works across
// Kubernetes versions) and a projection mapping a live object to the row shape
// the UI table expects. Adding a resource is one entry, not a new handler.
type resourceEntry struct {
	resource string
	project  func(*unstructured.Unstructured) (any, error)
	// enrich optionally post-processes the projected rows with extra cluster data
	// (e.g. surfacing a Job's pod-level error), keyed by the request's context/ns.
	enrich func(ctx context.Context, s *Server, contextName, ns string, rows []any)
}

// fromUnstructured decodes a dynamic object into a typed struct so the existing
// typed view builders (kube.To*View) can be reused unchanged.
func fromUnstructured[T any](u *unstructured.Unstructured) (*T, error) {
	var out T
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

var resourceCatalog = map[string]resourceEntry{
	"deployments": {resource: "deployments", project: func(u *unstructured.Unstructured) (any, error) {
		o, err := fromUnstructured[appsv1.Deployment](u)
		if err != nil {
			return nil, err
		}
		return kube.ToDeploymentView(o), nil
	}},
	"services": {resource: "services", project: func(u *unstructured.Unstructured) (any, error) {
		o, err := fromUnstructured[corev1.Service](u)
		if err != nil {
			return nil, err
		}
		return kube.ToServiceView(o), nil
	}},
	"ingresses": {resource: "ingresses", project: func(u *unstructured.Unstructured) (any, error) {
		o, err := fromUnstructured[networkingv1.Ingress](u)
		if err != nil {
			return nil, err
		}
		return kube.ToIngressView(o), nil
	}},
	"configmaps": {resource: "configmaps", project: func(u *unstructured.Unstructured) (any, error) {
		o, err := fromUnstructured[corev1.ConfigMap](u)
		if err != nil {
			return nil, err
		}
		return kube.ToConfigMapView(o), nil
	}},
	"statefulsets": {resource: "statefulsets", project: func(u *unstructured.Unstructured) (any, error) {
		o, err := fromUnstructured[appsv1.StatefulSet](u)
		if err != nil {
			return nil, err
		}
		return kube.ToStatefulSetView(o), nil
	}},
	"daemonsets": {resource: "daemonsets", project: func(u *unstructured.Unstructured) (any, error) {
		o, err := fromUnstructured[appsv1.DaemonSet](u)
		if err != nil {
			return nil, err
		}
		return kube.ToDaemonSetView(o), nil
	}},
	"replicasets": {resource: "replicasets", project: func(u *unstructured.Unstructured) (any, error) {
		o, err := fromUnstructured[appsv1.ReplicaSet](u)
		if err != nil {
			return nil, err
		}
		return kube.ToReplicaSetView(o), nil
	}},
	"jobs": {resource: "jobs", enrich: enrichJobs, project: func(u *unstructured.Unstructured) (any, error) {
		o, err := fromUnstructured[batchv1.Job](u)
		if err != nil {
			return nil, err
		}
		v := kube.ToJobView(o)
		return &v, nil // pointer so enrichJobs can annotate its status
	}},
	"cronjobs": {resource: "cronjobs", project: func(u *unstructured.Unstructured) (any, error) {
		o, err := fromUnstructured[batchv1.CronJob](u)
		if err != nil {
			return nil, err
		}
		return kube.ToCronJobView(o), nil
	}},
	"namespaces": {resource: "namespaces", project: func(u *unstructured.Unstructured) (any, error) {
		o, err := fromUnstructured[corev1.Namespace](u)
		if err != nil {
			return nil, err
		}
		return kube.ToNamespaceView(o), nil
	}},
	"secrets": {resource: "secrets", project: func(u *unstructured.Unstructured) (any, error) {
		o, err := fromUnstructured[corev1.Secret](u)
		if err != nil {
			return nil, err
		}
		return kube.ToSecretView(o), nil
	}},
	"nodes": {resource: "nodes", project: func(u *unstructured.Unstructured) (any, error) {
		o, err := fromUnstructured[corev1.Node](u)
		if err != nil {
			return nil, err
		}
		return nodeRow(o), nil
	}},
	"persistentvolumeclaims": {resource: "persistentvolumeclaims", enrich: enrichPVCMounts, project: func(u *unstructured.Unstructured) (any, error) {
		o, err := fromUnstructured[corev1.PersistentVolumeClaim](u)
		if err != nil {
			return nil, err
		}
		v := kube.ToPVCView(o)
		return &v, nil // pointer so enrichPVCMounts can annotate it
	}},
	"persistentvolumes": {resource: "persistentvolumes", project: func(u *unstructured.Unstructured) (any, error) {
		o, err := fromUnstructured[corev1.PersistentVolume](u)
		if err != nil {
			return nil, err
		}
		return kube.ToPVView(o), nil
	}},
	"storageclasses": {resource: "storageclasses", project: func(u *unstructured.Unstructured) (any, error) {
		o, err := fromUnstructured[storagev1.StorageClass](u)
		if err != nil {
			return nil, err
		}
		return kube.ToStorageClassView(o), nil
	}},
	"horizontalpodautoscalers": {resource: "horizontalpodautoscalers", project: func(u *unstructured.Unstructured) (any, error) {
		o, err := fromUnstructured[autoscalingv2.HorizontalPodAutoscaler](u)
		if err != nil {
			return nil, err
		}
		return kube.ToHPAView(o), nil
	}},
	"endpointslices": {resource: "endpointslices", project: func(u *unstructured.Unstructured) (any, error) {
		o, err := fromUnstructured[discoveryv1.EndpointSlice](u)
		if err != nil {
			return nil, err
		}
		return kube.ToEndpointSliceView(o), nil
	}},
	"networkpolicies": {resource: "networkpolicies", project: func(u *unstructured.Unstructured) (any, error) {
		o, err := fromUnstructured[networkingv1.NetworkPolicy](u)
		if err != nil {
			return nil, err
		}
		return kube.ToNetworkPolicyView(o), nil
	}},
	"ingressclasses": {resource: "ingressclasses", project: func(u *unstructured.Unstructured) (any, error) {
		o, err := fromUnstructured[networkingv1.IngressClass](u)
		if err != nil {
			return nil, err
		}
		return kube.ToIngressClassView(o), nil
	}},
	"serviceaccounts": {resource: "serviceaccounts", project: func(u *unstructured.Unstructured) (any, error) {
		o, err := fromUnstructured[corev1.ServiceAccount](u)
		if err != nil {
			return nil, err
		}
		return kube.ToServiceAccountView(o), nil
	}},
	"roles": {resource: "roles", project: func(u *unstructured.Unstructured) (any, error) {
		o, err := fromUnstructured[rbacv1.Role](u)
		if err != nil {
			return nil, err
		}
		return kube.ToRoleView(o), nil
	}},
	"clusterroles": {resource: "clusterroles", project: func(u *unstructured.Unstructured) (any, error) {
		o, err := fromUnstructured[rbacv1.ClusterRole](u)
		if err != nil {
			return nil, err
		}
		return kube.ToClusterRoleView(o), nil
	}},
	"rolebindings": {resource: "rolebindings", project: func(u *unstructured.Unstructured) (any, error) {
		o, err := fromUnstructured[rbacv1.RoleBinding](u)
		if err != nil {
			return nil, err
		}
		return kube.ToRoleBindingView(o), nil
	}},
	"clusterrolebindings": {resource: "clusterrolebindings", project: func(u *unstructured.Unstructured) (any, error) {
		o, err := fromUnstructured[rbacv1.ClusterRoleBinding](u)
		if err != nil {
			return nil, err
		}
		return kube.ToClusterRoleBindingView(o), nil
	}},
	"resourcequotas": {resource: "resourcequotas", project: func(u *unstructured.Unstructured) (any, error) {
		o, err := fromUnstructured[corev1.ResourceQuota](u)
		if err != nil {
			return nil, err
		}
		return kube.ToResourceQuotaView(o), nil
	}},
	"limitranges": {resource: "limitranges", project: func(u *unstructured.Unstructured) (any, error) {
		o, err := fromUnstructured[corev1.LimitRange](u)
		if err != nil {
			return nil, err
		}
		return kube.ToLimitRangeView(o), nil
	}},
	"poddisruptionbudgets": {resource: "poddisruptionbudgets", project: func(u *unstructured.Unstructured) (any, error) {
		o, err := fromUnstructured[policyv1.PodDisruptionBudget](u)
		if err != nil {
			return nil, err
		}
		return kube.ToPDBView(o), nil
	}},
	"priorityclasses": {resource: "priorityclasses", project: func(u *unstructured.Unstructured) (any, error) {
		o, err := fromUnstructured[schedulingv1.PriorityClass](u)
		if err != nil {
			return nil, err
		}
		return kube.ToPriorityClassView(o), nil
	}},
	"runtimeclasses": {resource: "runtimeclasses", project: func(u *unstructured.Unstructured) (any, error) {
		o, err := fromUnstructured[nodev1.RuntimeClass](u)
		if err != nil {
			return nil, err
		}
		return kube.ToRuntimeClassView(o), nil
	}},
}

// nodeView is the list projection of a Node (cluster-scoped). It lives here
// rather than in kube because it reuses the api-local nodeReady/nodeRoles.
type nodeView struct {
	Name    string `json:"name"`
	Status  string `json:"status"` // Ready | NotReady
	Roles   string `json:"roles"`
	Version string `json:"version"`
	Age     string `json:"age"`
}

func nodeRow(n *corev1.Node) nodeView {
	status := "NotReady"
	if nodeReady(n) {
		status = "Ready"
	}
	return nodeView{
		Name:    n.Name,
		Status:  status,
		Roles:   strings.Join(nodeRoles(n), ", "),
		Version: n.Status.NodeInfo.KubeletVersion,
		Age:     age(n.CreationTimestamp),
	}
}

// handleResourceList: GET /api/contexts/{ctx}/resources/{resource}?namespace=
// One generic lister for every catalogued resource, via the dynamic client.
func (s *Server) handleResourceList(w http.ResponseWriter, r *http.Request) {
	entry, ok := resourceCatalog[r.PathValue("resource")]
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Errorf("unknown resource %q", r.PathValue("resource")))
		return
	}
	res, err := s.mgr.ResolveResource(r.PathValue("ctx"), entry.resource)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	dyn, err := s.mgr.DynamicFor(r.PathValue("ctx"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := reqCtx(r)
	defer cancel()

	ns := ""
	if res.Namespaced {
		ns = namespaceParam(r)
	}
	list, err := dyn.Resource(res.GVR).Namespace(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	out := make([]any, 0, len(list.Items))
	for i := range list.Items {
		if v, perr := entry.project(&list.Items[i]); perr == nil {
			out = append(out, v)
		}
	}
	if entry.enrich != nil {
		entry.enrich(ctx, s, r.PathValue("ctx"), ns, out)
	}
	writeJSON(w, http.StatusOK, out)
}

// enrichJobs surfaces a Job's underlying pod problem (ImagePullBackOff, OOMKilled,
// …) as the job's status, since the Job object alone still reads "Running".
func enrichJobs(ctx context.Context, s *Server, contextName, ns string, rows []any) {
	client, err := s.mgr.ClientFor(contextName)
	if err != nil {
		return
	}
	pods, err := client.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return
	}
	problem := jobPodProblems(pods.Items)
	for _, row := range rows {
		jv, ok := row.(*kube.JobView)
		if !ok {
			continue
		}
		if r, has := problem[jv.Namespace+"/"+jv.Name]; has && jv.Status == "Running" {
			jv.Status = r
		}
	}
}

// jobPodProblems maps "namespace/job" to the first unrecoverable pod reason
// found among its pods (ImagePullBackOff, OOMKilled, ...).
func jobPodProblems(pods []corev1.Pod) map[string]string {
	problem := map[string]string{}
	for i := range pods {
		p := &pods[i]
		job := ownerJobName(p)
		if job == "" {
			continue
		}
		reason := kube.WaitingReason(p)
		switch reason {
		case "", "ContainerCreating", "PodInitializing", "Completed":
			continue // healthy / transient — not a job-blocking error
		}
		key := p.Namespace + "/" + job
		if _, seen := problem[key]; !seen {
			problem[key] = reason
		}
	}
	return problem
}

// ownerJobName returns the pod's owning Job name, if any.
func ownerJobName(p *corev1.Pod) string {
	for _, o := range p.OwnerReferences {
		if o.Controller != nil && *o.Controller && o.Kind == "Job" {
			return o.Name
		}
	}
	return ""
}

// enrichPVCMounts annotates each PVC with the pods that currently mount it (in
// the PVC's namespace). A Bound PVC with no mounting pod is normal — see
// enrichPVCConsumers — so this lets the list distinguish mounted from idle.
func enrichPVCMounts(ctx context.Context, s *Server, contextName, ns string, rows []any) {
	client, err := s.mgr.ClientFor(contextName)
	if err != nil {
		return
	}
	pods, err := client.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return
	}
	byClaim := pvcMountsByClaim(pods.Items)
	for _, row := range rows {
		pvc, ok := row.(*kube.PVCView)
		if !ok {
			continue
		}
		if mounts := byClaim[pvc.Namespace+"/"+pvc.Name]; len(mounts) > 0 {
			pvc.MountedBy = mounts
		}
	}
}

// pvcMountsByClaim scans every pod once and groups, by "namespace/claim" key,
// which pods mount which PVC and where (container + path) each mounts it.
func pvcMountsByClaim(pods []corev1.Pod) map[string][]kube.PVCMount {
	byClaim := map[string][]kube.PVCMount{}
	for i := range pods {
		p := &pods[i]
		volClaim := volumeClaimNames(p)
		if len(volClaim) == 0 {
			continue
		}
		perClaim := containerMountPoints(p, volClaim)
		// One entry per (pod, claim) — even if no container mounts it (rare).
		for _, ck := range uniqueValues(volClaim) {
			byClaim[ck] = append(byClaim[ck], kube.PVCMount{Pod: p.Name, Mounts: perClaim[ck]})
		}
	}
	return byClaim
}

// volumeClaimNames maps a pod's volume name to its "namespace/claim" key, for
// volumes backed by a PersistentVolumeClaim.
func volumeClaimNames(p *corev1.Pod) map[string]string {
	volClaim := map[string]string{}
	for _, v := range p.Spec.Volumes {
		if v.PersistentVolumeClaim != nil {
			volClaim[v.Name] = p.Namespace + "/" + v.PersistentVolumeClaim.ClaimName
		}
	}
	return volClaim
}

// containerMountPoints collects, per claim key, every (container, path) that
// mounts it — across both init and regular containers.
func containerMountPoints(p *corev1.Pod, volClaim map[string]string) map[string][]kube.PVCMountPoint {
	perClaim := map[string][]kube.PVCMountPoint{}
	addMounts := func(container string, mounts []corev1.VolumeMount) {
		for _, m := range mounts {
			if ck, ok := volClaim[m.Name]; ok {
				perClaim[ck] = append(perClaim[ck], kube.PVCMountPoint{Container: container, Path: m.MountPath})
			}
		}
	}
	for _, c := range p.Spec.InitContainers {
		addMounts(c.Name, c.VolumeMounts)
	}
	for _, c := range p.Spec.Containers {
		addMounts(c.Name, c.VolumeMounts)
	}
	return perClaim
}

// uniqueValues dedupes a map's values.
func uniqueValues(m map[string]string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(m))
	for _, v := range m {
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// namespaceParam reads the ?namespace= filter ("" == all namespaces).
func namespaceParam(r *http.Request) string {
	return r.URL.Query().Get("namespace")
}
