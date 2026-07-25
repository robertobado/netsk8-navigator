// Package api exposes the HTTP surface consumed by the web frontend. It stays
// deliberately thin: translate a request into a client-go call for the selected
// context and shape the result into a UI-friendly JSON payload.
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/robertobado/netsk8-navigator/backend/internal/config"
	"github.com/robertobado/netsk8-navigator/backend/internal/kube"
)

// Server wires the kube manager and preferences store into an http.Handler.
type Server struct {
	mgr *kube.Manager
	cfg *config.Store

	monMu   sync.Mutex
	mon     map[string]monResult // context -> discovered Prometheus source (cached)
	msCache map[string]bool      // context -> metrics-server availability (cached)
}

func NewServer(mgr *kube.Manager, cfg *config.Store) *Server {
	return &Server{mgr: mgr, cfg: cfg, mon: make(map[string]monResult), msCache: make(map[string]bool)}
}

// Routes builds the mux. Go 1.22+ pattern routing means no external router dep.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/preferences", s.handleGetAppPrefs)
	mux.HandleFunc("PUT /api/preferences", s.handlePutAppPrefs)
	mux.HandleFunc("GET /api/contexts/{ctx}/preferences", s.handleGetClusterPrefs)
	mux.HandleFunc("PUT /api/contexts/{ctx}/preferences", s.handlePutClusterPrefs)
	mux.HandleFunc("GET /api/contexts", s.handleContexts)
	mux.HandleFunc("GET /api/contexts/{ctx}/namespaces", s.handleNamespaces)
	mux.HandleFunc("GET /api/contexts/{ctx}/nodes", s.handleNodes)
	mux.HandleFunc("GET /api/contexts/{ctx}/pods", s.handlePods)
	mux.HandleFunc("GET /api/contexts/{ctx}/watch/pods", s.handlePodWatch)
	mux.HandleFunc("GET /api/contexts/{ctx}/resources/{resource}", s.handleResourceList)
	mux.HandleFunc("GET /api/contexts/{ctx}/pods/{namespace}/{name}/pending", s.handlePodPending)
	mux.HandleFunc("GET /api/contexts/{ctx}/pods/{namespace}/{name}/logs", s.handlePodLogs)
	mux.HandleFunc("GET /api/contexts/{ctx}/pods/{namespace}/{name}/exec", s.handlePodExec)
	mux.HandleFunc("GET /api/contexts/{ctx}/pods/{namespace}/{name}/events", s.handleEvents)
	mux.HandleFunc("GET /api/contexts/{ctx}/events", s.handleAllEvents)
	mux.HandleFunc("GET /api/contexts/{ctx}/events/{namespace}/{name}", s.handleEvents)
	mux.HandleFunc("GET /api/contexts/{ctx}/detail/{kind}/{namespace}/{name}", s.handleDetail)
	mux.HandleFunc("GET /api/contexts/{ctx}/manifest/{kind}/{namespace}/{name}", s.handleGetManifest)
	mux.HandleFunc("PUT /api/contexts/{ctx}/manifest/{kind}/{namespace}/{name}", s.handleApplyManifest)
	mux.HandleFunc("GET /api/contexts/{ctx}/topology", s.handleTopology)
	mux.HandleFunc("GET /api/contexts/{ctx}/monitoring", s.handleMonitoring)
	mux.HandleFunc("GET /api/contexts/{ctx}/metrics/{scope}", s.handleMetrics)
	mux.HandleFunc("GET /api/contexts/{ctx}/usage/{scope}", s.handleUsage)
	mux.HandleFunc("GET /api/contexts/{ctx}/podusage", s.handlePodsUsage)
	mux.HandleFunc("GET /api/contexts/{ctx}/nodeusage", s.handleNodesUsage)
	mux.HandleFunc("GET /api/contexts/{ctx}/deploymentusage", s.handleDeploymentsUsage)
	mux.HandleFunc("GET /api/contexts/{ctx}/pods-of/{kind}/{namespace}/{name}", s.handleWorkloadPods)
	mux.HandleFunc("GET /api/contexts/{ctx}/node-workloads/{node}", s.handleNodeWorkloads)
	mux.HandleFunc("GET /api/contexts/{ctx}/namespace-summary/{namespace}", s.handleNamespaceSummary)
	mux.HandleFunc("GET /api/contexts/{ctx}/serviceaccount-usage/{namespace}/{name}", s.handleServiceAccountUsage)
	mux.HandleFunc("GET /api/contexts/{ctx}/consumers/{kind}/{namespace}/{name}", s.handleConsumers)
	mux.HandleFunc("GET /api/contexts/{ctx}/overview", s.handleOverview)
	mux.HandleFunc("GET /api/contexts/{ctx}/issues", s.handleIssues)
	mux.HandleFunc("GET /api/contexts/{ctx}/routekinds", s.handleRouteKinds)
	mux.HandleFunc("GET /api/contexts/{ctx}/crd/{group}/{version}/{resource}", s.handleCRDList)
	mux.HandleFunc("GET /api/contexts/{ctx}/crd/{group}/{version}/{resource}/{namespace}/{name}/manifest", s.handleCRDManifest)
	mux.HandleFunc("GET /api/contexts/{ctx}/crd/{group}/{version}/{resource}/{namespace}/{name}/detail", s.handleCRDDetail)
	return withCORS(withLogging(mux))
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "ok",
		"kubeconfig": s.mgr.ConfigPath(),
	})
}

func (s *Server) handleContexts(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.mgr.Contexts())
}

func (s *Server) handleNamespaces(w http.ResponseWriter, r *http.Request) {
	client, err := s.mgr.ClientFor(r.PathValue("ctx"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := reqCtx(r)
	defer cancel()

	list, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	out := make([]map[string]any, 0, len(list.Items))
	for i := range list.Items {
		ns := &list.Items[i]
		out = append(out, map[string]any{
			"name":   ns.Name,
			"status": string(ns.Status.Phase),
			"age":    age(ns.CreationTimestamp),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleNodes(w http.ResponseWriter, r *http.Request) {
	client, err := s.mgr.ClientFor(r.PathValue("ctx"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := reqCtx(r)
	defer cancel()

	list, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	out := make([]map[string]any, 0, len(list.Items))
	for i := range list.Items {
		n := &list.Items[i]
		out = append(out, map[string]any{
			"name":    n.Name,
			"ready":   nodeReady(n),
			"roles":   nodeRoles(n),
			"version": n.Status.NodeInfo.KubeletVersion,
			"cpu":     n.Status.Capacity.Cpu().String(),
			"memory":  n.Status.Capacity.Memory().String(),
			"age":     age(n.CreationTimestamp),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handlePods(w http.ResponseWriter, r *http.Request) {
	client, err := s.mgr.ClientFor(r.PathValue("ctx"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := reqCtx(r)
	defer cancel()

	namespace := r.URL.Query().Get("namespace") // "" == all namespaces
	list, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	out := make([]kube.PodView, 0, len(list.Items))
	for i := range list.Items {
		out = append(out, kube.ToPodView(&list.Items[i]))
	}
	writeJSON(w, http.StatusOK, out)
}

// handleOverview returns cheap cluster-wide counts for the dashboard landing.
func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	client, err := s.mgr.ClientFor(r.PathValue("ctx"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := reqCtx(r)
	defer cancel()

	nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	pods, err := client.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	namespaces, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}

	var running, pending, failed, readyNodes int
	for i := range pods.Items {
		switch kube.PodPhase(&pods.Items[i]) {
		case "Running", "Succeeded":
			running++
		case "Pending":
			pending++
		case "Failed":
			failed++
		}
	}
	for i := range nodes.Items {
		if nodeReady(&nodes.Items[i]) {
			readyNodes++
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"nodes":      len(nodes.Items),
		"readyNodes": readyNodes,
		"pods":       len(pods.Items),
		"namespaces": len(namespaces.Items),
		"running":    running,
		"pending":    pending,
		"failed":     failed,
	})
}

// --- helpers -------------------------------------------------------------

func reqCtx(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), 30*time.Second)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func age(t metav1.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Time.UTC().Format(time.RFC3339)
}

func nodeReady(n *corev1.Node) bool {
	for _, c := range n.Status.Conditions {
		if c.Type == corev1.NodeReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}

func nodeRoles(n *corev1.Node) []string {
	roles := []string{}
	const prefix = "node-role.kubernetes.io/"
	for label := range n.Labels {
		if len(label) > len(prefix) && label[:len(prefix)] == prefix {
			roles = append(roles, label[len(prefix):])
		}
	}
	if len(roles) == 0 {
		return []string{"<none>"}
	}
	return roles
}
