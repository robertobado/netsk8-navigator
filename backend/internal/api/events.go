package api

import (
	"net/http"
	"sort"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
)

// eventView is the UI projection of a Kubernetes Event. The Object* fields
// identify the involved resource; they're populated only by the cluster-wide
// list (handleAllEvents), where events aren't already scoped to one object.
type eventView struct {
	Type       string `json:"type"` // Normal | Warning
	Reason     string `json:"reason"`
	Message    string `json:"message"`
	Count      int32  `json:"count"`
	First      string `json:"first"` // RFC3339
	Last       string `json:"last"`  // RFC3339
	Source     string `json:"source"`
	ObjectKind string `json:"objectKind,omitempty"`
	ObjectNS   string `json:"objectNamespace,omitempty"`
	ObjectName string `json:"objectName,omitempty"`
}

// toEventView projects a corev1.Event, tolerating the events.k8s.io/v1 shape
// where timing/count live under different fields.
func toEventView(e *corev1.Event) eventView {
	last := e.LastTimestamp.Time
	if last.IsZero() {
		last = e.EventTime.Time
	}
	first := e.FirstTimestamp.Time
	if first.IsZero() {
		first = last
	}
	count := e.Count
	if count == 0 {
		if e.Series != nil {
			count = e.Series.Count
		} else {
			count = 1
		}
	}
	source := e.Source.Component
	if source == "" {
		source = e.ReportingController
	}
	return eventView{
		Type:       e.Type,
		Reason:     e.Reason,
		Message:    e.Message,
		Count:      count,
		First:      rfc3339(first),
		Last:       rfc3339(last),
		Source:     source,
		ObjectKind: e.InvolvedObject.Kind,
		ObjectNS:   e.InvolvedObject.Namespace,
		ObjectName: e.InvolvedObject.Name,
	}
}

// handleEvents: GET /api/contexts/{ctx}/events/{namespace}/{name}?kind=Deployment
// (also serves the /pods/{ns}/{name}/events route). Events involving a resource,
// most recent first. Optional ?kind= disambiguates same-named objects.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	client, err := s.mgr.ClientFor(r.PathValue("ctx"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := reqCtx(r)
	defer cancel()

	ns := r.PathValue("namespace")
	name := r.PathValue("name")
	set := fields.Set{"involvedObject.name": name}
	if kind := r.URL.Query().Get("kind"); kind != "" {
		set["involvedObject.kind"] = kind
	}
	sel := fields.SelectorFromSet(set).String()
	list, err := client.CoreV1().Events(ns).List(ctx, metav1.ListOptions{FieldSelector: sel})
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}

	out := make([]eventView, 0, len(list.Items))
	for i := range list.Items {
		out = append(out, toEventView(&list.Items[i]))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Last > out[j].Last })

	writeJSON(w, http.StatusOK, out)
}

// handleAllEvents: GET /api/contexts/{ctx}/events?namespace=
// Cluster-wide events (optionally scoped to a namespace), most recent first.
// This is the global "Events" view; each row links back to its involved object.
func (s *Server) handleAllEvents(w http.ResponseWriter, r *http.Request) {
	client, err := s.mgr.ClientFor(r.PathValue("ctx"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := reqCtx(r)
	defer cancel()

	list, err := client.CoreV1().Events(namespaceParam(r)).List(ctx, metav1.ListOptions{})
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	out := make([]eventView, 0, len(list.Items))
	for i := range list.Items {
		out = append(out, toEventView(&list.Items[i]))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Last > out[j].Last })

	writeJSON(w, http.StatusOK, out)
}
