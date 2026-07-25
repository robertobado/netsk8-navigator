package kube

import (
	"testing"

	"k8s.io/client-go/rest"
)

func TestNewInClusterManager(t *testing.T) {
	m, err := newInClusterManager(&rest.Config{Host: "https://10.96.0.1:443"})
	if err != nil {
		t.Fatalf("newInClusterManager() error: %v", err)
	}

	if m.rawConfig.CurrentContext != inClusterContext {
		t.Errorf("CurrentContext = %q, want %q", m.rawConfig.CurrentContext, inClusterContext)
	}

	contexts := m.Contexts()
	if len(contexts) != 1 {
		t.Fatalf("got %d contexts, want 1", len(contexts))
	}
	c := contexts[0]
	if c.Name != inClusterContext || !c.Current {
		t.Errorf("context = %+v, want name=%q current=true", c, inClusterContext)
	}
	if c.Server != "https://10.96.0.1:443" {
		t.Errorf("Server = %q, want the rest.Config host", c.Server)
	}
	// No serviceaccount namespace file in a test environment, so it should
	// fall back to "default" rather than erroring.
	if c.Namespace != "default" {
		t.Errorf("Namespace = %q, want default", c.Namespace)
	}

	// The clientset/restConfig are pre-seeded, so ClientFor/RESTConfigFor must
	// return them without trying (and failing) to build from a kubeconfig.
	if _, err := m.ClientFor(inClusterContext); err != nil {
		t.Errorf("ClientFor(%q) error: %v", inClusterContext, err)
	}
	rc, err := m.RESTConfigFor(inClusterContext)
	if err != nil {
		t.Errorf("RESTConfigFor(%q) error: %v", inClusterContext, err)
	}
	if rc.Host != "https://10.96.0.1:443" {
		t.Errorf("RESTConfigFor host = %q", rc.Host)
	}
}
