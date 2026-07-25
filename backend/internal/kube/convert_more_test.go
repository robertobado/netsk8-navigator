package kube

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	nodev1 "k8s.io/api/node/v1"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func replicas(n int32) *int32 { return &n }

// The rest of the To*View projections are near-mechanical field copies (no
// branching beyond a nil-pointer default), covered here in one pass rather
// than one test function each.

func TestToConfigMapView(t *testing.T) {
	c := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "app-config", Namespace: "prod"},
		Data:       map[string]string{"a": "1", "b": "2"},
		BinaryData: map[string][]byte{"c": {0x1}},
	}
	v := ToConfigMapView(c)
	if v.Name != "app-config" || v.Keys != 3 {
		t.Errorf("got %+v", v)
	}
}

func TestToNamespaceView(t *testing.T) {
	n := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "prod"}, Status: corev1.NamespaceStatus{Phase: corev1.NamespaceTerminating}}
	if got := ToNamespaceView(n).Status; got != "Terminating" {
		t.Errorf("Status = %q", got)
	}
}

func TestToSecretView(t *testing.T) {
	s := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "creds", Namespace: "prod"},
		Type:       corev1.SecretTypeOpaque,
		Data:       map[string][]byte{"user": {}, "pass": {}},
	}
	v := ToSecretView(s)
	if v.Keys != 2 || v.Type != "Opaque" {
		t.Errorf("got %+v", v)
	}
}

func TestToPVView(t *testing.T) {
	sc := "fast"
	p := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: "pv-1"},
		Spec: corev1.PersistentVolumeSpec{
			StorageClassName: sc,
			ClaimRef:         &corev1.ObjectReference{Namespace: "prod", Name: "data"},
		},
	}
	v := ToPVView(p)
	if v.Claim != "prod/data" || v.StorageClass != "fast" {
		t.Errorf("got %+v", v)
	}
}

func TestToStorageClassView(t *testing.T) {
	sc := &storagev1.StorageClass{
		ObjectMeta:  metav1.ObjectMeta{Name: "fast", Annotations: map[string]string{"storageclass.kubernetes.io/is-default-class": "true"}},
		Provisioner: "ebs.csi.aws.com",
	}
	if v := ToStorageClassView(sc); !v.Default {
		t.Errorf("Default = %v, want true", v.Default)
	}
}

func TestToEndpointSliceView(t *testing.T) {
	name := "http"
	var port int32 = 80
	proto := corev1.ProtocolTCP
	yes, no := true, false
	s := &discoveryv1.EndpointSlice{
		ObjectMeta:  metav1.ObjectMeta{Name: "web-abc", Labels: map[string]string{"kubernetes.io/service-name": "web"}},
		AddressType: discoveryv1.AddressTypeIPv4,
		Endpoints: []discoveryv1.Endpoint{
			{Conditions: discoveryv1.EndpointConditions{Ready: &yes}},
			{Conditions: discoveryv1.EndpointConditions{Ready: &no}},
		},
		Ports: []discoveryv1.EndpointPort{{Name: &name, Port: &port, Protocol: &proto}},
	}
	v := ToEndpointSliceView(s)
	if v.Ready != 1 || v.Total != 2 || v.Service != "web" || v.Ports != "http:80/TCP" {
		t.Errorf("got %+v", v)
	}
}

func TestToNetworkPolicyView(t *testing.T) {
	n := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "deny-all", Namespace: "prod"},
		Spec: networkingv1.NetworkPolicySpec{
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress},
		},
	}
	v := ToNetworkPolicyView(n)
	if v.PolicyTypes != "Ingress, Egress" || v.PodSelector != "todos" {
		t.Errorf("got %+v", v)
	}
}

func TestToIngressClassView(t *testing.T) {
	c := &networkingv1.IngressClass{
		ObjectMeta: metav1.ObjectMeta{Name: "nginx", Annotations: map[string]string{"ingressclass.kubernetes.io/is-default-class": "true"}},
		Spec:       networkingv1.IngressClassSpec{Controller: "k8s.io/ingress-nginx"},
	}
	v := ToIngressClassView(c)
	if !v.Default || v.Controller != "k8s.io/ingress-nginx" {
		t.Errorf("got %+v", v)
	}
}

func TestToServiceAccountView(t *testing.T) {
	s := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "prod"},
		Secrets:    []corev1.ObjectReference{{Name: "web-token"}},
	}
	if v := ToServiceAccountView(s); v.Secrets != 1 {
		t.Errorf("got %+v", v)
	}
}

func TestToRoleViews(t *testing.T) {
	r := &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: "pod-reader", Namespace: "prod"}, Rules: []rbacv1.PolicyRule{{}, {}}}
	if v := ToRoleView(r); v.Rules != 2 || v.Namespace != "prod" {
		t.Errorf("Role: got %+v", v)
	}
	cr := &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "admin"}, Rules: []rbacv1.PolicyRule{{}}}
	if v := ToClusterRoleView(cr); v.Rules != 1 || v.Namespace != "" {
		t.Errorf("ClusterRole: got %+v", v)
	}
}

func TestToRoleBindingViews(t *testing.T) {
	rb := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "rb", Namespace: "prod"},
		RoleRef:    rbacv1.RoleRef{Kind: "Role", Name: "pod-reader"},
		Subjects:   []rbacv1.Subject{{Kind: "ServiceAccount", Name: "web"}},
	}
	v := ToRoleBindingView(rb)
	if v.Role != "Role/pod-reader" || len(v.Subjects) != 1 || v.Subjects[0] != "prod/web" {
		t.Errorf("RoleBinding: got %+v", v)
	}
	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "crb"},
		RoleRef:    rbacv1.RoleRef{Kind: "ClusterRole", Name: "admin"},
		Subjects:   []rbacv1.Subject{{Kind: "ServiceAccount", Name: "web", Namespace: "prod"}},
	}
	cv := ToClusterRoleBindingView(crb)
	if cv.Role != "ClusterRole/admin" || cv.Subjects[0] != "prod/web" {
		t.Errorf("ClusterRoleBinding: got %+v", cv)
	}
}

func TestToResourceQuotaAndLimitRangeViews(t *testing.T) {
	q := &corev1.ResourceQuota{ObjectMeta: metav1.ObjectMeta{Name: "quota", Namespace: "prod"}}
	if v := ToResourceQuotaView(q); v.Name != "quota" {
		t.Errorf("got %+v", v)
	}
	l := &corev1.LimitRange{ObjectMeta: metav1.ObjectMeta{Name: "limits", Namespace: "prod"}}
	if v := ToLimitRangeView(l); v.Name != "limits" {
		t.Errorf("got %+v", v)
	}
}

func TestToPDBView(t *testing.T) {
	min := intstr.FromInt(2)
	p := &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{Name: "pdb", Namespace: "prod"},
		Spec:       policyv1.PodDisruptionBudgetSpec{MinAvailable: &min},
		Status:     policyv1.PodDisruptionBudgetStatus{CurrentHealthy: 3, DesiredHealthy: 2, DisruptionsAllowed: 1},
	}
	v := ToPDBView(p)
	if v.Criteria != "min 2" || v.Current != 3 || v.Allowed != 1 {
		t.Errorf("got %+v", v)
	}

	max := intstr.FromInt(1)
	p2 := &policyv1.PodDisruptionBudget{Spec: policyv1.PodDisruptionBudgetSpec{MaxUnavailable: &max}}
	if got := ToPDBView(p2).Criteria; got != "max 1" {
		t.Errorf("Criteria = %q, want %q", got, "max 1")
	}
}

func TestToPriorityClassView(t *testing.T) {
	p := &schedulingv1.PriorityClass{ObjectMeta: metav1.ObjectMeta{Name: "high"}, Value: 1000, GlobalDefault: true}
	v := ToPriorityClassView(p)
	if v.Value != 1000 || !v.GlobalDefault || v.Preemption != string(corev1.PreemptLowerPriority) {
		t.Errorf("got %+v", v)
	}
}

func TestToRuntimeClassView(t *testing.T) {
	rc := &nodev1.RuntimeClass{ObjectMeta: metav1.ObjectMeta{Name: "gvisor"}, Handler: "runsc"}
	if v := ToRuntimeClassView(rc); v.Handler != "runsc" {
		t.Errorf("got %+v", v)
	}
}

func TestToStatefulSetView(t *testing.T) {
	o := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "prod"},
		Spec:       appsv1.StatefulSetSpec{Replicas: replicas(3), ServiceName: "db-headless"},
		Status:     appsv1.StatefulSetStatus{ReadyReplicas: 2},
	}
	v := ToStatefulSetView(o)
	if v.Ready != "2/3" || v.Service != "db-headless" {
		t.Errorf("got %+v", v)
	}
}

func TestToDaemonSetView(t *testing.T) {
	o := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: "fluentd", Namespace: "kube-system"},
		Status:     appsv1.DaemonSetStatus{NumberReady: 3, DesiredNumberScheduled: 4, UpdatedNumberScheduled: 3, NumberAvailable: 3},
	}
	v := ToDaemonSetView(o)
	if v.Ready != "3/4" || v.UpToDate != 3 || v.Available != 3 {
		t.Errorf("got %+v", v)
	}
}

func TestToCronJobView(t *testing.T) {
	suspend := true
	last := metav1.NewTime(metav1.Now().Time)
	o := &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{Name: "nightly", Namespace: "prod"},
		Spec:       batchv1.CronJobSpec{Schedule: "0 0 * * *", Suspend: &suspend},
		Status:     batchv1.CronJobStatus{Active: []corev1.ObjectReference{{}}, LastScheduleTime: &last},
	}
	v := ToCronJobView(o)
	if !v.Suspend || v.Active != 1 || v.Schedule != "0 0 * * *" || v.LastSchedule == "" {
		t.Errorf("got %+v", v)
	}
}
