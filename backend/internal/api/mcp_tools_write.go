package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// writeBlocked is the error every mutating tool returns when the user
// hasn't granted write access — mirrors demoModeBlocked's role for the REST
// API, just surfaced as a tool error instead of an HTTP 403.
func (s *Server) writeBlocked() error {
	if s.mcpFlags.AllowWrite() {
		return nil
	}
	return fmt.Errorf("write operations are disabled — enable 'Allow write' in netsk8-navigator's MCP panel to permit this")
}

// registerWriteTools wires up every mutating MCP tool. Each handler's first
// action is the writeBlocked() check, so "does this tool mutate the
// cluster" and "does it check AllowWrite first" stay visually inseparable.
func registerWriteTools(srv *mcp.Server, s *Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "apply_manifest",
		Description: "Apply a full YAML manifest to update an existing resource. Fetch the current manifest with get_manifest first and edit it, rather than guessing its shape. Requires write access to be enabled.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args applyManifestArgs) (*mcp.CallToolResult, any, error) {
		if err := s.writeBlocked(); err != nil {
			return nil, nil, err
		}
		body, err := json.Marshal(map[string]string{"yaml": args.YAML})
		if err != nil {
			return nil, nil, err
		}
		path := fmt.Sprintf("/api/contexts/%s/manifest/%s/%s/%s",
			url.PathEscape(args.Context), url.PathEscape(args.Kind), url.PathEscape(pathNamespace(args.Namespace)), url.PathEscape(args.Name))
		return toolResult(s.callREST(ctx, "PUT", path, body))
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "delete_resource",
		Description: "Delete a resource by kind/namespace/name. Irreversible for most kinds. Requires write access to be enabled.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args resourceKindArgs) (*mcp.CallToolResult, any, error) {
		if err := s.writeBlocked(); err != nil {
			return nil, nil, err
		}
		path := fmt.Sprintf("/api/contexts/%s/manifest/%s/%s/%s",
			url.PathEscape(args.Context), url.PathEscape(args.Kind), url.PathEscape(pathNamespace(args.Namespace)), url.PathEscape(args.Name))
		return toolResult(s.callREST(ctx, "DELETE", path, nil))
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "scale_resource",
		Description: "Scale a deployment, statefulset, or replicaset to a target replica count. Requires write access to be enabled.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args struct {
		Context   string `json:"context" jsonschema:"kubeconfig context name"`
		Kind      string `json:"kind" jsonschema:"deployment, statefulset, or replicaset"`
		Namespace string `json:"namespace" jsonschema:"resource namespace"`
		Name      string `json:"name" jsonschema:"resource name"`
		Replicas  int32  `json:"replicas" jsonschema:"desired replica count, >= 0"`
	},
	) (*mcp.CallToolResult, any, error) {
		if err := s.writeBlocked(); err != nil {
			return nil, nil, err
		}
		body, err := json.Marshal(map[string]int32{"replicas": args.Replicas})
		if err != nil {
			return nil, nil, err
		}
		path := fmt.Sprintf("/api/contexts/%s/scale/%s/%s/%s",
			url.PathEscape(args.Context), url.PathEscape(args.Kind), url.PathEscape(args.Namespace), url.PathEscape(args.Name))
		return toolResult(s.callREST(ctx, "PUT", path, body))
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "restart_rollout",
		Description: "Trigger a rolling restart of a deployment, statefulset, or daemonset (same mechanism as `kubectl rollout restart`). Requires write access to be enabled.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args resourceKindArgs) (*mcp.CallToolResult, any, error) {
		if err := s.writeBlocked(); err != nil {
			return nil, nil, err
		}
		path := fmt.Sprintf("/api/contexts/%s/rollout-restart/%s/%s/%s",
			url.PathEscape(args.Context), url.PathEscape(args.Kind), url.PathEscape(args.Namespace), url.PathEscape(args.Name))
		return toolResult(s.callREST(ctx, "POST", path, nil))
	})
}

type applyManifestArgs struct {
	Context   string `json:"context" jsonschema:"kubeconfig context name"`
	Kind      string `json:"kind" jsonschema:"manifest kind slug, e.g. deployment, service, configmap"`
	Namespace string `json:"namespace,omitempty" jsonschema:"resource namespace; omit for cluster-scoped kinds"`
	Name      string `json:"name" jsonschema:"resource name"`
	YAML      string `json:"yaml" jsonschema:"the full replacement manifest, as YAML"`
}
