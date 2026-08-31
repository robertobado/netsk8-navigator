package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	ktesting "k8s.io/client-go/testing"

	"github.com/robertobado/netsk8-navigator/backend/internal/config"
)

func TestValueSummary(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want string
	}{
		{"string", "hello", "hello"},
		{"bool true", true, "true"},
		{"bool false", false, "false"},
		{"int64", int64(42), "42"},
		{"float64", float64(3.5), "3.5"},
		{"slice", []any{1, 2, 3}, "3 items"},
		{"map", map[string]any{"a": 1, "b": 2}, "{2 fields}"},
		{"nil/unknown", nil, "—"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := valueSummary(c.in); got != c.want {
				t.Errorf("valueSummary(%v) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestAsInt(t *testing.T) {
	if got := asInt(int64(7)); got != 7 {
		t.Errorf("asInt(int64) = %d", got)
	}
	if got := asInt(float64(7.9)); got != 7 {
		t.Errorf("asInt(float64) = %d, want truncated 7", got)
	}
	if got := asInt("nope"); got != 0 {
		t.Errorf("asInt(string) = %d, want 0", got)
	}
}

func TestMatchSummary(t *testing.T) {
	t.Run("path with method", func(t *testing.T) {
		m := map[string]any{"method": "GET", "path": map[string]any{"type": "PathPrefix", "value": "/api"}}
		if got := matchSummary(m); got != "GET PathPrefix /api" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("path without method", func(t *testing.T) {
		m := map[string]any{"path": map[string]any{"type": "Exact", "value": "/health"}}
		if got := matchSummary(m); got != "Exact /health" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("no path falls back to valueSummary", func(t *testing.T) {
		m := map[string]any{"foo": "bar"}
		if got := matchSummary(m); got != "{1 fields}" {
			t.Errorf("got %q", got)
		}
	})
}

func TestBackendSummary(t *testing.T) {
	if got := backendSummary(map[string]any{"name": "web", "port": int64(80)}); got != "web:80" {
		t.Errorf("got %q, want web:80", got)
	}
	if got := backendSummary(map[string]any{"name": "web"}); got != "web" {
		t.Errorf("got %q, want just the name when there's no port", got)
	}
}

func TestSliceOf(t *testing.T) {
	m := map[string]any{"items": []any{map[string]any{"a": 1}, "not-a-map", map[string]any{"b": 2}}}
	got := sliceOf(m, "items")
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2 (non-map entries dropped)", len(got))
	}
	if _, ok := got[0]["a"]; !ok {
		t.Errorf("first entry = %+v, want the {a:1} map", got[0])
	}
}

func TestNestedRefs(t *testing.T) {
	obj := map[string]any{"spec": map[string]any{"rules": []any{
		map[string]any{"host": "a.example.com"},
		map[string]any{"host": "b.example.com"},
	}}}
	got := nestedRefs(obj, "spec", "rules")
	if len(got) != 2 {
		t.Fatalf("got %d refs, want 2", len(got))
	}
	if got[1]["host"] != "b.example.com" {
		t.Errorf("got %+v", got[1])
	}
	if got := nestedRefs(obj, "spec", "missing"); len(got) != 0 {
		t.Errorf("missing path should return empty, got %+v", got)
	}
}

func TestCrdConditions(t *testing.T) {
	obj := map[string]any{"status": map[string]any{"conditions": []any{
		map[string]any{"type": "Ready", "status": "True"},
		map[string]any{"type": "Accepted", "status": "False"},
		map[string]any{"type": "Unknown", "status": "Unknown"},
	}}}
	d := &resourceDetail{}
	crdConditions(d, obj)
	if len(d.Conditions) != 3 {
		t.Fatalf("got %d conditions, want 3", len(d.Conditions))
	}
	if d.Conditions[0].Tone != "ok" || d.Conditions[1].Tone != "err" || d.Conditions[2].Tone != "muted" {
		t.Errorf("tones = %+v", d.Conditions)
	}
}

func TestExtractHosts(t *testing.T) {
	t.Run("hostnames list", func(t *testing.T) {
		obj := map[string]any{"spec": map[string]any{"hostnames": []any{"a.example.com", "b.example.com"}}}
		got := extractHosts(obj)
		if len(got) != 2 || got[0] != "a.example.com" {
			t.Errorf("got %v", got)
		}
	})
	t.Run("contour virtualhost fqdn", func(t *testing.T) {
		obj := map[string]any{"spec": map[string]any{"virtualhost": map[string]any{"fqdn": "c.example.com"}}}
		if got := extractHosts(obj); len(got) != 1 || got[0] != "c.example.com" {
			t.Errorf("got %v", got)
		}
	})
	t.Run("single host field", func(t *testing.T) {
		obj := map[string]any{"spec": map[string]any{"host": "d.example.com"}}
		if got := extractHosts(obj); len(got) != 1 || got[0] != "d.example.com" {
			t.Errorf("got %v", got)
		}
	})
	t.Run("nothing present", func(t *testing.T) {
		if got := extractHosts(map[string]any{"spec": map[string]any{}}); got != nil {
			t.Errorf("got %v, want nil", got)
		}
	})
}

func TestExtractRefs(t *testing.T) {
	t.Run("parentRefs (Gateway API)", func(t *testing.T) {
		obj := map[string]any{"spec": map[string]any{"parentRefs": []any{
			map[string]any{"name": "my-gateway"},
		}}}
		if got := extractRefs(obj); len(got) != 1 || got[0] != "my-gateway" {
			t.Errorf("got %v", got)
		}
	})
	t.Run("gateways (Istio, plain strings)", func(t *testing.T) {
		obj := map[string]any{"spec": map[string]any{"gateways": []any{"istio-gw", "other-gw"}}}
		got := extractRefs(obj)
		if len(got) != 2 || got[0] != "istio-gw" {
			t.Errorf("got %v", got)
		}
	})
	t.Run("nothing present", func(t *testing.T) {
		if got := extractRefs(map[string]any{"spec": map[string]any{}}); got != nil {
			t.Errorf("got %v, want nil", got)
		}
	})
}

func TestCrdDetail_Dispatch(t *testing.T) {
	t.Run("HTTPRoute goes through routeCRDDetail", func(t *testing.T) {
		u := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "gateway.networking.k8s.io/v1", "kind": "HTTPRoute",
			"metadata": map[string]any{"name": "web", "namespace": "prod"},
			"spec":     map[string]any{"hostnames": []any{"web.example.com"}},
		}}
		d := crdDetail(u)
		if len(d.Hosts) != 1 || d.Hosts[0] != "web.example.com" {
			t.Errorf("got %+v, want routeCRDDetail to have run", d)
		}
	})
	t.Run("Gateway goes through gatewayCRDDetail", func(t *testing.T) {
		u := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "gateway.networking.k8s.io/v1", "kind": "Gateway",
			"metadata": map[string]any{"name": "gw", "namespace": "prod"},
			"spec":     map[string]any{"gatewayClassName": "nginx"},
		}}
		d := crdDetail(u)
		if len(d.Status) != 1 || d.Status[0].Value != "nginx" {
			t.Errorf("got %+v, want gatewayCRDDetail to have run", d)
		}
	})
	t.Run("anything else falls back to genericSpecSection", func(t *testing.T) {
		u := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "example.io/v1", "kind": "Widget",
			"metadata": map[string]any{"name": "w1"},
			"spec":     map[string]any{"color": "blue"},
		}}
		d := crdDetail(u)
		if len(d.Sections) != 1 || d.Sections[0].Title != "Spec" {
			t.Errorf("got %+v, want a generic Spec section", d)
		}
	})
}

func TestRouteCRDDetail_Rules(t *testing.T) {
	d := &resourceDetail{}
	obj := map[string]any{
		"spec": map[string]any{
			"parentRefs": []any{map[string]any{"name": "my-gateway", "namespace": "infra"}},
			"rules": []any{
				map[string]any{
					"matches":     []any{map[string]any{"path": map[string]any{"type": "PathPrefix", "value": "/api"}}},
					"backendRefs": []any{map[string]any{"name": "web-svc", "port": int64(80)}},
				},
			},
		},
	}
	routeCRDDetail(d, obj)
	if len(d.Sections) != 2 {
		t.Fatalf("got %d sections, want 2 (Gateways + Rule 1): %+v", len(d.Sections), d.Sections)
	}
	if d.Sections[0].Title != "Gateways" || d.Sections[0].Items[0].Label != "my-gateway" {
		t.Errorf("gateways section = %+v", d.Sections[0])
	}
	if d.Sections[1].Title != "Rule 1" || len(d.Sections[1].Items) != 2 {
		t.Errorf("rule section = %+v", d.Sections[1])
	}
}

func TestGatewayCRDDetail_Listeners(t *testing.T) {
	d := &resourceDetail{}
	obj := map[string]any{
		"spec": map[string]any{
			"gatewayClassName": "nginx",
			"listeners":        []any{map[string]any{"name": "http", "protocol": "HTTP", "port": int64(80), "hostname": "example.com"}},
		},
		"status": map[string]any{
			"addresses": []any{map[string]any{"value": "10.0.0.1"}},
		},
	}
	gatewayCRDDetail(d, obj)
	if len(d.Sections) != 2 {
		t.Fatalf("got %d sections, want 2 (Listeners + Addresses): %+v", len(d.Sections), d.Sections)
	}
	if d.Sections[0].Items[0].Value != "HTTP/80 · example.com" {
		t.Errorf("listener = %+v", d.Sections[0].Items[0])
	}
	if d.Sections[1].Items[0].Value != "10.0.0.1" {
		t.Errorf("addresses = %+v", d.Sections[1].Items[0])
	}
}

func TestGenericSpecSection(t *testing.T) {
	d := &resourceDetail{}
	genericSpecSection(d, map[string]any{"spec": map[string]any{"replicas": int64(3), "paused": false}})
	if len(d.Sections) != 1 || d.Sections[0].Title != "Spec" || len(d.Sections[0].Items) != 2 {
		t.Fatalf("got %+v", d.Sections)
	}
	// Sorted alphabetically: paused before replicas.
	if d.Sections[0].Items[0].Label != "paused" {
		t.Errorf("got %+v, want alphabetical order", d.Sections[0].Items)
	}

	t.Run("no spec at all adds nothing", func(t *testing.T) {
		d2 := &resourceDetail{}
		genericSpecSection(d2, map[string]any{})
		if len(d2.Sections) != 0 {
			t.Errorf("got %+v, want no sections", d2.Sections)
		}
	})
}

func TestFieldRow_ScalarStaysFlat(t *testing.T) {
	got := fieldRow("statusCode", int64(503))
	if got.Value != "503" || len(got.Grid) != 0 || got.Code != "" || len(got.Chips) != 0 {
		t.Errorf("got %+v", got)
	}
}

func TestFieldRow_SimpleArrayBecomesChips(t *testing.T) {
	got := fieldRow("dnsNames", []any{"example.com", "www.example.com"})
	if len(got.Chips) != 2 || got.Chips[0] != "example.com" || got.Value != "" {
		t.Errorf("got %+v", got)
	}
}

func TestFieldRow_ObjectWithOnlySimpleFieldsBecomesGrid(t *testing.T) {
	got := fieldRow("privateKey", map[string]any{"algorithm": "ECDSA", "size": int64(256)})
	if len(got.Grid) != 2 || got.Code != "" || got.Value != "" {
		t.Fatalf("got %+v", got)
	}
	// Sorted alphabetically: algorithm before size.
	if got.Grid[0].Label != "algorithm" || got.Grid[0].Value != "ECDSA" {
		t.Errorf("grid[0] = %+v", got.Grid[0])
	}
}

func TestFieldRow_ObjectNestedOneLevelDeeperFallsBackToYAML(t *testing.T) {
	// Reproduces the reported bug: directResponse: {statusCode, body: {inline}}.
	got := fieldRow("directResponse", map[string]any{
		"statusCode": int64(503),
		"body":       map[string]any{"inline": "unavailable"},
	})
	if got.Code == "" || len(got.Grid) != 0 || got.Value != "" {
		t.Fatalf("got %+v, want a YAML code fallback", got)
	}
	if !strings.Contains(got.Code, "statusCode") || !strings.Contains(got.Code, "inline") {
		t.Errorf("code = %q, want it to contain the nested fields", got.Code)
	}
}

func TestFieldRow_ArrayOfObjectsFallsBackToYAML(t *testing.T) {
	got := fieldRow("rules", []any{map[string]any{"path": "/api"}})
	if got.Code == "" || len(got.Grid) != 0 || len(got.Chips) != 0 {
		t.Fatalf("got %+v, want a YAML code fallback", got)
	}
}

func TestFieldRow_EmptyObjectAndArrayRenderAsPlaceholders(t *testing.T) {
	if got := fieldRow("empty", map[string]any{}); got.Value != "{}" || got.Grid != nil {
		t.Errorf("empty object: got %+v", got)
	}
	if got := fieldRow("empty", []any{}); got.Value != "[]" || got.Chips != nil {
		t.Errorf("empty array: got %+v", got)
	}
}

func TestAllSimple(t *testing.T) {
	if !allSimple(map[string]any{"a": "x", "b": []any{"y", "z"}}) {
		t.Error("scalars + simple array should be allSimple")
	}
	if allSimple(map[string]any{"a": map[string]any{"b": "c"}}) {
		t.Error("nested object should not be allSimple")
	}
	if allSimple(map[string]any{"a": []any{map[string]any{"b": "c"}}}) {
		t.Error("array of objects should not be allSimple")
	}
}

func TestHandleCRDList(t *testing.T) {
	s := newTestServer(t, &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "gateway.networking.k8s.io/v1", "kind": "HTTPRoute",
		"metadata": map[string]any{"name": "web", "namespace": "prod"},
		"spec":     map[string]any{"hostnames": []any{"web.example.com"}},
	}})
	rec := doRequest(t, s, "GET", "/api/contexts/test/crd/gateway.networking.k8s.io/v1/httproutes?namespace=prod", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var out []crdItem
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Name != "web" || out[0].Hosts != "web.example.com" {
		t.Errorf("got %+v", out)
	}
}

func TestHandleCRDManifest(t *testing.T) {
	s := newTestServer(t, &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "gateway.networking.k8s.io/v1", "kind": "HTTPRoute",
		"metadata": map[string]any{"name": "web", "namespace": "prod"},
	}})
	rec := doRequest(t, s, "GET", "/api/contexts/test/crd/gateway.networking.k8s.io/v1/httproutes/prod/web/manifest", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["yaml"] == "" {
		t.Error("expected non-empty yaml")
	}
}

func TestHandleCRDDetail(t *testing.T) {
	s := newTestServer(t, &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "gateway.networking.k8s.io/v1", "kind": "HTTPRoute",
		"metadata": map[string]any{"name": "web", "namespace": "prod"},
		"spec":     map[string]any{"hostnames": []any{"web.example.com"}},
	}})
	rec := doRequest(t, s, "GET", "/api/contexts/test/crd/gateway.networking.k8s.io/v1/httproutes/prod/web/detail", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var out resourceDetail
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Name != "web" || len(out.Hosts) != 1 {
		t.Errorf("got %+v", out)
	}
}

func TestStorageVersion(t *testing.T) {
	cases := []struct {
		name string
		in   []apiextensionsv1.CustomResourceDefinitionVersion
		want string
	}{
		{"storage version wins", []apiextensionsv1.CustomResourceDefinitionVersion{
			{Name: "v1alpha1", Served: true, Storage: false},
			{Name: "v1", Served: true, Storage: true},
		}, "v1"},
		{"falls back to served when nothing is marked storage", []apiextensionsv1.CustomResourceDefinitionVersion{
			{Name: "v1beta1", Served: true, Storage: false},
		}, "v1beta1"},
		{"no versions at all", nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := storageVersion(c.in); got != c.want {
				t.Errorf("storageVersion(%+v) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestHandleCRDKinds(t *testing.T) {
	namespaced := apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: "widgets.example.com"},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "example.com", Scope: apiextensionsv1.NamespaceScoped,
			Names:    apiextensionsv1.CustomResourceDefinitionNames{Kind: "Widget", Plural: "widgets"},
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{Name: "v1", Served: true, Storage: true}},
		},
	}
	clusterScoped := apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: "usages.kwok.x-k8s.io"},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "kwok.x-k8s.io", Scope: apiextensionsv1.ClusterScoped,
			Names:    apiextensionsv1.CustomResourceDefinitionNames{Kind: "ClusterResourceUsage", Plural: "clusterresourceusages"},
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{Name: "v1alpha1", Served: true, Storage: true}},
		},
	}
	// AcceptedNames (post-admission) should win over Spec.Names when set.
	acceptedNamesDiffer := apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: "gadgets.acme.io"},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "acme.io", Scope: apiextensionsv1.NamespaceScoped,
			Names:    apiextensionsv1.CustomResourceDefinitionNames{Kind: "GadgetOld", Plural: "gadgetolds"},
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{Name: "v1", Served: true, Storage: true}},
		},
		Status: apiextensionsv1.CustomResourceDefinitionStatus{
			AcceptedNames: apiextensionsv1.CustomResourceDefinitionNames{Kind: "Gadget", Plural: "gadgets"},
		},
	}

	s := newTestServerWithCRDs(t, []apiextensionsv1.CustomResourceDefinition{namespaced, clusterScoped, acceptedNamesDiffer})
	rec := doRequest(t, s, "GET", "/api/contexts/test/crdkinds", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var out []crdKind
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 3 {
		t.Fatalf("got %d kinds, want 3: %+v", len(out), out)
	}
	// Sorted by group then kind: acme.io/Gadget, example.com/Widget, kwok.x-k8s.io/ClusterResourceUsage.
	if out[0].Group != "acme.io" || out[0].Kind != "Gadget" || out[0].Resource != "gadgets" {
		t.Errorf("out[0] = %+v, want the acme.io Gadget (via AcceptedNames)", out[0])
	}
	if out[1].Group != "example.com" || !out[1].Namespaced {
		t.Errorf("out[1] = %+v, want namespaced example.com/Widget", out[1])
	}
	if out[2].Group != "kwok.x-k8s.io" || out[2].Namespaced {
		t.Errorf("out[2] = %+v, want cluster-scoped kwok.x-k8s.io/ClusterResourceUsage", out[2])
	}
}

func TestHandleCRDKinds_SkipsCRDWithNoServedVersion(t *testing.T) {
	dead := apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: "deads.example.com"},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "example.com", Scope: apiextensionsv1.NamespaceScoped,
			Names: apiextensionsv1.CustomResourceDefinitionNames{Kind: "Dead", Plural: "deads"},
		},
	}
	s := newTestServerWithCRDs(t, []apiextensionsv1.CustomResourceDefinition{dead})
	rec := doRequest(t, s, "GET", "/api/contexts/test/crdkinds", "")
	var out []crdKind
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Errorf("got %+v, want none — no served version to browse", out)
	}
}

func TestHandleCRDApply(t *testing.T) {
	s := newTestServer(t, &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "example.com/v1", "kind": "Widget",
		"metadata": map[string]any{"name": "w1", "namespace": "prod"},
		"spec":     map[string]any{"color": "blue"},
	}})
	body := `{"yaml":"apiVersion: example.com/v1\nkind: Widget\nmetadata:\n  name: w1\n  namespace: prod\nspec:\n  color: red\n"}`
	rec := doRequest(t, s, "PUT", "/api/contexts/test/crd/example.com/v1/widgets/prod/w1", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}

	rec2 := doRequest(t, s, "GET", "/api/contexts/test/crd/example.com/v1/widgets/prod/w1/manifest", "")
	var out map[string]string
	if err := json.Unmarshal(rec2.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out["yaml"], "color: red") {
		t.Errorf("apply should have persisted, got:\n%s", out["yaml"])
	}
}

func TestHandleCRDApply_ClusterScoped(t *testing.T) {
	s := newTestServer(t, &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "kwok.x-k8s.io/v1alpha1", "kind": "ClusterResourceUsage",
		"metadata": map[string]any{"name": "usage-from-annotation"},
	}})
	body := `{"yaml":"apiVersion: kwok.x-k8s.io/v1alpha1\nkind: ClusterResourceUsage\nmetadata:\n  name: usage-from-annotation\n  labels:\n    updated: \"true\"\n"}`
	rec := doRequest(t, s, "PUT", "/api/contexts/test/crd/kwok.x-k8s.io/v1alpha1/clusterresourceusages/-/usage-from-annotation", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
}

// The fake dynamic client's tracker doesn't understand DryRun (it persists
// unconditionally) — same gap TestHandleApplyManifest_DryRunDoesNotPersist
// works around, here for the generic CRD path.
func TestHandleCRDApply_DryRunDoesNotPersist(t *testing.T) {
	s := newTestServer(t, &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "example.com/v1", "kind": "Widget",
		"metadata": map[string]any{"name": "w1", "namespace": "prod"},
		"spec":     map[string]any{"color": "blue"},
	}})
	dyn := fakeDynamic(t, s)
	dyn.PrependReactor("update", "widgets", func(action ktesting.Action) (bool, runtime.Object, error) {
		ua, ok := action.(ktesting.UpdateActionImpl)
		if !ok || len(ua.GetUpdateOptions().DryRun) == 0 {
			return false, nil, nil
		}
		return true, ua.GetObject(), nil
	})

	body := `{"yaml":"apiVersion: example.com/v1\nkind: Widget\nmetadata:\n  name: w1\n  namespace: prod\nspec:\n  color: red\n"}`
	rec := doRequest(t, s, "PUT", "/api/contexts/test/crd/example.com/v1/widgets/prod/w1?dryRun=true", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out["yaml"], "color: red") {
		t.Errorf("dry-run response should preview the requested change, got:\n%s", out["yaml"])
	}

	rec2 := doRequest(t, s, "GET", "/api/contexts/test/crd/example.com/v1/widgets/prod/w1/manifest", "")
	var manifest map[string]string
	if err := json.Unmarshal(rec2.Body.Bytes(), &manifest); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(manifest["yaml"], "color: blue") {
		t.Errorf("dry-run must not persist — live object should still be blue, got:\n%s", manifest["yaml"])
	}
}

func TestHandleCRDDelete(t *testing.T) {
	s := newTestServer(t, &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "example.com/v1", "kind": "Widget",
		"metadata": map[string]any{"name": "w1", "namespace": "prod"},
	}})
	rec := doRequest(t, s, "DELETE", "/api/contexts/test/crd/example.com/v1/widgets/prod/w1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}

	rec2 := doRequest(t, s, "GET", "/api/contexts/test/crd/example.com/v1/widgets?namespace=prod", "")
	var out []crdItem
	if err := json.Unmarshal(rec2.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Errorf("after delete, got %d widgets, want 0", len(out))
	}
}

func TestHandleCRDDelete_ClusterScoped(t *testing.T) {
	s := newTestServer(t, &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "kwok.x-k8s.io/v1alpha1", "kind": "ClusterResourceUsage",
		"metadata": map[string]any{"name": "usage-from-annotation"},
	}})
	rec := doRequest(t, s, "DELETE", "/api/contexts/test/crd/kwok.x-k8s.io/v1alpha1/clusterresourceusages/-/usage-from-annotation", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleRouteKinds_NoneDiscovered(t *testing.T) {
	s := newTestServer(t)
	rec := doRequest(t, s, "GET", "/api/contexts/test/routekinds", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var out []routeKind
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Errorf("got %+v, want none — the fake discovery client serves nothing", out)
	}
}

// routeKindsDiscovery stubs ServerPreferredResources. The base fake
// clientset's FakeDiscovery hardcodes that one method to always return
// (nil, nil) regardless of any PrependReactor — it never invokes the
// reactor chain at all — so exercising handleRouteKinds' actual branches
// needs a real stand-in instead.
type routeKindsDiscovery struct {
	discovery.DiscoveryInterface
	lists []*metav1.APIResourceList
	err   error
}

func (d *routeKindsDiscovery) ServerPreferredResources() ([]*metav1.APIResourceList, error) {
	return d.lists, d.err
}

type clientWithDiscoveryStub struct {
	kubernetes.Interface
	disc discovery.DiscoveryInterface
}

func (c *clientWithDiscoveryStub) Discovery() discovery.DiscoveryInterfaces {
	return newDiscoveryInterfacesStub(c.disc)
}

// discoveryInterfacesStub adapts a plain discovery.DiscoveryInterface (all
// this test package's stubs implement) into the discovery.DiscoveryInterfaces
// kubernetes.Interface.Discovery() now returns as of client-go 0.37
// (DiscoveryInterface + the context-aware DiscoveryInterfaceWithContext
// added alongside it). Both embedded interfaces declare RESTClient() with
// an identical signature, which would otherwise make it an ambiguous
// (unpromoted) selector — the explicit method below resolves that.
type discoveryInterfacesStub struct {
	discovery.DiscoveryInterface
	discovery.DiscoveryInterfaceWithContext
}

func (d discoveryInterfacesStub) RESTClient() restclient.Interface {
	return d.DiscoveryInterface.RESTClient()
}

func newDiscoveryInterfacesStub(d discovery.DiscoveryInterface) discoveryInterfacesStub {
	return discoveryInterfacesStub{
		DiscoveryInterface:            d,
		DiscoveryInterfaceWithContext: discovery.ToDiscoveryInterfaceWithContext(d),
	}
}

// routeKindsManager wraps fakeManager so ClientFor's Discovery() reports
// caller-supplied preferred resources / error.
type routeKindsManager struct {
	*fakeManager
	lists []*metav1.APIResourceList
	err   error
}

func (m *routeKindsManager) ClientFor(ctx string) (kubernetes.Interface, error) {
	client, err := m.fakeManager.ClientFor(ctx)
	if err != nil {
		return nil, err
	}
	return &clientWithDiscoveryStub{Interface: client, disc: &routeKindsDiscovery{lists: m.lists, err: m.err}}, nil
}

func newRouteKindsServer(t *testing.T, lists []*metav1.APIResourceList, err error) *Server {
	t.Helper()
	cfg := config.NewStoreAt(filepath.Join(t.TempDir(), "config.json"))
	return NewServer(&routeKindsManager{fakeManager: newFakeManager(), lists: lists, err: err}, cfg, "")
}

func routeKindsAPIList(groupVersion string, resources ...metav1.APIResource) *metav1.APIResourceList {
	return &metav1.APIResourceList{GroupVersion: groupVersion, APIResources: resources}
}

func TestHandleRouteKinds_DiscoversKnownCandidatesAcrossFamilies(t *testing.T) {
	lists := []*metav1.APIResourceList{
		routeKindsAPIList("gateway.networking.k8s.io/v1",
			metav1.APIResource{Name: "gateways", Kind: "Gateway", Namespaced: true},
			metav1.APIResource{Name: "httproutes", Kind: "HTTPRoute", Namespaced: true},
			metav1.APIResource{Name: "gateways/status", Kind: "Gateway", Namespaced: true}, // subresource — must be skipped
		),
		routeKindsAPIList("traefik.io/v1alpha1",
			metav1.APIResource{Name: "ingressroutes", Kind: "IngressRoute", Namespaced: true},
		),
		routeKindsAPIList("apps/v1", // not a route candidate group at all — must be skipped
			metav1.APIResource{Name: "deployments", Kind: "Deployment", Namespaced: true},
		),
	}
	s := newRouteKindsServer(t, lists, nil)
	rec := doRequest(t, s, "GET", "/api/contexts/test/routekinds", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var out []routeKind
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 3 {
		t.Fatalf("got %d route kinds, want 3 (gateways, httproutes, ingressroutes): %+v", len(out), out)
	}
	// Sorted by candidate Order: Gateways(1), HTTPRoutes(2), IngressRoutes(10).
	if out[0].Label != "Gateways" || out[1].Label != "HTTPRoutes" || out[2].Label != "IngressRoutes" {
		t.Errorf("order wrong: %+v", out)
	}
	if out[0].Group != "gateway.networking.k8s.io" || out[0].Version != "v1" || out[0].Resource != "gateways" || !out[0].Namespaced {
		t.Errorf("gateways entry = %+v", out[0])
	}
}

func TestHandleRouteKinds_SkipsUnparseableGroupVersion(t *testing.T) {
	lists := []*metav1.APIResourceList{
		routeKindsAPIList("bad/group/version", metav1.APIResource{Name: "whatever"}),
		routeKindsAPIList("gateway.networking.k8s.io/v1", metav1.APIResource{Name: "gateways", Kind: "Gateway", Namespaced: true}),
	}
	s := newRouteKindsServer(t, lists, nil)
	rec := doRequest(t, s, "GET", "/api/contexts/test/routekinds", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var out []routeKind
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Label != "Gateways" {
		t.Errorf("got %+v, want the malformed GroupVersion entry skipped and gateways still discovered", out)
	}
}

func TestHandleRouteKinds_PartialDiscoveryErrorStillReturnsResults(t *testing.T) {
	lists := []*metav1.APIResourceList{
		routeKindsAPIList("gateway.networking.k8s.io/v1", metav1.APIResource{Name: "gateways", Kind: "Gateway", Namespaced: true}),
	}
	s := newRouteKindsServer(t, lists, fmt.Errorf("some aggregated API is unavailable"))
	rec := doRequest(t, s, "GET", "/api/contexts/test/routekinds", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var out []routeKind
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Errorf("got %+v, want the partial result kept despite a non-fatal discovery error", out)
	}
}

func TestHandleRouteKinds_DiscoveryErrorWithNoResults(t *testing.T) {
	s := newRouteKindsServer(t, nil, fmt.Errorf("discovery totally failed"))
	rec := doRequest(t, s, "GET", "/api/contexts/test/routekinds", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var out []routeKind
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Errorf("got %+v, want an empty result for a fatal discovery error", out)
	}
}

func TestHandleCRDList_ListFails(t *testing.T) {
	s := newTestServer(t, &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "example.com/v1", "kind": "Widget",
		"metadata": map[string]any{"name": "w1", "namespace": "prod"},
	}})
	dyn := fakeDynamic(t, s)
	dyn.PrependReactor("list", "widgets", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("etcd unavailable")
	})
	rec := doRequest(t, s, "GET", "/api/contexts/test/crd/example.com/v1/widgets?namespace=prod", "")
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502 (List failed)", rec.Code)
	}
}

func TestHandleCRDManifest_NotFound(t *testing.T) {
	s := newTestServer(t) // nothing seeded
	rec := doRequest(t, s, "GET", "/api/contexts/test/crd/example.com/v1/widgets/prod/missing/manifest", "")
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502 (resource doesn't exist)", rec.Code)
	}
}

func TestHandleCRDDetail_NotFound(t *testing.T) {
	s := newTestServer(t) // nothing seeded
	rec := doRequest(t, s, "GET", "/api/contexts/test/crd/example.com/v1/widgets/prod/missing/detail", "")
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502 (resource doesn't exist)", rec.Code)
	}
}

func TestHandleCRDApply_InvalidJSON(t *testing.T) {
	s := newTestServer(t)
	rec := doRequest(t, s, "PUT", "/api/contexts/test/crd/example.com/v1/widgets/prod/w1", `not-json`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (malformed JSON body)", rec.Code)
	}
}

func TestHandleCRDApply_InvalidYAML(t *testing.T) {
	s := newTestServer(t)
	body := `{"yaml":"not: valid: yaml: : :"}`
	rec := doRequest(t, s, "PUT", "/api/contexts/test/crd/example.com/v1/widgets/prod/w1", body)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (malformed YAML)", rec.Code)
	}
}

func TestHandleCRDApply_UpdateFails(t *testing.T) {
	s := newTestServer(t, &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "example.com/v1", "kind": "Widget",
		"metadata": map[string]any{"name": "w1", "namespace": "prod"},
		"spec":     map[string]any{"color": "blue"},
	}})
	dyn := fakeDynamic(t, s)
	dyn.PrependReactor("update", "widgets", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("conflict")
	})
	body := `{"yaml":"apiVersion: example.com/v1\nkind: Widget\nmetadata:\n  name: w1\n  namespace: prod\nspec:\n  color: red\n"}`
	rec := doRequest(t, s, "PUT", "/api/contexts/test/crd/example.com/v1/widgets/prod/w1", body)
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502 (Update failed)", rec.Code)
	}
}

func TestHandleCRDDelete_DeleteFails(t *testing.T) {
	s := newTestServer(t, &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "example.com/v1", "kind": "Widget",
		"metadata": map[string]any{"name": "w1", "namespace": "prod"},
	}})
	dyn := fakeDynamic(t, s)
	dyn.PrependReactor("delete", "widgets", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("etcd unavailable")
	})
	rec := doRequest(t, s, "DELETE", "/api/contexts/test/crd/example.com/v1/widgets/prod/w1", "")
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502 (Delete failed)", rec.Code)
	}
}
