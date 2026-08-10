package kube

import (
	"bytes"
	"os"
	"sync"
)

// tapMax bounds how much captured stderr we hold onto — enough for any
// realistic CLI error message (aws/gke-gcloud-auth-plugin/etc. failures are
// a few lines at most), not a general-purpose log buffer.
const tapMax = 8 << 10

var tap struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

// InstallStderrTap redirects the process-wide os.Stderr through an
// in-memory ring buffer while still forwarding every byte to the real
// stderr untouched. client-go's exec-credential plugin (used for aws eks
// get-token, gke-gcloud-auth-plugin, OIDC helpers, etc.) writes a failing
// subprocess's stderr straight to os.Stderr with no public hook to
// intercept it — this is the only way to recover that detail (e.g. "Token
// has expired") for error-message enrichment (see internal/api/mcp.go).
//
// Safe to call once, early in main() before any exec-credential plugin
// might run. Existing log.Printf output is unaffected: the log package
// captures its output target at init time, before this swap, so it keeps
// writing straight to the original fd.
func InstallStderrTap() {
	real := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		return // best-effort: exec-error enrichment just won't have detail
	}
	os.Stderr = w
	go func() {
		buf := make([]byte, 4096)
		for {
			n, readErr := r.Read(buf)
			if n > 0 {
				_, _ = real.Write(buf[:n])
				tap.mu.Lock()
				tap.buf.Write(buf[:n])
				if tap.buf.Len() > tapMax {
					b := tap.buf.Bytes()
					tail := b[len(b)-tapMax:]
					kept := make([]byte, len(tail))
					copy(kept, tail)
					tap.buf.Reset()
					tap.buf.Write(kept)
				}
				tap.mu.Unlock()
			}
			if readErr != nil {
				return
			}
		}
	}()
}

// RecentStderr returns everything captured since the tap was installed,
// bounded to the last 8 KiB.
func RecentStderr() string {
	tap.mu.Lock()
	defer tap.mu.Unlock()
	return tap.buf.String()
}
