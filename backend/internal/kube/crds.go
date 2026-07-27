package kube

import (
	"context"
	"fmt"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CRDsFor lists every CustomResourceDefinition the cluster has registered —
// the basis for a generic CRD browser, as opposed to a hardcoded allowlist.
// Caches the apiextensions clientset per context, the same way DynamicFor
// caches the dynamic client.
func (m *Manager) CRDsFor(ctx context.Context, contextName string) ([]apiextensionsv1.CustomResourceDefinition, error) {
	m.mu.RLock()
	c, ok := m.apiextClients[contextName]
	m.mu.RUnlock()

	if !ok {
		cfg, err := m.RESTConfigFor(contextName)
		if err != nil {
			return nil, err
		}
		c, err = apiextensionsclientset.NewForConfig(cfg)
		if err != nil {
			return nil, fmt.Errorf("building apiextensions clientset for %q: %w", contextName, err)
		}
		m.mu.Lock()
		m.apiextClients[contextName] = c
		m.mu.Unlock()
	}

	list, err := c.ApiextensionsV1().CustomResourceDefinitions().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return list.Items, nil
}
