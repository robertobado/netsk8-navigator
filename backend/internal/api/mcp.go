package api

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/robertobado/netsk8-navigator/backend/internal/kube"
)

// mcpInstructions is surfaced to every connecting client in `initialize` —
// the natural place to head off the two mistakes real usage turned up:
// guessing at a context name (each tool's schema also hard-enforces this via
// an enum, see contextInputSchema) and not knowing where to start triaging.
const mcpInstructions = "Always call list_contexts first and use one of the returned names verbatim as `context` in every other tool call — arbitrary strings are rejected. To triage what's wrong in a cluster, start with get_issues rather than listing pods yourself."

// buildMCPServer registers every tool exactly once against a single
// *mcp.Server instance. Per NewStreamableHTTPHandler's own doc comment, it's
// fine for getServer to return the same server on every call — so tools are
// registered once at startup, and each mutating tool checks s.mcpFlags at
// call time instead of the tool list changing dynamically.
func (s *Server) buildMCPServer() *mcp.Server {
	version := s.Version
	if version == "" {
		version = "dev" // unset in tests / a plain `go build` with no -ldflags — matches main.version's own convention
	}
	srv := mcp.NewServer(&mcp.Implementation{Name: "netsk8-navigator", Version: version}, &mcp.ServerOptions{
		Instructions: mcpInstructions,
	})
	contexts := contextNames(s.mgr.Contexts())
	registerReadTools(srv, s, contexts)
	registerWriteTools(srv, s, contexts)
	return srv
}

func contextNames(contexts []kube.ContextInfo) []string {
	names := make([]string, len(contexts))
	for i, c := range contexts {
		names[i] = c.Name
	}
	return names
}

// contextInputSchema builds T's input schema via reflection, same as AddTool
// would do implicitly, then constrains its "context" property to the live
// kubeconfig context names. Passing the result via Tool.InputSchema (rather
// than leaving it nil for AddTool to infer) makes the SDK use it as-is — it
// only auto-derives when InputSchema is nil. Since the server validates
// call arguments against this schema before invoking the handler, a typo'd
// context name now fails as an immediate schema-validation error instead of
// round-tripping through a REST call just to come back as a 400. Contexts
// are loaded once at process start (no kubeconfig hot-reload exists
// anywhere in this app today), so this enum is exactly as fresh as
// list_contexts' own output.
func contextInputSchema[T any](contexts []string) *jsonschema.Schema {
	s, err := jsonschema.ForType(reflect.TypeFor[T](), nil)
	if err != nil {
		panic(fmt.Errorf("mcp: building schema for %T: %w", *new(T), err))
	}
	enum := make([]any, len(contexts))
	for i, c := range contexts {
		enum[i] = c
	}
	if p, ok := s.Properties["context"]; ok {
		p.Enum = enum
	}
	return s
}

// RunStdio serves this app's MCP tools over stdio (JSON-RPC framed on
// stdin/stdout), blocking until stdin closes or ctx is cancelled — the
// entry point for --mcp-stdio. No HTTP, no auth token: the trust boundary
// is "whoever can spawn this process," the same as running kubectl
// directly. s.mcpFlags must already be set (see newStdioMCPFlags) before
// this is called.
func (s *Server) RunStdio(ctx context.Context) error {
	return s.buildMCPServer().Run(ctx, &mcp.StdioTransport{})
}

// mcpTokenHeader carries the /mcp bearer token (see mcpToken below). Not
// "Authorization": AUTH_PASSWORD's HTTP Basic Auth already wraps /mcp for
// free (main.go's wrapWithAuth covers the whole mux), and Basic and Bearer
// can't both occupy that one header — a separate header lets the two gates
// compose instead of colliding when both are configured.
const mcpTokenHeader = "X-Netsk8-MCP-Token" //nolint:gosec // a header name, not a credential value

// MCPHandler returns the /mcp http.Handler. It is always mounted (a plain
// http.ServeMux can't cleanly unregister a route once added), but 404s
// while MCP is toggled off — so a probe against the URL while disabled
// doesn't even reveal that an MCP server exists there. Once enabled, it
// additionally requires mcpTokenHeader to match the per-install token
// (internal/config.Store.MCPToken) — HTTP mode has no other credential by
// default, unlike stdio mode where spawning the process is itself the
// credential.
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
		token, err := s.cfg.MCPToken()
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.Header.Get(mcpTokenHeader)), []byte(token)) != 1 {
			http.Error(w, "missing or invalid "+mcpTokenHeader+" header", http.StatusUnauthorized)
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
//
// On a failure that looks like a client-go exec-credential plugin dying
// (e.g. an expired AWS SSO session), the response's "error" field is
// enriched with a concrete next step recovered from the plugin's own
// stderr — see execFailureHint.
func (s *Server) callREST(ctx context.Context, method, path string, body []byte) (status int, respBody []byte) {
	stderrMark := len(kube.RecentStderr())
	req := httptest.NewRequest(method, path, bytes.NewReader(body)).WithContext(ctx)
	req.RemoteAddr = "mcp-tool:0" // distinguishes MCP-triggered audit log lines from real browser requests
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)
	status, respBody = rec.Code, rec.Body.Bytes()

	if status >= 400 && bytes.Contains(respBody, []byte("exec: executable")) {
		if hint := s.execFailureHint(path, kube.RecentStderr()[stderrMark:]); hint != "" {
			respBody = appendErrorHint(respBody, hint)
		}
	}
	return status, respBody
}

// execHints matches captured exec-credential-plugin stderr against known
// failure phrasings, extensible one entry at a time. Only one is shipped —
// the one concretely reported and verified (an expired AWS SSO session) —
// rather than guessing at other providers' unverified message text.
var execHints = []struct {
	match func(stderr string) bool
	hint  func(cmd, profile string) string
}{
	{
		match: func(s string) bool {
			return strings.Contains(s, "Token has expired") ||
				strings.Contains(s, "SSO session associated with this profile has expired")
		},
		hint: func(cmd, profile string) string {
			if profile == "" {
				return ""
			}
			return fmt.Sprintf("credentials expired for profile %q — run: %s sso login --profile %s", profile, cmd, profile)
		},
	},
}

// execFailureHint recovers an actionable message for a failed cluster
// request by combining the context's exec-plugin configuration (which
// command, which profile — from the kubeconfig itself) with what that
// plugin actually printed to stderr (captured via kube.InstallStderrTap,
// since client-go's own error never includes it). path is a REST path of
// the form /api/contexts/{name}/...; stderr is what was newly written
// since the request began. Returns "" when nothing matched.
func (s *Server) execFailureHint(path, stderr string) string {
	if stderr == "" {
		return ""
	}
	contextName := contextNameFromPath(path)
	if contextName == "" {
		return ""
	}
	cmd, profile, ok := s.mgr.ExecInfoFor(contextName)
	if !ok {
		return ""
	}
	for _, h := range execHints {
		if h.match(stderr) {
			if hint := h.hint(cmd, profile); hint != "" {
				return hint
			}
		}
	}
	return ""
}

// contextNameFromPath extracts {name} from "/api/contexts/{name}/...".
func contextNameFromPath(path string) string {
	const prefix = "/api/contexts/"
	if !strings.HasPrefix(path, prefix) {
		return ""
	}
	rest := path[len(prefix):]
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		rest = rest[:i]
	}
	if i := strings.IndexByte(rest, '?'); i >= 0 {
		rest = rest[:i]
	}
	return rest
}

// appendErrorHint rewrites a {"error": "..."} REST body to append hint,
// falling back to the original bytes untouched if the body isn't the shape
// writeError produces (defensive — should never happen for a REST error
// response, but a best-effort enrichment must never break the underlying
// result).
func appendErrorHint(body []byte, hint string) []byte {
	var parsed struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil || parsed.Error == "" {
		return body
	}
	out, err := json.Marshal(map[string]string{"error": parsed.Error + " (" + hint + ")"})
	if err != nil {
		return body
	}
	return out
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
