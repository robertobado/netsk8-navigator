package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// writeBlockedFor is the error every mutating tool returns when the user
// hasn't granted write access for contextName — either globally, or because
// this specific context is pinned read-only regardless of the global
// toggle (e.g. production clusters). Mirrors demoModeBlocked's role for the
// REST API, just surfaced as a tool error instead of an HTTP 403.
func (s *Server) writeBlockedFor(contextName string) error {
	if s.mcpFlags.WriteAllowedFor(contextName) {
		return nil
	}
	if s.mcpFlags.AllowWrite() {
		return fmt.Errorf("write operations are disabled for context %q (pinned read-only in netsk8-navigator's MCP panel)", contextName)
	}
	if s.mcpFlags.Stdio() {
		return fmt.Errorf("write operations are disabled — enable 'Allow write' in netsk8-navigator's MCP panel (takes effect immediately, no restart needed); or reinstall with: netsk8-navigator mcp install --allow-write. "+
			"This stdio server (version %s) reads that setting from %s — if you just enabled it and this still fails, compare that against the app's own About dialog ('Config file', from GET /api/health's configPath); a different path means this MCP client process can't see the app's config at all, no matter what the toggle says",
			versionOrDev(s.Version), s.mcpFlags.GatePath())
	}
	return fmt.Errorf("write operations are disabled — enable 'Allow write' in netsk8-navigator's MCP panel to permit this")
}

func annotations(destructive, idempotent bool) *mcp.ToolAnnotations {
	d := destructive
	return &mcp.ToolAnnotations{DestructiveHint: &d, IdempotentHint: idempotent}
}

// registerWriteTools wires up every mutating MCP tool. Each handler's first
// action is the writeBlockedFor(args.Context) check, so "does this tool
// mutate the cluster" and "does it check write access first" stay visually
// inseparable. contexts feeds contextInputSchema, same as registerReadTools.
func registerWriteTools(srv *mcp.Server, s *Server, contexts []string) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "apply_manifest",
		Description: "Apply a full YAML manifest to update an existing resource. Fetch the current manifest with get_manifest first and edit it, rather than guessing its shape. Requires write access to be enabled.",
		Annotations: annotations(true, true),
		InputSchema: contextInputSchema[applyManifestArgs](contexts),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args applyManifestArgs) (*mcp.CallToolResult, any, error) {
		if err := s.writeBlockedFor(args.Context); err != nil {
			return nil, nil, err
		}
		body, err := json.Marshal(map[string]string{"yaml": args.YAML})
		if err != nil {
			return nil, nil, err
		}
		path := fmt.Sprintf("/api/contexts/%s/manifest/%s/%s/%s",
			url.PathEscape(args.Context), url.PathEscape(normalizeKindSlug(args.Kind)), url.PathEscape(pathNamespace(args.Namespace)), url.PathEscape(args.Name))
		return toolResult(s.callREST(ctx, "PUT", withDryRunQuery(path, args.DryRun), body))
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name: "delete_resource",
		Description: "Delete a resource by kind/namespace/name. Irreversible for most kinds. Supports the same switches as `kubectl delete`: " +
			"cascade (background/foreground/orphan), gracePeriodSeconds, force, dryRun, and ignoreNotFound. Requires write access to be enabled.",
		Annotations: annotations(true, false),
		InputSchema: contextInputSchema[deleteResourceArgs](contexts),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args deleteResourceArgs) (*mcp.CallToolResult, any, error) {
		if err := s.writeBlockedFor(args.Context); err != nil {
			return nil, nil, err
		}
		path := fmt.Sprintf("/api/contexts/%s/manifest/%s/%s/%s",
			url.PathEscape(args.Context), url.PathEscape(normalizeKindSlug(args.Kind)), url.PathEscape(pathNamespace(args.Namespace)), url.PathEscape(args.Name))
		return toolResult(s.callREST(ctx, "DELETE", withDeleteQuery(path, args.Cascade, args.GracePeriodSeconds, args.Force, args.DryRun, args.IgnoreNotFound), nil))
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name: "apply_crd_manifest",
		Description: "Apply a full YAML manifest to update an existing custom resource (an instance of a CRD, e.g. a Traefik IngressRoute or a cert-manager Certificate), addressed by group/version/resource from list_crd_kinds. " +
			"Fetch the current manifest with get_crd_manifest first and edit it. The YAML's metadata.name/namespace must match name/namespace. Requires write access to be enabled.",
		Annotations: annotations(true, true),
		InputSchema: contextInputSchema[applyCRDManifestArgs](contexts),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args applyCRDManifestArgs) (*mcp.CallToolResult, any, error) {
		if err := s.writeBlockedFor(args.Context); err != nil {
			return nil, nil, err
		}
		body, err := json.Marshal(map[string]string{"yaml": args.YAML})
		if err != nil {
			return nil, nil, err
		}
		path := crdInstancePath(args.Context, args.Group, args.Version, args.Resource, args.Namespace, args.Name)
		return toolResult(s.callREST(ctx, "PUT", withDryRunQuery(path, args.DryRun), body))
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name: "delete_crd_resource",
		Description: "Delete a custom resource (an instance of a CRD) addressed by group/version/resource from list_crd_kinds. Irreversible. " +
			"Supports the same switches as delete_resource: cascade (background/foreground/orphan), gracePeriodSeconds, force, dryRun, and ignoreNotFound. " +
			"Deleting a CustomResourceDefinition itself is not offered. Requires write access to be enabled.",
		Annotations: annotations(true, false),
		InputSchema: contextInputSchema[deleteCRDResourceArgs](contexts),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args deleteCRDResourceArgs) (*mcp.CallToolResult, any, error) {
		if err := s.writeBlockedFor(args.Context); err != nil {
			return nil, nil, err
		}
		if err := checkNotCRDDefinition(args.Group, args.Resource); err != nil {
			return nil, nil, err
		}
		path := crdInstancePath(args.Context, args.Group, args.Version, args.Resource, args.Namespace, args.Name)
		return toolResult(s.callREST(ctx, "DELETE", withDeleteQuery(path, args.Cascade, args.GracePeriodSeconds, args.Force, args.DryRun, args.IgnoreNotFound), nil))
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "scale_resource",
		Description: "Scale a deployment, statefulset, or replicaset to a target replica count. Requires write access to be enabled.",
		Annotations: annotations(false, true),
		InputSchema: contextInputSchema[scaleResourceArgs](contexts),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args scaleResourceArgs) (*mcp.CallToolResult, any, error) {
		if err := s.writeBlockedFor(args.Context); err != nil {
			return nil, nil, err
		}
		body, err := json.Marshal(map[string]int32{"replicas": args.Replicas})
		if err != nil {
			return nil, nil, err
		}
		path := fmt.Sprintf("/api/contexts/%s/scale/%s/%s/%s",
			url.PathEscape(args.Context), url.PathEscape(normalizeKindSlug(args.Kind)), url.PathEscape(args.Namespace), url.PathEscape(args.Name))
		return toolResult(s.callREST(ctx, "PUT", withDryRunQuery(path, args.DryRun), body))
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "restart_rollout",
		Description: "Trigger a rolling restart of a deployment, statefulset, or daemonset (same mechanism as `kubectl rollout restart`). Requires write access to be enabled.",
		Annotations: annotations(false, false),
		InputSchema: contextInputSchema[restartRolloutArgs](contexts),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args restartRolloutArgs) (*mcp.CallToolResult, any, error) {
		if err := s.writeBlockedFor(args.Context); err != nil {
			return nil, nil, err
		}
		path := fmt.Sprintf("/api/contexts/%s/rollout-restart/%s/%s/%s",
			url.PathEscape(args.Context), url.PathEscape(normalizeKindSlug(args.Kind)), url.PathEscape(args.Namespace), url.PathEscape(args.Name))
		return toolResult(s.callREST(ctx, "POST", withDryRunQuery(path, args.DryRun), nil))
	})
}

// withDryRunQuery appends ?dryRun=true — the single knob apply_manifest,
// scale_resource, and restart_rollout all share — when requested.
func withDryRunQuery(path string, dryRun bool) string {
	if !dryRun {
		return path
	}
	return path + "?dryRun=true"
}

// withDeleteQuery appends the kubectl-delete-style switches shared by
// delete_resource and delete_crd_resource as a query string;
// deleteQueryOptions (actions.go) parses it back out server-side, including
// validating cascade.
func withDeleteQuery(path, cascade string, gracePeriodSeconds *int64, force, dryRun, ignoreNotFound bool) string {
	q := url.Values{}
	if cascade != "" {
		q.Set("cascade", cascade)
	}
	if gracePeriodSeconds != nil {
		q.Set("gracePeriodSeconds", fmt.Sprintf("%d", *gracePeriodSeconds))
	}
	if force {
		q.Set("force", "true")
	}
	if dryRun {
		q.Set("dryRun", "true")
	}
	if ignoreNotFound {
		q.Set("ignoreNotFound", "true")
	}
	if len(q) == 0 {
		return path
	}
	return path + "?" + q.Encode()
}

// crdInstancePath builds the REST route the CRD write tools replay against,
// addressing an instance by GVR exactly like the CRD read tools do.
func crdInstancePath(context, group, version, resource, namespace, name string) string {
	return fmt.Sprintf("/api/contexts/%s/crd/%s/%s/%s/%s/%s",
		url.PathEscape(context), url.PathEscape(group), url.PathEscape(version), url.PathEscape(resource),
		url.PathEscape(pathNamespace(namespace)), url.PathEscape(name))
}

// checkNotCRDDefinition refuses to route a delete for a CustomResourceDefinition
// itself through the CRD-instance tool: deleting the definition cascades to
// every instance of it cluster-wide, a blast radius nothing else in this
// tool set has. Instances are what these tools are for.
func checkNotCRDDefinition(group, resource string) error {
	if group == "apiextensions.k8s.io" && resource == "customresourcedefinitions" {
		return fmt.Errorf("delete_crd_resource deletes CRD instances; deleting a CustomResourceDefinition itself would also delete every instance of it cluster-wide, so it is not offered here — use kubectl for that")
	}
	return nil
}

type applyManifestArgs struct {
	Context   string `json:"context" jsonschema:"kubeconfig context name"`
	Kind      string `json:"kind" jsonschema:"manifest kind slug, e.g. deployment, service, configmap"`
	Namespace string `json:"namespace,omitempty" jsonschema:"resource namespace; omit for cluster-scoped kinds"`
	Name      string `json:"name" jsonschema:"resource name"`
	YAML      string `json:"yaml" jsonschema:"the full replacement manifest, as YAML"`
	DryRun    bool   `json:"dryRun,omitempty" jsonschema:"validate and run admission/defaulting server-side without persisting, like kubectl apply --dry-run=server"`
}

type deleteResourceArgs struct {
	Context            string `json:"context" jsonschema:"kubeconfig context name"`
	Kind               string `json:"kind" jsonschema:"manifest kind slug, e.g. pod, deployment, service, configmap, node, namespace, secret"`
	Namespace          string `json:"namespace,omitempty" jsonschema:"resource namespace; omit for cluster-scoped kinds like node or namespace"`
	Name               string `json:"name" jsonschema:"resource name"`
	Cascade            string `json:"cascade,omitempty" jsonschema:"deletion propagation, like kubectl delete --cascade: background (default) deletes dependents async, foreground waits for dependents to be deleted first, orphan deletes only this object and leaves dependents behind (e.g. a Deployment's ReplicaSets/Pods keep running)"`
	GracePeriodSeconds *int64 `json:"gracePeriodSeconds,omitempty" jsonschema:"seconds to wait for graceful termination, like kubectl delete --grace-period; omit to use the resource's own terminationGracePeriodSeconds"`
	Force              bool   `json:"force,omitempty" jsonschema:"skip graceful termination and delete immediately, like kubectl delete --force --grace-period=0; use with care"`
	IgnoreNotFound     bool   `json:"ignoreNotFound,omitempty" jsonschema:"treat 'already gone' as success instead of an error, like kubectl delete --ignore-not-found; useful for idempotent cleanup"`
	DryRun             bool   `json:"dryRun,omitempty" jsonschema:"validate the delete server-side without actually removing anything, like kubectl delete --dry-run=server"`
}

type applyCRDManifestArgs struct {
	Context   string `json:"context" jsonschema:"kubeconfig context name"`
	Group     string `json:"group" jsonschema:"CRD API group, e.g. traefik.io — from list_crd_kinds"`
	Version   string `json:"version" jsonschema:"CRD API version, e.g. v1alpha1 — from list_crd_kinds"`
	Resource  string `json:"resource" jsonschema:"CRD plural resource name, e.g. ingressroutes — from list_crd_kinds"`
	Namespace string `json:"namespace,omitempty" jsonschema:"resource namespace; omit for a cluster-scoped kind"`
	Name      string `json:"name" jsonschema:"resource name"`
	YAML      string `json:"yaml" jsonschema:"the full replacement manifest, as YAML"`
	DryRun    bool   `json:"dryRun,omitempty" jsonschema:"validate and run admission/defaulting server-side without persisting, like kubectl apply --dry-run=server"`
}

type deleteCRDResourceArgs struct {
	Context            string `json:"context" jsonschema:"kubeconfig context name"`
	Group              string `json:"group" jsonschema:"CRD API group, e.g. traefik.io — from list_crd_kinds"`
	Version            string `json:"version" jsonschema:"CRD API version, e.g. v1alpha1 — from list_crd_kinds"`
	Resource           string `json:"resource" jsonschema:"CRD plural resource name, e.g. ingressroutes — from list_crd_kinds"`
	Namespace          string `json:"namespace,omitempty" jsonschema:"resource namespace; omit for a cluster-scoped kind"`
	Name               string `json:"name" jsonschema:"resource name"`
	Cascade            string `json:"cascade,omitempty" jsonschema:"deletion propagation, like kubectl delete --cascade: background (default), foreground (wait for dependents first), or orphan (leave dependents behind)"`
	GracePeriodSeconds *int64 `json:"gracePeriodSeconds,omitempty" jsonschema:"seconds to wait for graceful termination, like kubectl delete --grace-period"`
	Force              bool   `json:"force,omitempty" jsonschema:"delete immediately, like kubectl delete --force --grace-period=0; use with care"`
	IgnoreNotFound     bool   `json:"ignoreNotFound,omitempty" jsonschema:"treat 'already gone' as success instead of an error, like kubectl delete --ignore-not-found"`
	DryRun             bool   `json:"dryRun,omitempty" jsonschema:"validate the delete server-side without actually removing anything, like kubectl delete --dry-run=server"`
}

type restartRolloutArgs struct {
	Context   string `json:"context" jsonschema:"kubeconfig context name"`
	Kind      string `json:"kind" jsonschema:"deployment, statefulset, or daemonset"`
	Namespace string `json:"namespace,omitempty" jsonschema:"resource namespace; omit for cluster-scoped kinds"`
	Name      string `json:"name" jsonschema:"resource name"`
	DryRun    bool   `json:"dryRun,omitempty" jsonschema:"validate server-side without actually bumping the restart annotation, like kubectl rollout restart --dry-run=server"`
}

type scaleResourceArgs struct {
	Context   string `json:"context" jsonschema:"kubeconfig context name"`
	Kind      string `json:"kind" jsonschema:"deployment, statefulset, or replicaset"`
	Namespace string `json:"namespace" jsonschema:"resource namespace"`
	Name      string `json:"name" jsonschema:"resource name"`
	Replicas  int32  `json:"replicas" jsonschema:"desired replica count, >= 0"`
	DryRun    bool   `json:"dryRun,omitempty" jsonschema:"validate server-side without actually scaling, like kubectl scale --dry-run=server"`
}
