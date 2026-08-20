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

// crdListArgs/crdGetArgs address a CRD instance by its GVR straight from
// list_crd_kinds, instead of the fixed manifest-kind slug resourceKindArgs
// uses — that slug catalog only covers built-in kinds, never CRDs.
type crdListArgs struct {
	Context   string `json:"context" jsonschema:"kubeconfig context name"`
	Group     string `json:"group" jsonschema:"CRD API group, e.g. secrets-store.csi.x-k8s.io — from list_crd_kinds"`
	Version   string `json:"version" jsonschema:"CRD API version, e.g. v1 — from list_crd_kinds"`
	Resource  string `json:"resource" jsonschema:"CRD plural resource name, e.g. secretproviderclasses — from list_crd_kinds"`
	Namespace string `json:"namespace,omitempty" jsonschema:"optional namespace filter; omit for all namespaces"`
	Limit     int    `json:"limit,omitempty" jsonschema:"optional cap on the number of items returned; omit for no limit"`
}

type crdGetArgs struct {
	Context   string `json:"context" jsonschema:"kubeconfig context name"`
	Group     string `json:"group" jsonschema:"CRD API group, e.g. secrets-store.csi.x-k8s.io — from list_crd_kinds"`
	Version   string `json:"version" jsonschema:"CRD API version, e.g. v1 — from list_crd_kinds"`
	Resource  string `json:"resource" jsonschema:"CRD plural resource name, e.g. secretproviderclasses — from list_crd_kinds"`
	Namespace string `json:"namespace,omitempty" jsonschema:"resource namespace; omit for a cluster-scoped kind"`
	Name      string `json:"name" jsonschema:"resource name"`
}

// readOnly always sets IdempotentHint true alongside ReadOnlyHint: a read
// is idempotent by definition (repeating it can't change anything), so
// leaving IdempotentHint at its Go zero value (false) would read as a
// contradiction to a client — "read-only, but not idempotent."
func readOnly() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true}
}

// contextPath builds a path-escaped /api/contexts/{context}/{suffix} route —
// the shape every MCP read tool's REST call replays against.
func contextPath(context, suffix string) string {
	return "/api/contexts/" + url.PathEscape(context) + "/" + suffix
}

// withNamespaceQuery appends an optional ?namespace= query param — the shape
// list_pods/list_resources/list_crd_resources all share.
func withNamespaceQuery(path, namespace string) string {
	if namespace == "" {
		return path
	}
	return path + "?namespace=" + url.QueryEscape(namespace)
}

// registerSimpleGetTool registers a read-only tool whose handler is nothing
// but a GET to a fixed sub-path under /api/contexts/{context}/ — the shape
// list_namespaces, list_nodes, and get_overview all share exactly.
func registerSimpleGetTool(srv *mcp.Server, s *Server, contexts []string, name, description, subpath string) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        name,
		Description: description,
		Annotations: readOnly(),
		InputSchema: contextInputSchema[ctxArgs](contexts),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args ctxArgs) (*mcp.CallToolResult, any, error) {
		return toolResult(s.callREST(ctx, "GET", contextPath(args.Context, subpath), nil))
	})
}

// registerResourceGetTool registers a read-only tool whose handler is a GET
// to /api/contexts/{context}/{urlSegment}/{kind}/{namespace}/{name} — the
// shape get_resource_detail and get_manifest both share.
func registerResourceGetTool(srv *mcp.Server, s *Server, contexts []string, name, description, urlSegment string) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        name,
		Description: description,
		Annotations: readOnly(),
		InputSchema: contextInputSchema[resourceKindArgs](contexts),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args resourceKindArgs) (*mcp.CallToolResult, any, error) {
		suffix := fmt.Sprintf("%s/%s/%s/%s", urlSegment, url.PathEscape(args.Kind), url.PathEscape(pathNamespace(args.Namespace)), url.PathEscape(args.Name))
		return toolResult(s.callREST(ctx, "GET", contextPath(args.Context, suffix), nil))
	})
}

// registerCRDGetTool registers a read-only tool whose handler is a GET to
// /api/contexts/{context}/crd/{group}/{version}/{resource}/{namespace}/{name}/{urlSuffix}
// — the CRD-addressed counterpart to registerResourceGetTool, for kinds
// list_resources/get_resource_detail/get_manifest can't reach (any CRD, since
// those three only know the fixed built-in catalog/manifest-slug map).
func registerCRDGetTool(srv *mcp.Server, s *Server, contexts []string, name, description, urlSuffix string) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        name,
		Description: description,
		Annotations: readOnly(),
		InputSchema: contextInputSchema[crdGetArgs](contexts),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args crdGetArgs) (*mcp.CallToolResult, any, error) {
		suffix := fmt.Sprintf("crd/%s/%s/%s/%s/%s/%s",
			url.PathEscape(args.Group), url.PathEscape(args.Version), url.PathEscape(args.Resource),
			url.PathEscape(pathNamespace(args.Namespace)), url.PathEscape(args.Name), urlSuffix)
		return toolResult(s.callREST(ctx, "GET", contextPath(args.Context, suffix), nil))
	})
}

type listResourcesArgs struct {
	Context   string `json:"context" jsonschema:"kubeconfig context name"`
	Resource  string `json:"resource" jsonschema:"plural resource name, e.g. deployments, services, configmaps, jobs, secrets, ingresses"`
	Namespace string `json:"namespace,omitempty" jsonschema:"optional namespace filter; omit for all namespaces"`
	Limit     int    `json:"limit,omitempty" jsonschema:"optional cap on the number of items returned; omit for no limit"`
}

type getLogsArgs struct {
	Context   string `json:"context" jsonschema:"kubeconfig context name"`
	Namespace string `json:"namespace" jsonschema:"pod namespace"`
	Name      string `json:"name" jsonschema:"pod name"`
	Container string `json:"container,omitempty" jsonschema:"container name; omit for a single-container pod"`
	TailLines int64  `json:"tailLines,omitempty" jsonschema:"number of most recent lines to return (default 200, max 2000)"`
}

type getIssuesArgs struct {
	Context string `json:"context" jsonschema:"kubeconfig context name"`
	Limit   int    `json:"limit,omitempty" jsonschema:"optional cap on the number of items returned per section; omit for no limit"`
	Since   string `json:"since,omitempty" jsonschema:"optional RFC3339 timestamp; only items since this are returned"`
}

// registerListPodsTool, registerListResourcesTool, registerListCRDResourcesTool,
// registerGetLogsTool, and registerGetIssuesTool are each pulled out of
// registerReadTools (rather than inlined like list_contexts/list_crd_kinds)
// specifically because they post-process the REST response — the extra
// branching pushed registerReadTools itself over the cognitive-complexity
// limit when it was all inlined.

func registerListPodsTool(srv *mcp.Server, s *Server, contexts []string) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_pods",
		Description: "List pods in a cluster context, optionally filtered by namespace. limit/since keep the response small on a busy cluster.",
		Annotations: readOnly(),
		InputSchema: contextInputSchema[namespaceScopedListArgs](contexts),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args namespaceScopedListArgs) (*mcp.CallToolResult, any, error) {
		path := withNamespaceQuery(contextPath(args.Context, "pods"), args.Namespace)
		status, body := s.callREST(ctx, "GET", path, nil)
		if status < 200 || status >= 300 {
			return toolResult(status, body)
		}
		return toolResult(status, shapeItemList(body, args.Limit, args.Since, "age"))
	})
}

func registerListResourcesTool(srv *mcp.Server, s *Server, contexts []string) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_resources",
		Description: "List resources of a given built-in kind (e.g. deployments, services, configmaps, jobs, secrets, ingresses) in a cluster context, optionally filtered by namespace. limit keeps the response small on a busy cluster. This only knows a fixed set of built-in Kubernetes kinds — for a CustomResourceDefinition (CRD), e.g. SecretProviderClass, use list_crd_kinds then list_crd_resources instead.",
		Annotations: readOnly(),
		InputSchema: contextInputSchema[listResourcesArgs](contexts),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args listResourcesArgs) (*mcp.CallToolResult, any, error) {
		path := withNamespaceQuery(contextPath(args.Context, "resources/"+url.PathEscape(args.Resource)), args.Namespace)
		status, body := s.callREST(ctx, "GET", path, nil)
		if status < 200 || status >= 300 {
			return toolResult(status, body)
		}
		return toolResult(status, shapeItemList(body, args.Limit, "", ""))
	})
}

func registerListCRDResourcesTool(srv *mcp.Server, s *Server, contexts []string) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_crd_resources",
		Description: "List instances of a CRD kind (group/version/resource from list_crd_kinds), optionally filtered by namespace. limit keeps the response small on a busy cluster.",
		Annotations: readOnly(),
		InputSchema: contextInputSchema[crdListArgs](contexts),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args crdListArgs) (*mcp.CallToolResult, any, error) {
		suffix := fmt.Sprintf("crd/%s/%s/%s", url.PathEscape(args.Group), url.PathEscape(args.Version), url.PathEscape(args.Resource))
		path := withNamespaceQuery(contextPath(args.Context, suffix), args.Namespace)
		status, body := s.callREST(ctx, "GET", path, nil)
		if status < 200 || status >= 300 {
			return toolResult(status, body)
		}
		return toolResult(status, shapeItemList(body, args.Limit, "", ""))
	})
}

func registerGetLogsTool(srv *mcp.Server, s *Server, contexts []string) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_logs",
		Description: "Get the most recent log lines from a pod container (bounded, non-streaming — not a live tail).",
		Annotations: readOnly(),
		InputSchema: contextInputSchema[getLogsArgs](contexts),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args getLogsArgs) (*mcp.CallToolResult, any, error) {
		logs, err := s.fetchBoundedPodLogs(ctx, args.Context, args.Namespace, args.Name, args.Container, args.TailLines)
		if err != nil {
			return nil, nil, err
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: logs}}}, nil, nil
	})
}

func registerGetIssuesTool(srv *mcp.Server, s *Server, contexts []string) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_issues",
		Description: "Get the same unhealthy-resource feed the app's overview page shows: pending pods, failed/crash-looping pods, and not-ready nodes, each with a reason and message, plus a summary grouped by cause. The best starting point for triaging what's wrong in a cluster. limit/since keep the response small on a busy cluster; the summary always reflects the true total even when the lists are capped.",
		Annotations: readOnly(),
		InputSchema: contextInputSchema[getIssuesArgs](contexts),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args getIssuesArgs) (*mcp.CallToolResult, any, error) {
		status, body := s.callREST(ctx, "GET", contextPath(args.Context, "issues"), nil)
		if status < 200 || status >= 300 {
			return toolResult(status, body)
		}
		return toolResult(status, shapeIssues(body, args.Limit, args.Since))
	})
}

func registerReadTools(srv *mcp.Server, s *Server, contexts []string) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_contexts",
		Description: "List the kubeconfig contexts (clusters) this app can connect to.",
		Annotations: readOnly(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, any, error) {
		return toolResult(s.callREST(ctx, "GET", "/api/contexts", nil))
	})

	registerSimpleGetTool(srv, s, contexts, "list_namespaces", "List namespaces in a cluster context.", "namespaces")
	registerSimpleGetTool(srv, s, contexts, "list_nodes", "List nodes in a cluster context, with readiness, roles, version, and capacity.", "nodes")
	registerListPodsTool(srv, s, contexts)
	registerListResourcesTool(srv, s, contexts)

	registerResourceGetTool(srv, s, contexts, "get_resource_detail", "Get structured detail (status, conditions, images, related resources, etc.) for a single built-in resource by kind/namespace/name. For a CRD instance, use get_crd_detail instead.", "detail")
	registerResourceGetTool(srv, s, contexts, "get_manifest", "Get a built-in resource's current manifest as YAML — read this before apply_manifest to edit the live version rather than guessing its shape. For a CRD instance, use get_crd_manifest instead.", "manifest")

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_crd_kinds",
		Description: "List every CustomResourceDefinition (CRD) this cluster has registered — group, version, plural resource name, kind, and whether it's namespaced. Call this first to find a CRD's exact group/version/resource before list_crd_resources, get_crd_detail, or get_crd_manifest — list_resources/get_resource_detail/get_manifest only know built-in Kubernetes kinds, never CRDs.",
		Annotations: readOnly(),
		InputSchema: contextInputSchema[ctxArgs](contexts),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args ctxArgs) (*mcp.CallToolResult, any, error) {
		return toolResult(s.callREST(ctx, "GET", contextPath(args.Context, "crdkinds"), nil))
	})
	registerListCRDResourcesTool(srv, s, contexts)
	registerCRDGetTool(srv, s, contexts, "get_crd_detail", "Get structured detail for a single CRD instance by group/version/resource/namespace/name (from list_crd_kinds).", "detail")
	registerCRDGetTool(srv, s, contexts, "get_crd_manifest", "Get a CRD instance's current manifest as YAML, by group/version/resource/namespace/name (from list_crd_kinds).", "manifest")

	registerGetLogsTool(srv, s, contexts)
	registerSimpleGetTool(srv, s, contexts, "get_overview", "Get cluster-wide counts: node/pod/namespace totals, ready nodes, and pods by phase (running/pending/failed).", "overview")
	registerGetIssuesTool(srv, s, contexts)
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
