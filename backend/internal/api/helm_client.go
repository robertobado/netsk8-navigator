package api

import (
	"log"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/cli"
)

// helmSettings drives repo/chart-cache paths (~/.config/helm, ~/.cache/helm by
// default, or HELM_* env overrides) — the same paths the `helm` CLI itself
// uses, so repos added there are visible here and vice versa. Repos aren't
// per-cluster, so this is shared across every context.
var helmSettings = cli.New()

// helmRESTClientGetter adapts the app's per-context cluster access
// (clusterManager) to the interface Helm's SDK needs to build its own
// clients internally (kube.Factory, action.Configuration.Init).
type helmRESTClientGetter struct {
	mgr     clusterManager
	context string
}

func (g *helmRESTClientGetter) ToRESTConfig() (*rest.Config, error) {
	return g.mgr.RESTConfigFor(g.context)
}

func (g *helmRESTClientGetter) ToDiscoveryClient() (discovery.CachedDiscoveryInterface, error) {
	cfg, err := g.ToRESTConfig()
	if err != nil {
		return nil, err
	}
	dc, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return memory.NewMemCacheClient(dc), nil
}

func (g *helmRESTClientGetter) ToRESTMapper() (apimeta.RESTMapper, error) {
	return g.mgr.RESTMapperFor(g.context)
}

func (g *helmRESTClientGetter) ToRawKubeConfigLoader() clientcmd.ClientConfig {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	rules.ExplicitPath = g.mgr.ConfigPath()
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{CurrentContext: g.context})
}

var _ genericclioptions.RESTClientGetter = (*helmRESTClientGetter)(nil)

// newHelmConfig initializes a Helm action.Configuration for a cluster context
// and namespace, storing release state in Secrets (the same default `helm`
// itself uses) — no extra CRDs or storage of our own.
func (s *Server) newHelmConfig(contextName, namespace string) (*action.Configuration, error) {
	getter := &helmRESTClientGetter{mgr: s.mgr, context: contextName}
	cfg := new(action.Configuration)
	if err := cfg.Init(getter, namespace, "secret", helmDebugLog); err != nil {
		return nil, err
	}
	return cfg, nil
}

func helmDebugLog(format string, v ...any) {
	log.Printf("HELM "+format, v...)
}
