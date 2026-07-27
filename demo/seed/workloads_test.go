package main

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
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
