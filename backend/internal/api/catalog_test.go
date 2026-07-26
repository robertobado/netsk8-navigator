package api

import (
	"encoding/json"
	"net/http"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/robertobado/netsk8-navigator/backend/internal/kube"
)

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
