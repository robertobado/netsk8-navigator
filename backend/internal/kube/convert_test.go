package kube

import (
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestFormatAge(t *testing.T) {
	if got := formatAge(time.Time{}); got != "" {
		t.Errorf("zero time: got %q, want empty", got)
	}
	ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if got := formatAge(ts); got != "2026-01-02T03:04:05Z" {
		t.Errorf("got %q", got)
	}
}

func boolPtr(b bool) *bool { return &b }

func podWithContainerState(state corev1.ContainerState) *corev1.Pod {
	return &corev1.Pod{
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{{State: state}},
		},
	}
}

func TestWaitingReason(t *testing.T) {
	tests := []struct {
		name string
		pod  *corev1.Pod
		want string
	}{
		{
			name: "healthy running pod",
			pod: &corev1.Pod{Status: corev1.PodStatus{
				Phase:             corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{{State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}},
			}},
			want: "",
		},
		{
			name: "container waiting",
			pod:  podWithContainerState(corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff"}}),
			want: "ImagePullBackOff",
		},
		{
			name: "container terminated with reason",
			pod:  podWithContainerState(corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "OOMKilled"}}),
			want: "OOMKilled",
		},
		{
			name: "init container waiting takes priority over main",
			pod: &corev1.Pod{Status: corev1.PodStatus{
				InitContainerStatuses: []corev1.ContainerStatus{{State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "PodInitializing"}}}},
				ContainerStatuses:     []corev1.ContainerStatus{{State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff"}}}},
			}},
			want: "PodInitializing",
		},
		{
			name: "failed init container terminated non-completed",
			pod: &corev1.Pod{Status: corev1.PodStatus{
				InitContainerStatuses: []corev1.ContainerStatus{{State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "Error"}}}},
			}},
			want: "Error",
		},
		{
			name: "completed init container is not a problem",
			pod: &corev1.Pod{Status: corev1.PodStatus{
				Phase:                 corev1.PodRunning,
				InitContainerStatuses: []corev1.ContainerStatus{{State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "Completed"}}}},
				ContainerStatuses:     []corev1.ContainerStatus{{State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}},
			}},
			want: "",
		},
		{
			name: "pending unschedulable",
			pod: &corev1.Pod{Status: corev1.PodStatus{
				Phase: corev1.PodPending,
				Conditions: []corev1.PodCondition{
					{Type: corev1.PodScheduled, Status: corev1.ConditionFalse},
				},
			}},
			want: "Unschedulable",
		},
		{
			name: "pending but scheduled — no reason yet",
			pod: &corev1.Pod{Status: corev1.PodStatus{
				Phase: corev1.PodPending,
				Conditions: []corev1.PodCondition{
					{Type: corev1.PodScheduled, Status: corev1.ConditionTrue},
				},
			}},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := WaitingReason(tt.pod); got != tt.want {
				t.Errorf("WaitingReason() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPodPhase(t *testing.T) {
	running := &corev1.Pod{Status: corev1.PodStatus{Phase: corev1.PodRunning}}
	if got := PodPhase(running); got != "Running" {
		t.Errorf("got %q, want Running", got)
	}
	now := metav1.Now()
	terminating := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{DeletionTimestamp: &now},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning},
	}
	if got := PodPhase(terminating); got != "Terminating" {
		t.Errorf("got %q, want Terminating even though phase is Running", got)
	}
}

func TestToPodView(t *testing.T) {
	p := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "web-0", Namespace: "prod"},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app"}, {Name: "sidecar"}}},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{Ready: true, RestartCount: 2},
				{Ready: false, RestartCount: 1},
			},
		},
	}
	v := ToPodView(p)
	if v.Ready != 1 || v.Total != 2 {
		t.Errorf("ready/total = %d/%d, want 1/2", v.Ready, v.Total)
	}
	if v.Restarts != 3 {
		t.Errorf("restarts = %d, want 3", v.Restarts)
	}
	if v.Key() != "prod/web-0" {
		t.Errorf("Key() = %q", v.Key())
	}
}

func TestToPodView_DeletedAtAccountsForGracePeriod(t *testing.T) {
	deadline := time.Date(2026, 1, 1, 12, 0, 30, 0, time.UTC)
	grace := int64(30)
	p := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			DeletionTimestamp:          &metav1.Time{Time: deadline},
			DeletionGracePeriodSeconds: &grace,
		},
	}
	v := ToPodView(p)
	want := "2026-01-01T12:00:00Z" // deadline minus the 30s grace period
	if v.DeletedAt != want {
		t.Errorf("DeletedAt = %q, want %q", v.DeletedAt, want)
	}
}

func TestToDeploymentView_Status(t *testing.T) {
	replicas := func(n int32) *int32 { return &n }
	tests := []struct {
		name   string
		d      *appsv1.Deployment
		status string
		ready  string
	}{
		{"available", &appsv1.Deployment{Spec: appsv1.DeploymentSpec{Replicas: replicas(3)}, Status: appsv1.DeploymentStatus{ReadyReplicas: 3}}, "Available", "3/3"},
		{"progressing", &appsv1.Deployment{Spec: appsv1.DeploymentSpec{Replicas: replicas(3)}, Status: appsv1.DeploymentStatus{ReadyReplicas: 1}}, "Progressing", "1/3"},
		{"scaled to zero", &appsv1.Deployment{Spec: appsv1.DeploymentSpec{Replicas: replicas(0)}}, "Scaled to 0", "0/0"},
		{"nil replicas defaults to 1", &appsv1.Deployment{Status: appsv1.DeploymentStatus{ReadyReplicas: 1}}, "Available", "1/1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := ToDeploymentView(tt.d)
			if v.Status != tt.status {
				t.Errorf("Status = %q, want %q", v.Status, tt.status)
			}
			if v.Ready != tt.ready {
				t.Errorf("Ready = %q, want %q", v.Ready, tt.ready)
			}
		})
	}
}

func TestToServiceView_Ports(t *testing.T) {
	s := &corev1.Service{
		Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{
			{Port: 80, Protocol: corev1.ProtocolTCP},
			{Port: 443, NodePort: 30443, Protocol: corev1.ProtocolTCP},
		}},
	}
	v := ToServiceView(s)
	want := "80/TCP, 443:30443/TCP"
	if v.Ports != want {
		t.Errorf("Ports = %q, want %q", v.Ports, want)
	}
}

func TestToServiceView_ExternalIP(t *testing.T) {
	t.Run("prefers load balancer hostname", func(t *testing.T) {
		s := &corev1.Service{Status: corev1.ServiceStatus{LoadBalancer: corev1.LoadBalancerStatus{
			Ingress: []corev1.LoadBalancerIngress{{Hostname: "lb.example.com", IP: "1.2.3.4"}},
		}}}
		if got := ToServiceView(s).ExternalIP; got != "lb.example.com" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("falls back to load balancer IP", func(t *testing.T) {
		s := &corev1.Service{Status: corev1.ServiceStatus{LoadBalancer: corev1.LoadBalancerStatus{
			Ingress: []corev1.LoadBalancerIngress{{IP: "1.2.3.4"}},
		}}}
		if got := ToServiceView(s).ExternalIP; got != "1.2.3.4" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("falls back to spec.externalIPs when no LB", func(t *testing.T) {
		s := &corev1.Service{Spec: corev1.ServiceSpec{ExternalIPs: []string{"9.9.9.9"}}}
		if got := ToServiceView(s).ExternalIP; got != "9.9.9.9" {
			t.Errorf("got %q", got)
		}
	})
}

func TestToIngressView(t *testing.T) {
	class := "nginx"
	in := &networkingv1.Ingress{
		Spec: networkingv1.IngressSpec{
			IngressClassName: &class,
			Rules:            []networkingv1.IngressRule{{Host: "a.example.com"}, {Host: "b.example.com"}, {Host: ""}},
		},
		Status: networkingv1.IngressStatus{LoadBalancer: networkingv1.IngressLoadBalancerStatus{
			Ingress: []networkingv1.IngressLoadBalancerIngress{{IP: "10.0.0.1"}},
		}},
	}
	v := ToIngressView(in)
	if v.Class != "nginx" {
		t.Errorf("Class = %q", v.Class)
	}
	if v.Hosts != "a.example.com, b.example.com" {
		t.Errorf("Hosts = %q", v.Hosts)
	}
	if v.Address != "10.0.0.1" {
		t.Errorf("Address = %q", v.Address)
	}
}

func TestShortAccessModes(t *testing.T) {
	modes := []corev1.PersistentVolumeAccessMode{
		corev1.ReadWriteOnce, corev1.ReadOnlyMany, corev1.ReadWriteMany, corev1.ReadWriteOncePod, "Custom",
	}
	want := "RWO,ROX,RWX,RWOP,Custom"
	if got := ShortAccessModes(modes); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatSelector(t *testing.T) {
	t.Run("empty means everything", func(t *testing.T) {
		if got := FormatSelector(metav1.LabelSelector{}); got != "todos" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("sorted key=value pairs", func(t *testing.T) {
		sel := metav1.LabelSelector{MatchLabels: map[string]string{"b": "2", "a": "1"}}
		if got := FormatSelector(sel); got != "a=1, b=2" {
			t.Errorf("got %q", got)
		}
	})
}

func TestFormatSubjects(t *testing.T) {
	subjects := []rbacv1.Subject{
		{Kind: "ServiceAccount", Name: "default", Namespace: "kube-system"},
		{Kind: "ServiceAccount", Name: "default"}, // no namespace: falls back to defaultNS
		{Kind: "User", Name: "alice"},
		{Kind: "Group", Name: "admins"},
	}
	got := formatSubjects(subjects, "prod")
	want := []string{"kube-system/default", "prod/default", "user:alice", "group:admins"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("subject[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestToReplicaSetView_Current(t *testing.T) {
	replicas := func(n int32) *int32 { return &n }
	tests := []struct {
		name    string
		rs      *appsv1.ReplicaSet
		current bool
	}{
		{
			name:    "scaled up and owned — current",
			rs:      &appsv1.ReplicaSet{Spec: appsv1.ReplicaSetSpec{Replicas: replicas(3)}, ObjectMeta: metav1.ObjectMeta{OwnerReferences: []metav1.OwnerReference{{Controller: boolPtr(true), Kind: "Deployment", Name: "web"}}}},
			current: true,
		},
		{
			name:    "scaled to zero and owned — old revision",
			rs:      &appsv1.ReplicaSet{Spec: appsv1.ReplicaSetSpec{Replicas: replicas(0)}, ObjectMeta: metav1.ObjectMeta{OwnerReferences: []metav1.OwnerReference{{Controller: boolPtr(true), Kind: "Deployment", Name: "web"}}}},
			current: false,
		},
		{
			name:    "standalone (no controller) — always current",
			rs:      &appsv1.ReplicaSet{Spec: appsv1.ReplicaSetSpec{Replicas: replicas(0)}},
			current: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ToReplicaSetView(tt.rs).Current; got != tt.current {
				t.Errorf("Current = %v, want %v", got, tt.current)
			}
		})
	}
}

func TestToJobView(t *testing.T) {
	tests := []struct {
		name   string
		conds  []batchv1.JobCondition
		status string
	}{
		{"no conditions — running", nil, "Running"},
		{"complete", []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue}}, "Complete"},
		{"failed", []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue}}, "Failed"},
		{"suspended", []batchv1.JobCondition{{Type: batchv1.JobSuspended, Status: corev1.ConditionTrue}}, "Suspended"},
		{"condition present but false is ignored", []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionFalse}}, "Running"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := &batchv1.Job{Status: batchv1.JobStatus{Conditions: tt.conds}}
			if got := ToJobView(o).Status; got != tt.status {
				t.Errorf("Status = %q, want %q", got, tt.status)
			}
		})
	}
}

func TestToHPAView_DefaultMinReplicas(t *testing.T) {
	h := &autoscalingv2.HorizontalPodAutoscaler{Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
		MaxReplicas:    10,
		ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: "web"},
	}}
	v := ToHPAView(h)
	if v.MinPods != 1 {
		t.Errorf("MinPods = %d, want 1 (default)", v.MinPods)
	}
	if v.Reference != "Deployment/web" {
		t.Errorf("Reference = %q", v.Reference)
	}
}

func TestEndpointReady(t *testing.T) {
	yes, no := true, false
	if !endpointReady(discoveryv1.Endpoint{}) {
		t.Error("nil Ready should be treated as ready")
	}
	if !endpointReady(discoveryv1.Endpoint{Conditions: discoveryv1.EndpointConditions{Ready: &yes}}) {
		t.Error("explicit true should be ready")
	}
	if endpointReady(discoveryv1.Endpoint{Conditions: discoveryv1.EndpointConditions{Ready: &no}}) {
		t.Error("explicit false should not be ready")
	}
}

func TestToPVCView_CapacityFallsBackToRequest(t *testing.T) {
	c := &corev1.PersistentVolumeClaim{
		Spec: corev1.PersistentVolumeClaimSpec{Resources: corev1.VolumeResourceRequirements{
			Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("5Gi")},
		}},
	}
	if got := ToPVCView(c).Capacity; got != "5Gi" {
		t.Errorf("Capacity = %q, want 5Gi", got)
	}
}
