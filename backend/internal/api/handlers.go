// Package api exposes the HTTP surface consumed by the web frontend. It stays
// deliberately thin: translate a request into a client-go call for the selected
// context and shape the result into a UI-friendly JSON payload.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/robertobado/netsk8-navigator/backend/internal/config"
	"github.com/robertobado/netsk8-navigator/backend/internal/kube"
	"github.com/robertobado/netsk8-navigator/backend/internal/kubeconfig"
)

// clusterManager is the subset of *kube.Manager the API layer depends on, kept
// as an interface so handlers can be tested against client-go fakes instead of
// a live cluster. *kube.Manager satisfies it with no changes on its side.
type clusterManager interface {
	Contexts() []kube.ContextInfo
	ConfigPath() string
	ClientFor(contextName string) (kubernetes.Interface, error)
	DynamicFor(contextName string) (dynamic.Interface, error)
	ResolveResource(contextName, resource string) (kube.Resource, error)
	ResolveGVK(contextName string, gvk schema.GroupVersionKind) (kube.Resource, error)
	CRDsFor(ctx context.Context, contextName string) ([]apiextensionsv1.CustomResourceDefinition, error)
	RESTConfigFor(contextName string) (*rest.Config, error)
	RESTMapperFor(contextName string) (apimeta.RESTMapper, error)
	PodWatcherFor(contextName string) (*kube.PodWatcher, error)
	ExecInfoFor(contextName string) (command, profile string, ok bool)
	Reload() error
}

// Server wires the kube manager and preferences store into an http.Handler.
type Server struct {
	mgr        clusterManager
	cfg        *config.Store
	corsOrigin string
	upgrader   websocket.Upgrader

	// DemoMode disables pod exec and port-forward (no real kubelet to attach
	// to when the backend points at a simulated cluster, e.g. kwok) and is
	// reported via /api/health so the frontend can hide those affordances.
	DemoMode bool

	// Version is the running app's release version (main.version, stamped
	// via -ldflags at build time) — set by main() after NewServer, since
	// this package has no reason to otherwise know the app's own version.
	// Surfaced via /api/health and as the MCP server's serverInfo.version,
	// so both binaries, the MCP handshake, and the app bundle's own
	// metadata (see wails.json's info.productVersion) all report one
	// consistent number instead of three different ones.
	Version string

	// AuthEnabled reports whether AUTH_PASSWORD is set (HTTP Basic Auth
	// wraps the whole app) — set by main() after NewServer. Surfaced via
	// /api/health so the UI can warn when a sensitive toggle (MCP write
	// access) is being granted with no authentication in front of it.
	AuthEnabled bool

	monMu   sync.Mutex
	mon     map[string]monResult // context -> discovered Prometheus source (cached)
	msCache map[string]bool      // context -> metrics-server availability (cached)

	pfMu sync.Mutex
	pf   map[string]*pfSession // port-forward id -> active session

	updateChecker updateChecker

	// mcpFlags gates the /mcp endpoint (enabled) and its mutating tools
	// (allowWrite) — see mcpflags.go and mcp.go.
	mcpFlags *MCPFlags

	// kcfg edits the user's real kubeconfig — nil when running in-cluster
	// (no kubeconfig file to edit) or when internal/kubeconfig.NewEditor
	// failed to even read the file. See kubeconfig.go.
	kcfg *kubeconfig.Editor

	// appEv fans out native-code signals (e.g. the desktop app's "About"
	// menu item) to the frontend over SSE, standing in for Wails' JS bridge,
	// which this app never has present. See appevents.go.
	appEv appEvents

	// opener performs SetExternalOpener's native "open in the real browser"
	// call — nil in the plain server/browser binary. See externalopen.go.
	opener func(url string)
}

// SetKubeconfigEditor installs the kubeconfig-editing backend for
// /api/kubeconfig/*, mirroring SetMCPFlags — kept as a setter rather than a
// NewServer parameter so every existing call site (real and test) keeps
// compiling unchanged. Leave unset (nil) to keep /api/kubeconfig/* read-only
// (501).
func (s *Server) SetKubeconfigEditor(e *kubeconfig.Editor) {
	s.kcfg = e
}

// NewServer wires a Server. corsOrigin is the one extra origin (besides the
// documented dev-server ones) allowed to call the API cross-origin and to
// open the exec terminal's WebSocket — see CORS_ORIGIN in the README's
// Security model section. Pass "" for the default same-origin-only posture.
func NewServer(mgr clusterManager, cfg *config.Store, corsOrigin string) *Server {
	s := &Server{
		mgr: mgr, cfg: cfg, corsOrigin: corsOrigin,
		upgrader: websocket.Upgrader{CheckOrigin: wsOriginAllowed(corsOrigin)},
		mon:      make(map[string]monResult), msCache: make(map[string]bool),
		pf:       make(map[string]*pfSession),
		mcpFlags: &MCPFlags{},
	}
	// Hydrate from whatever was already persisted, so a previously-enabled
	// MCP toggle survives a process restart like every other preference.
	s.mcpFlags.applyFromAppPrefs(cfg.App())
	return s
}

// SetMCPFlags overrides the flags gating /mcp and the stdio transport,
// installed by --mcp-stdio in place of the preferences-derived default
// NewServer already set up — see newStdioMCPFlags.
func (s *Server) SetMCPFlags(f *MCPFlags) {
	s.mcpFlags = f
}

// Routes builds the mux. Go 1.22+ pattern routing means no external router dep.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/update-check", s.handleUpdateCheck)
	mux.HandleFunc("GET /api/preferences", s.handleGetAppPrefs)
	mux.HandleFunc("PUT /api/preferences", s.handlePutAppPrefs)
	mux.HandleFunc("GET /api/contexts/{ctx}/preferences", s.handleGetClusterPrefs)
	mux.HandleFunc("PUT /api/contexts/{ctx}/preferences", s.handlePutClusterPrefs)
	mux.HandleFunc("GET /api/mcp/token", s.handleGetMCPToken)
	mux.HandleFunc("POST /api/mcp/token/regenerate", s.handleRegenerateMCPToken)
	mux.HandleFunc("GET /api/app-events", s.handleAppEvents)
	mux.HandleFunc("POST /api/open-external", s.handleOpenExternal)
	mux.HandleFunc("GET /api/kubeconfig", s.handleKubeconfigView)
	mux.HandleFunc("PUT /api/kubeconfig/current-context", s.handleKubeconfigSetCurrentContext)
	mux.HandleFunc("PUT /api/kubeconfig/contexts/{name}", s.handleKubeconfigEditContext)
	mux.HandleFunc("POST /api/kubeconfig/contexts", s.handleKubeconfigCreateContext)
	mux.HandleFunc("DELETE /api/kubeconfig/contexts/{name}", s.handleKubeconfigDeleteContext)
	mux.HandleFunc("GET /api/kubeconfig/contexts/{name}/ping", s.handleKubeconfigPingContext)
	mux.HandleFunc("POST /api/kubeconfig/import/preview", s.handleKubeconfigImportPreview)
	mux.HandleFunc("POST /api/kubeconfig/import/commit", s.handleKubeconfigImportCommit)
	mux.HandleFunc("GET /api/kubeconfig/users/{name}/reveal", s.handleKubeconfigRevealSecret)
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
	mux.HandleFunc("DELETE /api/contexts/{ctx}/manifest/{kind}/{namespace}/{name}", s.handleDeleteResource)
	mux.HandleFunc("POST /api/contexts/{ctx}/create", s.handleCreateResource)
	mux.HandleFunc("PUT /api/contexts/{ctx}/scale/{kind}/{namespace}/{name}", s.handleScaleResource)
	mux.HandleFunc("POST /api/contexts/{ctx}/cordon/{name}", s.handleCordonNode)
	mux.HandleFunc("POST /api/contexts/{ctx}/rollout-restart/{kind}/{namespace}/{name}", s.handleRestartRollout)
	mux.HandleFunc("GET /api/contexts/{ctx}/rollout-history/{kind}/{namespace}/{name}", s.handleRolloutHistory)
	mux.HandleFunc("POST /api/contexts/{ctx}/rollout-undo/{kind}/{namespace}/{name}", s.handleRolloutUndo)
	mux.HandleFunc("POST /api/contexts/{ctx}/portforward/{namespace}/{name}", s.handleStartPortForward)
	mux.HandleFunc("DELETE /api/contexts/{ctx}/portforward/{id}", s.handleStopPortForward)
	mux.HandleFunc("GET /api/contexts/{ctx}/portforward", s.handleListPortForwards)
	mux.HandleFunc("GET /api/contexts/{ctx}/topology", s.handleTopology)
	mux.HandleFunc("GET /api/contexts/{ctx}/monitoring", s.handleMonitoring)
	mux.HandleFunc("GET /api/contexts/{ctx}/metrics/{scope}", s.handleMetrics)
	mux.HandleFunc("GET /api/contexts/{ctx}/usage/{scope}", s.handleUsage)
	mux.HandleFunc("GET /api/contexts/{ctx}/podusage", s.handlePodsUsage)
	mux.HandleFunc("GET /api/contexts/{ctx}/nodeusage", s.handleNodesUsage)
	mux.HandleFunc("GET /api/contexts/{ctx}/deploymentusage", s.handleDeploymentsUsage)
	mux.HandleFunc("GET /api/contexts/{ctx}/pods-of/{kind}/{namespace}/{name}", s.handleWorkloadPods)
	mux.HandleFunc("GET /api/contexts/{ctx}/pods-of/{kind}/{namespace}/{name}/logs", s.handleWorkloadLogs)
	mux.HandleFunc("GET /api/contexts/{ctx}/node-workloads/{node}", s.handleNodeWorkloads)
	mux.HandleFunc("GET /api/contexts/{ctx}/namespace-summary/{namespace}", s.handleNamespaceSummary)
	mux.HandleFunc("GET /api/contexts/{ctx}/serviceaccount-usage/{namespace}/{name}", s.handleServiceAccountUsage)
	mux.HandleFunc("GET /api/contexts/{ctx}/consumers/{kind}/{namespace}/{name}", s.handleConsumers)
	mux.HandleFunc("GET /api/contexts/{ctx}/overview", s.handleOverview)
	mux.HandleFunc("GET /api/contexts/{ctx}/issues", s.handleIssues)
	mux.HandleFunc("GET /api/contexts/{ctx}/routekinds", s.handleRouteKinds)
	mux.HandleFunc("GET /api/contexts/{ctx}/crdkinds", s.handleCRDKinds)
	mux.HandleFunc("GET /api/contexts/{ctx}/crd/{group}/{version}/{resource}", s.handleCRDList)
	mux.HandleFunc("PUT /api/contexts/{ctx}/crd/{group}/{version}/{resource}/{namespace}/{name}", s.handleCRDApply)
	mux.HandleFunc("DELETE /api/contexts/{ctx}/crd/{group}/{version}/{resource}/{namespace}/{name}", s.handleCRDDelete)
	mux.HandleFunc("GET /api/contexts/{ctx}/crd/{group}/{version}/{resource}/{namespace}/{name}/manifest", s.handleCRDManifest)
	mux.HandleFunc("GET /api/contexts/{ctx}/crd/{group}/{version}/{resource}/{namespace}/{name}/detail", s.handleCRDDetail)
	mux.HandleFunc("GET /api/contexts/{ctx}/helm/releases", s.handleHelmReleases)
	mux.HandleFunc("POST /api/contexts/{ctx}/helm/releases", s.handleHelmReleaseInstall)
	mux.HandleFunc("GET /api/contexts/{ctx}/helm/releases/{namespace}/{name}", s.handleHelmReleaseStatus)
	mux.HandleFunc("PUT /api/contexts/{ctx}/helm/releases/{namespace}/{name}", s.handleHelmReleaseUpgrade)
	mux.HandleFunc("DELETE /api/contexts/{ctx}/helm/releases/{namespace}/{name}", s.handleHelmReleaseUninstall)
	mux.HandleFunc("GET /api/contexts/{ctx}/helm/releases/{namespace}/{name}/manifest", s.handleHelmReleaseManifest)
	mux.HandleFunc("GET /api/contexts/{ctx}/helm/releases/{namespace}/{name}/history", s.handleHelmReleaseHistory)
	mux.HandleFunc("POST /api/contexts/{ctx}/helm/releases/{namespace}/{name}/rollback", s.handleHelmReleaseRollback)
	mux.HandleFunc("GET /api/helm/repos", s.handleHelmRepos)
	mux.HandleFunc("POST /api/helm/repos", s.handleAddHelmRepo)
	mux.HandleFunc("DELETE /api/helm/repos/{name}", s.handleRemoveHelmRepo)
	mux.HandleFunc("POST /api/helm/repos/{name}/refresh", s.handleRefreshHelmRepo)
	mux.HandleFunc("GET /api/helm/search", s.handleHelmSearch)
	mux.HandleFunc("GET /api/helm/charts/{repo}/{chart}", s.handleHelmChartDetail)
	return withCORS(s.corsOrigin, withLogging(mux))
}

// demoModeBlocked writes a 403 and returns true when DemoMode is on — used to
// gate pod exec and port-forward, which have no real kubelet to attach to
// when the backend points at a simulated cluster.
func (s *Server) demoModeBlocked(w http.ResponseWriter) bool {
	if !s.DemoMode {
		return false
	}
	writeError(w, http.StatusForbidden, fmt.Errorf("not available in demo mode"))
	return true
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":             "ok",
		"kubeconfig":         s.mgr.ConfigPath(),
		"demo":               s.DemoMode,
		"version":            s.Version,
		"authEnabled":        s.AuthEnabled,
		"kubeconfigEditable": s.kcfg != nil,
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
	list, err := client.CoreV1().Pods(namespace).List(ctx, listOptionsFromQuery(r))
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

// listOptionsFromQuery reads the optional ?labelSelector= and ?fieldSelector=
// filters shared by the pod, generic-resource, and CRD list endpoints, and
// returns them as ListOptions passed straight through to the API server — so
// the cluster does the filtering and only the matching objects come back,
// rather than listing everything and trimming here.
func listOptionsFromQuery(r *http.Request) metav1.ListOptions {
	return metav1.ListOptions{
		LabelSelector: r.URL.Query().Get("labelSelector"),
		FieldSelector: r.URL.Query().Get("fieldSelector"),
	}
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
