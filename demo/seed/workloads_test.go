package main

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestBindPVC(t *testing.T) {
	t.Run("completes an unbound PVC's bind", func(t *testing.T) {
		client := kubernetesfake.NewSimpleClientset(&corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "postgres-data", Namespace: "production"},
			Status:     corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimPending},
		})
		if err := bindPVC(context.Background(), client, "production", "postgres-data", "postgres-data-pv"); err != nil {
			t.Fatalf("bindPVC() error = %v", err)
		}
		pvc, err := client.CoreV1().PersistentVolumeClaims("production").Get(context.Background(), "postgres-data", metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if pvc.Spec.VolumeName != "postgres-data-pv" {
			t.Errorf("VolumeName = %q, want postgres-data-pv", pvc.Spec.VolumeName)
		}
		if pvc.Status.Phase != corev1.ClaimBound {
			t.Errorf("Phase = %q, want Bound", pvc.Status.Phase)
		}
	})

	t.Run("retries past a conflict from a concurrent real PV controller", func(t *testing.T) {
		// Regression guard: kwok's cluster runs a real kube-controller-manager,
		// which also races to bind a PV pre-claimed via ClaimRef -- the first
		// call this triggered in CI hit exactly this conflict and, before the
		// retry loop existed, made demo-seed log.Fatal and exit outright.
		client := kubernetesfake.NewSimpleClientset(&corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "postgres-data", Namespace: "production"},
			Status:     corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimPending},
		})
		conflictOnce := true
		client.PrependReactor("update", "persistentvolumeclaims", func(action ktesting.Action) (bool, runtime.Object, error) {
			if !conflictOnce {
				return false, nil, nil
			}
			conflictOnce = false
			gvr := schema.GroupResource{Group: "", Resource: "persistentvolumeclaims"}
			return true, nil, errors.NewConflict(gvr, "postgres-data", context.DeadlineExceeded)
		})
		if err := bindPVC(context.Background(), client, "production", "postgres-data", "postgres-data-pv"); err != nil {
			t.Fatalf("bindPVC() error = %v, want it to retry past the conflict", err)
		}
		pvc, err := client.CoreV1().PersistentVolumeClaims("production").Get(context.Background(), "postgres-data", metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if pvc.Spec.VolumeName != "postgres-data-pv" || pvc.Status.Phase != corev1.ClaimBound {
			t.Errorf("pvc after retry = %+v", pvc)
		}
	})

	t.Run("no-op when already bound", func(t *testing.T) {
		client := kubernetesfake.NewSimpleClientset(&corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "postgres-data", Namespace: "production"},
			Spec:       corev1.PersistentVolumeClaimSpec{VolumeName: "postgres-data-pv"},
			Status:     corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound},
		})
		if err := bindPVC(context.Background(), client, "production", "postgres-data", "postgres-data-pv"); err != nil {
			t.Fatalf("bindPVC() error = %v", err)
		}
	})
}
