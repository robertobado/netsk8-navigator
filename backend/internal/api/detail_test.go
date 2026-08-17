package api

import (
	"encoding/json"
	"net/http"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	nodev1 "k8s.io/api/node/v1"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func int32Ptr(i int32) *int32                     { return &i }
func protoPtr(p corev1.Protocol) *corev1.Protocol { return &p }
func intstrFromInt(i int32) intstr.IntOrString    { return intstr.FromInt32(i) }

func meta(name, ns string) metav1.ObjectMeta {
	return metav1.ObjectMeta{Name: name, Namespace: ns}
}

// --- small pure helpers -----------------------------------------------------

func TestOrDash(t *testing.T) {
	if got := orDash(""); got != "—" {
		t.Errorf("orDash(\"\") = %q, want em dash", got)
	}
	if got := orDash("x"); got != "x" {
		t.Errorf("orDash(\"x\") = %q, want x", got)
	}
}

func TestBoolTone(t *testing.T) {
	if got := boolTone(true); got != "muted" {
		t.Errorf("boolTone(true) = %q, want muted", got)
	}
	if got := boolTone(false); got != "err" {
		t.Errorf("boolTone(false) = %q, want err", got)
	}
}

func TestReplicaChip(t *testing.T) {
	cases := []struct {
		name         string
		ready, total int32
		value, tone  string
	}{
		{"none desired", 0, 0, "0/0", "muted"},
		{"ready meets desired", 3, 3, "3/3", "ok"},
		{"ready exceeds desired", 4, 3, "4/3", "ok"},
		{"under-ready", 1, 3, "1/3", "warn"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := replicaChip("Ready", c.ready, c.total)
			if got.Value != c.value {
				t.Errorf("value = %q, want %q", got.Value, c.value)
			}
			if got.Tone != c.tone {
				t.Errorf("tone = %q, want %q", got.Tone, c.tone)
			}
		})
	}
}

func TestIsPrintable(t *testing.T) {
	if !isPrintable([]byte("hello\nworld\t!")) {
		t.Error("plain text should be printable")
	}
	if isPrintable([]byte{0x00, 0x01, 0xff}) {
		t.Error("binary bytes should not be printable")
	}
}

func TestPhaseTone(t *testing.T) {
	cases := map[string]string{
		"Running": "ok", "Succeeded": "ok",
		"Pending": "warn", "ContainerCreating": "warn",
		"Failed": "err", "CrashLoopBackOff": "err",
		"Unknown-phase": "muted",
	}
	for phase, want := range cases {
		if got := phaseTone(phase); got != want {
			t.Errorf("phaseTone(%q) = %q, want %q", phase, got, want)
		}
	}
}

func TestVolumePhaseTone(t *testing.T) {
	cases := map[string]string{
		"Bound": "ok", "Available": "ok",
		"Pending": "warn", "Released": "warn",
		"Lost": "err", "Failed": "err",
		"Weird": "muted",
	}
	for phase, want := range cases {
		if got := volumePhaseTone(phase); got != want {
			t.Errorf("volumePhaseTone(%q) = %q, want %q", phase, got, want)
		}
	}
}

func TestContainerState(t *testing.T) {
	if got := containerState(corev1.ContainerStatus{State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}); got != "Running" {
		t.Errorf("running = %q", got)
	}
	if got := containerState(corev1.ContainerStatus{State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff"}}}); got != "Waiting: ImagePullBackOff" {
		t.Errorf("waiting = %q", got)
	}
	if got := containerState(corev1.ContainerStatus{State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "OOMKilled"}}}); got != "Terminated: OOMKilled" {
		t.Errorf("terminated = %q", got)
	}
	if got := containerState(corev1.ContainerStatus{}); got != "Unknown" {
		t.Errorf("empty = %q, want Unknown", got)
	}
}

func TestPvSource(t *testing.T) {
	cases := []struct {
		name string
		src  corev1.PersistentVolumeSource
		want string
	}{
		{"csi", corev1.PersistentVolumeSource{CSI: &corev1.CSIPersistentVolumeSource{Driver: "ebs.csi.aws.com"}}, "CSI: ebs.csi.aws.com"},
		{"hostpath", corev1.PersistentVolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/data"}}, "HostPath: /data"},
		{"nfs", corev1.PersistentVolumeSource{NFS: &corev1.NFSVolumeSource{Server: "nfs.local", Path: "/export"}}, "NFS: nfs.local/export"},
		{"none", corev1.PersistentVolumeSource{}, "—"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := pvSource(c.src); got != c.want {
				t.Errorf("pvSource() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestNpPeersAndPorts(t *testing.T) {
	if got := npPeers(nil); got != "any source/destination" {
		t.Errorf("npPeers(nil) = %q", got)
	}
	if got := npPorts(nil); got != "all ports" {
		t.Errorf("npPorts(nil) = %q", got)
	}
	cidr := "10.0.0.0/8"
	peers := []networkingv1.NetworkPolicyPeer{{IPBlock: &networkingv1.IPBlock{CIDR: cidr}}}
	if got := npPeers(peers); got != "ipBlock 10.0.0.0/8" {
		t.Errorf("npPeers(ipBlock) = %q", got)
	}
}

func TestFormatRules(t *testing.T) {
	rules := []rbacv1.PolicyRule{
		{Verbs: []string{"get", "list"}, APIGroups: []string{""}, Resources: []string{"pods"}},
		{Verbs: []string{"get"}, NonResourceURLs: []string{"/healthz"}},
	}
	got := formatRules(rules)
	if len(got) != 2 {
		t.Fatalf("got %d rules, want 2", len(got))
	}
	if got[0].Value != "core/pods" {
		t.Errorf("rule[0].Value = %q, want core/pods (empty apiGroup shown as core)", got[0].Value)
	}
	if got[1].Value != "/healthz" {
		t.Errorf("rule[1].Value = %q, want /healthz", got[1].Value)
	}
}

func TestHpaMetricSpecAndTarget(t *testing.T) {
	util := int32(80)
	m := autoscalingv2.MetricSpec{
		Type: autoscalingv2.ResourceMetricSourceType,
		Resource: &autoscalingv2.ResourceMetricSource{
			Name:   corev1.ResourceCPU,
			Target: autoscalingv2.MetricTarget{AverageUtilization: &util},
		},
	}
	key, label, target := hpaMetricSpec(m)
	if key != "resource/cpu" || label != "cpu" || target != "80%" {
		t.Errorf("got key=%q label=%q target=%q", key, label, target)
	}

	status := autoscalingv2.MetricStatus{
		Type: autoscalingv2.ResourceMetricSourceType,
		Resource: &autoscalingv2.ResourceMetricStatus{
			Name:    corev1.ResourceCPU,
			Current: autoscalingv2.MetricValueStatus{AverageUtilization: &util},
		},
	}
	sKey, cur := hpaMetricStatus(status)
	if sKey != key || cur != "80%" {
		t.Errorf("status key=%q cur=%q, want key=%q cur=80%%", sKey, cur, key)
	}
}

// --- workload builders -------------------------------------------------------

func TestDeploymentDetail(t *testing.T) {
	d := deploymentDetail(&appsv1.Deployment{
		ObjectMeta: meta("web", "prod"),
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(3),
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "web"}},
			Strategy: appsv1.DeploymentStrategy{Type: appsv1.RollingUpdateDeploymentStrategyType},
			Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "web", Image: "web:1.0"}}}},
		},
		Status: appsv1.DeploymentStatus{ReadyReplicas: 2, UpdatedReplicas: 3, AvailableReplicas: 2},
	})
	if d.Kind != "Deployment" || d.Name != "web" {
		t.Fatalf("got kind=%q name=%q", d.Kind, d.Name)
	}
	if d.Status[0].Label != "Ready" || d.Status[0].Value != "2/3" || d.Status[0].Tone != "warn" {
		t.Errorf("Ready chip = %+v", d.Status[0])
	}
	if len(d.Images) != 1 || d.Images[0].Value != "web:1.0" {
		t.Errorf("Images = %+v", d.Images)
	}
	if d.Sections[0].Title != "Strategy" {
		t.Errorf("Sections[0].Title = %q, want Strategy", d.Sections[0].Title)
	}
}

func TestCronJobDetail(t *testing.T) {
	t.Run("active", func(t *testing.T) {
		d := cronJobDetail(&batchv1.CronJob{
			ObjectMeta: meta("backup", "ops"),
			Spec:       batchv1.CronJobSpec{Schedule: "0 * * * *", ConcurrencyPolicy: batchv1.ForbidConcurrent},
		})
		if d.Status[0].Value != "Active" || d.Status[0].Tone != "ok" {
			t.Errorf("State chip = %+v, want Active/ok", d.Status[0])
		}
	})
	t.Run("suspended", func(t *testing.T) {
		suspend := true
		d := cronJobDetail(&batchv1.CronJob{
			ObjectMeta: meta("backup", "ops"),
			Spec:       batchv1.CronJobSpec{Suspend: &suspend},
		})
		if d.Status[0].Value != "Suspended" || d.Status[0].Tone != "warn" {
			t.Errorf("State chip = %+v, want Suspended/warn", d.Status[0])
		}
	})
}

func TestJobDetail(t *testing.T) {
	d := jobDetail(&batchv1.Job{
		ObjectMeta: meta("migrate", "ops"),
		Spec:       batchv1.JobSpec{Completions: int32Ptr(1)},
		Status:     batchv1.JobStatus{Succeeded: 1, Active: 0, Failed: 0},
	})
	if d.Status[0].Label != "Completions" || d.Status[0].Tone != "ok" {
		t.Errorf("Completions chip = %+v", d.Status[0])
	}
	if d.Status[2].Value != "0" || d.Status[2].Tone != "muted" {
		t.Errorf("Failed chip = %+v, want 0/muted", d.Status[2])
	}
}

func TestServiceDetail(t *testing.T) {
	d := serviceDetail(&corev1.Service{
		ObjectMeta: meta("web", "prod"),
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP, ClusterIP: "10.0.0.5",
			Ports: []corev1.ServicePort{{Port: 80, TargetPort: intstrFromInt(8080), Protocol: corev1.ProtocolTCP}},
		},
	})
	if d.Status[0].Value != "ClusterIP" || d.Status[1].Value != "10.0.0.5" {
		t.Errorf("Status = %+v", d.Status)
	}
	if len(d.Ports) != 1 || d.Ports[0].Port != "80 → 8080" {
		t.Errorf("Ports = %+v", d.Ports)
	}
}

func TestIngressDetail(t *testing.T) {
	svc := "web"
	d := ingressDetail(&networkingv1.Ingress{
		ObjectMeta: meta("web", "prod"),
		Spec: networkingv1.IngressSpec{
			Rules: []networkingv1.IngressRule{{
				Host: "example.com",
				IngressRuleValue: networkingv1.IngressRuleValue{HTTP: &networkingv1.HTTPIngressRuleValue{
					Paths: []networkingv1.HTTPIngressPath{{Path: "/", Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{Name: svc, Port: networkingv1.ServiceBackendPort{Number: 80}}}}},
				}},
			}},
		},
	})
	if len(d.Refs) != 1 || d.Refs[0].Name != "web" || d.Refs[0].Group != "Backends" {
		t.Errorf("Refs = %+v", d.Refs)
	}
	if d.Sections[0].Title != "example.com" {
		t.Errorf("Sections[0].Title = %q", d.Sections[0].Title)
	}
}

func TestNetworkPolicyDetail(t *testing.T) {
	d := networkPolicyDetail(&networkingv1.NetworkPolicy{
		ObjectMeta: meta("deny-all", "prod"),
		Spec: networkingv1.NetworkPolicySpec{
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
			Ingress:     []networkingv1.NetworkPolicyIngressRule{{}},
		},
	})
	if d.Status[0].Label != "Ingress" || d.Status[0].Value != "yes" {
		t.Errorf("Ingress chip = %+v", d.Status[0])
	}
	if d.Status[1].Value != "no" {
		t.Errorf("Egress chip = %+v, want no", d.Status[1])
	}
	if len(d.Sections) != 1 || d.Sections[0].Title != "Ingress (inbound)" {
		t.Errorf("Sections = %+v", d.Sections)
	}
	if d.Sections[0].Items[0].Label != "Rule 1" {
		t.Errorf("first rule label = %q, want 'Rule 1'", d.Sections[0].Items[0].Label)
	}
}

func TestConfigMapDetail(t *testing.T) {
	d := configMapDetail(&corev1.ConfigMap{
		ObjectMeta: meta("app-config", "prod"),
		Data:       map[string]string{"key.yaml": "a: 1"},
	})
	if d.Status[0].Label != "Keys" || d.Status[0].Value != "1" {
		t.Errorf("Keys chip = %+v", d.Status[0])
	}
	if len(d.Blocks) != 1 || d.Blocks[0].Title != "key.yaml" || d.Blocks[0].Masked {
		t.Errorf("Blocks = %+v", d.Blocks)
	}
}

func TestSecretDetail(t *testing.T) {
	d := secretDetail(&corev1.Secret{
		ObjectMeta: meta("db-creds", "prod"),
		Type:       corev1.SecretTypeOpaque,
		Data:       map[string][]byte{"password": []byte("hunter2")},
	})
	if d.Status[0].Value != "Opaque" {
		t.Errorf("Type chip = %+v", d.Status[0])
	}
	if len(d.Blocks) != 1 || !d.Blocks[0].Masked || d.Blocks[0].Body != "hunter2" {
		t.Errorf("Blocks = %+v", d.Blocks)
	}
}

func TestNamespaceDetail(t *testing.T) {
	d := namespaceDetail(&corev1.Namespace{
		ObjectMeta: meta("prod", ""),
		Spec:       corev1.NamespaceSpec{Finalizers: []corev1.FinalizerName{"kubernetes"}},
		Status:     corev1.NamespaceStatus{Phase: corev1.NamespaceActive},
	})
	if d.Status[0].Value != "Active" || d.Status[0].Tone != "ok" {
		t.Errorf("Phase chip = %+v", d.Status[0])
	}
	if len(d.Sections) != 1 || d.Sections[0].Items[0].Label != "Active finalizers" {
		t.Errorf("Sections = %+v", d.Sections)
	}
}

func TestPvcDetail(t *testing.T) {
	d := pvcDetail(&corev1.PersistentVolumeClaim{
		ObjectMeta: meta("data", "prod"),
		Spec:       corev1.PersistentVolumeClaimSpec{VolumeName: "pv-1"},
		Status:     corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound},
	})
	if d.Status[0].Value != "Bound" || d.Status[0].Tone != "ok" {
		t.Errorf("Status chip = %+v", d.Status[0])
	}
	if len(d.Refs) != 1 || d.Refs[0].Name != "pv-1" || d.Refs[0].Group != "Volume" {
		t.Errorf("Refs = %+v", d.Refs)
	}
}

func TestStorageClassDetail(t *testing.T) {
	yes := true
	d := storageClassDetail(&storagev1.StorageClass{
		ObjectMeta:           meta("fast", ""),
		Provisioner:          "ebs.csi.aws.com",
		AllowVolumeExpansion: &yes,
		Parameters:           map[string]string{"type": "gp3"},
	})
	if d.Status[0].Value != "ebs.csi.aws.com" {
		t.Errorf("Provisioner chip = %+v", d.Status[0])
	}
	found := false
	for _, s := range d.Sections {
		if s.Title == "Parameters" && len(s.Items) == 1 && s.Items[0].Label == "type" {
			found = true
		}
	}
	if !found {
		t.Errorf("Parameters section missing/wrong: %+v", d.Sections)
	}
}

func TestHpaDetail(t *testing.T) {
	d := hpaDetail(&autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: meta("web", "prod"),
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			MinReplicas:    int32Ptr(2),
			MaxReplicas:    10,
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: "web"},
		},
	})
	// Regression check: the target ref used to leak the Portuguese "Alvo"
	// instead of "Target" — this locks the English string in place.
	if len(d.Refs) != 1 || d.Refs[0].Group != "Target" || d.Refs[0].Name != "web" {
		t.Errorf("Refs = %+v, want a single Target ref to web", d.Refs)
	}
}

func TestEndpointSliceDetail(t *testing.T) {
	ready := true
	port := int32(8080)
	d := endpointSliceDetail(&discoveryv1.EndpointSlice{
		ObjectMeta:  metav1.ObjectMeta{Name: "web-abc", Namespace: "prod", Labels: map[string]string{"kubernetes.io/service-name": "web"}},
		AddressType: discoveryv1.AddressTypeIPv4,
		Ports:       []discoveryv1.EndpointPort{{Port: &port, Protocol: protoPtr(corev1.ProtocolTCP)}},
		Endpoints: []discoveryv1.Endpoint{{
			Addresses:  []string{"10.0.0.1"},
			Conditions: discoveryv1.EndpointConditions{Ready: &ready},
			TargetRef:  &corev1.ObjectReference{Kind: "Pod", Name: "web-1", Namespace: "prod"},
		}},
	})
	if len(d.Refs) != 2 { // Service ref + the one Pod endpoint
		t.Fatalf("Refs = %+v, want 2 (service + pod)", d.Refs)
	}
	if d.Refs[0].Group != "Service" || d.Refs[0].Name != "web" {
		t.Errorf("Refs[0] = %+v", d.Refs[0])
	}
}

// --- RBAC & governance builders ---------------------------------------------

func TestRoleDetail(t *testing.T) {
	d := roleDetail(&rbacv1.Role{
		ObjectMeta: meta("pod-reader", "prod"),
		Rules:      []rbacv1.PolicyRule{{Verbs: []string{"get"}, Resources: []string{"pods"}}},
	})
	if d.Status[0].Label != "Rules" || d.Status[0].Value != "1" {
		t.Errorf("Rules chip = %+v, want label Rules value 1", d.Status[0])
	}
}

func TestBindingDetail(t *testing.T) {
	d := roleBindingDetail(&rbacv1.RoleBinding{
		ObjectMeta: meta("read-pods", "prod"),
		RoleRef:    rbacv1.RoleRef{Kind: "Role", Name: "pod-reader"},
		Subjects:   []rbacv1.Subject{{Kind: "ServiceAccount", Name: "default", Namespace: "prod"}},
	})
	if d.Status[0].Value != "Role/pod-reader" {
		t.Errorf("Role chip = %+v", d.Status[0])
	}
	if len(d.Refs) != 2 { // the Role itself + the ServiceAccount subject
		t.Fatalf("Refs = %+v, want 2", d.Refs)
	}
	if d.Refs[0].Group != "Role" || d.Refs[1].Group != "ServiceAccounts" {
		t.Errorf("Refs = %+v", d.Refs)
	}
}

func TestServiceAccountDetail(t *testing.T) {
	d := serviceAccountDetail(&corev1.ServiceAccount{ObjectMeta: meta("default", "prod")})
	found := false
	for _, c := range d.Status {
		if c.Label == "Automount token" && c.Value == "inherits from pod" {
			found = true
		}
	}
	if !found {
		t.Errorf("Status = %+v, want an 'inherits from pod' automount chip", d.Status)
	}
}

func TestEnrichServiceAccountPermissions(t *testing.T) {
	t.Run("no bindings — nothing added", func(t *testing.T) {
		s := newTestServer(t)
		d := &resourceDetail{}
		enrichServiceAccountPermissions(t.Context(), s, "test", "prod", "web", d)
		if len(d.Sections) != 0 {
			t.Errorf("Sections = %+v, want none for an SA with no bindings", d.Sections)
		}
	})

	t.Run("bound role's rules are added", func(t *testing.T) {
		s := newTestServer(t,
			&rbacv1.Role{
				ObjectMeta: meta("pod-reader", "prod"),
				Rules:      []rbacv1.PolicyRule{{Verbs: []string{"get", "list"}, APIGroups: []string{""}, Resources: []string{"pods"}}},
			},
			&rbacv1.RoleBinding{
				ObjectMeta: meta("web-pod-reader", "prod"),
				RoleRef:    rbacv1.RoleRef{Kind: "Role", Name: "pod-reader", APIGroup: "rbac.authorization.k8s.io"},
				Subjects:   []rbacv1.Subject{{Kind: "ServiceAccount", Name: "web", Namespace: "prod"}},
			},
		)
		d := &resourceDetail{}
		enrichServiceAccountPermissions(t.Context(), s, "test", "prod", "web", d)
		if len(d.Sections) != 1 || d.Sections[0].Title != "Effective permissions (verbs → resources)" {
			t.Fatalf("Sections = %+v", d.Sections)
		}
		items := d.Sections[0].Items
		if len(items) != 1 || items[0].Label != "get,list" || items[0].Value != "core/pods" {
			t.Errorf("Items = %+v", items)
		}
	})
}

// TestHandleDetail_ServiceAccountIncludesPermissions confirms the enricher is
// actually wired into GET /detail/serviceaccount/... — not just callable directly.
func TestHandleDetail_ServiceAccountIncludesPermissions(t *testing.T) {
	s := newTestServer(t,
		&corev1.ServiceAccount{ObjectMeta: meta("web", "prod")},
		&rbacv1.Role{
			ObjectMeta: meta("pod-reader", "prod"),
			Rules:      []rbacv1.PolicyRule{{Verbs: []string{"get"}, APIGroups: []string{""}, Resources: []string{"pods"}}},
		},
		&rbacv1.RoleBinding{
			ObjectMeta: meta("web-pod-reader", "prod"),
			RoleRef:    rbacv1.RoleRef{Kind: "Role", Name: "pod-reader", APIGroup: "rbac.authorization.k8s.io"},
			Subjects:   []rbacv1.Subject{{Kind: "ServiceAccount", Name: "web", Namespace: "prod"}},
		},
	)
	rec := doRequest(t, s, "GET", "/api/contexts/test/detail/serviceaccount/prod/web", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var d resourceDetail
	if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, sec := range d.Sections {
		if sec.Title == "Effective permissions (verbs → resources)" {
			found = true
		}
	}
	if !found {
		t.Errorf("Sections = %+v, want an effective-permissions section", d.Sections)
	}
}

func TestResourceQuotaDetail(t *testing.T) {
	d := resourceQuotaDetail(&corev1.ResourceQuota{
		ObjectMeta: meta("quota", "prod"),
		Status: corev1.ResourceQuotaStatus{
			Hard: corev1.ResourceList{corev1.ResourcePods: resource.MustParse("10")},
			Used: corev1.ResourceList{corev1.ResourcePods: resource.MustParse("3")},
		},
	})
	if len(d.Sections) != 1 || d.Sections[0].Title != "Usage / limit" {
		t.Fatalf("Sections = %+v", d.Sections)
	}
	if d.Sections[0].Items[0].Value != "3 / 10" {
		t.Errorf("pods usage = %q, want '3 / 10'", d.Sections[0].Items[0].Value)
	}
}

func TestPdbDetail(t *testing.T) {
	d := pdbDetail(&policyv1.PodDisruptionBudget{
		ObjectMeta: meta("web-pdb", "prod"),
		Status:     policyv1.PodDisruptionBudgetStatus{CurrentHealthy: 2, DesiredHealthy: 2, DisruptionsAllowed: 1},
	})
	if d.Status[2].Label != "Disruptions allowed" || d.Status[2].Value != "1" || d.Status[2].Tone != "muted" {
		t.Errorf("Disruptions chip = %+v", d.Status[2])
	}
}

func TestPriorityClassDetail(t *testing.T) {
	d := priorityClassDetail(&schedulingv1.PriorityClass{
		ObjectMeta:    meta("high", ""),
		Value:         1000000,
		GlobalDefault: true,
	})
	if d.Status[1].Value != "Yes" || d.Status[1].Tone != "ok" {
		t.Errorf("Global default chip = %+v", d.Status[1])
	}
}

func TestRuntimeClassDetail(t *testing.T) {
	d := runtimeClassDetail(&nodev1.RuntimeClass{ObjectMeta: meta("gvisor", ""), Handler: "runsc"})
	if d.Status[0].Value != "runsc" {
		t.Errorf("Handler chip = %+v", d.Status[0])
	}
}

// --- pod & node --------------------------------------------------------------

func TestPodDetail(t *testing.T) {
	d := podDetail(&corev1.Pod{
		ObjectMeta: meta("web-1", "prod"),
		Spec:       corev1.PodSpec{NodeName: "node-1", Containers: []corev1.Container{{Name: "web", Image: "web:1.0"}}},
		Status: corev1.PodStatus{
			Phase:             corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{{Name: "web", Ready: true, RestartCount: 0}},
		},
	})
	if d.Status[0].Value != "Running" || d.Status[0].Tone != "ok" {
		t.Errorf("phase chip = %+v", d.Status[0])
	}
	if d.Status[1].Value != "1/1" {
		t.Errorf("ready chip = %+v", d.Status[1])
	}
	if d.Problem != nil {
		t.Errorf("Problem = %+v, want nil for a healthy pod", d.Problem)
	}
}

func TestPodDetail_ProblemMatchesIssueCarouselDetection(t *testing.T) {
	pending := podDetail(&corev1.Pod{
		ObjectMeta: meta("web-1", "prod"),
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
			ContainerStatuses: []corev1.ContainerStatus{{
				Name:  "web",
				State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff", Message: "pull failed"}},
			}},
		},
	})
	if pending.Problem == nil || pending.Problem.Reason != "ImagePullBackOff" || pending.Problem.Message != "pull failed" || pending.Problem.Tone != "warn" {
		t.Errorf("Pending Problem = %+v", pending.Problem)
	}

	crashLooping := podDetail(&corev1.Pod{
		ObjectMeta: meta("web-1", "prod"),
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{{
				Name: "web", Ready: false,
				State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff", Message: "back-off restarting"}},
			}},
		},
	})
	if crashLooping.Problem == nil || crashLooping.Problem.Reason != "CrashLoopBackOff" || crashLooping.Problem.Tone != "err" {
		t.Errorf("Running/crash-looping Problem = %+v", crashLooping.Problem)
	}

	failed := podDetail(&corev1.Pod{
		ObjectMeta: meta("web-1", "prod"),
		Status: corev1.PodStatus{
			Phase: corev1.PodFailed,
			ContainerStatuses: []corev1.ContainerStatus{{
				Name:  "web",
				State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "OOMKilled"}},
			}},
		},
	})
	if failed.Problem == nil || failed.Problem.Reason != "OOMKilled" || failed.Problem.Tone != "err" {
		t.Errorf("Failed Problem = %+v", failed.Problem)
	}
}

func TestPodDetail_CrashLoopingContainerOverridesPhase(t *testing.T) {
	d := podDetail(&corev1.Pod{
		ObjectMeta: meta("web-1", "prod"),
		Spec:       corev1.PodSpec{NodeName: "node-1", Containers: []corev1.Container{{Name: "web", Image: "web:1.0"}}},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning, // kubelet keeps a crash-looping pod's Phase Running
			ContainerStatuses: []corev1.ContainerStatus{{
				Name: "web", Ready: false,
				State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}},
			}},
		},
	})
	if d.Status[0].Value != "CrashLoopBackOff" || d.Status[0].Tone != "err" {
		t.Errorf("status chip = %+v, want CrashLoopBackOff/err", d.Status[0])
	}
}

func TestNodeDetail(t *testing.T) {
	d := nodeDetail(&corev1.Node{
		ObjectMeta: meta("node-1", ""),
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}},
		},
	})
	if d.Status[0].Value != "Ready" || d.Status[0].Tone != "ok" {
		t.Errorf("Ready chip = %+v", d.Status[0])
	}
	if d.Status[2].Value != "Yes" || d.Status[2].Tone != "ok" {
		t.Errorf("Schedulable chip = %+v", d.Status[2])
	}
}

func TestNodeLabel(t *testing.T) {
	n := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"topology.kubernetes.io/zone": "us-east-1a"}}}
	if got := nodeLabel(n, "topology.kubernetes.io/zone"); got != "us-east-1a" {
		t.Errorf("nodeLabel = %q", got)
	}
	if got := nodeLabel(n, "missing"); got != "—" {
		t.Errorf("nodeLabel(missing) = %q, want em dash", got)
	}
}

func TestReplicaSetDetail(t *testing.T) {
	d := replicaSetDetail(&appsv1.ReplicaSet{
		ObjectMeta: meta("web-abc123", "prod"),
		Spec:       appsv1.ReplicaSetSpec{Replicas: int32Ptr(3)},
		Status:     appsv1.ReplicaSetStatus{ReadyReplicas: 3, Replicas: 3, AvailableReplicas: 3},
	})
	if d.Kind != "ReplicaSet" || d.Status[0].Value != "3/3" || d.Status[0].Tone != "ok" {
		t.Errorf("got kind=%q status=%+v", d.Kind, d.Status[0])
	}
}

func TestStatefulSetDetail(t *testing.T) {
	d := statefulSetDetail(&appsv1.StatefulSet{
		ObjectMeta: meta("db", "prod"),
		Spec: appsv1.StatefulSetSpec{
			Replicas:            int32Ptr(3),
			ServiceName:         "db-headless",
			UpdateStrategy:      appsv1.StatefulSetUpdateStrategy{Type: appsv1.RollingUpdateStatefulSetStrategyType},
			PodManagementPolicy: appsv1.ParallelPodManagement,
		},
		Status: appsv1.StatefulSetStatus{ReadyReplicas: 2, CurrentReplicas: 3, UpdatedReplicas: 3},
	})
	if d.Status[0].Value != "2/3" || d.Status[0].Tone != "warn" {
		t.Errorf("Ready chip = %+v", d.Status[0])
	}
	if len(d.Sections) != 1 || d.Sections[0].Items[0].Value != "db-headless" {
		t.Errorf("Sections = %+v", d.Sections)
	}
}

func TestDaemonSetDetail(t *testing.T) {
	d := daemonSetDetail(&appsv1.DaemonSet{
		ObjectMeta: meta("node-exporter", "monitoring"),
		Spec:       appsv1.DaemonSetSpec{UpdateStrategy: appsv1.DaemonSetUpdateStrategy{Type: appsv1.RollingUpdateDaemonSetStrategyType}},
		Status: appsv1.DaemonSetStatus{
			NumberReady: 3, DesiredNumberScheduled: 3, CurrentNumberScheduled: 3,
			UpdatedNumberScheduled: 3, NumberMisscheduled: 0,
		},
	})
	if d.Status[0].Value != "3/3" || d.Status[0].Tone != "ok" {
		t.Errorf("Ready chip = %+v", d.Status[0])
	}
	if d.Status[3].Label != "Misscheduled" || d.Status[3].Tone != "muted" {
		t.Errorf("Misscheduled chip = %+v, want muted (0 misscheduled is good)", d.Status[3])
	}
}

func TestPvDetail(t *testing.T) {
	d := pvDetail(&corev1.PersistentVolume{
		ObjectMeta: meta("pv-1", ""),
		Spec: corev1.PersistentVolumeSpec{
			ClaimRef:                      &corev1.ObjectReference{Namespace: "prod", Name: "data"},
			PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimRetain,
			PersistentVolumeSource:        corev1.PersistentVolumeSource{CSI: &corev1.CSIPersistentVolumeSource{Driver: "ebs.csi.aws.com"}},
		},
		Status: corev1.PersistentVolumeStatus{Phase: corev1.VolumeBound},
	})
	if d.Status[0].Value != "Bound" || d.Status[0].Tone != "ok" {
		t.Errorf("Status chip = %+v", d.Status[0])
	}
	if len(d.Refs) != 1 || d.Refs[0].Name != "data" || d.Refs[0].Group != "Claim" {
		t.Errorf("Refs = %+v", d.Refs)
	}
}

func TestIngressClassDetail(t *testing.T) {
	d := ingressClassDetail(&networkingv1.IngressClass{
		ObjectMeta: metav1.ObjectMeta{Name: "nginx", Annotations: map[string]string{"ingressclass.kubernetes.io/is-default-class": "true"}},
		Spec:       networkingv1.IngressClassSpec{Controller: "k8s.io/ingress-nginx"},
	})
	if d.Status[0].Value != "k8s.io/ingress-nginx" {
		t.Errorf("Controller chip = %+v", d.Status[0])
	}
	if d.Status[1].Value != "Yes" || d.Status[1].Tone != "ok" {
		t.Errorf("Default chip = %+v", d.Status[1])
	}
}

func TestClusterRoleDetail(t *testing.T) {
	d := clusterRoleDetail(&rbacv1.ClusterRole{
		ObjectMeta: meta("cluster-admin-ish", ""),
		Rules:      []rbacv1.PolicyRule{{Verbs: []string{"*"}, Resources: []string{"*"}, APIGroups: []string{"*"}}},
	})
	if d.Status[0].Label != "Rules" || d.Status[0].Value != "1" {
		t.Errorf("Rules chip = %+v", d.Status[0])
	}
	if len(d.Sections) != 1 || d.Sections[0].Title != "Rules (verbs → resources)" {
		t.Errorf("Sections = %+v", d.Sections)
	}
}

func TestClusterRoleBindingDetail(t *testing.T) {
	d := clusterRoleBindingDetail(&rbacv1.ClusterRoleBinding{
		ObjectMeta: meta("cluster-admins", ""),
		RoleRef:    rbacv1.RoleRef{Kind: "ClusterRole", Name: "cluster-admin"},
		Subjects:   []rbacv1.Subject{{Kind: "Group", Name: "admins"}},
	})
	if d.Kind != "ClusterRoleBinding" || d.Namespace != "" {
		t.Errorf("got kind=%q namespace=%q, want cluster-scoped", d.Kind, d.Namespace)
	}
	if len(d.Refs) != 1 || d.Refs[0].Namespace != "" || d.Refs[0].Name != "cluster-admin" {
		t.Errorf("Refs = %+v, want a namespaceless ClusterRole ref", d.Refs)
	}
}

func TestLimitRangeDetail(t *testing.T) {
	d := limitRangeDetail(&corev1.LimitRange{
		ObjectMeta: meta("defaults", "prod"),
		Spec: corev1.LimitRangeSpec{Limits: []corev1.LimitRangeItem{{
			Type:    corev1.LimitTypeContainer,
			Default: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m")},
			Min:     corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m")},
		}}},
	})
	if len(d.Sections) != 1 || d.Sections[0].Title != "Container" {
		t.Fatalf("Sections = %+v", d.Sections)
	}
	if len(d.Sections[0].Items) != 2 {
		t.Errorf("got %d items, want 2 (default cpu + min cpu)", len(d.Sections[0].Items))
	}
}

// --- handleDetail (HTTP) ----------------------------------------------------

func TestHandleDetail(t *testing.T) {
	s := newTestServer(t, &appsv1.Deployment{
		ObjectMeta: meta("web", "prod"),
		Spec:       appsv1.DeploymentSpec{Replicas: replicas(2)},
	})
	rec := doRequest(t, s, "GET", "/api/contexts/test/detail/deployment/prod/web", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var d resourceDetail
	if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	if d.Kind != "Deployment" || d.Name != "web" {
		t.Errorf("got %+v", d)
	}
}

func TestHandleDetail_UnknownKind(t *testing.T) {
	s := newTestServer(t)
	rec := doRequest(t, s, "GET", "/api/contexts/test/detail/bogus/prod/web", "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestHandleDetail_NotFound(t *testing.T) {
	s := newTestServer(t)
	rec := doRequest(t, s, "GET", "/api/contexts/test/detail/deployment/prod/missing", "")
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rec.Code)
	}
}

func TestHandleDetail_SecretIsAudited(t *testing.T) {
	s := newTestServer(t, &corev1.Secret{ObjectMeta: meta("creds", "prod"), Data: map[string][]byte{"key": []byte("shh")}})
	rec := doRequest(t, s, "GET", "/api/contexts/test/detail/secret/prod/creds", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestEnrichPVCConsumers(t *testing.T) {
	t.Run("not bound — nothing added", func(t *testing.T) {
		s := newTestServer(t)
		d := &resourceDetail{Status: []chip{{Label: "Phase", Value: "Pending"}}}
		enrichPVCConsumers(t.Context(), s, "test", "prod", "data", d)
		if len(d.Status) != 1 {
			t.Errorf("got %+v, want no change for an unbound claim", d.Status)
		}
	})

	t.Run("bound with a mounting pod", func(t *testing.T) {
		s := newTestServer(t, &corev1.Pod{
			ObjectMeta: meta("web-1", "prod"),
			Spec: corev1.PodSpec{Volumes: []corev1.Volume{{
				Name:         "data",
				VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "data"}},
			}}},
		})
		d := &resourceDetail{Status: []chip{{Label: "Phase", Value: string(corev1.ClaimBound)}}}
		enrichPVCConsumers(t.Context(), s, "test", "prod", "data", d)
		if len(d.Refs) != 1 || d.Refs[0].Name != "web-1" {
			t.Errorf("Refs = %+v", d.Refs)
		}
		last := d.Status[len(d.Status)-1]
		if last.Label != "Mounted" || last.Value != "Yes" {
			t.Errorf("got %+v, want Mounted=Yes", last)
		}
	})

	t.Run("bound with no mounting pod", func(t *testing.T) {
		s := newTestServer(t)
		d := &resourceDetail{Status: []chip{{Label: "Phase", Value: string(corev1.ClaimBound)}}}
		enrichPVCConsumers(t.Context(), s, "test", "prod", "data", d)
		last := d.Status[len(d.Status)-1]
		if last.Label != "Mounted" || last.Value != "No" {
			t.Errorf("got %+v, want Mounted=No", last)
		}
	})
}

// --- additional coverage: small pure helpers ---------------------------------

func TestOwnerOf(t *testing.T) {
	t.Run("no owner refs", func(t *testing.T) {
		k, n := ownerOf(metav1.ObjectMeta{})
		if k != "" || n != "" {
			t.Errorf("got kind=%q name=%q, want empty", k, n)
		}
	})
	t.Run("controller owner", func(t *testing.T) {
		isController := true
		k, n := ownerOf(metav1.ObjectMeta{OwnerReferences: []metav1.OwnerReference{
			{Kind: "ReplicaSet", Name: "web-abc123", Controller: &isController},
		}})
		if k != "ReplicaSet" || n != "web-abc123" {
			t.Errorf("got kind=%q name=%q", k, n)
		}
	})
	t.Run("non-controller owner ref is skipped", func(t *testing.T) {
		notController := false
		k, n := ownerOf(metav1.ObjectMeta{OwnerReferences: []metav1.OwnerReference{
			{Kind: "ReplicaSet", Name: "web-abc123", Controller: &notController},
		}})
		if k != "" || n != "" {
			t.Errorf("got kind=%q name=%q, want empty when Controller=false", k, n)
		}
	})
}

func TestWorkloadConditions(t *testing.T) {
	got := workloadConditions([]appsv1.DeploymentCondition{
		{Type: appsv1.DeploymentAvailable, Status: corev1.ConditionTrue},
		{Type: appsv1.DeploymentProgressing, Status: corev1.ConditionFalse},
		{Type: appsv1.DeploymentReplicaFailure, Status: corev1.ConditionUnknown},
	})
	if len(got) != 3 {
		t.Fatalf("got %d conditions, want 3", len(got))
	}
	if got[0].Tone != "ok" {
		t.Errorf("True condition tone = %q, want ok", got[0].Tone)
	}
	if got[1].Tone != "err" {
		t.Errorf("False condition tone = %q, want err", got[1].Tone)
	}
	if got[2].Tone != "muted" {
		t.Errorf("Unknown condition tone = %q, want muted", got[2].Tone)
	}
}

func TestAppendUnique(t *testing.T) {
	if got := appendUnique([]string{"a", "b"}, "b"); len(got) != 2 {
		t.Errorf("got %+v, want no duplicate appended", got)
	}
	if got := appendUnique([]string{"a"}, "b"); len(got) != 2 || got[1] != "b" {
		t.Errorf("got %+v, want b appended", got)
	}
}

func TestIngressStatusChips(t *testing.T) {
	t.Run("no class, no address", func(t *testing.T) {
		got := ingressStatusChips(&networkingv1.Ingress{})
		if got[0].Value != "—" || got[1].Value != "—" {
			t.Errorf("got %+v, want dashes for absent class/address", got)
		}
	})
	t.Run("class and hostname address", func(t *testing.T) {
		class := "nginx"
		got := ingressStatusChips(&networkingv1.Ingress{
			Spec: networkingv1.IngressSpec{IngressClassName: &class},
			Status: networkingv1.IngressStatus{LoadBalancer: networkingv1.IngressLoadBalancerStatus{
				Ingress: []networkingv1.IngressLoadBalancerIngress{{Hostname: "lb.example.com"}},
			}},
		})
		if got[0].Value != "nginx" || got[1].Value != "lb.example.com" {
			t.Errorf("got %+v", got)
		}
	})
	t.Run("IP address fallback when hostname empty", func(t *testing.T) {
		got := ingressStatusChips(&networkingv1.Ingress{
			Status: networkingv1.IngressStatus{LoadBalancer: networkingv1.IngressLoadBalancerStatus{
				Ingress: []networkingv1.IngressLoadBalancerIngress{{IP: "9.9.9.9"}},
			}},
		})
		if got[1].Value != "9.9.9.9" {
			t.Errorf("Address = %q, want 9.9.9.9", got[1].Value)
		}
	})
}

func TestIngressTLSSection(t *testing.T) {
	if s := ingressTLSSection(&networkingv1.Ingress{}); s != nil {
		t.Errorf("got %+v, want nil for no TLS", s)
	}
	s := ingressTLSSection(&networkingv1.Ingress{
		Spec: networkingv1.IngressSpec{TLS: []networkingv1.IngressTLS{{Hosts: []string{"a.example.com", "b.example.com"}}}},
	})
	if s == nil || s.Title != "TLS" || s.Items[0].Value != "a.example.com, b.example.com" {
		t.Errorf("got %+v", s)
	}
}

func TestIngressPathBackend(t *testing.T) {
	t.Run("no backend service", func(t *testing.T) {
		svc, port := ingressPathBackend(networkingv1.HTTPIngressPath{})
		if svc != "" || port != "" {
			t.Errorf("got svc=%q port=%q, want empty", svc, port)
		}
	})
	t.Run("named port", func(t *testing.T) {
		svc, port := ingressPathBackend(networkingv1.HTTPIngressPath{
			Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{Name: "web", Port: networkingv1.ServiceBackendPort{Name: "http"}}},
		})
		if svc != "web" || port != "http" {
			t.Errorf("got svc=%q port=%q", svc, port)
		}
	})
	t.Run("numbered port", func(t *testing.T) {
		svc, port := ingressPathBackend(networkingv1.HTTPIngressPath{
			Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{Name: "web", Port: networkingv1.ServiceBackendPort{Number: 8080}}},
		})
		if svc != "web" || port != "8080" {
			t.Errorf("got svc=%q port=%q", svc, port)
		}
	})
}

func TestNpPorts(t *testing.T) {
	port := intstrFromInt(80)
	proto := corev1.ProtocolTCP
	if got := npPorts([]networkingv1.NetworkPolicyPort{{Port: &port, Protocol: &proto}}); got != "80/TCP" {
		t.Errorf("port+protocol = %q, want 80/TCP", got)
	}
	if got := npPorts([]networkingv1.NetworkPolicyPort{{Protocol: &proto}}); got != "/TCP" {
		t.Errorf("protocol only = %q, want /TCP", got)
	}
	if got := npPorts([]networkingv1.NetworkPolicyPort{{Port: &port}}); got != "80" {
		t.Errorf("port only = %q, want 80", got)
	}
}

func TestPvSource_LocalAndAWSEBS(t *testing.T) {
	if got := pvSource(corev1.PersistentVolumeSource{Local: &corev1.LocalVolumeSource{Path: "/mnt/disk1"}}); got != "Local: /mnt/disk1" {
		t.Errorf("local = %q, want Local: /mnt/disk1", got)
	}
	if got := pvSource(corev1.PersistentVolumeSource{AWSElasticBlockStore: &corev1.AWSElasticBlockStoreVolumeSource{VolumeID: "vol-123"}}); got != "AWS EBS: vol-123" {
		t.Errorf("aws ebs = %q, want AWS EBS: vol-123", got)
	}
}

func TestHpaMetricTarget(t *testing.T) {
	cases := []struct {
		name string
		t    autoscalingv2.MetricTarget
		want string
	}{
		{"utilization", autoscalingv2.MetricTarget{AverageUtilization: int32Ptr(80)}, "80%"},
		{"average value", autoscalingv2.MetricTarget{AverageValue: resource.NewQuantity(100, resource.DecimalSI)}, "100"},
		{"plain value", autoscalingv2.MetricTarget{Value: resource.NewQuantity(5, resource.DecimalSI)}, "5"},
		{"none set", autoscalingv2.MetricTarget{}, "—"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := hpaMetricTarget(c.t); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestHpaMetricCurrent(t *testing.T) {
	cases := []struct {
		name string
		v    autoscalingv2.MetricValueStatus
		want string
	}{
		{"utilization", autoscalingv2.MetricValueStatus{AverageUtilization: int32Ptr(50)}, "50%"},
		{"average value", autoscalingv2.MetricValueStatus{AverageValue: resource.NewQuantity(20, resource.DecimalSI)}, "20"},
		{"plain value", autoscalingv2.MetricValueStatus{Value: resource.NewQuantity(3, resource.DecimalSI)}, "3"},
		{"none set", autoscalingv2.MetricValueStatus{}, "—"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := hpaMetricCurrent(c.v); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestHpaMetricSpec_AllTypes(t *testing.T) {
	util := int32(70)
	cases := []struct {
		name       string
		m          autoscalingv2.MetricSpec
		key, label string
	}{
		{"container resource", autoscalingv2.MetricSpec{
			Type:              autoscalingv2.ContainerResourceMetricSourceType,
			ContainerResource: &autoscalingv2.ContainerResourceMetricSource{Name: corev1.ResourceMemory, Container: "app", Target: autoscalingv2.MetricTarget{AverageUtilization: &util}},
		}, "container/app/memory", "memory (app)"},
		{"pods", autoscalingv2.MetricSpec{
			Type: autoscalingv2.PodsMetricSourceType,
			Pods: &autoscalingv2.PodsMetricSource{Metric: autoscalingv2.MetricIdentifier{Name: "queue_depth"}, Target: autoscalingv2.MetricTarget{Value: resource.NewQuantity(10, resource.DecimalSI)}},
		}, "pods/queue_depth", "queue_depth"},
		{"object", autoscalingv2.MetricSpec{
			Type:   autoscalingv2.ObjectMetricSourceType,
			Object: &autoscalingv2.ObjectMetricSource{Metric: autoscalingv2.MetricIdentifier{Name: "requests-per-second"}, Target: autoscalingv2.MetricTarget{Value: resource.NewQuantity(1000, resource.DecimalSI)}},
		}, "object/requests-per-second", "requests-per-second"},
		{"external", autoscalingv2.MetricSpec{
			Type:     autoscalingv2.ExternalMetricSourceType,
			External: &autoscalingv2.ExternalMetricSource{Metric: autoscalingv2.MetricIdentifier{Name: "queue_length"}, Target: autoscalingv2.MetricTarget{Value: resource.NewQuantity(30, resource.DecimalSI)}},
		}, "external/queue_length", "queue_length"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			key, label, _ := hpaMetricSpec(c.m)
			if key != c.key || label != c.label {
				t.Errorf("got key=%q label=%q, want key=%q label=%q", key, label, c.key, c.label)
			}
		})
	}
	t.Run("unknown type falls back to the raw type string", func(t *testing.T) {
		key, label, target := hpaMetricSpec(autoscalingv2.MetricSpec{Type: "Weird"})
		if key != "Weird" || label != "Weird" || target != "—" {
			t.Errorf("got key=%q label=%q target=%q", key, label, target)
		}
	})
}

func TestHpaMetricStatus_AllTypes(t *testing.T) {
	cases := []struct {
		name string
		m    autoscalingv2.MetricStatus
		key  string
	}{
		{"container resource", autoscalingv2.MetricStatus{
			Type:              autoscalingv2.ContainerResourceMetricSourceType,
			ContainerResource: &autoscalingv2.ContainerResourceMetricStatus{Name: corev1.ResourceMemory, Container: "app", Current: autoscalingv2.MetricValueStatus{AverageUtilization: int32Ptr(60)}},
		}, "container/app/memory"},
		{"pods", autoscalingv2.MetricStatus{
			Type: autoscalingv2.PodsMetricSourceType,
			Pods: &autoscalingv2.PodsMetricStatus{Metric: autoscalingv2.MetricIdentifier{Name: "queue_depth"}, Current: autoscalingv2.MetricValueStatus{Value: resource.NewQuantity(4, resource.DecimalSI)}},
		}, "pods/queue_depth"},
		{"object", autoscalingv2.MetricStatus{
			Type:   autoscalingv2.ObjectMetricSourceType,
			Object: &autoscalingv2.ObjectMetricStatus{Metric: autoscalingv2.MetricIdentifier{Name: "rps"}, Current: autoscalingv2.MetricValueStatus{Value: resource.NewQuantity(500, resource.DecimalSI)}},
		}, "object/rps"},
		{"external", autoscalingv2.MetricStatus{
			Type:     autoscalingv2.ExternalMetricSourceType,
			External: &autoscalingv2.ExternalMetricStatus{Metric: autoscalingv2.MetricIdentifier{Name: "ql"}, Current: autoscalingv2.MetricValueStatus{Value: resource.NewQuantity(9, resource.DecimalSI)}},
		}, "external/ql"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			key, _ := hpaMetricStatus(c.m)
			if key != c.key {
				t.Errorf("got key=%q, want %q", key, c.key)
			}
		})
	}
	t.Run("unknown type", func(t *testing.T) {
		key, cur := hpaMetricStatus(autoscalingv2.MetricStatus{Type: "Weird"})
		if key != "Weird" || cur != "—" {
			t.Errorf("got key=%q cur=%q", key, cur)
		}
	})
}

func TestEndpointSliceEndpoints(t *testing.T) {
	ready := true
	notReady := false
	o := &discoveryv1.EndpointSlice{
		ObjectMeta: meta("web-abc", "prod"),
		Endpoints: []discoveryv1.Endpoint{
			{Addresses: []string{"10.0.0.1"}, Conditions: discoveryv1.EndpointConditions{Ready: &ready}, TargetRef: &corev1.ObjectReference{Kind: "Pod", Name: "web-1"}},
			{Addresses: []string{"10.0.0.2"}, Conditions: discoveryv1.EndpointConditions{Ready: &notReady}},
			{Addresses: []string{"10.0.0.3"}, Conditions: discoveryv1.EndpointConditions{Ready: &ready}, TargetRef: &corev1.ObjectReference{Kind: "Node", Name: "external-thing"}},
		},
	}
	refs, orphans := endpointSliceEndpoints(o, "Endpoints (3)")
	if len(refs) != 1 || refs[0].Name != "web-1" || refs[0].Namespace != "prod" {
		t.Fatalf("refs = %+v, want just the Pod-backed endpoint, defaulted to the slice's namespace", refs)
	}
	if len(orphans) != 2 {
		t.Fatalf("orphans = %+v, want 2 (not-ready + non-Pod target)", orphans)
	}
	if orphans[0].Value != "not ready" {
		t.Errorf("not-ready orphan = %+v, want value 'not ready'", orphans[0])
	}
	if orphans[1].Value != "ready" {
		t.Errorf("Node-targeted endpoint = %+v, want treated as a plain ready orphan", orphans[1])
	}
}

func TestIngressClassDetail_ParametersAndNonDefault(t *testing.T) {
	group := "k8s.io"
	scope := "Cluster"
	d := ingressClassDetail(&networkingv1.IngressClass{
		ObjectMeta: meta("custom", ""),
		Spec: networkingv1.IngressClassSpec{
			Controller: "example.com/ingress",
			Parameters: &networkingv1.IngressClassParametersReference{Kind: "ConfigMap", Name: "params", APIGroup: &group, Scope: &scope},
		},
	})
	if d.Status[1].Value != "No" || d.Status[1].Tone != "muted" {
		t.Errorf("Default chip = %+v, want No/muted", d.Status[1])
	}
	if len(d.Sections) != 1 || d.Sections[0].Title != "Parameters" {
		t.Fatalf("Sections = %+v", d.Sections)
	}
	items := d.Sections[0].Items
	if len(items) != 4 {
		t.Fatalf("got %d items, want 4 (Kind, Name, API group, Scope)", len(items))
	}
	if items[2].Label != "API group" || items[2].Value != "k8s.io" {
		t.Errorf("API group item = %+v", items[2])
	}
	if items[3].Label != "Scope" || items[3].Value != "Cluster" {
		t.Errorf("Scope item = %+v", items[3])
	}
}

func TestServiceAccountDetail_AutomountAndPullSecrets(t *testing.T) {
	t.Run("automount true", func(t *testing.T) {
		yes := true
		d := serviceAccountDetail(&corev1.ServiceAccount{ObjectMeta: meta("web", "prod"), AutomountServiceAccountToken: &yes})
		if d.Status[1].Value != "yes" {
			t.Errorf("Automount chip = %+v, want yes", d.Status[1])
		}
	})
	t.Run("automount false", func(t *testing.T) {
		no := false
		d := serviceAccountDetail(&corev1.ServiceAccount{ObjectMeta: meta("web", "prod"), AutomountServiceAccountToken: &no})
		if d.Status[1].Value != "no" {
			t.Errorf("Automount chip = %+v, want no", d.Status[1])
		}
	})
	t.Run("secrets and image pull secrets", func(t *testing.T) {
		d := serviceAccountDetail(&corev1.ServiceAccount{
			ObjectMeta:       meta("web", "prod"),
			Secrets:          []corev1.ObjectReference{{Name: "web-token"}},
			ImagePullSecrets: []corev1.LocalObjectReference{{Name: "registry-creds"}},
		})
		if len(d.Refs) != 1 || d.Refs[0].Name != "web-token" {
			t.Errorf("Refs = %+v", d.Refs)
		}
		if len(d.Sections) != 1 || d.Sections[0].Title != "Image pull secrets" || d.Sections[0].Items[0].Label != "registry-creds" {
			t.Errorf("Sections = %+v", d.Sections)
		}
	})
}

func TestRuntimeClassSchedulingSection(t *testing.T) {
	t.Run("no scheduling", func(t *testing.T) {
		if s := runtimeClassSchedulingSection(&nodev1.RuntimeClass{}); s != nil {
			t.Errorf("got %+v, want nil", s)
		}
	})
	t.Run("scheduling set but empty yields nil section", func(t *testing.T) {
		if s := runtimeClassSchedulingSection(&nodev1.RuntimeClass{Scheduling: &nodev1.Scheduling{}}); s != nil {
			t.Errorf("got %+v, want nil", s)
		}
	})
	t.Run("node selector and tolerations", func(t *testing.T) {
		s := runtimeClassSchedulingSection(&nodev1.RuntimeClass{
			Scheduling: &nodev1.Scheduling{
				NodeSelector: map[string]string{"kubernetes.io/arch": "amd64"},
				Tolerations:  []corev1.Toleration{{Key: "special", Value: "true", Effect: corev1.TaintEffectNoSchedule}},
			},
		})
		if s == nil || s.Title != "Scheduling" || len(s.Items) != 2 {
			t.Fatalf("got %+v", s)
		}
		if s.Items[1].Label != "toleration special=true" {
			t.Errorf("toleration item = %+v", s.Items[1])
		}
	})
}

func TestRuntimeClassOverheadSection(t *testing.T) {
	if s := runtimeClassOverheadSection(&nodev1.RuntimeClass{}); s != nil {
		t.Errorf("got %+v, want nil for no Overhead", s)
	}
	s := runtimeClassOverheadSection(&nodev1.RuntimeClass{
		Overhead: &nodev1.Overhead{PodFixed: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("250m"),
			corev1.ResourceMemory: resource.MustParse("64Mi"),
		}},
	})
	if s == nil || s.Title != "Overhead" || len(s.Items) != 2 || s.Items[0].Label != "cpu" || s.Items[1].Label != "memory" {
		t.Errorf("got %+v, want cpu then memory (sorted by label)", s)
	}
}

func TestRuntimeClassDetail_WithSchedulingAndOverhead(t *testing.T) {
	d := runtimeClassDetail(&nodev1.RuntimeClass{
		ObjectMeta: meta("gvisor", ""),
		Handler:    "runsc",
		Scheduling: &nodev1.Scheduling{NodeSelector: map[string]string{"arch": "amd64"}},
		Overhead:   &nodev1.Overhead{PodFixed: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("64Mi")}},
	})
	if len(d.Sections) != 2 {
		t.Fatalf("Sections = %+v, want a Scheduling section and an Overhead section", d.Sections)
	}
	if d.Sections[0].Title != "Scheduling" || d.Sections[1].Title != "Overhead" {
		t.Errorf("Sections = %+v", d.Sections)
	}
}

func TestNodeAddresses(t *testing.T) {
	ip, host := nodeAddresses(&corev1.Node{Status: corev1.NodeStatus{Addresses: []corev1.NodeAddress{
		{Type: corev1.NodeInternalIP, Address: "10.0.0.5"},
		{Type: corev1.NodeHostName, Address: "node-1.internal"},
		{Type: corev1.NodeExternalIP, Address: "1.2.3.4"},
	}}})
	if ip != "10.0.0.5" || host != "node-1.internal" {
		t.Errorf("got ip=%q host=%q, want ip=10.0.0.5 host=node-1.internal (external IP ignored)", ip, host)
	}
}

func TestNodeTaintsSection(t *testing.T) {
	if s := nodeTaintsSection(&corev1.Node{}); s != nil {
		t.Errorf("got %+v, want nil for no taints", s)
	}
	s := nodeTaintsSection(&corev1.Node{Spec: corev1.NodeSpec{Taints: []corev1.Taint{
		{Key: "dedicated", Value: "gpu", Effect: corev1.TaintEffectNoSchedule},
		{Key: "spot", Effect: corev1.TaintEffectPreferNoSchedule},
	}}})
	if s == nil || s.Title != "Taints" || len(s.Items) != 2 {
		t.Fatalf("got %+v", s)
	}
	if s.Items[0].Label != "dedicated=gpu" {
		t.Errorf("taint with value = %+v, want label dedicated=gpu", s.Items[0])
	}
	if s.Items[1].Label != "spot" {
		t.Errorf("taint without value = %+v, want label spot", s.Items[1])
	}
}

func TestDeploymentDetail_RollingUpdateStrategy(t *testing.T) {
	surge := intstrFromInt(1)
	unavail := intstrFromInt(0)
	d := deploymentDetail(&appsv1.Deployment{
		ObjectMeta: meta("web", "prod"),
		Spec: appsv1.DeploymentSpec{
			Strategy: appsv1.DeploymentStrategy{
				Type:          appsv1.RollingUpdateDeploymentStrategyType,
				RollingUpdate: &appsv1.RollingUpdateDeployment{MaxSurge: &surge, MaxUnavailable: &unavail},
			},
		},
	})
	items := d.Sections[0].Items
	if len(items) != 3 {
		t.Fatalf("got %d items, want 3 (Type, Max surge, Max unavailable)", len(items))
	}
	if items[1].Label != "Max surge" || items[1].Value != "1" {
		t.Errorf("Max surge item = %+v", items[1])
	}
	if items[2].Label != "Max unavailable" || items[2].Value != "0" {
		t.Errorf("Max unavailable item = %+v", items[2])
	}
}

func TestServiceDetail_ExternalAddressSources(t *testing.T) {
	t.Run("load balancer hostname", func(t *testing.T) {
		d := serviceDetail(&corev1.Service{
			ObjectMeta: meta("web", "prod"),
			Status:     corev1.ServiceStatus{LoadBalancer: corev1.LoadBalancerStatus{Ingress: []corev1.LoadBalancerIngress{{Hostname: "lb.example.com"}}}},
		})
		if !hasKV(d.Sections, "External", "lb.example.com") {
			t.Errorf("Sections = %+v, want External=lb.example.com", d.Sections)
		}
	})
	t.Run("load balancer IP falls back when hostname empty", func(t *testing.T) {
		d := serviceDetail(&corev1.Service{
			ObjectMeta: meta("web", "prod"),
			Status:     corev1.ServiceStatus{LoadBalancer: corev1.LoadBalancerStatus{Ingress: []corev1.LoadBalancerIngress{{IP: "1.2.3.4"}}}},
		})
		if !hasKV(d.Sections, "External", "1.2.3.4") {
			t.Errorf("Sections = %+v, want External=1.2.3.4", d.Sections)
		}
	})
	t.Run("external IPs used when no load balancer", func(t *testing.T) {
		d := serviceDetail(&corev1.Service{
			ObjectMeta: meta("web", "prod"),
			Spec:       corev1.ServiceSpec{ExternalIPs: []string{"5.6.7.8"}},
		})
		if !hasKV(d.Sections, "External", "5.6.7.8") {
			t.Errorf("Sections = %+v, want External=5.6.7.8", d.Sections)
		}
	})
	t.Run("node port surfaces as Extra", func(t *testing.T) {
		d := serviceDetail(&corev1.Service{
			ObjectMeta: meta("web", "prod"),
			Spec: corev1.ServiceSpec{
				Type:  corev1.ServiceTypeNodePort,
				Ports: []corev1.ServicePort{{Port: 80, TargetPort: intstrFromInt(8080), NodePort: 30080}},
			},
		})
		if len(d.Ports) != 1 || d.Ports[0].Extra != "node 30080" {
			t.Errorf("Ports = %+v, want Extra 'node 30080'", d.Ports)
		}
	})
}

// hasKV reports whether any section carries an item with the given label/value.
func hasKV(sections []section, label, value string) bool {
	for _, s := range sections {
		for _, i := range s.Items {
			if i.Label == label && i.Value == value {
				return true
			}
		}
	}
	return false
}

func TestConfigMapDetail_BinaryData(t *testing.T) {
	d := configMapDetail(&corev1.ConfigMap{
		ObjectMeta: meta("app-config", "prod"),
		BinaryData: map[string][]byte{"cert.der": {0x01, 0x02, 0x03}},
	})
	found := false
	for _, s := range d.Sections {
		if s.Title == "Binary data" && len(s.Items) == 1 && s.Items[0].Label == "cert.der" && s.Items[0].Value == "3 bytes (binary)" {
			found = true
		}
	}
	if !found {
		t.Errorf("Sections = %+v, want a Binary data section", d.Sections)
	}
}

func TestResourceQuotaDetail_Scopes(t *testing.T) {
	d := resourceQuotaDetail(&corev1.ResourceQuota{
		ObjectMeta: meta("quota", "prod"),
		Spec:       corev1.ResourceQuotaSpec{Scopes: []corev1.ResourceQuotaScope{corev1.ResourceQuotaScopeBestEffort}},
	})
	if len(d.Status) != 1 || d.Status[0].Label != "Scopes" || d.Status[0].Value != "BestEffort" {
		t.Errorf("Status = %+v, want a single Scopes=BestEffort chip", d.Status)
	}
}

func TestHpaDetail_MetricsAndConditions(t *testing.T) {
	d := hpaDetail(&autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: meta("web", "prod"),
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			MaxReplicas:    5,
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "CustomThing", Name: "widget"},
			Metrics: []autoscalingv2.MetricSpec{{
				Type:     autoscalingv2.ResourceMetricSourceType,
				Resource: &autoscalingv2.ResourceMetricSource{Name: corev1.ResourceCPU, Target: autoscalingv2.MetricTarget{AverageUtilization: int32Ptr(80)}},
			}},
		},
		Status: autoscalingv2.HorizontalPodAutoscalerStatus{
			CurrentMetrics: []autoscalingv2.MetricStatus{{
				Type:     autoscalingv2.ResourceMetricSourceType,
				Resource: &autoscalingv2.ResourceMetricStatus{Name: corev1.ResourceCPU, Current: autoscalingv2.MetricValueStatus{AverageUtilization: int32Ptr(50)}},
			}},
			Conditions: []autoscalingv2.HorizontalPodAutoscalerCondition{
				{Type: autoscalingv2.AbleToScale, Status: corev1.ConditionTrue},
				{Type: autoscalingv2.ScalingLimited, Status: corev1.ConditionFalse},
			},
		},
	})
	// MinReplicas unset defaults to 1.
	if d.Status[1].Value != "1" {
		t.Errorf("Min chip = %+v, want default 1", d.Status[1])
	}
	if len(d.Sections) != 2 {
		t.Fatalf("Sections = %+v, want a Metrics section and a plain Target section", d.Sections)
	}
	if d.Sections[0].Items[0].Value != "50% / 80%" {
		t.Errorf("Metrics item = %+v, want '50%% / 80%%'", d.Sections[0].Items[0])
	}
	// "CustomThing" isn't a known manifest slug, so the target renders as a
	// plain kv row instead of a clickable ref.
	if len(d.Refs) != 0 || d.Sections[1].Title != "Target" || d.Sections[1].Items[0].Label != "CustomThing" {
		t.Errorf("got Refs=%+v Sections[1]=%+v, want a plain CustomThing target row and no Refs", d.Refs, d.Sections[1])
	}
	if len(d.Conditions) != 2 || d.Conditions[0].Tone != "ok" || d.Conditions[1].Tone != "warn" {
		t.Errorf("Conditions = %+v", d.Conditions)
	}
}

func TestHpaDetail_MetricWithNoCurrentReading(t *testing.T) {
	d := hpaDetail(&autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: meta("web", "prod"),
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			MaxReplicas:    5,
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: "web"},
			Metrics: []autoscalingv2.MetricSpec{{
				Type: autoscalingv2.PodsMetricSourceType,
				Pods: &autoscalingv2.PodsMetricSource{Metric: autoscalingv2.MetricIdentifier{Name: "requests"}, Target: autoscalingv2.MetricTarget{AverageValue: resource.NewQuantity(100, resource.DecimalSI)}},
			}},
		},
	})
	if d.Sections[0].Items[0].Value != "— / 100" {
		t.Errorf("got %q, want '— / 100' when no status metric matches", d.Sections[0].Items[0].Value)
	}
}
