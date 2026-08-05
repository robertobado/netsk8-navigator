package api

import (
	"context"
	"fmt"
	"net/url"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerReadTools wires up every read-only MCP tool. Each one replays a
// request against the matching REST route via callREST — see mcp.go.

type ctxArgs struct {
	Context string `json:"context" jsonschema:"kubeconfig context name, from list_contexts"`
}

type namespaceScopedListArgs struct {
	Context   string `json:"context" jsonschema:"kubeconfig context name, from list_contexts"`
	Namespace string `json:"namespace,omitempty" jsonschema:"optional namespace filter; omit for all namespaces"`
}

type resourceKindArgs struct {
	Context   string `json:"context" jsonschema:"kubeconfig context name"`
	Kind      string `json:"kind" jsonschema:"manifest kind slug, e.g. pod, deployment, service, configmap, node, namespace, secret"`
	Namespace string `json:"namespace,omitempty" jsonschema:"resource namespace; omit for cluster-scoped kinds like node or namespace"`
	Name      string `json:"name" jsonschema:"resource name"`
}

func registerReadTools(srv *mcp.Server, s *Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_contexts",
		Description: "List the kubeconfig contexts (clusters) this app can connect to.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, any, error) {
		return toolResult(s.callREST(ctx, "GET", "/api/contexts", nil))
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_namespaces",
		Description: "List namespaces in a cluster context.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args ctxArgs) (*mcp.CallToolResult, any, error) {
		path := "/api/contexts/" + url.PathEscape(args.Context) + "/namespaces"
		return toolResult(s.callREST(ctx, "GET", path, nil))
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_nodes",
		Description: "List nodes in a cluster context, with readiness, roles, version, and capacity.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args ctxArgs) (*mcp.CallToolResult, any, error) {
		path := "/api/contexts/" + url.PathEscape(args.Context) + "/nodes"
		return toolResult(s.callREST(ctx, "GET", path, nil))
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_pods",
		Description: "List pods in a cluster context, optionally filtered by namespace.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args namespaceScopedListArgs) (*mcp.CallToolResult, any, error) {
		path := "/api/contexts/" + url.PathEscape(args.Context) + "/pods"
		if args.Namespace != "" {
			path += "?namespace=" + url.QueryEscape(args.Namespace)
		}
		return toolResult(s.callREST(ctx, "GET", path, nil))
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_resources",
		Description: "List resources of a given kind (e.g. deployments, services, configmaps, jobs, secrets, ingresses) in a cluster context, optionally filtered by namespace.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args struct {
		Context   string `json:"context" jsonschema:"kubeconfig context name"`
		Resource  string `json:"resource" jsonschema:"plural resource name, e.g. deployments, services, configmaps, jobs, secrets, ingresses"`
		Namespace string `json:"namespace,omitempty" jsonschema:"optional namespace filter; omit for all namespaces"`
	},
	) (*mcp.CallToolResult, any, error) {
		path := "/api/contexts/" + url.PathEscape(args.Context) + "/resources/" + url.PathEscape(args.Resource)
		if args.Namespace != "" {
			path += "?namespace=" + url.QueryEscape(args.Namespace)
		}
		return toolResult(s.callREST(ctx, "GET", path, nil))
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_resource_detail",
		Description: "Get structured detail (status, conditions, images, related resources, etc.) for a single resource by kind/namespace/name.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args resourceKindArgs) (*mcp.CallToolResult, any, error) {
		path := fmt.Sprintf("/api/contexts/%s/detail/%s/%s/%s",
			url.PathEscape(args.Context), url.PathEscape(args.Kind), url.PathEscape(pathNamespace(args.Namespace)), url.PathEscape(args.Name))
		return toolResult(s.callREST(ctx, "GET", path, nil))
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_manifest",
		Description: "Get a resource's current manifest as YAML — read this before apply_manifest to edit the live version rather than guessing its shape.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args resourceKindArgs) (*mcp.CallToolResult, any, error) {
		path := fmt.Sprintf("/api/contexts/%s/manifest/%s/%s/%s",
			url.PathEscape(args.Context), url.PathEscape(args.Kind), url.PathEscape(pathNamespace(args.Namespace)), url.PathEscape(args.Name))
		return toolResult(s.callREST(ctx, "GET", path, nil))
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_logs",
		Description: "Get the most recent log lines from a pod container (bounded, non-streaming — not a live tail).",
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
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args ctxArgs) (*mcp.CallToolResult, any, error) {
		path := "/api/contexts/" + url.PathEscape(args.Context) + "/overview"
		return toolResult(s.callREST(ctx, "GET", path, nil))
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_issues",
		Description: "Get the same unhealthy-resource feed the app's overview page shows: pending pods, failed/crash-looping pods, and not-ready nodes, each with a reason and message. The best starting point for triaging what's wrong in a cluster.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args ctxArgs) (*mcp.CallToolResult, any, error) {
		path := "/api/contexts/" + url.PathEscape(args.Context) + "/issues"
		return toolResult(s.callREST(ctx, "GET", path, nil))
	})
}
