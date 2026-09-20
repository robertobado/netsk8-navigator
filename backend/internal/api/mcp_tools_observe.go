package api

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The two "what is this cluster doing right now" tools — kubectl events and
// kubectl top — that the browse tools alone couldn't answer: an agent
// investigating a starved pod had to fall back to a terminal for both.

type getEventsArgs struct {
	Context      string       `json:"context" jsonschema:"kubeconfig context name, from list_contexts"`
	Namespace    string       `json:"namespace,omitempty" jsonschema:"optional namespace; omit for events across all namespaces (required when name is set)"`
	Name         string       `json:"name,omitempty" jsonschema:"optional involved-object name (e.g. a pod or deployment) — only events about that object; needs namespace"`
	Kind         string       `json:"kind,omitempty" jsonschema:"optional involved-object kind, e.g. Pod or Deployment, to disambiguate same-named objects (with name)"`
	WarningsOnly bool         `json:"warningsOnly,omitempty" jsonschema:"when true, only Warning events — usually what you want when something is wrong"`
	Limit        int          `json:"limit,omitempty" jsonschema:"cap on the number of events returned, most recent first; defaults to 50"`
	Filter       outputFilter `json:"filter,omitempty" jsonschema:"optional server-side filters that shrink the response before it's returned, to save context-window tokens: jq (gojq program), grep / grepV (RE2 line filters), head, tail, maxBytes"`
}

type getUsageArgs struct {
	Context   string       `json:"context" jsonschema:"kubeconfig context name, from list_contexts"`
	Scope     string       `json:"scope" jsonschema:"what to measure: pods (like kubectl top pods) or nodes (like kubectl top nodes)"`
	Namespace string       `json:"namespace,omitempty" jsonschema:"for scope=pods: optional namespace; omit for all namespaces"`
	Sort      string       `json:"sort,omitempty" jsonschema:"cpu (default) or memory — highest first"`
	Limit     int          `json:"limit,omitempty" jsonschema:"for scope=pods: cap on rows returned after sorting; defaults to 20"`
	Filter    outputFilter `json:"filter,omitempty" jsonschema:"optional server-side filters that shrink the response before it's returned, to save context-window tokens: jq (gojq program), grep / grepV (RE2 line filters), head, tail, maxBytes"`
}

func registerObserveTools(srv *mcp.Server, s *Server, contexts []string) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "get_events",
		Description: "Kubernetes events (like `kubectl events`), most recent first — the first place to look for why a pod is pending, restarting, evicted or failing to pull. " +
			"Scope it to one object with namespace+name (+kind), and use warningsOnly and limit to keep it small.",
		Annotations: readOnly(),
		InputSchema: contextInputSchema[getEventsArgs](contexts),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args getEventsArgs) (*mcp.CallToolResult, any, error) {
		if err := s.readBlockedFor(args.Context); err != nil {
			return nil, nil, err
		}
		path, err := eventsPath(args)
		if err != nil {
			return nil, nil, err
		}
		status, body := s.callREST(ctx, "GET", path, nil)
		return finishRead(status, body, args.Filter, false)
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name: "get_usage",
		Description: "Live CPU and memory usage (like `kubectl top pods` / `kubectl top nodes`) from the cluster's metrics-server, highest first, with each pod's request and limit alongside so starvation or a too-tight limit is visible at a glance. " +
			"Reports available=false when the cluster has no metrics-server.",
		Annotations: readOnly(),
		InputSchema: contextInputSchema[getUsageArgs](contexts),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args getUsageArgs) (*mcp.CallToolResult, any, error) {
		if err := s.readBlockedFor(args.Context); err != nil {
			return nil, nil, err
		}
		return s.usageResult(ctx, args)
	})
}

// eventsPath builds the REST route for get_events: the per-object route when a
// name is given, the list route otherwise, with the type/limit switches
// filterEvents (events.go) reads back out.
func eventsPath(args getEventsArgs) (string, error) {
	q := url.Values{}
	if args.WarningsOnly {
		q.Set("type", "Warning")
	}
	limit := args.Limit
	if limit <= 0 {
		limit = 50
	}
	q.Set("limit", strconv.Itoa(limit))

	var path string
	if args.Name != "" {
		if args.Namespace == "" {
			return "", fmt.Errorf("namespace is required when name is set")
		}
		if args.Kind != "" {
			q.Set("kind", args.Kind)
		}
		path = contextPath(args.Context, "events/"+url.PathEscape(args.Namespace)+"/"+url.PathEscape(args.Name))
	} else {
		if args.Namespace != "" {
			q.Set("namespace", args.Namespace)
		}
		path = contextPath(args.Context, "events")
	}
	return path + "?" + q.Encode(), nil
}

// gaugeIn mirrors the REST gauge (usage.go) — used/request/limit/total in
// cores or bytes.
type gaugeIn struct {
	Used    float64 `json:"used"`
	Request float64 `json:"request"`
	Limit   float64 `json:"limit"`
	Total   float64 `json:"total"`
}

type podUsageOut struct {
	Namespace            string  `json:"namespace"`
	Name                 string  `json:"name"`
	CPUMillicores        int64   `json:"cpuMillicores"`
	CPURequestMillicores int64   `json:"cpuRequestMillicores"`
	CPULimitMillicores   int64   `json:"cpuLimitMillicores"`
	MemoryMiB            float64 `json:"memoryMiB"`
	MemoryRequestMiB     float64 `json:"memoryRequestMiB"`
	MemoryLimitMiB       float64 `json:"memoryLimitMiB"`
}

type nodeUsageOut struct {
	Name                     string  `json:"name"`
	CPUMillicores            int64   `json:"cpuMillicores"`
	CPUAllocatableMillicores int64   `json:"cpuAllocatableMillicores"`
	CPUPercent               float64 `json:"cpuPercent"`
	MemoryMiB                float64 `json:"memoryMiB"`
	MemoryAllocatableMiB     float64 `json:"memoryAllocatableMiB"`
	MemoryPercent            float64 `json:"memoryPercent"`
}

func millicores(cores float64) int64 { return int64(math.Round(cores * 1000)) }

func mib(bytes float64) float64 { return math.Round(bytes/(1024*1024)*10) / 10 }

func percent(used, total float64) float64 {
	if total <= 0 {
		return 0
	}
	return math.Round(used/total*1000) / 10
}

// usageResult replays the pods/nodes usage REST route and reshapes its
// UI-oriented payload (a map keyed "ns/pod", raw cores and bytes) into what an
// agent wants: a list sorted by consumption, in millicores and MiB.
func (s *Server) usageResult(ctx context.Context, args getUsageArgs) (*mcp.CallToolResult, any, error) {
	scope := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(args.Scope)), "s")
	sortBy := strings.ToLower(strings.TrimSpace(args.Sort))
	if sortBy == "" {
		sortBy = "cpu"
	}
	if sortBy != "cpu" && sortBy != "memory" {
		return nil, nil, fmt.Errorf("sort must be cpu or memory, got %q", args.Sort)
	}

	var path string
	switch scope {
	case "pod":
		path = contextPath(args.Context, "podusage")
		if args.Namespace != "" {
			path += "?namespace=" + url.QueryEscape(args.Namespace)
		}
	case "node":
		path = contextPath(args.Context, "nodeusage")
	default:
		return nil, nil, fmt.Errorf("scope must be pods or nodes, got %q", args.Scope)
	}

	status, body := s.callREST(ctx, "GET", path, nil)
	if status < 200 || status >= 300 {
		return toolResult(status, body)
	}
	var out any
	var err error
	if scope == "pod" {
		out, err = reshapePodUsage(body, sortBy, args.Limit)
	} else {
		out, err = reshapeNodeUsage(body, sortBy)
	}
	if err != nil {
		return nil, nil, err
	}
	enc, err := json.Marshal(out)
	if err != nil {
		return nil, nil, err
	}
	return finishRead(status, enc, args.Filter, false)
}

func unavailableUsage(scope string) map[string]any {
	return map[string]any{
		"available": false, "scope": scope,
		"note": "this cluster does not serve the Metrics API (metrics-server is not installed or not reachable)",
	}
}

func reshapePodUsage(body []byte, sortBy string, limit int) (any, error) {
	var in struct {
		Available bool `json:"available"`
		Items     map[string]struct {
			CPU    gaugeIn `json:"cpu"`
			Memory gaugeIn `json:"memory"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &in); err != nil {
		return nil, fmt.Errorf("decoding pod usage: %w", err)
	}
	if !in.Available {
		return unavailableUsage("pods"), nil
	}
	items := make([]podUsageOut, 0, len(in.Items))
	for key, e := range in.Items {
		ns, name, _ := strings.Cut(key, "/")
		items = append(items, podUsageOut{
			Namespace: ns, Name: name,
			CPUMillicores: millicores(e.CPU.Used), CPURequestMillicores: millicores(e.CPU.Request), CPULimitMillicores: millicores(e.CPU.Limit),
			MemoryMiB: mib(e.Memory.Used), MemoryRequestMiB: mib(e.Memory.Request), MemoryLimitMiB: mib(e.Memory.Limit),
		})
	}
	sort.Slice(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if sortBy == "memory" && a.MemoryMiB != b.MemoryMiB {
			return a.MemoryMiB > b.MemoryMiB
		}
		if sortBy == "cpu" && a.CPUMillicores != b.CPUMillicores {
			return a.CPUMillicores > b.CPUMillicores
		}
		if a.Namespace != b.Namespace {
			return a.Namespace < b.Namespace
		}
		return a.Name < b.Name
	})
	total := len(items)
	if limit <= 0 {
		limit = 20
	}
	if limit < total {
		items = items[:limit]
	}
	return map[string]any{"available": true, "scope": "pods", "sortedBy": sortBy, "total": total, "returned": len(items), "items": items}, nil
}

func reshapeNodeUsage(body []byte, sortBy string) (any, error) {
	var in struct {
		Available bool `json:"available"`
		Items     []struct {
			Name   string  `json:"name"`
			CPU    gaugeIn `json:"cpu"`
			Memory gaugeIn `json:"memory"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &in); err != nil {
		return nil, fmt.Errorf("decoding node usage: %w", err)
	}
	if !in.Available {
		return unavailableUsage("nodes"), nil
	}
	items := make([]nodeUsageOut, 0, len(in.Items))
	for _, n := range in.Items {
		items = append(items, nodeUsageOut{
			Name:          n.Name,
			CPUMillicores: millicores(n.CPU.Used), CPUAllocatableMillicores: millicores(n.CPU.Total), CPUPercent: percent(n.CPU.Used, n.CPU.Total),
			MemoryMiB: mib(n.Memory.Used), MemoryAllocatableMiB: mib(n.Memory.Total), MemoryPercent: percent(n.Memory.Used, n.Memory.Total),
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if sortBy == "memory" {
			return items[i].MemoryPercent > items[j].MemoryPercent
		}
		return items[i].CPUPercent > items[j].CPUPercent
	})
	return map[string]any{"available": true, "scope": "nodes", "sortedBy": sortBy, "total": len(items), "returned": len(items), "items": items}, nil
}
