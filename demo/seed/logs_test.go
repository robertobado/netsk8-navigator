package main

import (
	"strings"
	"testing"
	"time"
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
