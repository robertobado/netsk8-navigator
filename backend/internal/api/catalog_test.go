package api

import (
	"encoding/json"
	"net/http"
	"path/filepath"
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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"

	"github.com/robertobado/netsk8-navigator/backend/internal/config"
	"github.com/robertobado/netsk8-navigator/backend/internal/kube"
)

// testGVRs (in testutil_test.go) only seeds the resources exercised by the
// handler tests that existed before this file grew to cover the rest of the
// catalog. Extending it here — rather than editing testutil_test.go — lets
// these additional catalog entries resolve through fakeManager.ResolveResource
// without touching the shared test infra.
func init() {
	extra := map[string]kube.Resource{
		"ingresses":                {GVR: networkingv1.SchemeGroupVersion.WithResource("ingresses"), Namespaced: true},
		"cronjobs":                 {GVR: batchv1.SchemeGroupVersion.WithResource("cronjobs"), Namespaced: true},
		"persistentvolumes":        {GVR: corev1.SchemeGroupVersion.WithResource("persistentvolumes"), Namespaced: false},
		"storageclasses":           {GVR: storagev1.SchemeGroupVersion.WithResource("storageclasses"), Namespaced: false},
		"horizontalpodautoscalers": {GVR: autoscalingv2.SchemeGroupVersion.WithResource("horizontalpodautoscalers"), Namespaced: true},
		"endpointslices":           {GVR: discoveryv1.SchemeGroupVersion.WithResource("endpointslices"), Namespaced: true},
		"networkpolicies":          {GVR: networkingv1.SchemeGroupVersion.WithResource("networkpolicies"), Namespaced: true},
		"ingressclasses":           {GVR: networkingv1.SchemeGroupVersion.WithResource("ingressclasses"), Namespaced: false},
		"roles":                    {GVR: rbacv1.SchemeGroupVersion.WithResource("roles"), Namespaced: true},
		"clusterroles":             {GVR: rbacv1.SchemeGroupVersion.WithResource("clusterroles"), Namespaced: false},
		"rolebindings":             {GVR: rbacv1.SchemeGroupVersion.WithResource("rolebindings"), Namespaced: true},
		"clusterrolebindings":      {GVR: rbacv1.SchemeGroupVersion.WithResource("clusterrolebindings"), Namespaced: false},
		"resourcequotas":           {GVR: corev1.SchemeGroupVersion.WithResource("resourcequotas"), Namespaced: true},
		"limitranges":              {GVR: corev1.SchemeGroupVersion.WithResource("limitranges"), Namespaced: true},
		"poddisruptionbudgets":     {GVR: policyv1.SchemeGroupVersion.WithResource("poddisruptionbudgets"), Namespaced: true},
		"priorityclasses":          {GVR: schedulingv1.SchemeGroupVersion.WithResource("priorityclasses"), Namespaced: false},
		"runtimeclasses":           {GVR: nodev1.SchemeGroupVersion.WithResource("runtimeclasses"), Namespaced: false},
	}
	for k, v := range extra {
		testGVRs[k] = v
	}
}

func TestHandleResourceList_Nodes(t *testing.T) {
	s := newTestServer(t, &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}},
			NodeInfo:   corev1.NodeSystemInfo{KubeletVersion: "v1.30.0"},
		},
	})
	rec := doRequest(t, s, "GET", "/api/contexts/test/resources/nodes", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var out []nodeView
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Name != "node-1" || out[0].Status != "Ready" || out[0].Version != "v1.30.0" {
		t.Errorf("got %+v", out)
	}
}

func TestHandleResourceList_Jobs_EnrichesStuckStatus(t *testing.T) {
	s := newTestServer(t,
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "backup", Namespace: "prod"}},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "backup-1", Namespace: "prod", OwnerReferences: []metav1.OwnerReference{
				{Kind: "Job", Name: "backup", Controller: boolPtr(true)},
			}},
			Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{
				{State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}},
			}},
		},
	)
	rec := doRequest(t, s, "GET", "/api/contexts/test/resources/jobs?namespace=prod", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var out []kube.JobView
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Status != "CrashLoopBackOff" {
		t.Errorf("got %+v, want the job's status surfaced from its stuck pod", out)
	}
}

func TestHandleResourceList_Jobs_HealthyPodLeavesStatusAlone(t *testing.T) {
	s := newTestServer(t,
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "backup", Namespace: "prod"}},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "backup-1", Namespace: "prod", OwnerReferences: []metav1.OwnerReference{
				{Kind: "Job", Name: "backup", Controller: boolPtr(true)},
			}},
			Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{
				{State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ContainerCreating"}}},
			}},
		},
	)
	rec := doRequest(t, s, "GET", "/api/contexts/test/resources/jobs?namespace=prod", "")
	var out []kube.JobView
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Status != "Running" {
		t.Errorf("got %+v, want Running (transient reasons are excluded)", out)
	}
}

func TestHandleResourceList_PVCs_EnrichesMountedBy(t *testing.T) {
	s := newTestServer(t,
		&corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "data", Namespace: "prod"},
			Status:     corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "web-1", Namespace: "prod"},
			Spec: corev1.PodSpec{
				Volumes: []corev1.Volume{{
					Name:         "data-vol",
					VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "data"}},
				}},
				Containers: []corev1.Container{{
					Name:         "app",
					VolumeMounts: []corev1.VolumeMount{{Name: "data-vol", MountPath: "/data"}},
				}},
			},
		},
	)
	rec := doRequest(t, s, "GET", "/api/contexts/test/resources/persistentvolumeclaims?namespace=prod", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var out []kube.PVCView
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || len(out[0].MountedBy) != 1 || out[0].MountedBy[0].Pod != "web-1" {
		t.Errorf("got %+v", out)
	}
	if len(out[0].MountedBy[0].Mounts) != 1 || out[0].MountedBy[0].Mounts[0].Path != "/data" {
		t.Errorf("mount points = %+v", out[0].MountedBy[0].Mounts)
	}
}

// TestHandleResourceList_CatalogEntries exercises every resourceCatalog entry
// not already covered above by seeding one object of that kind and checking
// the projected row shape — it's the project (and, where relevant, enrich)
// closure that matters here, not exhaustively re-verifying kube.To*View.
func verifyServicesRow(t *testing.T, body []byte) {
	var out []kube.ServiceView
	mustUnmarshal(t, body, &out)
	if len(out) != 1 || out[0].Type != "ClusterIP" || out[0].ClusterIP != "10.0.0.5" || out[0].Ports != "80/TCP" {
		t.Errorf("got %+v", out)
	}
}

func verifyIngressesRow(t *testing.T, body []byte) {
	var out []kube.IngressView
	mustUnmarshal(t, body, &out)
	if len(out) != 1 || out[0].Class != "nginx" || out[0].Hosts != "example.com" {
		t.Errorf("got %+v", out)
	}
}

func verifyStatefulsetsRow(t *testing.T, body []byte) {
	var out []kube.StatefulSetView
	mustUnmarshal(t, body, &out)
	if len(out) != 1 || out[0].Ready != "2/3" || out[0].Service != "db-svc" {
		t.Errorf("got %+v", out)
	}
}

func verifyDaemonsetsRow(t *testing.T, body []byte) {
	var out []kube.DaemonSetView
	mustUnmarshal(t, body, &out)
	if len(out) != 1 || out[0].Ready != "3/4" || out[0].UpToDate != 3 || out[0].Available != 3 {
		t.Errorf("got %+v", out)
	}
}

func verifyReplicasetsRow(t *testing.T, body []byte) {
	var out []kube.ReplicaSetView
	mustUnmarshal(t, body, &out)
	if len(out) != 1 || out[0].Ready != "2/2" || out[0].OwnerKind != "Deployment" || out[0].OwnerName != "web" ||
		out[0].Revision != "2" || !out[0].Current {
		t.Errorf("got %+v", out)
	}
}

func verifyCronjobsRow(t *testing.T, body []byte) {
	var out []kube.CronJobView
	mustUnmarshal(t, body, &out)
	if len(out) != 1 || out[0].Schedule != "0 0 * * *" || out[0].Active != 1 || out[0].Suspend {
		t.Errorf("got %+v", out)
	}
}

func verifySecretsRow(t *testing.T, body []byte) {
	var out []kube.SecretView
	mustUnmarshal(t, body, &out)
	if len(out) != 1 || out[0].Type != "Opaque" || out[0].Keys != 1 {
		t.Errorf("got %+v", out)
	}
}

func verifyServiceaccountsRow(t *testing.T, body []byte) {
	var out []kube.ServiceAccountView
	mustUnmarshal(t, body, &out)
	if len(out) != 1 || out[0].Secrets != 1 {
		t.Errorf("got %+v", out)
	}
}

func verifyPersistentvolumesRow(t *testing.T, body []byte) {
	var out []kube.PVView
	mustUnmarshal(t, body, &out)
	if len(out) != 1 || out[0].Capacity != "10Gi" || out[0].AccessModes != "RWO" ||
		out[0].Reclaim != "Retain" || out[0].Status != "Available" || out[0].StorageClass != "standard" {
		t.Errorf("got %+v", out)
	}
}

func verifyStorageclassesRow(t *testing.T, body []byte) {
	var out []kube.StorageClassView
	mustUnmarshal(t, body, &out)
	if len(out) != 1 || !out[0].Default || out[0].Provisioner != "kubernetes.io/aws-ebs" ||
		out[0].Reclaim != "Delete" || out[0].Binding != "WaitForFirstConsumer" {
		t.Errorf("got %+v", out)
	}
}

func verifyHorizontalpodautoscalersRow(t *testing.T, body []byte) {
	var out []kube.HPAView
	mustUnmarshal(t, body, &out)
	if len(out) != 1 || out[0].Reference != "Deployment/web" || out[0].MinPods != 1 || out[0].MaxPods != 5 {
		t.Errorf("got %+v", out)
	}
}

func verifyEndpointslicesRow(t *testing.T, body []byte) {
	var out []kube.EndpointSliceView
	mustUnmarshal(t, body, &out)
	if len(out) != 1 || out[0].Service != "web" || out[0].Ready != 1 || out[0].Total != 1 || out[0].Ports != "http:80/TCP" {
		t.Errorf("got %+v", out)
	}
}

func verifyNetworkpoliciesRow(t *testing.T, body []byte) {
	var out []kube.NetworkPolicyView
	mustUnmarshal(t, body, &out)
	if len(out) != 1 || out[0].PolicyTypes != "Ingress" || out[0].PodSelector != "all" {
		t.Errorf("got %+v", out)
	}
}

func verifyIngressclassesRow(t *testing.T, body []byte) {
	var out []kube.IngressClassView
	mustUnmarshal(t, body, &out)
	if len(out) != 1 || out[0].Controller != "k8s.io/ingress-nginx" || !out[0].Default {
		t.Errorf("got %+v", out)
	}
}

func verifyRolesRow(t *testing.T, body []byte) {
	var out []kube.RoleView
	mustUnmarshal(t, body, &out)
	if len(out) != 1 || out[0].Namespace != "prod" || out[0].Rules != 1 {
		t.Errorf("got %+v", out)
	}
}

func verifyClusterrolesRow(t *testing.T, body []byte) {
	var out []kube.RoleView
	mustUnmarshal(t, body, &out)
	if len(out) != 1 || out[0].Namespace != "" || out[0].Rules != 1 {
		t.Errorf("got %+v", out)
	}
}

func verifyRolebindingsRow(t *testing.T, body []byte) {
	var out []kube.RoleBindingView
	mustUnmarshal(t, body, &out)
	if len(out) != 1 || out[0].Role != "Role/pod-reader" || len(out[0].Subjects) != 1 || out[0].Subjects[0] != "prod/default" {
		t.Errorf("got %+v", out)
	}
}

func verifyClusterrolebindingsRow(t *testing.T, body []byte) {
	var out []kube.RoleBindingView
	mustUnmarshal(t, body, &out)
	if len(out) != 1 || out[0].Role != "ClusterRole/admin" || len(out[0].Subjects) != 1 || out[0].Subjects[0] != "user:alice" {
		t.Errorf("got %+v", out)
	}
}

func verifyResourcequotasRow(t *testing.T, body []byte) {
	var out []kube.ResourceQuotaView
	mustUnmarshal(t, body, &out)
	if len(out) != 1 || out[0].Name != "compute-quota" || out[0].Namespace != "prod" {
		t.Errorf("got %+v", out)
	}
}

func verifyLimitrangesRow(t *testing.T, body []byte) {
	var out []kube.LimitRangeView
	mustUnmarshal(t, body, &out)
	if len(out) != 1 || out[0].Name != "limits" || out[0].Namespace != "prod" {
		t.Errorf("got %+v", out)
	}
}

func verifyPoddisruptionbudgetsRow(t *testing.T, body []byte) {
	var out []kube.PDBView
	mustUnmarshal(t, body, &out)
	if len(out) != 1 || out[0].Criteria != "min 1" || out[0].Current != 2 || out[0].Desired != 2 || out[0].Allowed != 0 {
		t.Errorf("got %+v", out)
	}
}

func verifyPriorityclassesRow(t *testing.T, body []byte) {
	var out []kube.PriorityClassView
	mustUnmarshal(t, body, &out)
	if len(out) != 1 || out[0].Value != 1000 || out[0].GlobalDefault || out[0].Preemption != "PreemptLowerPriority" {
		t.Errorf("got %+v", out)
	}
}

func verifyRuntimeclassesRow(t *testing.T, body []byte) {
	var out []kube.RuntimeClassView
	mustUnmarshal(t, body, &out)
	if len(out) != 1 || out[0].Handler != "runsc" {
		t.Errorf("got %+v", out)
	}
}
func TestHandleResourceList_CatalogEntries(t *testing.T) {
	trueVal := true
	falseVal := false
	minAvail := intstr.FromInt(1)
	reclaimDelete := corev1.PersistentVolumeReclaimDelete
	bindingMode := storagev1.VolumeBindingWaitForFirstConsumer
	ingressClassName := "nginx"
	portName := "http"
	port := int32(80)
	proto := corev1.ProtocolTCP

	cases := []struct {
		label  string
		path   string
		objs   []runtime.Object
		verify func(t *testing.T, body []byte)
	}{
		{
			label: "services",
			path:  "/api/contexts/test/resources/services?namespace=prod",
			objs: []runtime.Object{&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "prod"},
				Spec: corev1.ServiceSpec{
					Type:      corev1.ServiceTypeClusterIP,
					ClusterIP: "10.0.0.5",
					Ports:     []corev1.ServicePort{{Port: 80, Protocol: corev1.ProtocolTCP}},
				},
			}},
			verify: verifyServicesRow,
		},
		{
			label: "ingresses",
			path:  "/api/contexts/test/resources/ingresses?namespace=prod",
			objs: []runtime.Object{&networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{Name: "web-ingress", Namespace: "prod"},
				Spec: networkingv1.IngressSpec{
					IngressClassName: &ingressClassName,
					Rules:            []networkingv1.IngressRule{{Host: "example.com"}},
				},
			}},
			verify: verifyIngressesRow,
		},
		{
			label: "statefulsets",
			path:  "/api/contexts/test/resources/statefulsets?namespace=prod",
			objs: []runtime.Object{&appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "prod"},
				Spec:       appsv1.StatefulSetSpec{ServiceName: "db-svc", Replicas: int32Ptr(3)},
				Status:     appsv1.StatefulSetStatus{ReadyReplicas: 2},
			}},
			verify: verifyStatefulsetsRow,
		},
		{
			label: "daemonsets",
			path:  "/api/contexts/test/resources/daemonsets?namespace=prod",
			objs: []runtime.Object{&appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{Name: "agent", Namespace: "prod"},
				Status: appsv1.DaemonSetStatus{
					NumberReady: 3, DesiredNumberScheduled: 4, UpdatedNumberScheduled: 3, NumberAvailable: 3,
				},
			}},
			verify: verifyDaemonsetsRow,
		},
		{
			label: "replicasets",
			path:  "/api/contexts/test/resources/replicasets?namespace=prod",
			objs: []runtime.Object{&appsv1.ReplicaSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "web-abc123", Namespace: "prod",
					Annotations:     map[string]string{"deployment.kubernetes.io/revision": "2"},
					OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: "web", Controller: &trueVal}},
				},
				Spec:   appsv1.ReplicaSetSpec{Replicas: int32Ptr(2)},
				Status: appsv1.ReplicaSetStatus{ReadyReplicas: 2},
			}},
			verify: verifyReplicasetsRow,
		},
		{
			label: "cronjobs",
			path:  "/api/contexts/test/resources/cronjobs?namespace=prod",
			objs: []runtime.Object{&batchv1.CronJob{
				ObjectMeta: metav1.ObjectMeta{Name: "nightly", Namespace: "prod"},
				Spec:       batchv1.CronJobSpec{Schedule: "0 0 * * *", Suspend: &falseVal},
				Status:     batchv1.CronJobStatus{Active: []corev1.ObjectReference{{Name: "nightly-1"}}},
			}},
			verify: verifyCronjobsRow,
		},
		{
			label: "secrets",
			path:  "/api/contexts/test/resources/secrets?namespace=prod",
			objs: []runtime.Object{&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "creds", Namespace: "prod"},
				Type:       corev1.SecretTypeOpaque,
				Data:       map[string][]byte{"password": []byte("x")},
			}},
			verify: verifySecretsRow,
		},
		{
			label: "serviceaccounts",
			path:  "/api/contexts/test/resources/serviceaccounts?namespace=prod",
			objs: []runtime.Object{&corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{Name: "default", Namespace: "prod"},
				Secrets:    []corev1.ObjectReference{{Name: "default-token"}},
			}},
			verify: verifyServiceaccountsRow,
		},
		{
			label: "persistentvolumes",
			path:  "/api/contexts/test/resources/persistentvolumes",
			objs: []runtime.Object{&corev1.PersistentVolume{
				ObjectMeta: metav1.ObjectMeta{Name: "pv-1"},
				Spec: corev1.PersistentVolumeSpec{
					Capacity:                      corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("10Gi")},
					AccessModes:                   []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
					PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimRetain,
					StorageClassName:              "standard",
				},
				Status: corev1.PersistentVolumeStatus{Phase: corev1.VolumeAvailable},
			}},
			verify: verifyPersistentvolumesRow,
		},
		{
			label: "storageclasses",
			path:  "/api/contexts/test/resources/storageclasses",
			objs: []runtime.Object{&storagev1.StorageClass{
				ObjectMeta:        metav1.ObjectMeta{Name: "standard", Annotations: map[string]string{"storageclass.kubernetes.io/is-default-class": "true"}},
				Provisioner:       "kubernetes.io/aws-ebs",
				ReclaimPolicy:     &reclaimDelete,
				VolumeBindingMode: &bindingMode,
			}},
			verify: verifyStorageclassesRow,
		},
		{
			label: "horizontalpodautoscalers",
			path:  "/api/contexts/test/resources/horizontalpodautoscalers?namespace=prod",
			objs: []runtime.Object{&autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{Name: "web-hpa", Namespace: "prod"},
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
					ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: "web"},
					MaxReplicas:    5,
				},
			}},
			verify: verifyHorizontalpodautoscalersRow,
		},
		{
			label: "endpointslices",
			path:  "/api/contexts/test/resources/endpointslices?namespace=prod",
			objs: []runtime.Object{&discoveryv1.EndpointSlice{
				ObjectMeta:  metav1.ObjectMeta{Name: "web-abcde", Namespace: "prod", Labels: map[string]string{"kubernetes.io/service-name": "web"}},
				AddressType: discoveryv1.AddressTypeIPv4,
				Endpoints: []discoveryv1.Endpoint{{
					Addresses:  []string{"10.0.0.1"},
					Conditions: discoveryv1.EndpointConditions{Ready: &trueVal},
				}},
				Ports: []discoveryv1.EndpointPort{{Name: &portName, Port: &port, Protocol: &proto}},
			}},
			verify: verifyEndpointslicesRow,
		},
		{
			label: "networkpolicies",
			path:  "/api/contexts/test/resources/networkpolicies?namespace=prod",
			objs: []runtime.Object{&networkingv1.NetworkPolicy{
				ObjectMeta: metav1.ObjectMeta{Name: "deny-all", Namespace: "prod"},
				Spec:       networkingv1.NetworkPolicySpec{PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}},
			}},
			verify: verifyNetworkpoliciesRow,
		},
		{
			label: "ingressclasses",
			path:  "/api/contexts/test/resources/ingressclasses",
			objs: []runtime.Object{&networkingv1.IngressClass{
				ObjectMeta: metav1.ObjectMeta{Name: "nginx", Annotations: map[string]string{"ingressclass.kubernetes.io/is-default-class": "true"}},
				Spec:       networkingv1.IngressClassSpec{Controller: "k8s.io/ingress-nginx"},
			}},
			verify: verifyIngressclassesRow,
		},
		{
			label: "roles",
			path:  "/api/contexts/test/resources/roles?namespace=prod",
			objs: []runtime.Object{&rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{Name: "pod-reader", Namespace: "prod"},
				Rules:      []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"get", "list"}}},
			}},
			verify: verifyRolesRow,
		},
		{
			label: "clusterroles",
			path:  "/api/contexts/test/resources/clusterroles",
			objs: []runtime.Object{&rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{Name: "admin"},
				Rules:      []rbacv1.PolicyRule{{APIGroups: []string{"*"}, Resources: []string{"*"}, Verbs: []string{"*"}}},
			}},
			verify: verifyClusterrolesRow,
		},
		{
			label: "rolebindings",
			path:  "/api/contexts/test/resources/rolebindings?namespace=prod",
			objs: []runtime.Object{&rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{Name: "pod-reader-binding", Namespace: "prod"},
				RoleRef:    rbacv1.RoleRef{Kind: "Role", Name: "pod-reader"},
				Subjects:   []rbacv1.Subject{{Kind: "ServiceAccount", Name: "default"}},
			}},
			verify: verifyRolebindingsRow,
		},
		{
			label: "clusterrolebindings",
			path:  "/api/contexts/test/resources/clusterrolebindings",
			objs: []runtime.Object{&rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{Name: "admin-binding"},
				RoleRef:    rbacv1.RoleRef{Kind: "ClusterRole", Name: "admin"},
				Subjects:   []rbacv1.Subject{{Kind: "User", Name: "alice"}},
			}},
			verify: verifyClusterrolebindingsRow,
		},
		{
			label: "resourcequotas",
			path:  "/api/contexts/test/resources/resourcequotas?namespace=prod",
			objs: []runtime.Object{&corev1.ResourceQuota{
				ObjectMeta: metav1.ObjectMeta{Name: "compute-quota", Namespace: "prod"},
			}},
			verify: verifyResourcequotasRow,
		},
		{
			label: "limitranges",
			path:  "/api/contexts/test/resources/limitranges?namespace=prod",
			objs: []runtime.Object{&corev1.LimitRange{
				ObjectMeta: metav1.ObjectMeta{Name: "limits", Namespace: "prod"},
			}},
			verify: verifyLimitrangesRow,
		},
		{
			label: "poddisruptionbudgets",
			path:  "/api/contexts/test/resources/poddisruptionbudgets?namespace=prod",
			objs: []runtime.Object{&policyv1.PodDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{Name: "web-pdb", Namespace: "prod"},
				Spec:       policyv1.PodDisruptionBudgetSpec{MinAvailable: &minAvail},
				Status:     policyv1.PodDisruptionBudgetStatus{CurrentHealthy: 2, DesiredHealthy: 2, DisruptionsAllowed: 0},
			}},
			verify: verifyPoddisruptionbudgetsRow,
		},
		{
			label: "priorityclasses",
			path:  "/api/contexts/test/resources/priorityclasses",
			objs: []runtime.Object{&schedulingv1.PriorityClass{
				ObjectMeta: metav1.ObjectMeta{Name: "high"},
				Value:      1000,
			}},
			verify: verifyPriorityclassesRow,
		},
		{
			label: "runtimeclasses",
			path:  "/api/contexts/test/resources/runtimeclasses",
			objs: []runtime.Object{&nodev1.RuntimeClass{
				ObjectMeta: metav1.ObjectMeta{Name: "gvisor"},
				Handler:    "runsc",
			}},
			verify: verifyRuntimeclassesRow,
		},
	}

	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			s := newTestServer(t, tc.objs...)
			rec := doRequest(t, s, "GET", tc.path, "")
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
			}
			tc.verify(t, rec.Body.Bytes())
		})
	}
}

// mustUnmarshal decodes the response body, failing the (sub)test on error.
func mustUnmarshal(t *testing.T, body []byte, out any) {
	t.Helper()
	if err := json.Unmarshal(body, out); err != nil {
		t.Fatalf("unmarshal: %v, body=%s", err, body)
	}
}

// TestHandleResourceList_ResolveResourceError covers the ResolveResource
// error branch of handleResourceList: a resource in resourceCatalog whose GVR
// the cluster's RESTMapper can't resolve (a stale/CRD-less mapper, a version
// the cluster doesn't serve, ...) should surface as 502, not panic or 200.
// This uses a bespoke fakeManager rather than newTestServer so the shared
// testGVRs map (which registers "services") isn't in play.
func TestHandleResourceList_ResolveResourceError(t *testing.T) {
	cfg := config.NewStoreAt(filepath.Join(t.TempDir(), "config.json"))
	fm := &fakeManager{
		client:  kubernetesfake.NewSimpleClientset(),
		dynamic: dynamicfake.NewSimpleDynamicClient(scheme.Scheme),
		gvrs:    map[string]kube.Resource{}, // "services" is cataloged but deliberately unresolvable here
	}
	s := NewServer(fm, cfg, "")
	rec := doRequest(t, s, "GET", "/api/contexts/test/resources/services", "")
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want %d, body=%s", rec.Code, http.StatusBadGateway, rec.Body.String())
	}
}
