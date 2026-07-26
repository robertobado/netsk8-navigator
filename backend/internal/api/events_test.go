package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestToEventView(t *testing.T) {
	t.Run("core v1 Event shape", func(t *testing.T) {
		last := metav1.NewTime(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
		e := &corev1.Event{
			Type: "Warning", Reason: "BackOff", Message: "crash looping", Count: 3,
			FirstTimestamp: metav1.NewTime(last.Add(-time.Hour)), LastTimestamp: last,
			Source:         corev1.EventSource{Component: "kubelet"},
			InvolvedObject: corev1.ObjectReference{Kind: "Pod", Namespace: "prod", Name: "web-1"},
		}
		v := toEventView(e)
		if v.Type != "Warning" || v.Reason != "BackOff" || v.Count != 3 || v.Source != "kubelet" {
			t.Errorf("got %+v", v)
		}
		if v.ObjectKind != "Pod" || v.ObjectNS != "prod" || v.ObjectName != "web-1" {
			t.Errorf("got %+v", v)
		}
	})

	t.Run("events.k8s.io/v1 shape falls back to EventTime/Series/ReportingController", func(t *testing.T) {
		eventTime := metav1.NewMicroTime(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
		e := &corev1.Event{
			EventTime:           eventTime,
			Series:              &corev1.EventSeries{Count: 7},
			ReportingController: "my-controller",
		}
		v := toEventView(e)
		if v.Count != 7 || v.Source != "my-controller" {
			t.Errorf("got %+v", v)
		}
		if v.First != v.Last {
			t.Errorf("first should default to last when FirstTimestamp is zero, got first=%q last=%q", v.First, v.Last)
		}
	})

	t.Run("no count signal at all defaults to 1", func(t *testing.T) {
		v := toEventView(&corev1.Event{})
		if v.Count != 1 {
			t.Errorf("count = %d, want 1", v.Count)
		}
	})
}

func TestHandleEvents(t *testing.T) {
	newer := metav1.NewTime(time.Now())
	older := metav1.NewTime(time.Now().Add(-time.Hour))
	s := newTestServer(t,
		&corev1.Event{
			ObjectMeta:     metav1.ObjectMeta{Name: "e1", Namespace: "prod"},
			InvolvedObject: corev1.ObjectReference{Name: "web-1", Kind: "Pod"},
			Reason:         "Older", LastTimestamp: older,
		},
		&corev1.Event{
			ObjectMeta:     metav1.ObjectMeta{Name: "e2", Namespace: "prod"},
			InvolvedObject: corev1.ObjectReference{Name: "web-1", Kind: "Pod"},
			Reason:         "Newer", LastTimestamp: newer,
		},
		&corev1.Event{
			ObjectMeta:     metav1.ObjectMeta{Name: "e3", Namespace: "prod"},
			InvolvedObject: corev1.ObjectReference{Name: "other-pod", Kind: "Pod"},
			Reason:         "Unrelated", LastTimestamp: newer,
		},
	)
	rec := doRequest(t, s, "GET", "/api/contexts/test/events/prod/web-1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var out []eventView
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("got %d events, want 2 (scoped to web-1)", len(out))
	}
	if out[0].Reason != "Newer" {
		t.Errorf("expected most-recent-first ordering, got %+v", out)
	}
}

func TestHandleAllEvents(t *testing.T) {
	s := newTestServer(t,
		&corev1.Event{ObjectMeta: metav1.ObjectMeta{Name: "e1", Namespace: "prod"}, Reason: "InProd"},
		&corev1.Event{ObjectMeta: metav1.ObjectMeta{Name: "e2", Namespace: "staging"}, Reason: "InStaging"},
	)

	t.Run("all namespaces", func(t *testing.T) {
		rec := doRequest(t, s, "GET", "/api/contexts/test/events", "")
		var out []eventView
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		if len(out) != 2 {
			t.Errorf("got %d events, want 2", len(out))
		}
	})

	t.Run("scoped to one namespace", func(t *testing.T) {
		rec := doRequest(t, s, "GET", "/api/contexts/test/events?namespace=prod", "")
		var out []eventView
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		if len(out) != 1 || out[0].Reason != "InProd" {
			t.Errorf("got %+v", out)
		}
	})
}
