package kube

import (
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Resource is a resolved handle to a Kubernetes resource on a specific cluster:
// the GroupVersionResource the cluster serves plus whether it is namespaced.
type Resource struct {
	GVR        schema.GroupVersionResource
	Namespaced bool
}

// ResolveResource maps a plural resource name (e.g. "deployments", "ingresses",
// "httproutes") to the GVR the cluster actually serves, via its discovery-backed
// RESTMapper. This is what keeps the API layer version-agnostic: the mapper picks
// the served version (networking.k8s.io/v1 vs v1beta1, gateway v1 vs v1beta1, …).
func (m *Manager) ResolveResource(contextName, resource string) (Resource, error) {
	mapper, err := m.RESTMapperFor(contextName)
	if err != nil {
		return Resource{}, err
	}
	gvk, err := mapper.KindFor(schema.GroupVersionResource{Resource: resource})
	if err != nil {
		return Resource{}, err
	}
	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return Resource{}, err
	}
	return Resource{GVR: mapping.Resource, Namespaced: mapping.Scope.Name() == meta.RESTScopeNameNamespace}, nil
}
