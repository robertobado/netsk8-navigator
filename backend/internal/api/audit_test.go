package api

import (
	"log"
	"net/http/httptest"
	"strings"
	"testing"
)

// captureLog redirects the standard logger's output for the duration of fn.
func captureLog(t *testing.T, fn func()) string {
	t.Helper()
	var buf strings.Builder
	orig := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(orig)
	fn()
	return buf.String()
}

func TestAudit_SanitizesNewlinesInFields(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	out := captureLog(t, func() {
		audit(r, "delete-resource", "name", "evil\nAUDIT action=fake-admin-login")
	})
	if strings.Count(out, "\n") != 1 {
		t.Errorf("expected exactly one newline (the log line's own terminator), got: %q", out)
	}
	if !strings.Contains(out, `name=evil\nAUDIT action=fake-admin-login`) {
		t.Errorf("expected the injected newline to be escaped, got: %q", out)
	}
}

func TestAudit_SanitizesRemoteAddr(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "1.2.3.4\r\ninjected"
	out := captureLog(t, func() {
		audit(r, "read-secret")
	})
	if strings.Contains(out, "\r") || strings.Count(out, "\n") != 1 {
		t.Errorf("expected RemoteAddr's control characters to be escaped, got: %q", out)
	}
}

func TestSanitizeLogValue(t *testing.T) {
	cases := map[string]string{
		"clean":      "clean",
		"a\nb":       `a\nb`,
		"a\r\nb":     `a\r\nb`,
		"":           "",
		"no-issue-1": "no-issue-1",
		"多字节-safe":   "多字节-safe",
	}
	for in, want := range cases {
		if got := sanitizeLogValue(in); got != want {
			t.Errorf("sanitizeLogValue(%q) = %q, want %q", in, got, want)
		}
	}
}
