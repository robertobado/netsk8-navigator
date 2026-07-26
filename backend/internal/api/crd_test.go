package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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
