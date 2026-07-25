package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// promSource is a discovered Prometheus-compatible metrics backend in the
// cluster, reachable through the Kubernetes API server proxy.
type promSource struct {
	Kind       string `json:"kind"` // prometheus|thanos|mimir|victoriametrics|influxdb
	Namespace  string `json:"namespace"`
	Service    string `json:"service"`
	Port       int32  `json:"port"`
	PathPrefix string `json:"-"` // e.g. "/prometheus" for Mimir
	Supported  bool   `json:"-"` // false for InfluxDB (no Prometheus HTTP API)
}

type monResult struct {
	checked bool
	src     *promSource
}

var monPriority = map[string]int{"prometheus": 5, "thanos": 4, "victoriametrics": 3, "mimir": 2, "influxdb": 0}

// handleMonitoring reports whether a usable metrics backend was found.
func (s *Server) handleMonitoring(w http.ResponseWriter, r *http.Request) {
	client, err := s.mgr.ClientFor(r.PathValue("ctx"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := reqCtx(r)
	defer cancel()

	metricsServer := s.hasMetricsServer(ctx, client, r.PathValue("ctx"))
	src := s.discoverProm(ctx, client, r.PathValue("ctx"))
	if src == nil {
		writeJSON(w, http.StatusOK, map[string]any{"available": false, "metricsServer": metricsServer})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"available":     src.Supported,
		"kind":          src.Kind,
		"namespace":     src.Namespace,
		"service":       src.Service,
		"port":          src.Port,
		"metricsServer": metricsServer,
	})
}

func (s *Server) discoverProm(ctx context.Context, client *kubernetes.Clientset, contextName string) *promSource {
	s.monMu.Lock()
	if r, ok := s.mon[contextName]; ok {
		s.monMu.Unlock()
		return r.src
	}
	s.monMu.Unlock()

	list, err := client.CoreV1().Services("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil // don't cache transient errors
	}
	var best *promSource
	for i := range list.Items {
		if c := matchSource(&list.Items[i]); c != nil && (best == nil || monPriority[c.Kind] > monPriority[best.Kind]) {
			best = c
		}
	}
	s.monMu.Lock()
	s.mon[contextName] = monResult{checked: true, src: best}
	s.monMu.Unlock()
	return best
}

func matchSource(svc *corev1.Service) *promSource {
	n := strings.ToLower(svc.Name)
	for _, s := range []string{"alertmanager", "node-exporter", "pushgateway", "kube-state-metrics", "operator", "grafana", "blackbox", "adapter"} {
		if strings.Contains(n, s) {
			return nil
		}
	}
	mk := func(kind, prefix string, sup bool, names []string, nums []int32) *promSource {
		p := pickPort(svc, names, nums)
		if p == 0 {
			return nil
		}
		return &promSource{Kind: kind, Namespace: svc.Namespace, Service: svc.Name, Port: p, PathPrefix: prefix, Supported: sup}
	}
	switch {
	case strings.Contains(n, "thanos") && (strings.Contains(n, "query") || strings.Contains(n, "querier")):
		return mk("thanos", "", true, []string{"http", "web", "grpc"}, []int32{9090, 10902, 10901})
	case strings.Contains(n, "mimir"):
		// Match the query-serving service: the monolithic "mimir", or the
		// query-frontend/gateway/nginx in a microservices deployment. Skip other
		// components (ingester, distributor, compactor, store-gateway, ...).
		if n != "mimir" && !strings.Contains(n, "query-frontend") && !strings.Contains(n, "gateway") && !strings.Contains(n, "nginx") {
			return nil
		}
		return mk("mimir", "/prometheus", true, []string{"http", "http-metrics"}, []int32{9009, 8080, 80})
	case strings.Contains(n, "vmselect"):
		return mk("victoriametrics", "/select/0/prometheus", true, []string{"http"}, []int32{8481})
	case strings.Contains(n, "victoria-metrics") || strings.Contains(n, "vmsingle"):
		return mk("victoriametrics", "", true, []string{"http"}, []int32{8428})
	case strings.Contains(n, "prometheus"):
		return mk("prometheus", "", true, []string{"web", "http", "http-web"}, []int32{9090})
	case strings.Contains(n, "influxdb"):
		return mk("influxdb", "", false, []string{"http", "api"}, []int32{8086})
	}
	return nil
}

func pickPort(svc *corev1.Service, names []string, nums []int32) int32 {
	for _, p := range svc.Spec.Ports {
		for _, nm := range names {
			if strings.EqualFold(p.Name, nm) {
				return p.Port
			}
		}
		for _, num := range nums {
			if p.Port == num {
				return p.Port
			}
		}
	}
	if len(svc.Spec.Ports) > 0 {
		return svc.Spec.Ports[0].Port
	}
	return 0
}

// --- metrics ------------------------------------------------------------

type metricPoint struct {
	T int64   `json:"t"`
	V float64 `json:"v"`
}
type metricSeries struct {
	Points []metricPoint `json:"points"`
	Unit   string        `json:"unit"`
}

// handleMetrics: GET /api/contexts/{ctx}/metrics/{scope}?namespace=&name=&range=1h
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	client, err := s.mgr.ClientFor(r.PathValue("ctx"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := reqCtx(r)
	defer cancel()

	src := s.discoverProm(ctx, client, r.PathValue("ctx"))
	if src == nil || !src.Supported {
		writeJSON(w, http.StatusOK, map[string]any{"available": false})
		return
	}

	scope := r.PathValue("scope")
	q := r.URL.Query()
	cpuQ, memQ, err := metricQueries(ctx, client, scope, q.Get("namespace"), q.Get("name"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	rng := parseRange(q.Get("range"))
	end := time.Now()
	start := end.Add(-rng)
	step := time.Duration(rng.Seconds()/60) * time.Second
	if step < 15*time.Second {
		step = 15 * time.Second
	}

	cpu, _ := s.promQueryRange(ctx, client, src, cpuQ, start, end, step)
	mem, _ := s.promQueryRange(ctx, client, src, memQ, start, end, step)

	writeJSON(w, http.StatusOK, map[string]any{
		"available": true,
		"source":    src.Kind,
		"cpu":       metricSeries{Points: cpu, Unit: "cores"},
		"memory":    metricSeries{Points: mem, Unit: "bytes"},
	})
}

func metricQueries(ctx context.Context, client *kubernetes.Clientset, scope, ns, name string) (cpu, mem string, err error) {
	switch scope {
	case "cluster":
		return `sum(rate(container_cpu_usage_seconds_total{container!=""}[5m]))`,
			`sum(container_memory_working_set_bytes{container!=""})`, nil
	case "pod":
		if ns == "" || name == "" {
			return "", "", fmt.Errorf("namespace and name are required")
		}
		sel := fmt.Sprintf(`namespace=%q,pod=%q,container!=""`, ns, name)
		return fmt.Sprintf(`sum(rate(container_cpu_usage_seconds_total{%s}[5m]))`, sel),
			fmt.Sprintf(`sum(container_memory_working_set_bytes{%s})`, sel), nil
	case "node":
		if name == "" {
			return "", "", fmt.Errorf("name is required")
		}
		re := nodeInstanceRegex(ctx, client, name)
		return fmt.Sprintf(`sum(rate(node_cpu_seconds_total{mode!="idle",instance=~%q}[5m]))`, re),
			fmt.Sprintf(`sum(node_memory_MemTotal_bytes{instance=~%q} - node_memory_MemAvailable_bytes{instance=~%q})`, re, re), nil
	}
	return "", "", fmt.Errorf("unknown scope %q", scope)
}

// nodeInstanceRegex builds a regex matching node-exporter's `instance` label,
// which is usually the node's internal IP (":port" optional) or its name.
func nodeInstanceRegex(ctx context.Context, client *kubernetes.Clientset, node string) string {
	alts := []string{regexpEscape(node)}
	if n, err := client.CoreV1().Nodes().Get(ctx, node, metav1.GetOptions{}); err == nil {
		for _, a := range n.Status.Addresses {
			if a.Type == corev1.NodeInternalIP {
				alts = append([]string{regexpEscape(a.Address)}, alts...)
			}
		}
	}
	return "(" + strings.Join(alts, "|") + ")(:.*)?"
}

func regexpEscape(s string) string {
	r := strings.NewReplacer(".", `\.`, "-", `\-`)
	return r.Replace(s)
}

func parseRange(s string) time.Duration {
	if d, err := time.ParseDuration(s); err == nil && d >= 5*time.Minute && d <= 48*time.Hour {
		return d
	}
	return time.Hour
}

func (s *Server) promQueryRange(ctx context.Context, client *kubernetes.Clientset, src *promSource, query string, start, end time.Time, step time.Duration) ([]metricPoint, error) {
	params := map[string]string{
		"query": query,
		"start": strconv.FormatInt(start.Unix(), 10),
		"end":   strconv.FormatInt(end.Unix(), 10),
		"step":  strconv.Itoa(int(step.Seconds())),
	}
	raw, err := client.CoreV1().Services(src.Namespace).
		ProxyGet("http", src.Service, strconv.Itoa(int(src.Port)), src.PathPrefix+"/api/v1/query_range", params).
		DoRaw(ctx)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Data struct {
			Result []struct {
				Values [][2]any `json:"values"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, err
	}
	points := []metricPoint{}
	if len(resp.Data.Result) > 0 {
		for _, v := range resp.Data.Result[0].Values {
			ts, _ := v[0].(float64)
			str, _ := v[1].(string)
			val, _ := strconv.ParseFloat(str, 64)
			points = append(points, metricPoint{T: int64(ts), V: val})
		}
	}
	return points, nil
}
