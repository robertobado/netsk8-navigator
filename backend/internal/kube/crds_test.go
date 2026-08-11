package kube

import (
	"context"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apiextensionsfake "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/fake"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestCRDsFor(t *testing.T) {
	crd := apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: "widgets.example.com"},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "example.com",
			Names: apiextensionsv1.CustomResourceDefinitionNames{Kind: "Widget", Plural: "widgets"},
			Scope: apiextensionsv1.NamespaceScoped,
		},
	}
	m := &Manager{apiextClients: map[string]apiextensionsclientset.Interface{
		"test": apiextensionsfake.NewSimpleClientset(&crd),
	}}

	got, err := m.CRDsFor(context.Background(), "test")
	if err != nil {
		t.Fatalf("CRDsFor() error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d CRDs, want 1", len(got))
	}
	if got[0].Name != "widgets.example.com" {
		t.Errorf("Name = %q, want widgets.example.com", got[0].Name)
	}
}

// TestCRDsFor_BuildsAndCachesClientOnFirstUse exercises the branch
// TestCRDsFor's pre-seeded apiextClients map skips: building the
// apiextensions clientset from the context's REST config on first use. The
// client is cached before the List call, so this holds regardless of
// whether that call (against managerWithContext's unroutable fake server)
// actually succeeds.
func TestCRDsFor_BuildsAndCachesClientOnFirstUse(t *testing.T) {
	m := managerWithContext("test-context")
	m.apiextClients = make(map[string]apiextensionsclientset.Interface)

	_, _ = m.CRDsFor(context.Background(), "test-context")
	if _, ok := m.apiextClients["test-context"]; !ok {
		t.Error("want the apiextensions clientset cached after the first call")
	}
}
