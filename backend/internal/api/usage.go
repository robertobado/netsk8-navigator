package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// gauge is an instantaneous usage reading vs. a ceiling (0 = unbounded).
// For pods, Request/Limit carry the summed container reservations/ceilings so the
// UI can show both; Total mirrors the effective ceiling (limit, else request).
// For node/cluster only Total (allocatable) is set.
type gauge struct {
	Used    float64 `json:"used"`
	Request float64 `json:"request"`
	Limit   float64 `json:"limit"`
	Total   float64 `json:"total"`
	Unit    string  `json:"unit"`
}

type mUsage struct {
	CPU    resource.Quantity `json:"cpu"`
	Memory resource.Quantity `json:"memory"`
}

// metricsAPIPath is the metrics-server API group/version every usage handler
// reads from.
const metricsAPIPath = "/apis/metrics.k8s.io/v1beta1"

// podsMetricsPath returns the metrics-server pods endpoint, scoped to ns when set.
func podsMetricsPath(ns string) string {
	if ns == "" {
		return metricsAPIPath + "/pods"
	}
	return metricsAPIPath + "/namespaces/" + ns + "/pods"
}

// hasMetricsServer reports whether the Metrics API (metrics-server) is served,
// enabling instantaneous CPU/memory gauges even without Prometheus. Cached.
func (s *Server) hasMetricsServer(ctx context.Context, client kubernetes.Interface, name string) bool {
	s.monMu.Lock()
	if v, ok := s.msCache[name]; ok {
		s.monMu.Unlock()
		return v
	}
	s.monMu.Unlock()

	_, err := client.CoreV1().RESTClient().Get().AbsPath(metricsAPIPath).DoRaw(ctx)
	ok := err == nil
	s.monMu.Lock()
	s.msCache[name] = ok
	s.monMu.Unlock()
	return ok
}

// handleUsage: GET /api/contexts/{ctx}/usage/{scope}?namespace=&name=
func (s *Server) handleUsage(w http.ResponseWriter, r *http.Request) {
	client, err := s.mgr.ClientFor(r.PathValue("ctx"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := reqCtx(r)
	defer cancel()

	if !s.hasMetricsServer(ctx, client, r.PathValue("ctx")) {
		writeJSON(w, http.StatusOK, map[string]any{"available": false})
		return
	}

	q := r.URL.Query()
	cpu, mem, err := usageFor(ctx, client, r.PathValue("scope"), q.Get("namespace"), q.Get("name"))
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"available": true, "cpu": cpu, "memory": mem})
}

// podUsageEntry is a pod's CPU + memory gauges, keyed by "<namespace>/<name>".
type podUsageEntry struct {
	CPU    gauge `json:"cpu"`
	Memory gauge `json:"memory"`
}

// handlePodsUsage: GET /api/contexts/{ctx}/podusage?namespace=
// Returns per-pod CPU/memory usage vs. request/limit for every pod in one shot,
// so the pod table can render inline mini gauges without one call per row.
func (s *Server) handlePodsUsage(w http.ResponseWriter, r *http.Request) {
	client, err := s.mgr.ClientFor(r.PathValue("ctx"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := reqCtx(r)
	defer cancel()

	if !s.hasMetricsServer(ctx, client, r.PathValue("ctx")) {
		writeJSON(w, http.StatusOK, map[string]any{"available": false})
		return
	}
	ns := r.URL.Query().Get("namespace") // "" == all namespaces

	items, err := livePodUsage(ctx, client, ns)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	addPodRequestsLimits(ctx, client, ns, items)

	writeJSON(w, http.StatusOK, map[string]any{"available": true, "items": items})
}

// livePodUsage fetches live per-pod CPU/memory usage in a single
// metrics-server call, keyed by "namespace/pod".
func livePodUsage(ctx context.Context, client kubernetes.Interface, ns string) (map[string]podUsageEntry, error) {
	raw, err := client.CoreV1().RESTClient().Get().AbsPath(podsMetricsPath(ns)).DoRaw(ctx)
	if err != nil {
		return nil, err
	}
	var pm struct {
		Items []struct {
			Metadata struct {
				Name      string `json:"name"`
				Namespace string `json:"namespace"`
			} `json:"metadata"`
			Containers []struct {
				Usage mUsage `json:"usage"`
			} `json:"containers"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &pm); err != nil {
		return nil, err
	}
	items := make(map[string]podUsageEntry, len(pm.Items))
	for _, it := range pm.Items {
		var e podUsageEntry
		e.CPU.Unit, e.Memory.Unit = "cores", "bytes"
		for _, c := range it.Containers {
			e.CPU.Used += c.Usage.CPU.AsApproximateFloat64()
			e.Memory.Used += c.Usage.Memory.AsApproximateFloat64()
		}
		items[it.Metadata.Namespace+"/"+it.Metadata.Name] = e
	}
	return items, nil
}

// addPodRequestsLimits fills in each pod's request/limit/total from its spec
// (one list call), leaving pods missing from items (e.g. a metrics-server lag)
// with a zeroed usage but correct request/limit.
func addPodRequestsLimits(ctx context.Context, client kubernetes.Interface, ns string, items map[string]podUsageEntry) {
	pods, err := client.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return
	}
	for i := range pods.Items {
		p := &pods.Items[i]
		key := p.Namespace + "/" + p.Name
		e, ok := items[key]
		if !ok {
			e.CPU.Unit, e.Memory.Unit = "cores", "bytes"
		}
		var limCPU, limMem, reqCPU, reqMem float64
		for _, c := range p.Spec.Containers {
			limCPU += c.Resources.Limits.Cpu().AsApproximateFloat64()
			limMem += c.Resources.Limits.Memory().AsApproximateFloat64()
			reqCPU += c.Resources.Requests.Cpu().AsApproximateFloat64()
			reqMem += c.Resources.Requests.Memory().AsApproximateFloat64()
		}
		e.CPU.Request, e.CPU.Limit, e.CPU.Total = reqCPU, limCPU, pickCeiling(limCPU, reqCPU)
		e.Memory.Request, e.Memory.Limit, e.Memory.Total = reqMem, limMem, pickCeiling(limMem, reqMem)
		items[key] = e
	}
}

// nodeUsageItem is a node's CPU + memory gauges (used vs. allocatable).
type nodeUsageItem struct {
	Name   string `json:"name"`
	CPU    gauge  `json:"cpu"`
	Memory gauge  `json:"memory"`
}

// handleNodesUsage: GET /api/contexts/{ctx}/nodeusage
// Per-node CPU/memory usage vs. allocatable, sorted by peak utilization (desc).
func (s *Server) handleNodesUsage(w http.ResponseWriter, r *http.Request) {
	client, err := s.mgr.ClientFor(r.PathValue("ctx"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := reqCtx(r)
	defer cancel()

	if !s.hasMetricsServer(ctx, client, r.PathValue("ctx")) {
		writeJSON(w, http.StatusOK, map[string]any{"available": false})
		return
	}

	raw, err := client.CoreV1().RESTClient().Get().AbsPath(metricsAPIPath + "/nodes").DoRaw(ctx)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	var nm struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Usage mUsage `json:"usage"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &nm); err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}

	used := make(map[string]mUsage, len(nm.Items))
	for _, it := range nm.Items {
		used[it.Metadata.Name] = it.Usage
	}

	items := []nodeUsageItem{}
	if nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{}); err == nil {
		for i := range nodes.Items {
			n := &nodes.Items[i]
			u := used[n.Name]
			it := nodeUsageItem{Name: n.Name}
			it.CPU = gauge{Used: u.CPU.AsApproximateFloat64(), Total: n.Status.Allocatable.Cpu().AsApproximateFloat64(), Unit: "cores"}
			it.Memory = gauge{Used: u.Memory.AsApproximateFloat64(), Total: n.Status.Allocatable.Memory().AsApproximateFloat64(), Unit: "bytes"}
			items = append(items, it)
		}
	}
	sort.Slice(items, func(i, j int) bool { return nodePeak(items[i]) > nodePeak(items[j]) })

	writeJSON(w, http.StatusOK, map[string]any{"available": true, "items": items})
}

// nodePeak is a node's most-pressured resource ratio (used for sorting).
func nodePeak(n nodeUsageItem) float64 {
	c, m := 0.0, 0.0
	if n.CPU.Total > 0 {
		c = n.CPU.Used / n.CPU.Total
	}
	if n.Memory.Total > 0 {
		m = n.Memory.Used / n.Memory.Total
	}
	if c > m {
		return c
	}
	return m
}

// handleDeploymentsUsage: GET /api/contexts/{ctx}/deploymentusage?namespace=
// Per-deployment CPU/memory usage vs. request/limit, aggregated from the pods it
// owns (Deployment → ReplicaSet → Pod), keyed by "<namespace>/<name>".
func (s *Server) handleDeploymentsUsage(w http.ResponseWriter, r *http.Request) {
	client, err := s.mgr.ClientFor(r.PathValue("ctx"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := reqCtx(r)
	defer cancel()

	if !s.hasMetricsServer(ctx, client, r.PathValue("ctx")) {
		writeJSON(w, http.StatusOK, map[string]any{"available": false})
		return
	}
	ns := r.URL.Query().Get("namespace")

	usedCPU, usedMem, err := podUsageMaps(ctx, client, ns)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	rsToDeploy := replicaSetOwners(ctx, client, ns)
	items := aggregateDeploymentUsage(ctx, client, ns, rsToDeploy, usedCPU, usedMem)
	finalizeUsageTotals(items)

	writeJSON(w, http.StatusOK, map[string]any{"available": true, "items": items})
}

// podUsageMaps fetches live per-pod CPU/memory usage in a single
// metrics-server call, keyed by "namespace/pod".
func podUsageMaps(ctx context.Context, client kubernetes.Interface, ns string) (usedCPU, usedMem map[string]float64, err error) {
	raw, err := client.CoreV1().RESTClient().Get().AbsPath(podsMetricsPath(ns)).DoRaw(ctx)
	if err != nil {
		return nil, nil, err
	}
	var pm struct {
		Items []struct {
			Metadata   struct{ Name, Namespace string } `json:"metadata"`
			Containers []struct {
				Usage mUsage `json:"usage"`
			} `json:"containers"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &pm); err != nil {
		return nil, nil, err
	}
	usedCPU, usedMem = map[string]float64{}, map[string]float64{}
	for _, it := range pm.Items {
		k := it.Metadata.Namespace + "/" + it.Metadata.Name
		for _, c := range it.Containers {
			usedCPU[k] += c.Usage.CPU.AsApproximateFloat64()
			usedMem[k] += c.Usage.Memory.AsApproximateFloat64()
		}
	}
	return usedCPU, usedMem, nil
}

// replicaSetOwners maps "namespace/replicaset" to its owning Deployment's name.
func replicaSetOwners(ctx context.Context, client kubernetes.Interface, ns string) map[string]string {
	rsToDeploy := map[string]string{}
	rss, err := client.AppsV1().ReplicaSets(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return rsToDeploy
	}
	for i := range rss.Items {
		rs := &rss.Items[i]
		if deploy := controllerOwnerName(rs.OwnerReferences, "Deployment"); deploy != "" {
			rsToDeploy[rs.Namespace+"/"+rs.Name] = deploy
		}
	}
	return rsToDeploy
}

// aggregateDeploymentUsage sums each pod's usage/requests/limits into its
// owning Deployment (Pod → ReplicaSet → Deployment).
func aggregateDeploymentUsage(
	ctx context.Context, client kubernetes.Interface, ns string, rsToDeploy map[string]string, usedCPU, usedMem map[string]float64,
) map[string]podUsageEntry {
	items := map[string]podUsageEntry{}
	pods, err := client.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return items
	}
	for i := range pods.Items {
		p := &pods.Items[i]
		rsName := controllerOwnerName(p.OwnerReferences, "ReplicaSet")
		if rsName == "" {
			continue
		}
		deploy := rsToDeploy[p.Namespace+"/"+rsName]
		if deploy == "" {
			continue
		}
		key := p.Namespace + "/" + deploy
		e := items[key]
		e.CPU.Unit, e.Memory.Unit = "cores", "bytes"
		pk := p.Namespace + "/" + p.Name
		e.CPU.Used += usedCPU[pk]
		e.Memory.Used += usedMem[pk]
		for _, c := range p.Spec.Containers {
			e.CPU.Request += c.Resources.Requests.Cpu().AsApproximateFloat64()
			e.CPU.Limit += c.Resources.Limits.Cpu().AsApproximateFloat64()
			e.Memory.Request += c.Resources.Requests.Memory().AsApproximateFloat64()
			e.Memory.Limit += c.Resources.Limits.Memory().AsApproximateFloat64()
		}
		items[key] = e
	}
	return items
}

// finalizeUsageTotals fills in each entry's Total (limit, else request) now
// that every pod has been aggregated.
func finalizeUsageTotals(items map[string]podUsageEntry) {
	for k, e := range items {
		e.CPU.Total = pickCeiling(e.CPU.Limit, e.CPU.Request)
		e.Memory.Total = pickCeiling(e.Memory.Limit, e.Memory.Request)
		items[k] = e
	}
}

// controllerOwnerName returns the name of the owner reference of kind that is
// the controller, if any.
func controllerOwnerName(refs []metav1.OwnerReference, kind string) string {
	for _, o := range refs {
		if o.Controller != nil && *o.Controller && o.Kind == kind {
			return o.Name
		}
	}
	return ""
}

func usageFor(ctx context.Context, client kubernetes.Interface, scope, ns, name string) (cpu, mem gauge, err error) {
	cpu.Unit, mem.Unit = "cores", "bytes"
	switch scope {
	case "node":
		return nodeUsage(ctx, client, name)
	case "pod":
		return podUsage(ctx, client, ns, name)
	case "cluster":
		return clusterUsage(ctx, client)
	}
	return cpu, mem, errUnknownScope
}

func nodeUsage(ctx context.Context, client kubernetes.Interface, name string) (cpu, mem gauge, err error) {
	cpu.Unit, mem.Unit = "cores", "bytes"
	raw, err := client.CoreV1().RESTClient().Get().AbsPath(metricsAPIPath + "/nodes/" + name).DoRaw(ctx)
	if err != nil {
		return cpu, mem, err
	}
	var m struct {
		Usage mUsage `json:"usage"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return cpu, mem, err
	}
	cpu.Used = m.Usage.CPU.AsApproximateFloat64()
	mem.Used = m.Usage.Memory.AsApproximateFloat64()

	if node, err := client.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{}); err == nil {
		cpu.Total = node.Status.Allocatable.Cpu().AsApproximateFloat64()
		mem.Total = node.Status.Allocatable.Memory().AsApproximateFloat64()
	}
	return cpu, mem, nil
}

func podUsage(ctx context.Context, client kubernetes.Interface, ns, name string) (cpu, mem gauge, err error) {
	cpu.Unit, mem.Unit = "cores", "bytes"
	raw, err := client.CoreV1().RESTClient().Get().AbsPath(podsMetricsPath(ns) + "/" + name).DoRaw(ctx)
	if err != nil {
		return cpu, mem, err
	}
	var m struct {
		Containers []struct {
			Usage mUsage `json:"usage"`
		} `json:"containers"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return cpu, mem, err
	}
	for _, c := range m.Containers {
		cpu.Used += c.Usage.CPU.AsApproximateFloat64()
		mem.Used += c.Usage.Memory.AsApproximateFloat64()
	}

	// Ceiling = sum of container limits (fall back to requests).
	if pod, err := client.CoreV1().Pods(ns).Get(ctx, name, metav1.GetOptions{}); err == nil {
		var limCPU, limMem, reqCPU, reqMem float64
		for _, c := range pod.Spec.Containers {
			limCPU += c.Resources.Limits.Cpu().AsApproximateFloat64()
			limMem += c.Resources.Limits.Memory().AsApproximateFloat64()
			reqCPU += c.Resources.Requests.Cpu().AsApproximateFloat64()
			reqMem += c.Resources.Requests.Memory().AsApproximateFloat64()
		}
		cpu.Request, cpu.Limit = reqCPU, limCPU
		mem.Request, mem.Limit = reqMem, limMem
		cpu.Total = pickCeiling(limCPU, reqCPU)
		mem.Total = pickCeiling(limMem, reqMem)
	}
	return cpu, mem, nil
}

func clusterUsage(ctx context.Context, client kubernetes.Interface) (cpu, mem gauge, err error) {
	cpu.Unit, mem.Unit = "cores", "bytes"
	raw, err := client.CoreV1().RESTClient().Get().AbsPath(metricsAPIPath + "/nodes").DoRaw(ctx)
	if err != nil {
		return cpu, mem, err
	}
	var m struct {
		Items []struct {
			Usage mUsage `json:"usage"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return cpu, mem, err
	}
	for _, it := range m.Items {
		cpu.Used += it.Usage.CPU.AsApproximateFloat64()
		mem.Used += it.Usage.Memory.AsApproximateFloat64()
	}
	if nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{}); err == nil {
		for i := range nodes.Items {
			n := &nodes.Items[i]
			cpu.Total += n.Status.Allocatable.Cpu().AsApproximateFloat64()
			mem.Total += n.Status.Allocatable.Memory().AsApproximateFloat64()
		}
	}
	return cpu, mem, nil
}

func pickCeiling(limit, request float64) float64 {
	if limit > 0 {
		return limit
	}
	return request
}

var errUnknownScope = &scopeError{}

type scopeError struct{}

func (*scopeError) Error() string { return "unknown scope" }
