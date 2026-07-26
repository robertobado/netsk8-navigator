package main

import (
	"context"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
)

func TestFormatCRILine(t *testing.T) {
	ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	got := formatCRILine(ts, "hello world")
	want := "2026-01-02T03:04:05.000000000Z stdout F hello world\n"
	if got != want {
		t.Errorf("formatCRILine() = %q, want %q", got, want)
	}
}

func TestFormatCRILine_ParsesBackWithCRILayout(t *testing.T) {
	// Regression guard: this exact layout is what
	// k8s.io/cri-client/pkg/logs.parseCRILog expects — a line kwok's Logs
	// simulation can't parse is silently dropped (empty log output), not an
	// error, so this format must stay parseable by that exact scheme.
	line := formatCRILine(time.Now(), "content")
	parts := strings.SplitN(strings.TrimSuffix(line, "\n"), " ", 3)
	if len(parts) != 3 {
		t.Fatalf("expected 3 space-separated fields (timestamp, stream, rest), got %d: %q", len(parts), line)
	}
	if _, err := time.Parse(criTimeFormat, parts[0]); err != nil {
		t.Errorf("timestamp field %q did not parse as criTimeFormat: %v", parts[0], err)
	}
	if parts[1] != "stdout" {
		t.Errorf("stream field = %q, want stdout", parts[1])
	}
	if !strings.HasPrefix(parts[2], "F content") {
		t.Errorf("tag+content field = %q, want to start with %q", parts[2], "F content")
	}
}

func TestRandomLogLine_IncludesAppName(t *testing.T) {
	for i := 0; i < 20; i++ {
		line := randomLogLine("checkout")
		if !strings.Contains(line, "[checkout]") {
			t.Errorf("randomLogLine(%q) = %q, want it to contain the app name tag", "checkout", line)
		}
	}
}

func TestContainsVerb(t *testing.T) {
	cases := map[string]bool{
		"heartbeat ok":          false,
		"handling request %s":   true,
		"cache miss for key %s": true,
		"":                      false,
	}
	for tmpl, want := range cases {
		if got := containsVerb(tmpl); got != want {
			t.Errorf("containsVerb(%q) = %v, want %v", tmpl, got, want)
		}
	}
}

func TestRandomID_Length(t *testing.T) {
	id := randomID()
	if len(id) != 6 {
		t.Errorf("randomID() = %q, want length 6", id)
	}
}

func TestBreakPod_StaysRunningWithCrashLoopingContainer(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "flaky-service-1", Namespace: "staging"},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "flaky-service"}}},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
				{Type: corev1.ContainersReady, Status: corev1.ConditionTrue},
			},
			ContainerStatuses: []corev1.ContainerStatus{{
				Name:  "flaky-service",
				Ready: true,
				State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
			}},
		},
	}
	client := kubernetesfake.NewSimpleClientset(pod)

	breakPod(context.Background(), client, pod)

	got, err := client.CoreV1().Pods("staging").Get(context.Background(), "flaky-service-1", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	// The whole point of breakPod (vs kwok's own pod-container-running-failed
	// stage) is that the pod itself never leaves Running — only the container
	// looks broken. A terminal pod phase here would make the owning
	// ReplicaSet spawn a replacement, causing unbounded pod churn.
	if got.Status.Phase != corev1.PodRunning {
		t.Errorf("pod phase = %q, want it to stay Running", got.Status.Phase)
	}
	cs := got.Status.ContainerStatuses[0]
	if cs.Ready {
		t.Error("container should be marked not ready")
	}
	if cs.State.Waiting == nil || cs.State.Waiting.Reason != "CrashLoopBackOff" {
		t.Errorf("container state = %+v, want a Waiting state with reason CrashLoopBackOff", cs.State)
	}
	if cs.RestartCount < 8 || cs.RestartCount > 27 {
		t.Errorf("restartCount = %d, want a plausible non-zero value in [8,27]", cs.RestartCount)
	}
	for _, c := range got.Status.Conditions {
		if (c.Type == corev1.PodReady || c.Type == corev1.ContainersReady) && c.Status != corev1.ConditionFalse {
			t.Errorf("condition %s = %s, want False", c.Type, c.Status)
		}
	}
}
