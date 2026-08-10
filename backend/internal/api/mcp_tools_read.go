package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerReadTools wires up every read-only MCP tool. Each one replays a
// request against the matching REST route via callREST — see mcp.go.
// contexts feeds contextInputSchema so every tool's "context" argument is
// constrained to the kubeconfig's actual context names.

type ctxArgs struct {
	Context string `json:"context" jsonschema:"kubeconfig context name, from list_contexts"`
}

type namespaceScopedListArgs struct {
	Context   string `json:"context" jsonschema:"kubeconfig context name, from list_contexts"`
	Namespace string `json:"namespace,omitempty" jsonschema:"optional namespace filter; omit for all namespaces"`
	Limit     int    `json:"limit,omitempty" jsonschema:"optional cap on the number of items returned; omit for no limit"`
	Since     string `json:"since,omitempty" jsonschema:"optional RFC3339 timestamp; only items created/changed at or after this are returned"`
}

type resourceKindArgs struct {
	Context   string `json:"context" jsonschema:"kubeconfig context name"`
	Kind      string `json:"kind" jsonschema:"manifest kind slug, e.g. pod, deployment, service, configmap, node, namespace, secret"`
	Namespace string `json:"namespace,omitempty" jsonschema:"resource namespace; omit for cluster-scoped kinds like node or namespace"`
	Name      string `json:"name" jsonschema:"resource name"`
}

// readOnly always sets IdempotentHint true alongside ReadOnlyHint: a read
// is idempotent by definition (repeating it can't change anything), so
// leaving IdempotentHint at its Go zero value (false) would read as a
// contradiction to a client — "read-only, but not idempotent."
func readOnly() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true}
}

func registerReadTools(srv *mcp.Server, s *Server, contexts []string) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_contexts",
		Description: "List the kubeconfig contexts (clusters) this app can connect to.",
		Annotations: readOnly(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, any, error) {
		return toolResult(s.callREST(ctx, "GET", "/api/contexts", nil))
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_namespaces",
		Description: "List namespaces in a cluster context.",
		Annotations: readOnly(),
		InputSchema: contextInputSchema[ctxArgs](contexts),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args ctxArgs) (*mcp.CallToolResult, any, error) {
		path := "/api/contexts/" + url.PathEscape(args.Context) + "/namespaces"
		return toolResult(s.callREST(ctx, "GET", path, nil))
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_nodes",
		Description: "List nodes in a cluster context, with readiness, roles, version, and capacity.",
		Annotations: readOnly(),
		InputSchema: contextInputSchema[ctxArgs](contexts),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args ctxArgs) (*mcp.CallToolResult, any, error) {
		path := "/api/contexts/" + url.PathEscape(args.Context) + "/nodes"
		return toolResult(s.callREST(ctx, "GET", path, nil))
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_pods",
		Description: "List pods in a cluster context, optionally filtered by namespace. limit/since keep the response small on a busy cluster.",
		Annotations: readOnly(),
		InputSchema: contextInputSchema[namespaceScopedListArgs](contexts),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args namespaceScopedListArgs) (*mcp.CallToolResult, any, error) {
		path := "/api/contexts/" + url.PathEscape(args.Context) + "/pods"
		if args.Namespace != "" {
			path += "?namespace=" + url.QueryEscape(args.Namespace)
		}
		status, body := s.callREST(ctx, "GET", path, nil)
		if status < 200 || status >= 300 {
			return toolResult(status, body)
		}
		return toolResult(status, shapeItemList(body, args.Limit, args.Since, "age"))
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_resources",
		Description: "List resources of a given kind (e.g. deployments, services, configmaps, jobs, secrets, ingresses) in a cluster context, optionally filtered by namespace. limit keeps the response small on a busy cluster.",
		Annotations: readOnly(),
		InputSchema: contextInputSchema[struct {
			Context   string `json:"context" jsonschema:"kubeconfig context name"`
			Resource  string `json:"resource" jsonschema:"plural resource name, e.g. deployments, services, configmaps, jobs, secrets, ingresses"`
			Namespace string `json:"namespace,omitempty" jsonschema:"optional namespace filter; omit for all namespaces"`
			Limit     int    `json:"limit,omitempty" jsonschema:"optional cap on the number of items returned; omit for no limit"`
		}](contexts),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args struct {
		Context   string `json:"context" jsonschema:"kubeconfig context name"`
		Resource  string `json:"resource" jsonschema:"plural resource name, e.g. deployments, services, configmaps, jobs, secrets, ingresses"`
		Namespace string `json:"namespace,omitempty" jsonschema:"optional namespace filter; omit for all namespaces"`
		Limit     int    `json:"limit,omitempty" jsonschema:"optional cap on the number of items returned; omit for no limit"`
	},
	) (*mcp.CallToolResult, any, error) {
		path := "/api/contexts/" + url.PathEscape(args.Context) + "/resources/" + url.PathEscape(args.Resource)
		if args.Namespace != "" {
			path += "?namespace=" + url.QueryEscape(args.Namespace)
		}
		status, body := s.callREST(ctx, "GET", path, nil)
		if status < 200 || status >= 300 {
			return toolResult(status, body)
		}
		return toolResult(status, shapeItemList(body, args.Limit, "", ""))
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_resource_detail",
		Description: "Get structured detail (status, conditions, images, related resources, etc.) for a single resource by kind/namespace/name.",
		Annotations: readOnly(),
		InputSchema: contextInputSchema[resourceKindArgs](contexts),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args resourceKindArgs) (*mcp.CallToolResult, any, error) {
		path := fmt.Sprintf("/api/contexts/%s/detail/%s/%s/%s",
			url.PathEscape(args.Context), url.PathEscape(args.Kind), url.PathEscape(pathNamespace(args.Namespace)), url.PathEscape(args.Name))
		return toolResult(s.callREST(ctx, "GET", path, nil))
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_manifest",
		Description: "Get a resource's current manifest as YAML — read this before apply_manifest to edit the live version rather than guessing its shape.",
		Annotations: readOnly(),
		InputSchema: contextInputSchema[resourceKindArgs](contexts),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args resourceKindArgs) (*mcp.CallToolResult, any, error) {
		path := fmt.Sprintf("/api/contexts/%s/manifest/%s/%s/%s",
			url.PathEscape(args.Context), url.PathEscape(args.Kind), url.PathEscape(pathNamespace(args.Namespace)), url.PathEscape(args.Name))
		return toolResult(s.callREST(ctx, "GET", path, nil))
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_logs",
		Description: "Get the most recent log lines from a pod container (bounded, non-streaming — not a live tail).",
		Annotations: readOnly(),
		InputSchema: contextInputSchema[struct {
			Context   string `json:"context" jsonschema:"kubeconfig context name"`
			Namespace string `json:"namespace" jsonschema:"pod namespace"`
			Name      string `json:"name" jsonschema:"pod name"`
			Container string `json:"container,omitempty" jsonschema:"container name; omit for a single-container pod"`
			TailLines int64  `json:"tailLines,omitempty" jsonschema:"number of most recent lines to return (default 200, max 2000)"`
		}](contexts),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args struct {
		Context   string `json:"context" jsonschema:"kubeconfig context name"`
		Namespace string `json:"namespace" jsonschema:"pod namespace"`
		Name      string `json:"name" jsonschema:"pod name"`
		Container string `json:"container,omitempty" jsonschema:"container name; omit for a single-container pod"`
		TailLines int64  `json:"tailLines,omitempty" jsonschema:"number of most recent lines to return (default 200, max 2000)"`
	},
	) (*mcp.CallToolResult, any, error) {
		logs, err := s.fetchBoundedPodLogs(ctx, args.Context, args.Namespace, args.Name, args.Container, args.TailLines)
		if err != nil {
			return nil, nil, err
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: logs}}}, nil, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_overview",
		Description: "Get cluster-wide counts: node/pod/namespace totals, ready nodes, and pods by phase (running/pending/failed).",
		Annotations: readOnly(),
		InputSchema: contextInputSchema[ctxArgs](contexts),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args ctxArgs) (*mcp.CallToolResult, any, error) {
		path := "/api/contexts/" + url.PathEscape(args.Context) + "/overview"
		return toolResult(s.callREST(ctx, "GET", path, nil))
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_issues",
		Description: "Get the same unhealthy-resource feed the app's overview page shows: pending pods, failed/crash-looping pods, and not-ready nodes, each with a reason and message, plus a summary grouped by cause. The best starting point for triaging what's wrong in a cluster. limit/since keep the response small on a busy cluster; the summary always reflects the true total even when the lists are capped.",
		Annotations: readOnly(),
		InputSchema: contextInputSchema[struct {
			Context string `json:"context" jsonschema:"kubeconfig context name"`
			Limit   int    `json:"limit,omitempty" jsonschema:"optional cap on the number of items returned per section; omit for no limit"`
			Since   string `json:"since,omitempty" jsonschema:"optional RFC3339 timestamp; only items since this are returned"`
		}](contexts),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args struct {
		Context string `json:"context" jsonschema:"kubeconfig context name"`
		Limit   int    `json:"limit,omitempty" jsonschema:"optional cap on the number of items returned per section; omit for no limit"`
		Since   string `json:"since,omitempty" jsonschema:"optional RFC3339 timestamp; only items since this are returned"`
	},
	) (*mcp.CallToolResult, any, error) {
		path := "/api/contexts/" + url.PathEscape(args.Context) + "/issues"
		status, body := s.callREST(ctx, "GET", path, nil)
		if status < 200 || status >= 300 {
			return toolResult(status, body)
		}
		return toolResult(status, shapeIssues(body, args.Limit, args.Since))
	})
}

// shapeItemList post-processes a bare-array (or {"items":[...]}) REST
// response for an MCP list tool: applies since (filtering on a top-level
// RFC3339 field named by sinceField, when set) and limit, and — only when
// either was actually provided — wraps the result as
// {"items":[...],"total":N,"returned":M} so the agent can tell it's a
// partial view. Omitting both leaves the response byte-identical to what
// the REST handler already returns, so this is purely additive.
func shapeItemList(body []byte, limit int, since string, sinceField string) []byte {
	if limit <= 0 && since == "" {
		return body
	}
	var items []json.RawMessage
	if err := json.Unmarshal(body, &items); err != nil {
		return body // not a bare array — leave it alone rather than guess
	}
	if since != "" && sinceField != "" {
		items = filterSince(items, sinceField, since)
	}
	total := len(items)
	if limit > 0 && limit < len(items) {
		items = items[:limit]
	}
	out, err := json.Marshal(map[string]any{"items": items, "total": total, "returned": len(items)})
	if err != nil {
		return body
	}
	return out
}

// filterSince keeps only items whose top-level field is an RFC3339
// timestamp at or after cutoff. Items missing the field, or where it
// doesn't parse, are kept — filtering conservatively (never silently
// dropping something we can't confidently classify) beats a false negative.
func filterSince(items []json.RawMessage, field, since string) []json.RawMessage {
	cutoff, err := time.Parse(time.RFC3339, since)
	if err != nil {
		return items
	}
	out := items[:0]
	for _, item := range items {
		var obj map[string]any
		if err := json.Unmarshal(item, &obj); err != nil {
			out = append(out, item)
			continue
		}
		ts, ok := obj[field].(string)
		if !ok {
			out = append(out, item)
			continue
		}
		t, err := time.Parse(time.RFC3339, ts)
		if err != nil || !t.Before(cutoff) {
			out = append(out, item)
		}
	}
	return out
}

// issueItemShape mirrors issues.go's issueItem — duplicated narrowly here
// (only the fields this post-processing needs) rather than exported from
// issues.go, so the REST response shape stays exactly what it is today and
// this stays purely a consumer of it, like every other MCP tool.
type issueItemShape struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
	Since     string `json:"since"`
	Reason    string `json:"reason"`
	Message   string `json:"message"`
}

// shapeIssues post-processes GET .../issues's {"pending":[...],"failed":[...],"nodesNotReady":[...]}
// body: always dedupes repeated long tokens in each message (kubelet's own
// wrapped-error strings commonly repeat the full image reference several
// times in one string) and always adds a "summary" grouping the
// pre-truncation full set by (kind, reason) with counts, namespaces, and a
// few examples. limit/since additionally cap/filter each section, adding
// matching *Total counts — applied after the summary is computed, so the
// summary always reflects the true total.
func shapeIssues(body []byte, limit int, since string) []byte {
	var parsed struct {
		Pending       []issueItemShape `json:"pending"`
		Failed        []issueItemShape `json:"failed"`
		NodesNotReady []issueItemShape `json:"nodesNotReady"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return body
	}
	for _, list := range [][]issueItemShape{parsed.Pending, parsed.Failed, parsed.NodesNotReady} {
		for i := range list {
			list[i].Message = dedupeRepeatedTokens(list[i].Message)
		}
	}

	summary := summarizeIssues(parsed.Pending, parsed.Failed, parsed.NodesNotReady)

	out := map[string]any{
		"pending":       parsed.Pending,
		"failed":        parsed.Failed,
		"nodesNotReady": parsed.NodesNotReady,
		"summary":       summary,
	}
	if limit > 0 || since != "" {
		pendingTotal, failedTotal, nodesTotal := len(parsed.Pending), len(parsed.Failed), len(parsed.NodesNotReady)
		out["pending"] = truncateIssues(parsed.Pending, limit, since)
		out["failed"] = truncateIssues(parsed.Failed, limit, since)
		out["nodesNotReady"] = truncateIssues(parsed.NodesNotReady, limit, since)
		out["pendingTotal"] = pendingTotal
		out["failedTotal"] = failedTotal
		out["nodesNotReadyTotal"] = nodesTotal
	}
	marshaled, err := json.Marshal(out)
	if err != nil {
		return body
	}
	return marshaled
}

func truncateIssues(items []issueItemShape, limit int, since string) []issueItemShape {
	if since != "" {
		if cutoff, err := time.Parse(time.RFC3339, since); err == nil {
			out := items[:0]
			for _, it := range items {
				t, err := time.Parse(time.RFC3339, it.Since)
				if err != nil || !t.Before(cutoff) {
					out = append(out, it)
				}
			}
			items = out
		}
	}
	if limit > 0 && limit < len(items) {
		items = items[:limit]
	}
	return items
}

type issueSummaryEntry struct {
	Kind       string   `json:"kind"`
	Reason     string   `json:"reason"`
	Count      int      `json:"count"`
	Namespaces []string `json:"namespaces"`
	Examples   []struct {
		Namespace string `json:"namespace,omitempty"`
		Name      string `json:"name"`
	} `json:"examples"`
}

// summarizeIssues groups every issue across all three sections by (kind,
// reason) — the direct answer to "66 pending pods were really ~6 causes
// across 4 namespaces."
func summarizeIssues(sections ...[]issueItemShape) []issueSummaryEntry {
	type key struct{ kind, reason string }
	byKey := map[key]*issueSummaryEntry{}
	var order []key
	for _, section := range sections {
		for _, it := range section {
			k := key{it.Kind, it.Reason}
			e, ok := byKey[k]
			if !ok {
				e = &issueSummaryEntry{Kind: it.Kind, Reason: it.Reason}
				byKey[k] = e
				order = append(order, k)
			}
			e.Count++
			if it.Namespace != "" && !containsStr(e.Namespaces, it.Namespace) {
				e.Namespaces = append(e.Namespaces, it.Namespace)
			}
			if len(e.Examples) < 3 {
				e.Examples = append(e.Examples, struct {
					Namespace string `json:"namespace,omitempty"`
					Name      string `json:"name"`
				}{it.Namespace, it.Name})
			}
		}
	}
	out := make([]issueSummaryEntry, 0, len(order))
	for _, k := range order {
		out = append(out, *byKey[k])
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Count > out[j].Count })
	return out
}

func containsStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// dedupeRepeatedTokens collapses later occurrences of any single
// whitespace-delimited token ≥30 chars that appears more than once in msg.
// Not image-specific — kubelet's waiting-container messages commonly repeat
// the full image reference several times in one wrapped-error string, but
// this is generic enough to be safe against anything else that happens to
// repeat the same way, and never touches a message with no such token.
func dedupeRepeatedTokens(msg string) string {
	fields := strings.Fields(msg)
	seen := make(map[string]bool, len(fields))
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		// Trailing punctuation (a colon before a nested error, a trailing
		// quote/comma/paren) shouldn't stop the same underlying token from
		// being recognized as a repeat.
		key := strings.TrimRight(f, ":,.\"')]")
		if len(key) >= 30 {
			if seen[key] {
				out = append(out, "…")
				continue
			}
			seen[key] = true
		}
		out = append(out, f)
	}
	return strings.Join(out, " ")
}
