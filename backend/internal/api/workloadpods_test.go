package api

import (
	"encoding/json"
	"net/http"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/robertobado/netsk8-navigator/backend/internal/kube"
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

func TestHandleWorkloadPods_Deployment(t *testing.T) {
	s := newTestServer(t,
		&appsv1.ReplicaSet{
			ObjectMeta: metav1.ObjectMeta{Name: "web-abc123", Namespace: "prod", OwnerReferences: []metav1.OwnerReference{
				{Kind: "Deployment", Name: "web", Controller: boolPtr(true)},
			}},
		},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "web-1", Namespace: "prod", OwnerReferences: []metav1.OwnerReference{
			{Kind: "ReplicaSet", Name: "web-abc123", Controller: boolPtr(true)},
		}}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "other-1", Namespace: "prod", OwnerReferences: []metav1.OwnerReference{
			{Kind: "ReplicaSet", Name: "other-xyz", Controller: boolPtr(true)},
		}}},
	)
	rec := doRequest(t, s, "GET", "/api/contexts/test/pods-of/deployment/prod/web", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var out []kube.PodView
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Name != "web-1" {
		t.Errorf("got %+v, want only web-1", out)
	}
}

func TestHandleWorkloadPods_Service(t *testing.T) {
	s := newTestServer(t,
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "web-svc", Namespace: "prod"},
			Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": "web"}},
		},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "web-1", Namespace: "prod", Labels: map[string]string{"app": "web"}}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "other-1", Namespace: "prod", Labels: map[string]string{"app": "other"}}},
	)
	rec := doRequest(t, s, "GET", "/api/contexts/test/pods-of/service/prod/web-svc", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var out []kube.PodView
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Name != "web-1" {
		t.Errorf("got %+v, want only web-1", out)
	}
}

func TestHandleWorkloadPods_DaemonSet(t *testing.T) {
	s := newTestServer(t,
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "fluentd-1", Namespace: "prod", OwnerReferences: []metav1.OwnerReference{
			{Kind: "DaemonSet", Name: "fluentd", Controller: boolPtr(true)},
		}}},
	)
	rec := doRequest(t, s, "GET", "/api/contexts/test/pods-of/daemonset/prod/fluentd", "")
	var out []kube.PodView
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Name != "fluentd-1" {
		t.Errorf("got %+v", out)
	}
}
