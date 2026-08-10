package kube

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestInstallStderrTap_CapturesWhatsWrittenToStderr(t *testing.T) {
	original := os.Stderr
	t.Cleanup(func() { os.Stderr = original })

	InstallStderrTap()
	tap.mu.Lock()
	tap.buf.Reset() // isolate from anything captured by an earlier test in this run
	tap.mu.Unlock()

	marker := fmt.Sprintf("exec-plugin-test-marker-%d", time.Now().UnixNano())
	fmt.Fprintln(os.Stderr, marker)

	// The tap forwards asynchronously via a goroutine; poll briefly rather
	// than assume it's landed by the time Fprintln returns.
	deadline := time.Now().Add(2 * time.Second)
	for {
		if strings.Contains(RecentStderr(), marker) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("RecentStderr() never contained %q", marker)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
