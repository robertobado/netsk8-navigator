package api

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := podConsumes(tc.pod, tc.isConfigMap, tc.refName); got != tc.want {
				t.Errorf("podConsumes() = %v, want %v", got, tc.want)
			}
		})
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
