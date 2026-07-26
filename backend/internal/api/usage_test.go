package api

import (
	"context"
	"testing"

	kubernetesfake "k8s.io/client-go/kubernetes/fake"
)

func TestNodePeak(t *testing.T) {
	cases := []struct {
		name string
		n    nodeUsageItem
		want float64
	}{
		{"cpu more pressured", nodeUsageItem{CPU: gauge{Used: 80, Total: 100}, Memory: gauge{Used: 20, Total: 100}}, 0.8},
		{"memory more pressured", nodeUsageItem{CPU: gauge{Used: 10, Total: 100}, Memory: gauge{Used: 90, Total: 100}}, 0.9},
		{"no ceilings", nodeUsageItem{CPU: gauge{Used: 10}, Memory: gauge{Used: 10}}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := nodePeak(c.n); got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestPickCeiling(t *testing.T) {
	if got := pickCeiling(10, 5); got != 10 {
		t.Errorf("limit set: got %v, want 10", got)
	}
	if got := pickCeiling(0, 5); got != 5 {
		t.Errorf("no limit: got %v, want request (5)", got)
	}
	if got := pickCeiling(0, 0); got != 0 {
		t.Errorf("neither set: got %v, want 0", got)
	}
}

// hasMetricsServer and everything gated behind it aren't exercised here: the
// fake typed clientset's RESTClient() has no working transport configured, so
// a raw AbsPath().DoRaw() call panics instead of erroring cleanly (unlike a
// real cluster without metrics-server, which just 404s). Same category as
// podexec.go/podlogs.go — needs a live cluster (or a much heavier fake HTTP
// transport) to exercise safely.

func TestUsageFor_UnknownScope(t *testing.T) {
	client := kubernetesfake.NewSimpleClientset()
	if _, _, err := usageFor(context.Background(), client, "bogus", "", ""); err == nil {
		t.Error("want an error for an unknown scope")
	}
}
