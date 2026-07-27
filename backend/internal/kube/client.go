// Package kube handles kubeconfig loading, context enumeration and building
// per-context Kubernetes clientsets. It relies on client-go's clientcmd so that
// exec-based credential plugins (aws eks get-token, gke-gcloud-auth-plugin,
// OIDC, etc.) are resolved natively — the reason we picked Go for the backend.
package kube

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	apiextensionsclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// inClusterContext is the synthetic context name used when the app falls back
// to its own pod's service account credentials (see NewManager).
const inClusterContext = "in-cluster"

// Manager owns the loaded kubeconfig and caches per-context clients so we don't
// rebuild REST configs (and re-run exec auth plugins) on every request. Besides
// the typed clientset it holds a dynamic client and a discovery-backed RESTMapper,
// which let the API layer work with any resource — core, CRDs, any served version.
type Manager struct {
	mu            sync.RWMutex
	rawConfig     clientcmdapi.Config
	configPath    string
	clients       map[string]*kubernetes.Clientset
	restConfigs   map[string]*rest.Config
	dynamics      map[string]dynamic.Interface
	mappers       map[string]meta.RESTMapper
	watchers      map[string]*PodWatcher
	apiextClients map[string]apiextensionsclientset.Interface
}

// ContextInfo is a UI-friendly view of a single kubeconfig context.
type ContextInfo struct {
	Name      string `json:"name"`
	Cluster   string `json:"cluster"`
	User      string `json:"user"`
	Namespace string `json:"namespace"`
	Server    string `json:"server"`
	Current   bool   `json:"current"`
}

// NewManager loads the kubeconfig using the standard loading rules (respects
// the KUBECONFIG env var, falling back to ~/.kube/config). If no kubeconfig
// was explicitly requested and none is found — the common case for a pod
// running inside a cluster, where there's no kubeconfig file at all — it
// falls back to the pod's own in-cluster service account credentials and
// talks to the cluster it's running in, exposed as a single synthetic
// "in-cluster" context.
func NewManager() (*Manager, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	raw, err := rules.Load()
	explicit := rules.ExplicitPath != ""
	switch {
	case err != nil && explicit:
		return nil, fmt.Errorf("loading kubeconfig from %s: %w", rules.ExplicitPath, err)
	case err == nil && len(raw.Contexts) > 0:
		return &Manager{
			rawConfig:     *raw,
			configPath:    rules.GetDefaultFilename(),
			clients:       make(map[string]*kubernetes.Clientset),
			restConfigs:   make(map[string]*rest.Config),
			dynamics:      make(map[string]dynamic.Interface),
			mappers:       make(map[string]meta.RESTMapper),
			apiextClients: make(map[string]apiextensionsclientset.Interface),
		}, nil
	}

	inClusterCfg, icErr := rest.InClusterConfig()
	if icErr != nil {
		return nil, fmt.Errorf("no kubeconfig found (set KUBECONFIG to use one) and not running inside a cluster: %w", icErr)
	}
	return newInClusterManager(inClusterCfg)
}

// newInClusterManager wraps the pod's own service-account rest.Config as a
// single-context Manager, so the rest of the app (which is built around
// kubeconfig-style named contexts) needs no special-casing.
func newInClusterManager(cfg *rest.Config) (*Manager, error) {
	cfg.QPS = 50
	cfg.Burst = 100
	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("building in-cluster clientset: %w", err)
	}

	ns := "default"
	if b, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace"); err == nil {
		if s := strings.TrimSpace(string(b)); s != "" {
			ns = s
		}
	}

	return &Manager{
		rawConfig: clientcmdapi.Config{
			CurrentContext: inClusterContext,
			Contexts: map[string]*clientcmdapi.Context{
				inClusterContext: {Cluster: inClusterContext, AuthInfo: inClusterContext, Namespace: ns},
			},
			Clusters: map[string]*clientcmdapi.Cluster{
				inClusterContext: {Server: cfg.Host},
			},
		},
		configPath:    "in-cluster (service account)",
		clients:       map[string]*kubernetes.Clientset{inClusterContext: clientset},
		restConfigs:   map[string]*rest.Config{inClusterContext: cfg},
		dynamics:      make(map[string]dynamic.Interface),
		mappers:       make(map[string]meta.RESTMapper),
		apiextClients: make(map[string]apiextensionsclientset.Interface),
	}, nil
}

// Contexts returns all contexts declared in the kubeconfig, sorted by name.
func (m *Manager) Contexts() []ContextInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]ContextInfo, 0, len(m.rawConfig.Contexts))
	for name, ctx := range m.rawConfig.Contexts {
		info := ContextInfo{
			Name:      name,
			Cluster:   ctx.Cluster,
			User:      ctx.AuthInfo,
			Namespace: ctx.Namespace,
			Current:   name == m.rawConfig.CurrentContext,
		}
		if cluster, ok := m.rawConfig.Clusters[ctx.Cluster]; ok {
			info.Server = cluster.Server
		}
		if info.Namespace == "" {
			info.Namespace = "default"
		}
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ClientFor returns a cached clientset for the given context name, building it
// (and running any exec auth plugin) on first use. Returns the kubernetes.Interface
// (rather than the concrete *Clientset) so the API layer can depend on an
// interface and be tested against client-go's fake clientset.
func (m *Manager) ClientFor(contextName string) (kubernetes.Interface, error) {
	m.mu.RLock()
	if c, ok := m.clients[contextName]; ok {
		m.mu.RUnlock()
		return c, nil
	}
	m.mu.RUnlock()

	if _, ok := m.rawConfig.Contexts[contextName]; !ok {
		return nil, fmt.Errorf("context %q not found in kubeconfig", contextName)
	}

	override := &clientcmd.ConfigOverrides{CurrentContext: contextName}
	clientConfig := clientcmd.NewNonInteractiveClientConfig(
		m.rawConfig, contextName, override, clientcmd.NewDefaultClientConfigLoadingRules())

	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("building rest config for %q: %w", contextName, err)
	}
	// Sensible defaults for an interactive UI backend.
	restConfig.QPS = 50
	restConfig.Burst = 100

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("building clientset for %q: %w", contextName, err)
	}

	m.mu.Lock()
	m.clients[contextName] = clientset
	m.restConfigs[contextName] = restConfig
	m.mu.Unlock()
	return clientset, nil
}

// RESTConfigFor returns the *rest.Config for a context, building it (via
// ClientFor) if needed. Used by the exec bridge, which needs the raw config
// to construct a SPDY executor.
func (m *Manager) RESTConfigFor(contextName string) (*rest.Config, error) {
	m.mu.RLock()
	if c, ok := m.restConfigs[contextName]; ok {
		m.mu.RUnlock()
		return c, nil
	}
	m.mu.RUnlock()

	if _, err := m.ClientFor(contextName); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.restConfigs[contextName], nil
}

// DynamicFor returns a cached dynamic client for the context — the single client
// used to list/get/update any resource (typed kinds and CRDs alike).
func (m *Manager) DynamicFor(contextName string) (dynamic.Interface, error) {
	m.mu.RLock()
	if d, ok := m.dynamics[contextName]; ok {
		m.mu.RUnlock()
		return d, nil
	}
	m.mu.RUnlock()

	cfg, err := m.RESTConfigFor(contextName)
	if err != nil {
		return nil, err
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("building dynamic client for %q: %w", contextName, err)
	}
	m.mu.Lock()
	m.dynamics[contextName] = dyn
	m.mu.Unlock()
	return dyn, nil
}

// RESTMapperFor returns a cached discovery-backed RESTMapper for the context. It
// resolves a Kind/resource to the GroupVersionResource the cluster actually
// serves, so the API layer stays correct across Kubernetes versions.
func (m *Manager) RESTMapperFor(contextName string) (meta.RESTMapper, error) {
	m.mu.RLock()
	if mp, ok := m.mappers[contextName]; ok {
		m.mu.RUnlock()
		return mp, nil
	}
	m.mu.RUnlock()

	client, err := m.ClientFor(contextName)
	if err != nil {
		return nil, err
	}
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(client.Discovery()))
	m.mu.Lock()
	m.mappers[contextName] = mapper
	m.mu.Unlock()
	return mapper, nil
}

// ConfigPath exposes the resolved kubeconfig path (handy for the UI header).
func (m *Manager) ConfigPath() string { return m.configPath }
