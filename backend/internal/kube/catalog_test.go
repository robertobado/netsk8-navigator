package kube

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// staticMapper builds a RESTMapper by hand (no discovery/network involved) so
// ResolveResource/ResolveGVK can be tested against a Manager whose mappers
// cache is pre-seeded — mirroring what a real discovery-backed RESTMapper
// would report for the given GVK/scope pairs.
func staticMapper(entries map[schema.GroupVersionKind]apimeta.RESTScope) apimeta.RESTMapper {
	versions := map[schema.GroupVersion]bool{}
	for gvk := range entries {
		versions[gvk.GroupVersion()] = true
	}
	gvs := make([]schema.GroupVersion, 0, len(versions))
	for gv := range versions {
		gvs = append(gvs, gv)
	}
	mapper := apimeta.NewDefaultRESTMapper(gvs)
	for gvk, scope := range entries {
		mapper.Add(gvk, scope)
	}
	return mapper
}

func TestResolveResource(t *testing.T) {
	mapper := staticMapper(map[schema.GroupVersionKind]apimeta.RESTScope{
		corev1.SchemeGroupVersion.WithKind("Pod"):  apimeta.RESTScopeNamespace,
		corev1.SchemeGroupVersion.WithKind("Node"): apimeta.RESTScopeRoot,
	})
	m := &Manager{mappers: map[string]apimeta.RESTMapper{"test-context": mapper}}

	t.Run("resolves a known namespaced resource", func(t *testing.T) {
		res, err := m.ResolveResource("test-context", "pods")
		if err != nil {
			t.Fatalf("ResolveResource() error: %v", err)
		}
		want := corev1.SchemeGroupVersion.WithResource("pods")
		if res.GVR != want || !res.Namespaced {
			t.Errorf("got %+v, want GVR=%v Namespaced=true", res, want)
		}
	})

	t.Run("resolves a known cluster-scoped resource", func(t *testing.T) {
		res, err := m.ResolveResource("test-context", "nodes")
		if err != nil {
			t.Fatalf("ResolveResource() error: %v", err)
		}
		want := corev1.SchemeGroupVersion.WithResource("nodes")
		if res.GVR != want || res.Namespaced {
			t.Errorf("got %+v, want GVR=%v Namespaced=false", res, want)
		}
	})

	t.Run("a resource the mapper doesn't know about errors", func(t *testing.T) {
		if _, err := m.ResolveResource("test-context", "frobnicators"); err == nil {
			t.Error("want an error for a resource with no known GVK")
		}
	})

	t.Run("an unknown context propagates RESTMapperFor's error", func(t *testing.T) {
		m2 := &Manager{
			rawConfig: clientcmdapi.Config{Contexts: map[string]*clientcmdapi.Context{}},
			mappers:   make(map[string]apimeta.RESTMapper),
			clients:   make(map[string]*kubernetes.Clientset),
		}
		if _, err := m2.ResolveResource("missing", "pods"); err == nil {
			t.Error("want an error when the context isn't in the kubeconfig")
		}
	})
}

func TestResolveGVK(t *testing.T) {
	mapper := staticMapper(map[schema.GroupVersionKind]apimeta.RESTScope{
		corev1.SchemeGroupVersion.WithKind("ConfigMap"): apimeta.RESTScopeNamespace,
		corev1.SchemeGroupVersion.WithKind("Namespace"): apimeta.RESTScopeRoot,
	})
	m := &Manager{mappers: map[string]apimeta.RESTMapper{"test-context": mapper}}

	t.Run("resolves a known namespaced GVK", func(t *testing.T) {
		res, err := m.ResolveGVK("test-context", corev1.SchemeGroupVersion.WithKind("ConfigMap"))
		if err != nil {
			t.Fatalf("ResolveGVK() error: %v", err)
		}
		want := corev1.SchemeGroupVersion.WithResource("configmaps")
		if res.GVR != want || !res.Namespaced {
			t.Errorf("got %+v, want GVR=%v Namespaced=true", res, want)
		}
	})

	t.Run("resolves a known cluster-scoped GVK", func(t *testing.T) {
		res, err := m.ResolveGVK("test-context", corev1.SchemeGroupVersion.WithKind("Namespace"))
		if err != nil {
			t.Fatalf("ResolveGVK() error: %v", err)
		}
		want := corev1.SchemeGroupVersion.WithResource("namespaces")
		if res.GVR != want || res.Namespaced {
			t.Errorf("got %+v, want GVR=%v Namespaced=false", res, want)
		}
	})

	t.Run("an unknown kind errors", func(t *testing.T) {
		if _, err := m.ResolveGVK("test-context", corev1.SchemeGroupVersion.WithKind("Frobnicator")); err == nil {
			t.Error("want an error for a kind the mapper doesn't know about")
		}
	})

	t.Run("an unknown context propagates RESTMapperFor's error", func(t *testing.T) {
		m2 := &Manager{
			rawConfig: clientcmdapi.Config{Contexts: map[string]*clientcmdapi.Context{}},
			mappers:   make(map[string]apimeta.RESTMapper),
			clients:   make(map[string]*kubernetes.Clientset),
		}
		if _, err := m2.ResolveGVK("missing", corev1.SchemeGroupVersion.WithKind("ConfigMap")); err == nil {
			t.Error("want an error when the context isn't in the kubeconfig")
		}
	})
}
