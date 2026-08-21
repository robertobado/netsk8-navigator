package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	ktesting "k8s.io/client-go/testing"

	"github.com/robertobado/netsk8-navigator/backend/internal/config"
)

// erroringResponseWrapper simulates a ProxyGet against a Service with no
// real backend behind it — DoRaw fails the way a real "connection refused"
// would, instead of the fake clientset's default of a nil ResponseWrapper
// (which panics on .DoRaw()).
type erroringResponseWrapper struct{}

func (erroringResponseWrapper) DoRaw(context.Context) ([]byte, error) {
	return nil, errors.New("connection refused")
}
func (erroringResponseWrapper) Stream(context.Context) (io.ReadCloser, error) {
	return nil, errors.New("connection refused")
}

func TestPickPort(t *testing.T) {
	svc := &corev1.Service{Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{
		{Name: "metrics", Port: 9100},
		{Name: "http-web", Port: 9090},
	}}}
	if got := pickPort(svc, []string{"http-web"}, nil); got != 9090 {
		t.Errorf("name match = %d, want 9090", got)
	}
	if got := pickPort(svc, nil, []int32{9100}); got != 9100 {
		t.Errorf("number match = %d, want 9100", got)
	}
	if got := pickPort(svc, []string{"nope"}, []int32{1234}); got != 9100 {
		t.Errorf("no match falls back to first port = %d, want 9100", got)
	}
	if got := pickPort(&corev1.Service{}, nil, nil); got != 0 {
		t.Errorf("no ports at all = %d, want 0", got)
	}
}

func TestRegexpEscape(t *testing.T) {
	if got := regexpEscape("10.0.0.1"); got != `10\.0\.0\.1` {
		t.Errorf("got %q", got)
	}
	if got := regexpEscape("ip-10-0-0-1"); got != `ip\-10\-0\-0\-1` {
		t.Errorf("got %q", got)
	}
}

func TestParseRange(t *testing.T) {
	if got := parseRange("6h"); got != 6*time.Hour {
		t.Errorf("valid range got %v", got)
	}
	if got := parseRange("garbage"); got != time.Hour {
		t.Errorf("invalid input should default to 1h, got %v", got)
	}
	if got := parseRange("1m"); got != time.Hour {
		t.Errorf("below the 5m floor should default to 1h, got %v", got)
	}
	if got := parseRange("72h"); got != time.Hour {
		t.Errorf("above the 48h ceiling should default to 1h, got %v", got)
	}
}

func TestMatchSource(t *testing.T) {
	svcWithPort := func(name string, ports ...corev1.ServicePort) *corev1.Service {
		return &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "observability"}, Spec: corev1.ServiceSpec{Ports: ports}}
	}

	t.Run("prometheus service matches", func(t *testing.T) {
		src := matchSource(svcWithPort("prometheus-server", corev1.ServicePort{Name: "http-web", Port: 9090}))
		if src == nil || src.Kind != "prometheus" || src.Port != 9090 {
			t.Errorf("got %+v", src)
		}
	})

	t.Run("excluded component names never match, even if they mention prometheus", func(t *testing.T) {
		if src := matchSource(svcWithPort("prometheus-alertmanager", corev1.ServicePort{Name: "http-web", Port: 9090})); src != nil {
			t.Errorf("got %+v, want nil (alertmanager excluded)", src)
		}
	})

	t.Run("mimir monolithic service matches", func(t *testing.T) {
		src := matchSource(svcWithPort("mimir", corev1.ServicePort{Name: "http-metrics", Port: 8080}))
		if src == nil || src.Kind != "mimir" || src.PathPrefix != "/prometheus" {
			t.Errorf("got %+v", src)
		}
	})

	t.Run("mimir non-query component doesn't match", func(t *testing.T) {
		if src := matchSource(svcWithPort("mimir-ingester", corev1.ServicePort{Name: "http-metrics", Port: 8080})); src != nil {
			t.Errorf("got %+v, want nil", src)
		}
	})

	t.Run("no matching name", func(t *testing.T) {
		if src := matchSource(svcWithPort("web-frontend", corev1.ServicePort{Name: "http", Port: 80})); src != nil {
			t.Errorf("got %+v, want nil", src)
		}
	})

	t.Run("name matches but no usable port", func(t *testing.T) {
		if src := matchSource(svcWithPort("prometheus-server")); src != nil {
			t.Errorf("got %+v, want nil when there are no ports at all", src)
		}
	})
}

func TestMetricQueries(t *testing.T) {
	ctx := context.Background()
	client := kubernetesfake.NewSimpleClientset()

	t.Run("cluster scope needs no args", func(t *testing.T) {
		cpu, mem, err := metricQueries(ctx, client, "cluster", "", "")
		if err != nil || cpu == "" || mem == "" {
			t.Errorf("got cpu=%q mem=%q err=%v", cpu, mem, err)
		}
	})
	t.Run("pod scope requires namespace and name", func(t *testing.T) {
		if _, _, err := metricQueries(ctx, client, "pod", "", ""); err == nil {
			t.Error("want an error when namespace/name are missing")
		}
		cpu, _, err := metricQueries(ctx, client, "pod", "prod", "web-1")
		if err != nil || cpu == "" {
			t.Errorf("got cpu=%q err=%v", cpu, err)
		}
	})
	t.Run("node scope requires name", func(t *testing.T) {
		if _, _, err := metricQueries(ctx, client, "node", "", ""); err == nil {
			t.Error("want an error when name is missing")
		}
	})
	t.Run("unknown scope errors", func(t *testing.T) {
		if _, _, err := metricQueries(ctx, client, "bogus", "", ""); err == nil {
			t.Error("want an error for an unknown scope")
		}
	})
}

// TestHandleMetrics_UnreachableSourceReportsUnavailable guards against a
// regression where a Prometheus-look-alike Service that discoverProm matches
// by name/port, but that doesn't actually answer queries (e.g. a demo/test
// Service with no real Prometheus behind it), made handleMetrics report
// "available: true" with points:null instead of gracefully degrading —
// which crashed the frontend (series.points.at on null).
func TestHandleMetrics_UnreachableSourceReportsUnavailable(t *testing.T) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "prometheus", Namespace: "monitoring"},
		Spec:       corev1.ServiceSpec{Ports: []corev1.ServicePort{{Name: "http-web", Port: 9090}}},
	}
	s := newTestServer(t, svc)
	fakeClient(t, s).PrependProxyReactor("services", func(ktesting.Action) (bool, rest.ResponseWrapper, error) {
		return true, erroringResponseWrapper{}, nil
	})

	rec := doRequest(t, s, "GET", "/api/contexts/test/metrics/cluster", "")
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body["available"] != false {
		t.Errorf("available = %v, want false when the matched source can't actually be queried", body["available"])
	}
	if _, ok := body["cpu"]; ok {
		t.Errorf("body should not carry a cpu series when unavailable, got %v", body)
	}
}

// successResponseWrapper simulates a ProxyGet against a real Prometheus,
// returning body on DoRaw instead of erroringResponseWrapper's failure.
type successResponseWrapper struct{ body string }

func (s successResponseWrapper) DoRaw(context.Context) ([]byte, error) {
	return []byte(s.body), nil
}
func (s successResponseWrapper) Stream(context.Context) (io.ReadCloser, error) {
	return nil, errors.New("not implemented")
}

func TestPromQueryRange(t *testing.T) {
	src := &promSource{Kind: "prometheus", Namespace: "monitoring", Service: "prometheus", Port: 9090, Supported: true}
	start, end := time.Unix(1620000000, 0), time.Unix(1620000030, 0)

	t.Run("parses points from a successful response", func(t *testing.T) {
		client := kubernetesfake.NewSimpleClientset()
		client.PrependProxyReactor("services", func(ktesting.Action) (bool, rest.ResponseWrapper, error) {
			return true, successResponseWrapper{`{"data":{"result":[{"values":[[1620000000,"1.5"],[1620000015,"2.5"]]}]}}`}, nil
		})
		points, err := (&Server{}).promQueryRange(context.Background(), client, src, "up", start, end, 15*time.Second)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(points) != 2 || points[0].V != 1.5 || points[1].V != 2.5 {
			t.Errorf("got %+v", points)
		}
	})

	t.Run("no result rows yields an empty (non-nil) slice", func(t *testing.T) {
		client := kubernetesfake.NewSimpleClientset()
		client.PrependProxyReactor("services", func(ktesting.Action) (bool, rest.ResponseWrapper, error) {
			return true, successResponseWrapper{`{"data":{"result":[]}}`}, nil
		})
		points, err := (&Server{}).promQueryRange(context.Background(), client, src, "up", start, end, 15*time.Second)
		if err != nil || points == nil || len(points) != 0 {
			t.Errorf("got points=%+v err=%v, want an empty non-nil slice", points, err)
		}
	})

	t.Run("malformed JSON is an error", func(t *testing.T) {
		client := kubernetesfake.NewSimpleClientset()
		client.PrependProxyReactor("services", func(ktesting.Action) (bool, rest.ResponseWrapper, error) {
			return true, successResponseWrapper{"not json"}, nil
		})
		if _, err := (&Server{}).promQueryRange(context.Background(), client, src, "up", start, end, 15*time.Second); err == nil {
			t.Error("want an error for malformed JSON")
		}
	})
}

func TestHandleMonitoring(t *testing.T) {
	t.Run("ClientFor error", func(t *testing.T) {
		cfg := config.NewStoreAt(filepath.Join(t.TempDir(), "config.json"))
		s := NewServer(clientForErrManager{newFakeManager()}, cfg, "")
		rec := doRequest(t, s, "GET", "/api/contexts/test/monitoring", "")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("neither Prometheus nor metrics-server available", func(t *testing.T) {
		s := newTestServerWithMetrics(t, metricsRoundTripper{responses: map[string]string{}})
		rec := doRequest(t, s, "GET", "/api/contexts/test/monitoring", "")
		if rec.Code != 200 {
			t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
		}
		var body map[string]any
		mustUnmarshalRec(t, rec, &body)
		if body["available"] != false || body["metricsServer"] != false {
			t.Errorf("got %+v", body)
		}
		if _, ok := body["kind"]; ok {
			t.Errorf("no kind expected when no source was discovered, got %+v", body)
		}
	})

	t.Run("metrics-server available, no Prometheus", func(t *testing.T) {
		rt := metricsRoundTripper{responses: map[string]string{metricsAPIPath: "{}"}}
		s := newTestServerWithMetrics(t, rt)
		rec := doRequest(t, s, "GET", "/api/contexts/test/monitoring", "")
		var body map[string]any
		mustUnmarshalRec(t, rec, &body)
		if body["available"] != false || body["metricsServer"] != true {
			t.Errorf("got %+v", body)
		}
	})

	t.Run("supported Prometheus source found, plus metrics-server", func(t *testing.T) {
		svc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "prometheus", Namespace: "monitoring"},
			Spec:       corev1.ServiceSpec{Ports: []corev1.ServicePort{{Name: "http-web", Port: 9090}}},
		}
		rt := metricsRoundTripper{responses: map[string]string{metricsAPIPath: "{}"}}
		s := newTestServerWithMetrics(t, rt, svc)
		rec := doRequest(t, s, "GET", "/api/contexts/test/monitoring", "")
		var body map[string]any
		mustUnmarshalRec(t, rec, &body)
		if body["available"] != true || body["kind"] != "prometheus" || body["namespace"] != "monitoring" ||
			body["service"] != "prometheus" || body["metricsServer"] != true {
			t.Errorf("got %+v", body)
		}
	})

	t.Run("unsupported source (InfluxDB) reports available:false but still identifies it", func(t *testing.T) {
		svc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "influxdb", Namespace: "monitoring"},
			Spec:       corev1.ServiceSpec{Ports: []corev1.ServicePort{{Name: "http", Port: 8086}}},
		}
		rt := metricsRoundTripper{responses: map[string]string{}}
		s := newTestServerWithMetrics(t, rt, svc)
		rec := doRequest(t, s, "GET", "/api/contexts/test/monitoring", "")
		var body map[string]any
		mustUnmarshalRec(t, rec, &body)
		if body["available"] != false || body["kind"] != "influxdb" {
			t.Errorf("got %+v", body)
		}
	})
}

func mustUnmarshalRec(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		t.Fatalf("unmarshal: %v, body=%s", err, rec.Body.String())
	}
}

func TestHandleMetrics_EndToEnd(t *testing.T) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "prometheus", Namespace: "monitoring"},
		Spec:       corev1.ServiceSpec{Ports: []corev1.ServicePort{{Name: "http-web", Port: 9090}}},
	}
	s := newTestServer(t, svc)
	fakeClient(t, s).PrependProxyReactor("services", func(ktesting.Action) (bool, rest.ResponseWrapper, error) {
		return true, successResponseWrapper{`{"data":{"result":[{"values":[[1620000000,"1.5"]]}]}}`}, nil
	})

	rec := doRequest(t, s, "GET", "/api/contexts/test/metrics/cluster", "")
	if rec.Code != 200 {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Available bool         `json:"available"`
		Source    string       `json:"source"`
		CPU       metricSeries `json:"cpu"`
		Memory    metricSeries `json:"memory"`
	}
	mustUnmarshalRec(t, rec, &body)
	if !body.Available || body.Source != "prometheus" {
		t.Fatalf("got %+v", body)
	}
	if len(body.CPU.Points) != 1 || body.CPU.Points[0].V != 1.5 || body.CPU.Unit != "cores" {
		t.Errorf("cpu series = %+v", body.CPU)
	}
	if body.Memory.Unit != "bytes" {
		t.Errorf("memory series = %+v", body.Memory)
	}
}

func TestHandleMetrics_ClientForError(t *testing.T) {
	cfg := config.NewStoreAt(filepath.Join(t.TempDir(), "config.json"))
	s := NewServer(clientForErrManager{newFakeManager()}, cfg, "")
	rec := doRequest(t, s, "GET", "/api/contexts/test/metrics/cluster", "")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestHandleMetrics_NoSourceAvailable(t *testing.T) {
	s := newTestServer(t)
	rec := doRequest(t, s, "GET", "/api/contexts/test/metrics/cluster", "")
	var body map[string]any
	mustUnmarshalRec(t, rec, &body)
	if body["available"] != false {
		t.Errorf("got %+v, want available:false when no source is discovered", body)
	}
}

func TestHandleMetrics_BadScope(t *testing.T) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "prometheus", Namespace: "monitoring"},
		Spec:       corev1.ServiceSpec{Ports: []corev1.ServicePort{{Name: "http-web", Port: 9090}}},
	}
	s := newTestServer(t, svc)
	rec := doRequest(t, s, "GET", "/api/contexts/test/metrics/pod", "") // pod scope with no namespace/name
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestHandleMetrics_MemoryQueryFails(t *testing.T) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "prometheus", Namespace: "monitoring"},
		Spec:       corev1.ServiceSpec{Ports: []corev1.ServicePort{{Name: "http-web", Port: 9090}}},
	}
	s := newTestServer(t, svc)
	calls := 0
	fakeClient(t, s).PrependProxyReactor("services", func(ktesting.Action) (bool, rest.ResponseWrapper, error) {
		calls++
		if calls == 1 { // cpu query succeeds
			return true, successResponseWrapper{`{"data":{"result":[]}}`}, nil
		}
		return true, erroringResponseWrapper{}, nil // memory query fails
	})
	rec := doRequest(t, s, "GET", "/api/contexts/test/metrics/cluster", "")
	var body map[string]any
	mustUnmarshalRec(t, rec, &body)
	if body["available"] != false {
		t.Errorf("got %+v, want available:false when the memory query fails", body)
	}
}

func TestNodeInstanceRegex(t *testing.T) {
	ctx := context.Background()

	t.Run("node not found falls back to its name", func(t *testing.T) {
		client := kubernetesfake.NewSimpleClientset()
		if got := nodeInstanceRegex(ctx, client, "node-1"); got != "(node\\-1)(:.*)?" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("includes the node's internal IP when found", func(t *testing.T) {
		client := kubernetesfake.NewSimpleClientset(&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
			Status:     corev1.NodeStatus{Addresses: []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: "10.0.0.5"}}},
		})
		got := nodeInstanceRegex(ctx, client, "node-1")
		if got != `(10\.0\.0\.5|node\-1)(:.*)?` {
			t.Errorf("got %q", got)
		}
	})
}
