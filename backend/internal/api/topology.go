package api

import (
	"errors"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/robertobado/netsk8-navigator/backend/internal/kube"
)

var errNamespaceRequired = errors.New("namespace is required for the topology view")

type topoNode struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

type topoEdge struct {
	Source string `json:"source"`
	Target string `json:"target"`
}

type topoGraph struct {
	Nodes []topoNode `json:"nodes"`
	Edges []topoEdge `json:"edges"`
}

// handleTopology builds a relationship graph for a namespace: Deployments and
// Services linked to the Pods they select (via label-selector matching, which
// avoids walking the Deployment->ReplicaSet->Pod ownerRef chain).
// GET /api/contexts/{ctx}/topology?namespace=ns
func (s *Server) handleTopology(w http.ResponseWriter, r *http.Request) {
	client, err := s.mgr.ClientFor(r.PathValue("ctx"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	namespace := r.URL.Query().Get("namespace")
	if namespace == "" {
		writeError(w, http.StatusBadRequest, errNamespaceRequired)
		return
	}
	ctx, cancel := reqCtx(r)
	defer cancel()

	pods, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	deployments, err := client.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	services, err := client.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}

	graph := topoGraph{Nodes: []topoNode{}, Edges: []topoEdge{}}

	for i := range pods.Items {
		p := &pods.Items[i]
		graph.Nodes = append(graph.Nodes, topoNode{
			ID: "pod/" + p.Name, Kind: "pod", Name: p.Name, Status: kube.PodPhase(p),
		})
	}
	for i := range deployments.Items {
		d := &deployments.Items[i]
		id := "deployment/" + d.Name
		status := "Progressing"
		if d.Spec.Replicas != nil && d.Status.ReadyReplicas == *d.Spec.Replicas {
			status = "Available"
		}
		graph.Nodes = append(graph.Nodes, topoNode{ID: id, Kind: "deployment", Name: d.Name, Status: status})
		if d.Spec.Selector != nil {
			linkToPods(&graph, id, d.Spec.Selector.MatchLabels, pods)
		}
	}
	for i := range services.Items {
		svc := &services.Items[i]
		id := "service/" + svc.Name
		graph.Nodes = append(graph.Nodes, topoNode{ID: id, Kind: "service", Name: svc.Name, Status: string(svc.Spec.Type)})
		linkToPods(&graph, id, svc.Spec.Selector, pods)
	}

	writeJSON(w, http.StatusOK, graph)
}

// linkToPods adds an edge from source to every pod whose labels are a superset
// of the given selector (an empty selector matches nothing).
func linkToPods(g *topoGraph, source string, selector map[string]string, pods *corev1.PodList) {
	if len(selector) == 0 {
		return
	}
	for i := range pods.Items {
		p := &pods.Items[i]
		if labelsMatch(selector, p.Labels) {
			g.Edges = append(g.Edges, topoEdge{Source: source, Target: "pod/" + p.Name})
		}
	}
}

func labelsMatch(selector, labels map[string]string) bool {
	for k, v := range selector {
		if labels[k] != v {
			return false
		}
	}
	return true
}
