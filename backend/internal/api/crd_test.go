package api

import "testing"

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
