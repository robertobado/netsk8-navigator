package api

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
)

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
