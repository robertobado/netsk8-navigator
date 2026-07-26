package main

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

// logsGVR is kwok's own Logs CRD (kwok.x-k8s.io/v1alpha1), enabled on the
// demo cluster via `kwokctl create cluster --enable-crds=Logs`. It's how a
// kwok-simulated pod (no real kubelet) answers `kubectl logs`/our backend's
// GetLogs — see docs/DEMO_CLUSTER.md.
var logsGVR = schema.GroupVersionResource{Group: "kwok.x-k8s.io", Version: "v1alpha1", Resource: "logs"}

func dynamicClientFor(cfg *rest.Config) (dynamic.Interface, error) {
	return dynamic.NewForConfig(cfg)
}
