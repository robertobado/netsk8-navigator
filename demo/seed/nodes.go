package main

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// demoNode describes one fake node to create. chaos marks the node with
// ../kwok/stages.yaml's node-not-ready label, so the app's Issues carousel
// has something real to show.
type demoNode struct {
	name  string
	zone  string
	chaos bool
}

var demoNodes = []demoNode{
	{name: "node-a1", zone: "us-east-1a"},
	{name: "node-a2", zone: "us-east-1a"},
	{name: "node-b1", zone: "us-east-1b"},
	{name: "node-b2", zone: "us-east-1b", chaos: true},
}

func seedNodes(ctx context.Context, client kubernetes.Interface) error {
	for _, n := range demoNodes {
		labels := map[string]string{
			"kubernetes.io/hostname":           n.name,
			"topology.kubernetes.io/zone":      n.zone,
			"node.kubernetes.io/instance-type": "kwok",
		}
		if n.chaos {
			labels["node-not-ready.stage.kwok.x-k8s.io"] = "true"
		}
		// Tells a real metrics-server (v0.7.0+) to scrape this node's
		// resource usage at kwok's custom path instead of the standard
		// /metrics/resource — see ../kwok/metrics.yaml's Metric resource,
		// which is what actually answers requests at this path.
		annotations := map[string]string{
			"metrics.k8s.io/resource-metrics-path": fmt.Sprintf("/metrics/nodes/%s/metrics/resource", n.name),
		}
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: n.name, Labels: labels, Annotations: annotations},
			Spec:       corev1.NodeSpec{},
			Status: corev1.NodeStatus{
				Allocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("8"),
					corev1.ResourceMemory: resource.MustParse("32Gi"),
					corev1.ResourcePods:   resource.MustParse("110"),
				},
				Capacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("8"),
					corev1.ResourceMemory: resource.MustParse("32Gi"),
					corev1.ResourcePods:   resource.MustParse("110"),
				},
			},
		}
		if _, err := client.CoreV1().Nodes().Create(ctx, node, metav1.CreateOptions{}); err != nil && !isAlreadyExists(err) {
			return fmt.Errorf("creating node %s: %w", n.name, err)
		}
	}
	return nil
}
