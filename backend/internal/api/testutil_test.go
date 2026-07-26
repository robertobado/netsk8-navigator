package api

import (
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	dynamic "k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ktesting "k8s.io/client-go/testing"

	"github.com/robertobado/netsk8-navigator/backend/internal/config"
	"github.com/robertobado/netsk8-navigator/backend/internal/kube"
)

// testGVRs hand-maps the resource names exercised by these tests to the GVR/
// scope a real discovery-backed RESTMapper would resolve. Standing in for
// ResolveResource lets handler tests run without a live cluster.
var testGVRs = map[string]kube.Resource{
	"deployments":            {GVR: appsv1.SchemeGroupVersion.WithResource("deployments"), Namespaced: true},
	"pods":                   {GVR: corev1.SchemeGroupVersion.WithResource("pods"), Namespaced: true},
	"configmaps":             {GVR: corev1.SchemeGroupVersion.WithResource("configmaps"), Namespaced: true},
	"services":               {GVR: corev1.SchemeGroupVersion.WithResource("services"), Namespaced: true},
	"namespaces":             {GVR: corev1.SchemeGroupVersion.WithResource("namespaces"), Namespaced: false},
	"serviceaccounts":        {GVR: corev1.SchemeGroupVersion.WithResource("serviceaccounts"), Namespaced: true},
	"persistentvolumeclaims": {GVR: corev1.SchemeGroupVersion.WithResource("persistentvolumeclaims"), Namespaced: true},
	"secrets":                {GVR: corev1.SchemeGroupVersion.WithResource("secrets"), Namespaced: true},
	"nodes":                  {GVR: corev1.SchemeGroupVersion.WithResource("nodes"), Namespaced: false},
	"jobs":                   {GVR: batchv1.SchemeGroupVersion.WithResource("jobs"), Namespaced: true},
	"statefulsets":           {GVR: appsv1.SchemeGroupVersion.WithResource("statefulsets"), Namespaced: true},
	"daemonsets":             {GVR: appsv1.SchemeGroupVersion.WithResource("daemonsets"), Namespaced: true},
	"replicasets":            {GVR: appsv1.SchemeGroupVersion.WithResource("replicasets"), Namespaced: true},
}

// fakeManager is a test-only clusterManager backed by client-go's fake
// clientsets, so handlers can be exercised through real HTTP routing without a
// live cluster or kubeconfig.
type fakeManager struct {
	client  kubernetes.Interface
	dynamic dynamic.Interface
	gvrs    map[string]kube.Resource
}

func newFakeManager(objs ...runtime.Object) *fakeManager {
	client := kubernetesfake.NewSimpleClientset(objs...)
	// The fake clientset's default List reactor ignores FieldSelector entirely
	// (a known client-go testing gap), but handleNodeWorkloads relies on
	// "spec.nodeName=..." to scope pods to a node — filter for real here so
	// that handler's test actually exercises its filtering behavior.
	client.PrependReactor("list", "pods", func(action ktesting.Action) (bool, runtime.Object, error) {
		la, ok := action.(ktesting.ListAction)
		if !ok {
			return false, nil, nil
		}
		restr := la.GetListRestrictions()
		if restr.Fields == nil || restr.Fields.Empty() {
			return false, nil, nil // no field selector — defer to the default reactor
		}
		obj, err := client.Tracker().List(
			corev1.SchemeGroupVersion.WithResource("pods"),
			corev1.SchemeGroupVersion.WithKind("Pod"),
			la.GetNamespace(),
		)
		if err != nil {
			return true, nil, err
		}
		all, ok := obj.(*corev1.PodList)
		if !ok {
			return false, nil, nil
		}
		filtered := &corev1.PodList{}
		for _, p := range all.Items {
			set := fields.Set{"spec.nodeName": p.Spec.NodeName, "status.phase": string(p.Status.Phase)}
			if restr.Fields.Matches(set) {
				filtered.Items = append(filtered.Items, p)
			}
		}
		return true, filtered, nil
	})
	// Same gap, same fix, for Events — handleEvents filters by
	// "involvedObject.name"/"involvedObject.kind".
	client.PrependReactor("list", "events", func(action ktesting.Action) (bool, runtime.Object, error) {
		la, ok := action.(ktesting.ListAction)
		if !ok {
			return false, nil, nil
		}
		restr := la.GetListRestrictions()
		if restr.Fields == nil || restr.Fields.Empty() {
			return false, nil, nil
		}
		obj, err := client.Tracker().List(
			corev1.SchemeGroupVersion.WithResource("events"),
			corev1.SchemeGroupVersion.WithKind("Event"),
			la.GetNamespace(),
		)
		if err != nil {
			return true, nil, err
		}
		all, ok := obj.(*corev1.EventList)
		if !ok {
			return false, nil, nil
		}
		filtered := &corev1.EventList{}
		for _, e := range all.Items {
			set := fields.Set{"involvedObject.name": e.InvolvedObject.Name, "involvedObject.kind": e.InvolvedObject.Kind}
			if restr.Fields.Matches(set) {
				filtered.Items = append(filtered.Items, e)
			}
		}
		return true, filtered, nil
	})
	return &fakeManager{
		client:  client,
		dynamic: dynamicfake.NewSimpleDynamicClient(scheme.Scheme, objs...),
		gvrs:    testGVRs,
	}
}

func (f *fakeManager) Contexts() []kube.ContextInfo {
	return []kube.ContextInfo{{Name: "test", Cluster: "test", Current: true}}
}
func (f *fakeManager) ConfigPath() string                             { return "/fake/kubeconfig" }
func (f *fakeManager) ClientFor(string) (kubernetes.Interface, error) { return f.client, nil }
func (f *fakeManager) DynamicFor(string) (dynamic.Interface, error)   { return f.dynamic, nil }
func (f *fakeManager) ResolveResource(_ string, resource string) (kube.Resource, error) {
	r, ok := f.gvrs[resource]
	if !ok {
		return kube.Resource{}, fmt.Errorf("fakeManager: no GVR registered for %q", resource)
	}
	return r, nil
}
func (f *fakeManager) RESTConfigFor(string) (*rest.Config, error) {
	return nil, fmt.Errorf("fakeManager: RESTConfigFor not supported in tests")
}
func (f *fakeManager) PodWatcherFor(string) (*kube.PodWatcher, error) {
	return nil, fmt.Errorf("fakeManager: PodWatcherFor not supported in tests")
}

// newTestServer builds a Server wired to a fakeManager seeded with objs, and a
// real (but hermetic — never written to unless a preferences endpoint is hit)
// config.Store.
func newTestServer(t *testing.T, objs ...runtime.Object) *Server {
	t.Helper()
	// A disposable temp-file store, not config.NewStore()'s real OS config dir —
	// otherwise a test that exercises the PUT preferences handlers would write
	// to the developer's actual ~/Library/Application Support/netsk8/config.json.
	cfg := config.NewStoreAt(filepath.Join(t.TempDir(), "config.json"))
	return NewServer(newFakeManager(objs...), cfg, "")
}

// doRequest sends method+path through the real routing/middleware stack
// (Server.Routes()) and returns the recorder.
func doRequest(t *testing.T, s *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	s.Routes().ServeHTTP(rec, r)
	return rec
}
