package main

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// demoNamespaces are created in addition to the "default" namespace, which
// always exists on a fresh cluster.
var demoNamespaces = []string{"production", "staging", "monitoring"}

func seedNamespaces(ctx context.Context, client kubernetes.Interface) error {
	for _, name := range demoNamespaces {
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
		if _, err := client.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{}); err != nil && !isAlreadyExists(err) {
			return fmt.Errorf("creating namespace %s: %w", name, err)
		}
	}
	return nil
}
