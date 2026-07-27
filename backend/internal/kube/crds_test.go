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
