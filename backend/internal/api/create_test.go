package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ktesting "k8s.io/client-go/testing"

	"github.com/robertobado/netsk8-navigator/backend/internal/kube"
)

func TestHandleCreateResource(t *testing.T) {
	s := newTestServer(t)
	body := `{"yaml":"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cfg\n  namespace: prod\ndata:\n  key: value\n"}`
	rec := doRequest(t, s, "POST", "/api/contexts/test/create", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["kind"] != "ConfigMap" || out["namespace"] != "prod" || out["name"] != "cfg" {
		t.Errorf("unexpected response: %+v", out)
	}

	list := doRequest(t, s, "GET", "/api/contexts/test/resources/configmaps?namespace=prod", "")
	var cms []kube.ConfigMapView
	if err := json.Unmarshal(list.Body.Bytes(), &cms); err != nil {
		t.Fatal(err)
	}
	if len(cms) != 1 || cms[0].Name != "cfg" {
		t.Errorf("created ConfigMap not found in list: %+v", cms)
	}
}

// Namespaced resources with no metadata.namespace in the YAML default to
// "default" — the same behavior `kubectl create` has when --namespace is omitted.
func TestHandleCreateResource_DefaultsNamespace(t *testing.T) {
	s := newTestServer(t)
	body := `{"yaml":"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cfg\ndata: {}\n"}`
	rec := doRequest(t, s, "POST", "/api/contexts/test/create", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["namespace"] != "default" {
		t.Errorf("namespace = %q, want %q", out["namespace"], "default")
	}
}

// Cluster-scoped kinds (Namespace) never get a namespace segment, even if the
// dynamic client's URL construction would otherwise accept one.
func TestHandleCreateResource_ClusterScoped(t *testing.T) {
	s := newTestServer(t)
	body := `{"yaml":"apiVersion: v1\nkind: Namespace\nmetadata:\n  name: shiny\n"}`
	rec := doRequest(t, s, "POST", "/api/contexts/test/create", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["namespace"] != "" {
		t.Errorf("namespace = %q, want empty for a cluster-scoped kind", out["namespace"])
	}

	list := doRequest(t, s, "GET", "/api/contexts/test/resources/namespaces", "")
	var nss []map[string]any
	if err := json.Unmarshal(list.Body.Bytes(), &nss); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ns := range nss {
		if ns["name"] == "shiny" {
			found = true
		}
	}
	if !found {
		t.Errorf("created Namespace not found in list: %+v", nss)
	}
}

func TestHandleCreateResource_MissingName(t *testing.T) {
	s := newTestServer(t)
	body := `{"yaml":"apiVersion: v1\nkind: ConfigMap\nmetadata: {}\n"}`
	rec := doRequest(t, s, "POST", "/api/contexts/test/create", body)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (missing metadata.name)", rec.Code)
	}
}

func TestHandleCreateResource_MissingKind(t *testing.T) {
	s := newTestServer(t)
	body := `{"yaml":"metadata:\n  name: cfg\n"}`
	rec := doRequest(t, s, "POST", "/api/contexts/test/create", body)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (missing apiVersion/kind)", rec.Code)
	}
}

func TestHandleCreateResource_UnknownKind(t *testing.T) {
	s := newTestServer(t)
	body := `{"yaml":"apiVersion: totally.made.up/v1\nkind: NotAThing\nmetadata:\n  name: x\n"}`
	rec := doRequest(t, s, "POST", "/api/contexts/test/create", body)
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502 (kind not resolvable by the cluster)", rec.Code)
	}
}

func TestHandleCreateResource_InvalidYAML(t *testing.T) {
	s := newTestServer(t)
	rec := doRequest(t, s, "POST", "/api/contexts/test/create", `{"yaml":"not: [valid"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// The fake dynamic client's tracker doesn't understand DryRun (it persists
// unconditionally) — same gap TestHandleApplyManifest_DryRunDoesNotPersist
// works around for Update, patched here for Create.
func TestHandleCreateResource_DryRunDoesNotPersist(t *testing.T) {
	s := newTestServer(t)
	dyn := fakeDynamic(t, s)
	dyn.PrependReactor("create", "configmaps", func(action ktesting.Action) (bool, runtime.Object, error) {
		ca, ok := action.(ktesting.CreateActionImpl)
		if !ok || len(ca.GetCreateOptions().DryRun) == 0 {
			return false, nil, nil
		}
		return true, ca.GetObject(), nil
	})

	body := `{"yaml":"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cfg\n  namespace: prod\ndata: {}\n"}`
	rec := doRequest(t, s, "POST", "/api/contexts/test/create?dryRun=true", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out["yaml"], "name: cfg") {
		t.Errorf("dry-run response should preview the object, got:\n%s", out["yaml"])
	}

	list := doRequest(t, s, "GET", "/api/contexts/test/resources/configmaps?namespace=prod", "")
	var cms []corev1.ConfigMap
	_ = json.Unmarshal(list.Body.Bytes(), &cms)
	if len(cms) != 0 {
		t.Errorf("dry-run must not persist — got %d configmaps", len(cms))
	}
}
