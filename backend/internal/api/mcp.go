package api

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// mcpServerVersion is the MCP protocol-level version string some clients
// render in a "connected servers" UI — independent of the app's own release
// version (main.version), which the MCP server has no reason to know about.
const mcpServerVersion = "0.1.0"

// buildMCPServer registers every tool exactly once against a single
// *mcp.Server instance. Per NewStreamableHTTPHandler's own doc comment, it's
// fine for getServer to return the same server on every call — so tools are
// registered once at startup, and each mutating tool checks s.mcpFlags at
// call time instead of the tool list changing dynamically.
func (s *Server) buildMCPServer() *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "netsk8-navigator", Version: mcpServerVersion}, nil)
	registerReadTools(srv, s)
	registerWriteTools(srv, s)
	return srv
}

// MCPHandler returns the /mcp http.Handler. It is always mounted (a plain
// http.ServeMux can't cleanly unregister a route once added), but 404s
// while MCP is toggled off — so a probe against the URL while disabled
// doesn't even reveal that an MCP server exists there.
func (s *Server) MCPHandler() http.Handler {
	mcpSrv := s.buildMCPServer()
	h := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return mcpSrv }, &mcp.StreamableHTTPOptions{
		// Session-lifecycle logging only — per-tool-call audit trail is
		// already covered by the same audit(r, ...) calls the underlying
		// REST handlers make (see callREST below), whether the request came
		// from a browser or from a tool's synthetic in-process request.
		Logger:         slog.New(slog.NewTextHandler(os.Stderr, nil)),
		SessionTimeout: 30 * time.Minute,
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.mcpFlags.Enabled() {
			http.NotFound(w, r)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// callREST replays a request against the existing REST handlers in-process —
// the same httptest.NewRequest/httptest.NewRecorder pattern this codebase's
// own handler tests already use throughout internal/api — so every MCP tool
// reuses real, already-tested handler code with zero duplicated logic.
// Rebuilding s.Routes() per call is a little wasteful (a fresh ~70-entry mux
// each time) but tool calls are agent-driven, not a hot loop, so simplicity
// wins over caching it.
func (s *Server) callREST(ctx context.Context, method, path string, body []byte) (status int, respBody []byte) {
	req := httptest.NewRequest(method, path, bytes.NewReader(body)).WithContext(ctx)
	req.RemoteAddr = "mcp-tool:0" // distinguishes MCP-triggered audit log lines from real browser requests
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
}

// toolResult turns a REST response into an MCP tool result: the raw JSON
// body as text content on success (2xx), or a Go error on failure — which
// the SDK packs into CallToolResult with IsError set, the documented way to
// signal a tool-level (not protocol-level) failure.
func toolResult(status int, body []byte) (*mcp.CallToolResult, any, error) {
	if status < 200 || status >= 300 {
		return nil, nil, fmt.Errorf("request failed (%d): %s", status, string(body))
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(body)}}}, nil, nil
}

// pathNamespace returns ns, or "-" when it's empty — the same placeholder
// convention the frontend already uses (frontend/src/lib/api.ts's getDetail)
// for cluster-scoped kinds' namespace URL segment, which the underlying
// handlers ignore (via resolveSlug's Namespaced check) but which the route
// pattern still requires to be a non-empty segment.
func pathNamespace(ns string) string {
	if ns == "" {
		return "-"
	}
	return ns
}
