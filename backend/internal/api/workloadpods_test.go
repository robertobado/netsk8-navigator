package api

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestOwnedByRS(t *testing.T) {
	rsOwned := map[string]bool{"web-abc123": true}

	t.Run("owned by a tracked ReplicaSet", func(t *testing.T) {
		p := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{OwnerReferences: []metav1.OwnerReference{
			{Kind: "ReplicaSet", Name: "web-abc123", Controller: boolPtr(true)},
		}}}
		if !ownedByRS(p, rsOwned) {
			t.Error("want true")
		}
	})
	t.Run("ReplicaSet not in the tracked set", func(t *testing.T) {
		p := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{OwnerReferences: []metav1.OwnerReference{
			{Kind: "ReplicaSet", Name: "other-xyz", Controller: boolPtr(true)},
		}}}
		if ownedByRS(p, rsOwned) {
			t.Error("want false")
		}
	})
	t.Run("non-controller owner reference is ignored", func(t *testing.T) {
		p := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{OwnerReferences: []metav1.OwnerReference{
			{Kind: "ReplicaSet", Name: "web-abc123"},
		}}}
		if ownedByRS(p, rsOwned) {
			t.Error("want false when Controller isn't set")
		}
	})
	t.Run("no owners", func(t *testing.T) {
		if ownedByRS(&corev1.Pod{}, rsOwned) {
			t.Error("want false")
		}
	})
}

func TestOwnedBy(t *testing.T) {
	p := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{OwnerReferences: []metav1.OwnerReference{
		{Kind: "DaemonSet", Name: "fluentd", Controller: boolPtr(true)},
	}}}
	if !ownedBy(p, "DaemonSet", "fluentd") {
		t.Error("want true for matching kind+name")
	}
	if ownedBy(p, "DaemonSet", "other") {
		t.Error("want false for a different name")
	}
	if ownedBy(p, "", "fluentd") {
		t.Error("want false when kind is empty")
	}
}
