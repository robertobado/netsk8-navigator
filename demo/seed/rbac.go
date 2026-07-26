package main

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// seedRBAC creates enough Roles/RoleBindings/ClusterRoles/ClusterRoleBindings
// for the "effective permissions" view (ServiceAccount detail/expansion) to
// have real, varied content to show.
func seedRBAC(ctx context.Context, client kubernetes.Interface) error {
	sas := []struct{ name, namespace string }{
		{"ci-deployer", "production"},
		{"readonly-viewer", "staging"},
	}
	for _, sa := range sas {
		obj := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: sa.name, Namespace: sa.namespace}}
		if _, err := client.CoreV1().ServiceAccounts(sa.namespace).Create(ctx, obj, metav1.CreateOptions{}); err != nil && !isAlreadyExists(err) {
			return fmt.Errorf("creating service account %s/%s: %w", sa.namespace, sa.name, err)
		}
	}

	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Name: "pod-reader", Namespace: "production"},
		Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{""}, Resources: []string{"pods", "pods/log"}, Verbs: []string{"get", "list", "watch"}},
			{APIGroups: []string{"apps"}, Resources: []string{"deployments"}, Verbs: []string{"get", "list"}},
		},
	}
	if _, err := client.RbacV1().Roles("production").Create(ctx, role, metav1.CreateOptions{}); err != nil && !isAlreadyExists(err) {
		return fmt.Errorf("creating role pod-reader: %w", err)
	}
	roleBinding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "ci-deployer-pod-reader", Namespace: "production"},
		Subjects:   []rbacv1.Subject{{Kind: "ServiceAccount", Name: "ci-deployer", Namespace: "production"}},
		RoleRef:    rbacv1.RoleRef{APIGroup: "rbac.authorization.k8s.io", Kind: "Role", Name: "pod-reader"},
	}
	if _, err := client.RbacV1().RoleBindings("production").Create(ctx, roleBinding, metav1.CreateOptions{}); err != nil && !isAlreadyExists(err) {
		return fmt.Errorf("creating role binding ci-deployer-pod-reader: %w", err)
	}

	clusterRole := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: "namespace-viewer"},
		Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{""}, Resources: []string{"namespaces"}, Verbs: []string{"get", "list", "watch"}},
		},
	}
	if _, err := client.RbacV1().ClusterRoles().Create(ctx, clusterRole, metav1.CreateOptions{}); err != nil && !isAlreadyExists(err) {
		return fmt.Errorf("creating cluster role namespace-viewer: %w", err)
	}
	clusterRoleBinding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "readonly-viewer-namespace-viewer"},
		Subjects:   []rbacv1.Subject{{Kind: "ServiceAccount", Name: "readonly-viewer", Namespace: "staging"}},
		RoleRef:    rbacv1.RoleRef{APIGroup: "rbac.authorization.k8s.io", Kind: "ClusterRole", Name: "namespace-viewer"},
	}
	if _, err := client.RbacV1().ClusterRoleBindings().Create(ctx, clusterRoleBinding, metav1.CreateOptions{}); err != nil && !isAlreadyExists(err) {
		return fmt.Errorf("creating cluster role binding readonly-viewer-namespace-viewer: %w", err)
	}
	return nil
}
