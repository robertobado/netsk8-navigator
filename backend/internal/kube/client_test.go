package kube

import (
	"os"
	"path/filepath"
	"testing"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func TestNewManager(t *testing.T) {
	t.Run("loads a valid kubeconfig with contexts", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config")
		kubeconfig := "apiVersion: v1\n" +
			"kind: Config\n" +
			"clusters:\n" +
			"- name: test-cluster\n" +
			"  cluster:\n" +
			"    server: https://example.com\n" +
			"contexts:\n" +
			"- name: test-context\n" +
			"  context:\n" +
			"    cluster: test-cluster\n" +
			"current-context: test-context\n"
		if err := os.WriteFile(path, []byte(kubeconfig), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("KUBECONFIG", path)

		m, err := NewManager()
		if err != nil {
			t.Fatalf("NewManager() error: %v", err)
		}
		if m.ConfigPath() != path {
			t.Errorf("ConfigPath() = %q, want %q", m.ConfigPath(), path)
		}
		contexts := m.Contexts()
		if len(contexts) != 1 || contexts[0].Name != "test-context" {
			t.Errorf("contexts = %+v", contexts)
		}
	})

	t.Run("an explicit KUBECONFIG that doesn't exist errors", func(t *testing.T) {
		t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "missing"))
		if _, err := NewManager(); err == nil {
			t.Error("want an error for a missing explicit KUBECONFIG")
		}
	})

	t.Run("no kubeconfig anywhere and not running in-cluster errors", func(t *testing.T) {
		// clientcmd's default ~/.kube/config candidate is resolved once at
		// package init and isn't affected by a later HOME override — so this
		// forces the "loaded, but zero contexts" path instead: a valid but
		// empty kubeconfig, explicitly pointed to, falls through NewManager's
		// switch to the in-cluster fallback, which then fails in a test env.
		path := filepath.Join(t.TempDir(), "empty-config")
		if err := os.WriteFile(path, []byte("apiVersion: v1\nkind: Config\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("KUBECONFIG", path)
		t.Setenv("KUBERNETES_SERVICE_HOST", "")
		if _, err := NewManager(); err == nil {
			t.Error("want an error when neither a kubeconfig nor in-cluster credentials are available")
		}
	})
}

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

func managerWithContext(name string) *Manager {
	return &Manager{
		rawConfig: clientcmdapi.Config{
			Clusters:  map[string]*clientcmdapi.Cluster{"c": {Server: "https://127.0.0.1:1"}},
			AuthInfos: map[string]*clientcmdapi.AuthInfo{"u": {}},
			Contexts:  map[string]*clientcmdapi.Context{name: {Cluster: "c", AuthInfo: "u"}},
		},
		clients:     make(map[string]*kubernetes.Clientset),
		restConfigs: make(map[string]*rest.Config),
		dynamics:    make(map[string]dynamic.Interface),
		mappers:     make(map[string]apimeta.RESTMapper),
	}
}

func TestDynamicFor(t *testing.T) {
	m := managerWithContext("test-context")
	d1, err := m.DynamicFor("test-context")
	if err != nil {
		t.Fatalf("DynamicFor() error: %v", err)
	}
	d2, err := m.DynamicFor("test-context")
	if err != nil {
		t.Fatalf("second DynamicFor() error: %v", err)
	}
	if d1 != d2 {
		t.Error("want the cached dynamic client on the second call")
	}
}

func TestRESTMapperFor(t *testing.T) {
	m := managerWithContext("test-context")
	mp1, err := m.RESTMapperFor("test-context")
	if err != nil {
		t.Fatalf("RESTMapperFor() error: %v", err)
	}
	mp2, err := m.RESTMapperFor("test-context")
	if err != nil {
		t.Fatalf("second RESTMapperFor() error: %v", err)
	}
	if mp1 != mp2 {
		t.Error("want the cached RESTMapper on the second call")
	}
}

func TestClientFor(t *testing.T) {
	t.Run("unknown context errors without building anything", func(t *testing.T) {
		m := managerWithContext("known")
		if _, err := m.ClientFor("unknown"); err == nil {
			t.Error("want an error for an unknown context")
		}
	})

	t.Run("builds a client for a known context and caches it", func(t *testing.T) {
		m := managerWithContext("test-context")
		c1, err := m.ClientFor("test-context")
		if err != nil {
			t.Fatalf("ClientFor() error: %v", err)
		}
		c2, err := m.ClientFor("test-context")
		if err != nil {
			t.Fatalf("second ClientFor() error: %v", err)
		}
		if c1 != c2 {
			t.Error("want the cached client on the second call, not a freshly built one")
		}
	})
}

func TestRESTConfigFor(t *testing.T) {
	m := managerWithContext("test-context")
	rc, err := m.RESTConfigFor("test-context")
	if err != nil {
		t.Fatalf("RESTConfigFor() error: %v", err)
	}
	if rc.Host != "https://127.0.0.1:1" {
		t.Errorf("Host = %q, want https://127.0.0.1:1", rc.Host)
	}
	rc2, err := m.RESTConfigFor("test-context")
	if err != nil {
		t.Fatalf("second RESTConfigFor() error: %v", err)
	}
	if rc != rc2 {
		t.Error("want the cached rest.Config on the second call")
	}
}
