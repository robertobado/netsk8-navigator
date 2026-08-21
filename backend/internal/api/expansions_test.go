package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ktesting "k8s.io/client-go/testing"
)

func boolPtr(b bool) *bool { return &b }

func TestControllerRef(t *testing.T) {
	t.Run("no owners", func(t *testing.T) {
		kind, name := controllerRef(nil)
		if kind != "" || name != "" {
			t.Errorf("got %q/%q, want empty", kind, name)
		}
	})
	t.Run("skips non-controller owners", func(t *testing.T) {
		refs := []metav1.OwnerReference{
			{Kind: "ConfigMap", Name: "sidecar-config"}, // Controller nil — not a controller
			{Kind: "ReplicaSet", Name: "web-abc123", Controller: boolPtr(true)},
		}
		kind, name := controllerRef(refs)
		if kind != "ReplicaSet" || name != "web-abc123" {
			t.Errorf("got %q/%q", kind, name)
		}
	})
}

func TestWorkloadOf(t *testing.T) {
	rsToDeploy := map[string]string{"prod/web-abc123": "web"}

	t.Run("replicaset resolves to owning deployment", func(t *testing.T) {
		p := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Namespace: "prod", OwnerReferences: []metav1.OwnerReference{
				{Kind: "ReplicaSet", Name: "web-abc123", Controller: boolPtr(true)},
			}},
		}
		kind, name := workloadOf(p, rsToDeploy)
		if kind != "Deployment" || name != "web" {
			t.Errorf("got %q/%q, want Deployment/web", kind, name)
		}
	})

	t.Run("replicaset with no known deployment stays a replicaset", func(t *testing.T) {
		p := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Namespace: "prod", OwnerReferences: []metav1.OwnerReference{
				{Kind: "ReplicaSet", Name: "orphan-xyz", Controller: boolPtr(true)},
			}},
		}
		kind, name := workloadOf(p, rsToDeploy)
		if kind != "ReplicaSet" || name != "orphan-xyz" {
			t.Errorf("got %q/%q", kind, name)
		}
	})

	t.Run("standalone pod", func(t *testing.T) {
		kind, name := workloadOf(&corev1.Pod{}, rsToDeploy)
		if kind != "Pod" || name != "" {
			t.Errorf("got %q/%q, want Pod/\"\"", kind, name)
		}
	})

	t.Run("non-replicaset controller passes through", func(t *testing.T) {
		p := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{OwnerReferences: []metav1.OwnerReference{
			{Kind: "DaemonSet", Name: "fluentd", Controller: boolPtr(true)},
		}}}
		kind, name := workloadOf(p, rsToDeploy)
		if kind != "DaemonSet" || name != "fluentd" {
			t.Errorf("got %q/%q", kind, name)
		}
	})
}

func TestGroupPodsByWorkload(t *testing.T) {
	pods := []corev1.Pod{
		{ObjectMeta: metav1.ObjectMeta{Name: "web-1", Namespace: "prod", OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: "web-abc", Controller: boolPtr(true)}}}},
		{ObjectMeta: metav1.ObjectMeta{Name: "web-2", Namespace: "prod", OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: "web-abc", Controller: boolPtr(true)}}}},
		{ObjectMeta: metav1.ObjectMeta{Name: "debug", Namespace: "prod"}}, // standalone
	}
	rsToDeploy := map[string]string{"prod/web-abc": "web"}
	groups := groupPodsByWorkload(pods, rsToDeploy)

	if len(groups) != 2 {
		t.Fatalf("got %d groups, want 2", len(groups))
	}
	var deploy, standalone *nodeWorkloadGroup
	for i := range groups {
		switch groups[i].Kind {
		case "Deployment":
			deploy = &groups[i]
		case "Pod":
			standalone = &groups[i]
		}
	}
	if deploy == nil || len(deploy.Pods) != 2 || deploy.Slug != "deployment" {
		t.Errorf("deployment group = %+v", deploy)
	}
	if standalone == nil || len(standalone.Pods) != 1 || standalone.Slug != "" {
		t.Errorf("standalone group = %+v", standalone)
	}
}

func TestSortNodeGroups(t *testing.T) {
	groups := []nodeWorkloadGroup{
		{Kind: "Pod", Name: ""}, // standalone bucket — must sort last
		{Kind: "StatefulSet", Name: "db"},
		{Kind: "Deployment", Name: "web"},
		{Kind: "Deployment", Name: "api"},
	}
	sortNodeGroups(groups)
	want := []string{"Deployment/api", "Deployment/web", "StatefulSet/db", "Pod/"}
	for i, g := range groups {
		got := g.Kind + "/" + g.Name
		if got != want[i] {
			t.Errorf("position %d = %q, want %q", i, got, want[i])
		}
	}
}

func TestSubjectsIncludeSA(t *testing.T) {
	subjects := []rbacv1.Subject{
		{Kind: "User", Name: "deploy-bot"},
		{Kind: "ServiceAccount", Name: "web", Namespace: "prod"},
	}
	if !subjectsIncludeSA(subjects, "prod", "web") {
		t.Error("expected match on namespace+name")
	}
	if subjectsIncludeSA(subjects, "staging", "web") {
		t.Error("should not match a different namespace")
	}
	// A ClusterRoleBinding subject with no namespace matches any namespace (kube's own rule).
	wildcard := []rbacv1.Subject{{Kind: "ServiceAccount", Name: "web"}}
	if !subjectsIncludeSA(wildcard, "any-ns", "web") {
		t.Error("empty subject namespace should match any namespace")
	}
}

func TestPodConsumes(t *testing.T) {
	cases := []struct {
		name        string
		pod         *corev1.Pod
		isConfigMap bool
		refName     string
		want        bool
	}{
		{
			name: "configmap via volume",
			pod: &corev1.Pod{Spec: corev1.PodSpec{Volumes: []corev1.Volume{
				{VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: "app-config"}}}},
			}}},
			isConfigMap: true, refName: "app-config", want: true,
		},
		{
			name: "configmap via volume — different name doesn't match",
			pod: &corev1.Pod{Spec: corev1.PodSpec{Volumes: []corev1.Volume{
				{VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: "app-config"}}}},
			}}},
			isConfigMap: true, refName: "other-config", want: false,
		},
		{
			name:        "secret via imagePullSecrets",
			pod:         &corev1.Pod{Spec: corev1.PodSpec{ImagePullSecrets: []corev1.LocalObjectReference{{Name: "regcred"}}}},
			isConfigMap: false, refName: "regcred", want: true,
		},
		{
			name:        "imagePullSecrets never satisfies a configmap lookup",
			pod:         &corev1.Pod{Spec: corev1.PodSpec{ImagePullSecrets: []corev1.LocalObjectReference{{Name: "regcred"}}}},
			isConfigMap: true, refName: "regcred", want: false,
		},
		{
			name: "projected volume source",
			pod: &corev1.Pod{Spec: corev1.PodSpec{Volumes: []corev1.Volume{
				{VolumeSource: corev1.VolumeSource{Projected: &corev1.ProjectedVolumeSource{Sources: []corev1.VolumeProjection{
					{Secret: &corev1.SecretProjection{LocalObjectReference: corev1.LocalObjectReference{Name: "tls"}}},
				}}}},
			}}},
			isConfigMap: false, refName: "tls", want: true,
		},
		{
			name: "container envFrom",
			pod: &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{
				{EnvFrom: []corev1.EnvFromSource{{ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "env-config"}}}}},
			}}},
			isConfigMap: true, refName: "env-config", want: true,
		},
		{
			name: "container env valueFrom",
			pod: &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{
				{Env: []corev1.EnvVar{{ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "db-secret"}, Key: "password"}}}}},
			}}},
			isConfigMap: false, refName: "db-secret", want: true,
		},
		{
			name: "init container counts too",
			pod: &corev1.Pod{Spec: corev1.PodSpec{InitContainers: []corev1.Container{
				{EnvFrom: []corev1.EnvFromSource{{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "init-secret"}}}}},
			}}},
			isConfigMap: false, refName: "init-secret", want: true,
		},
		{
			name:        "no reference at all",
			pod:         &corev1.Pod{},
			isConfigMap: true, refName: "anything", want: false,
		},
		{
			name: "secret via volume",
			pod: &corev1.Pod{Spec: corev1.PodSpec{Volumes: []corev1.Volume{
				{VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "db-tls"}}},
			}}},
			isConfigMap: false, refName: "db-tls", want: true,
		},
		{
			name: "projected configmap source",
			pod: &corev1.Pod{Spec: corev1.PodSpec{Volumes: []corev1.Volume{
				{VolumeSource: corev1.VolumeSource{Projected: &corev1.ProjectedVolumeSource{Sources: []corev1.VolumeProjection{
					{ConfigMap: &corev1.ConfigMapProjection{LocalObjectReference: corev1.LocalObjectReference{Name: "app-config"}}},
				}}}},
			}}},
			isConfigMap: true, refName: "app-config", want: true,
		},
		{
			name: "projected source present but type mismatch matches nothing",
			pod: &corev1.Pod{Spec: corev1.PodSpec{Volumes: []corev1.Volume{
				{VolumeSource: corev1.VolumeSource{Projected: &corev1.ProjectedVolumeSource{Sources: []corev1.VolumeProjection{
					{ConfigMap: &corev1.ConfigMapProjection{LocalObjectReference: corev1.LocalObjectReference{Name: "app-config"}}},
				}}}},
			}}},
			isConfigMap: false, refName: "app-config", want: false,
		},
		{
			name: "env var without valueFrom is skipped, later match still found",
			pod: &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{
				{Env: []corev1.EnvVar{
					{Name: "PLAIN", Value: "just-a-value"},
					{Name: "SECRET", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "db-secret"}, Key: "password"}}},
				}},
			}}},
			isConfigMap: false, refName: "db-secret", want: true,
		},
		{
			name: "env var configmap keyRef",
			pod: &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{
				{Env: []corev1.EnvVar{{ValueFrom: &corev1.EnvVarSource{ConfigMapKeyRef: &corev1.ConfigMapKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "app-config"}, Key: "url"}}}}},
			}}},
			isConfigMap: true, refName: "app-config", want: true,
		},
		{
			name: "env var valueFrom set but not a configmap/secret keyRef matches nothing",
			pod: &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{
				{Env: []corev1.EnvVar{{ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"}}}}},
			}}},
			isConfigMap: false, refName: "anything", want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := podConsumes(tc.pod, tc.isConfigMap, tc.refName); got != tc.want {
				t.Errorf("podConsumes() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestHandleNodeWorkloads_ClientForError(t *testing.T) {
	s := NewServer(clientForErrManager{newFakeManager()}, testConfigStore(t), "")
	rec := doRequest(t, s, "GET", "/api/contexts/test/node-workloads/worker-1", "")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestHandleNodeWorkloads_ListError(t *testing.T) {
	s := newTestServer(t)
	fakeClient(t, s).PrependReactor("list", "pods", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("etcd unavailable")
	})
	rec := doRequest(t, s, "GET", "/api/contexts/test/node-workloads/worker-1", "")
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rec.Code)
	}
}

// TestHandleNodeWorkloads_ReplicaSetGroupsUnderDeployment exercises
// replicaSetToDeployment's actual List+loop against a live ReplicaSet with a
// Deployment controller — TestGroupPodsByWorkload only unit-tests the
// pre-built rsToDeploy map, never replicaSetToDeployment itself.
func TestHandleNodeWorkloads_ReplicaSetGroupsUnderDeployment(t *testing.T) {
	s := newTestServer(t,
		&appsv1.ReplicaSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: "web-abc123", Namespace: "prod",
				OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: "web", Controller: boolPtr(true)}},
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "web-abc123-xyz", Namespace: "prod",
				OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: "web-abc123", Controller: boolPtr(true)}},
			},
			Spec: corev1.PodSpec{NodeName: "worker-1"},
		},
	)
	rec := doRequest(t, s, "GET", "/api/contexts/test/node-workloads/worker-1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var groups []nodeWorkloadGroup
	if err := json.Unmarshal(rec.Body.Bytes(), &groups); err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || groups[0].Kind != "Deployment" || groups[0].Name != "web" {
		t.Errorf("got %+v, want a single Deployment/web group", groups)
	}
}

// TestHandleNodeWorkloads_ReplicaSetListError exercises
// replicaSetToDeployment's List error branch — grouping falls back to
// "ReplicaSet" (rather than resolving to the owning Deployment) instead of
// failing the whole request.
func TestHandleNodeWorkloads_ReplicaSetListError(t *testing.T) {
	s := newTestServer(t,
		&appsv1.ReplicaSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: "web-abc123", Namespace: "prod",
				OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: "web", Controller: boolPtr(true)}},
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "web-abc123-xyz", Namespace: "prod",
				OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: "web-abc123", Controller: boolPtr(true)}},
			},
			Spec: corev1.PodSpec{NodeName: "worker-1"},
		},
	)
	fakeClient(t, s).PrependReactor("list", "replicasets", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("etcd unavailable")
	})
	rec := doRequest(t, s, "GET", "/api/contexts/test/node-workloads/worker-1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var groups []nodeWorkloadGroup
	if err := json.Unmarshal(rec.Body.Bytes(), &groups); err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || groups[0].Kind != "ReplicaSet" {
		t.Errorf("got %+v, want a fallback ReplicaSet group when the RS→Deployment lookup fails", groups)
	}
}

func TestHandleNamespaceSummary_DynamicForError(t *testing.T) {
	mgr := &countedFailManager{fakeManager: newFakeManager(), dynamicForFailAt: 1}
	s := NewServer(mgr, testConfigStore(t), "")
	rec := doRequest(t, s, "GET", "/api/contexts/test/namespace-summary/prod", "")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestHandleServiceAccountUsage_ClientForError(t *testing.T) {
	s := NewServer(clientForErrManager{newFakeManager()}, testConfigStore(t), "")
	rec := doRequest(t, s, "GET", "/api/contexts/test/serviceaccount-usage/prod/web", "")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestHandleServiceAccountUsage_PodsListError(t *testing.T) {
	s := newTestServer(t)
	fakeClient(t, s).PrependReactor("list", "pods", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("etcd unavailable")
	})
	// podsUsingSA swallows the error (returns an empty slice) rather than
	// failing the request — assert 200 with no pods, not 5xx.
	rec := doRequest(t, s, "GET", "/api/contexts/test/serviceaccount-usage/prod/web", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (pod-list errors are swallowed)", rec.Code)
	}
	var out saUsage
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Pods) != 0 {
		t.Errorf("Pods = %+v, want empty", out.Pods)
	}
}

// TestHandleServiceAccountUsage_DefaultServiceAccountName covers
// podsUsingSA's "" → "default" normalization — a pod with no
// spec.serviceAccountName runs as the namespace's implicit "default" SA.
func TestHandleServiceAccountUsage_DefaultServiceAccountName(t *testing.T) {
	s := newTestServer(t,
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "implicit", Namespace: "prod"}},
	)
	rec := doRequest(t, s, "GET", "/api/contexts/test/serviceaccount-usage/prod/default", "")
	var out saUsage
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Pods) != 1 || out.Pods[0].Name != "implicit" {
		t.Errorf("got %+v, want the SA-less pod to count as running as \"default\"", out.Pods)
	}
}

// TestHandleServiceAccountUsage_UnrelatedBindingsAndDuplicateRules covers:
//   - bindingsForSA's "subject doesn't name this SA" skip, for both
//     RoleBinding and ClusterRoleBinding (an unrelated binding of each kind
//     is seeded alongside the matching ones already covered by
//     TestHandleServiceAccountUsage_EffectivePermissions).
//   - roleRefRules falling through to its final "return nil" when the
//     referenced Role doesn't exist (a dangling RoleRef).
//   - dedupeRules dropping an identical rule pulled in twice (two bindings
//     to the same Role).
func TestHandleServiceAccountUsage_UnrelatedBindingsAndDuplicateRules(t *testing.T) {
	s := newTestServer(t,
		&rbacv1.Role{
			ObjectMeta: metav1.ObjectMeta{Name: "pod-reader", Namespace: "prod"},
			Rules:      []rbacv1.PolicyRule{{Verbs: []string{"get"}, APIGroups: []string{""}, Resources: []string{"pods"}}},
		},
		&rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "web-pod-reader-1", Namespace: "prod"},
			RoleRef:    rbacv1.RoleRef{Kind: "Role", Name: "pod-reader", APIGroup: "rbac.authorization.k8s.io"},
			Subjects:   []rbacv1.Subject{{Kind: "ServiceAccount", Name: "web", Namespace: "prod"}},
		},
		&rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "web-pod-reader-2", Namespace: "prod"},
			RoleRef:    rbacv1.RoleRef{Kind: "Role", Name: "pod-reader", APIGroup: "rbac.authorization.k8s.io"},
			Subjects:   []rbacv1.Subject{{Kind: "ServiceAccount", Name: "web", Namespace: "prod"}}, // same rule again — dedupeRules should collapse it
		},
		&rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "other-sa-binding", Namespace: "prod"},
			RoleRef:    rbacv1.RoleRef{Kind: "Role", Name: "dangling", APIGroup: "rbac.authorization.k8s.io"},
			Subjects:   []rbacv1.Subject{{Kind: "ServiceAccount", Name: "someone-else", Namespace: "prod"}}, // doesn't name "web" — must be skipped
		},
		&rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "web-dangling-role", Namespace: "prod"},
			RoleRef:    rbacv1.RoleRef{Kind: "Role", Name: "does-not-exist", APIGroup: "rbac.authorization.k8s.io"},
			Subjects:   []rbacv1.Subject{{Kind: "ServiceAccount", Name: "web", Namespace: "prod"}}, // matches "web" but its Role doesn't exist
		},
		&rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "other-sa-cluster-binding"},
			RoleRef:    rbacv1.RoleRef{Kind: "ClusterRole", Name: "node-viewer", APIGroup: "rbac.authorization.k8s.io"},
			Subjects:   []rbacv1.Subject{{Kind: "ServiceAccount", Name: "someone-else", Namespace: "prod"}}, // doesn't name "web" — must be skipped
		},
	)
	rec := doRequest(t, s, "GET", "/api/contexts/test/serviceaccount-usage/prod/web", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var out saUsage
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	// 3 bindings match "web" (web-pod-reader-1, web-pod-reader-2, web-dangling-role); the other 2 don't.
	if len(out.Bindings) != 3 {
		t.Errorf("Bindings = %+v, want 3 (unrelated bindings excluded)", out.Bindings)
	}
	if len(out.Permissions) != 1 {
		t.Errorf("Permissions = %+v, want 1 deduped row (the dangling-role binding contributes no rules)", out.Permissions)
	}
}

func TestHandleConsumers_ClientForError(t *testing.T) {
	s := NewServer(clientForErrManager{newFakeManager()}, testConfigStore(t), "")
	rec := doRequest(t, s, "GET", "/api/contexts/test/consumers/configmap/prod/app-config", "")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestHandleConsumers_ListError(t *testing.T) {
	s := newTestServer(t)
	fakeClient(t, s).PrependReactor("list", "pods", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("etcd unavailable")
	})
	rec := doRequest(t, s, "GET", "/api/contexts/test/consumers/configmap/prod/app-config", "")
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rec.Code)
	}
}

func TestKindSlug(t *testing.T) {
	tests := map[string]string{
		"Deployment":  "deployment",
		"StatefulSet": "statefulset",
		"DaemonSet":   "daemonset",
		"ReplicaSet":  "replicaset",
		"Job":         "job",
		"CronJob":     "cronjob",
		"Pod":         "", // not in the map — no detail slug
	}
	for kind, want := range tests {
		if got := kindSlug(kind); got != want {
			t.Errorf("kindSlug(%q) = %q, want %q", kind, got, want)
		}
	}
}
