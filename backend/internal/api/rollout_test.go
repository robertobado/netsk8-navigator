package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const testDepUID = types.UID("dep-uid")

func rolloutFixtures() (*appsv1.Deployment, *appsv1.ReplicaSet, *appsv1.ReplicaSet) {
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: "web", Namespace: "prod", UID: testDepUID,
			Annotations: map[string]string{revisionAnnotation: "2"},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: replicas(2),
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "web"}},
		},
	}
	rsOld := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "web-1", Namespace: "prod",
			Labels:          map[string]string{"app": "web"},
			Annotations:     map[string]string{revisionAnnotation: "1"},
			OwnerReferences: []metav1.OwnerReference{{UID: testDepUID}},
		},
		Spec: appsv1.ReplicaSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "web"}},
			Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "web", Image: "app:v1"}}}},
		},
	}
	rsNew := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "web-2", Namespace: "prod",
			Labels:          map[string]string{"app": "web"},
			Annotations:     map[string]string{revisionAnnotation: "2"},
			OwnerReferences: []metav1.OwnerReference{{UID: testDepUID}},
		},
		Spec: appsv1.ReplicaSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "web"}},
			Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "web", Image: "app:v2"}}}},
		},
	}
	return dep, rsOld, rsNew
}

func TestHandleRolloutHistory(t *testing.T) {
	dep, rsOld, rsNew := rolloutFixtures()
	s := newTestServer(t, dep, rsOld, rsNew)

	rec := doRequest(t, s, "GET", "/api/contexts/test/rollout-history/deployment/prod/web", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var out []revisionInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("got %d revisions, want 2: %+v", len(out), out)
	}
	if out[0].Revision != 2 || !out[0].Current || out[0].Images[0] != "app:v2" {
		t.Errorf("newest revision wrong: %+v", out[0])
	}
	if out[1].Revision != 1 || out[1].Current || out[1].Images[0] != "app:v1" {
		t.Errorf("oldest revision wrong: %+v", out[1])
	}
}

func TestHandleRolloutHistory_RejectsNonDeploymentKind(t *testing.T) {
	s := newTestServer(t)
	rec := doRequest(t, s, "GET", "/api/contexts/test/rollout-history/statefulset/prod/web", "")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestHandleRolloutUndo(t *testing.T) {
	dep, rsOld, rsNew := rolloutFixtures()
	s := newTestServer(t, dep, rsOld, rsNew)

	rec := doRequest(t, s, "POST", "/api/contexts/test/rollout-undo/deployment/prod/web", `{"toRevision":1}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}

	fm, ok := s.mgr.(*fakeManager)
	if !ok {
		t.Fatal("expected a *fakeManager")
	}
	updated, err := fm.client.AppsV1().Deployments("prod").Get(context.Background(), "web", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := updated.Spec.Template.Spec.Containers[0].Image; got != "app:v1" {
		t.Errorf("after undo, deployment image = %q, want app:v1 (revision 1's template)", got)
	}
}

func TestHandleRolloutUndo_UnknownRevision(t *testing.T) {
	dep, rsOld, rsNew := rolloutFixtures()
	s := newTestServer(t, dep, rsOld, rsNew)

	rec := doRequest(t, s, "POST", "/api/contexts/test/rollout-undo/deployment/prod/web", `{"toRevision":99}`)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestHandleRolloutUndo_RejectsNonDeploymentKind(t *testing.T) {
	s := newTestServer(t)
	rec := doRequest(t, s, "POST", "/api/contexts/test/rollout-undo/daemonset/prod/web", `{"toRevision":1}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}
