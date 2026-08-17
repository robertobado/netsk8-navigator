package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ktesting "k8s.io/client-go/testing"

	"github.com/robertobado/netsk8-navigator/backend/internal/kube"
)

func TestHandleDeleteResource(t *testing.T) {
	s := newTestServer(t, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "prod"},
		Spec:       appsv1.DeploymentSpec{Replicas: replicas(2)},
	})
	rec := doRequest(t, s, "DELETE", "/api/contexts/test/manifest/deployment/prod/web", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}

	rec2 := doRequest(t, s, "GET", "/api/contexts/test/resources/deployments?namespace=prod", "")
	var out []kube.DeploymentView
	if err := json.Unmarshal(rec2.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Errorf("after delete, got %d deployments, want 0", len(out))
	}
}

func TestHandleDeleteResource_UnknownKind(t *testing.T) {
	s := newTestServer(t)
	rec := doRequest(t, s, "DELETE", "/api/contexts/test/manifest/notaresource/ns/name", "")
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502 (unsupported kind)", rec.Code)
	}
}

func TestHandleScaleResource(t *testing.T) {
	s := newTestServer(t, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "prod"},
		Spec:       appsv1.DeploymentSpec{Replicas: replicas(2)},
	})
	rec := doRequest(t, s, "PUT", "/api/contexts/test/scale/deployment/prod/web", `{"replicas":5}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}

	rec2 := doRequest(t, s, "GET", "/api/contexts/test/resources/deployments?namespace=prod", "")
	var out []kube.DeploymentView
	if err := json.Unmarshal(rec2.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Ready != "0/5" {
		t.Errorf("after scale, got %+v, want replicas=5 reflected", out)
	}
}

func TestHandleScaleResource_RejectsNonScalableKind(t *testing.T) {
	s := newTestServer(t)
	rec := doRequest(t, s, "PUT", "/api/contexts/test/scale/service/prod/web", `{"replicas":3}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (service isn't scalable)", rec.Code)
	}
}

func TestHandleScaleResource_RejectsNegativeReplicas(t *testing.T) {
	s := newTestServer(t, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "prod"},
		Spec:       appsv1.DeploymentSpec{Replicas: replicas(2)},
	})
	rec := doRequest(t, s, "PUT", "/api/contexts/test/scale/deployment/prod/web", `{"replicas":-1}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (negative replicas)", rec.Code)
	}
}

func TestHandleScaleResource_RejectsMissingReplicas(t *testing.T) {
	s := newTestServer(t, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "prod"},
		Spec:       appsv1.DeploymentSpec{Replicas: replicas(2)},
	})
	rec := doRequest(t, s, "PUT", "/api/contexts/test/scale/deployment/prod/web", `{}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (replicas missing)", rec.Code)
	}
}

func TestHandleRestartRollout(t *testing.T) {
	s := newTestServer(t, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "prod"},
		Spec:       appsv1.DeploymentSpec{Replicas: replicas(2)},
	})
	rec := doRequest(t, s, "POST", "/api/contexts/test/rollout-restart/deployment/prod/web", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}

	rec2 := doRequest(t, s, "GET", "/api/contexts/test/manifest/deployment/prod/web", "")
	var out map[string]string
	if err := json.Unmarshal(rec2.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out["yaml"], "kubectl.kubernetes.io/restartedAt") {
		t.Errorf("expected restartedAt annotation in manifest, got:\n%s", out["yaml"])
	}
}

func TestHandleRestartRollout_RejectsNonRestartableKind(t *testing.T) {
	s := newTestServer(t)
	rec := doRequest(t, s, "POST", "/api/contexts/test/rollout-restart/service/prod/web", "")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (service can't be restarted)", rec.Code)
	}
}

func TestHandleDeleteResource_DeleteFails(t *testing.T) {
	s := newTestServer(t, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "prod"},
		Spec:       appsv1.DeploymentSpec{Replicas: replicas(2)},
	})
	dyn := fakeDynamic(t, s)
	dyn.PrependReactor("delete", "deployments", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("etcd unavailable")
	})
	rec := doRequest(t, s, "DELETE", "/api/contexts/test/manifest/deployment/prod/web", "")
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502 (Delete failed)", rec.Code)
	}
}

func TestHandleScaleResource_InvalidJSON(t *testing.T) {
	s := newTestServer(t, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "prod"},
		Spec:       appsv1.DeploymentSpec{Replicas: replicas(2)},
	})
	rec := doRequest(t, s, "PUT", "/api/contexts/test/scale/deployment/prod/web", `not-json`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (malformed JSON body)", rec.Code)
	}
}

func TestHandleScaleResource_ResourceNotFound(t *testing.T) {
	s := newTestServer(t) // no deployment seeded
	rec := doRequest(t, s, "PUT", "/api/contexts/test/scale/deployment/prod/web", `{"replicas":5}`)
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502 (resource doesn't exist)", rec.Code)
	}
}

func TestHandleScaleResource_UpdateFails(t *testing.T) {
	s := newTestServer(t, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "prod"},
		Spec:       appsv1.DeploymentSpec{Replicas: replicas(2)},
	})
	dyn := fakeDynamic(t, s)
	dyn.PrependReactor("update", "deployments", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("conflict")
	})
	rec := doRequest(t, s, "PUT", "/api/contexts/test/scale/deployment/prod/web", `{"replicas":5}`)
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502 (Update failed)", rec.Code)
	}
}

func TestHandleRestartRollout_ResourceNotFound(t *testing.T) {
	s := newTestServer(t) // no deployment seeded
	rec := doRequest(t, s, "POST", "/api/contexts/test/rollout-restart/deployment/prod/web", "")
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502 (resource doesn't exist)", rec.Code)
	}
}

func TestHandleRestartRollout_UpdateFails(t *testing.T) {
	s := newTestServer(t, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "prod"},
		Spec:       appsv1.DeploymentSpec{Replicas: replicas(2)},
	})
	dyn := fakeDynamic(t, s)
	dyn.PrependReactor("update", "deployments", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("conflict")
	})
	rec := doRequest(t, s, "POST", "/api/contexts/test/rollout-restart/deployment/prod/web", "")
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502 (Update failed)", rec.Code)
	}
}
