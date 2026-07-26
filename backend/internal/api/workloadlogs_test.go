package api

import (
	"net/http"
	"testing"
)

// The actual SSE fan-in (streamPodLogsInto, the flusher loop) needs a live
// kubelet log stream — same category as podlogs.go/podexec.go/watch.go, which
// have no unit tests either. This covers the handler's own branching logic:
// resolving the workload's pods and rejecting an empty result before ever
// touching the streaming machinery.
func TestHandleWorkloadLogs_NoPods(t *testing.T) {
	s := newTestServer(t)
	rec := doRequest(t, s, "GET", "/api/contexts/test/pods-of/deployment/prod/web/logs", "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 (no pods found)", rec.Code)
	}
}
