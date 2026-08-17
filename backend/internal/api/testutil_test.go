package api

import (
	"context"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
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

// testGVKs stands in for a discovery-backed RESTMapper's Kind→GVR resolution
// (ResolveGVK), keyed by the Kind as it appears in a manifest's own "kind"
// field — used by the generic create-from-YAML endpoint.
var testGVKs = map[string]kube.Resource{
	"Deployment": testGVRs["deployments"],
	"ConfigMap":  testGVRs["configmaps"],
	"Namespace":  testGVRs["namespaces"],
	"Pod":        testGVRs["pods"],
}

// fakeManager is a test-only clusterManager backed by client-go's fake
// clientsets, so handlers can be exercised through real HTTP routing without a
// live cluster or kubeconfig.
type fakeManager struct {
	client   kubernetes.Interface
	dynamic  dynamic.Interface
	gvrs     map[string]kube.Resource
	crds     []apiextensionsv1.CustomResourceDefinition
	execInfo map[string][2]string // context -> [command, profile], for ExecInfoFor
}

// withExecInfo seeds what ExecInfoFor returns for a context — for tests of
// the MCP exec-credential-failure hint (see mcp_test.go).
func (f *fakeManager) withExecInfo(contextName, command, profile string) *fakeManager {
	if f.execInfo == nil {
		f.execInfo = map[string][2]string{}
	}
	f.execInfo[contextName] = [2]string{command, profile}
	return f
}

// withCRDs seeds the CRDs CRDsFor returns, for tests exercising the generic
// CRD-kind discovery endpoint without a live cluster.
func (f *fakeManager) withCRDs(crds ...apiextensionsv1.CustomResourceDefinition) *fakeManager {
	f.crds = crds
	return f
}

// podsFieldSelectorReactor stands in for the fake clientset's default List
// reactor, which ignores FieldSelector entirely (a known client-go testing
// gap) — handleNodeWorkloads relies on "spec.nodeName=..." to scope pods to a
// node, so this filters for real, letting that handler's test actually
// exercise its filtering behavior.
func podsFieldSelectorReactor(client *kubernetesfake.Clientset) ktesting.ReactionFunc {
	return func(action ktesting.Action) (bool, runtime.Object, error) {
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
	}
}

// eventsFieldSelectorReactor is the same fix as podsFieldSelectorReactor, for
// handleEvents' "involvedObject.name"/"involvedObject.kind" filter.
func eventsFieldSelectorReactor(client *kubernetesfake.Clientset) ktesting.ReactionFunc {
	return func(action ktesting.Action) (bool, runtime.Object, error) {
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
	}
}

func newFakeManager(objs ...runtime.Object) *fakeManager {
	client := kubernetesfake.NewSimpleClientset(objs...)
	// The fake clientset's default List reactor ignores FieldSelector entirely
	// (a known client-go testing gap), but handleNodeWorkloads relies on
	// "spec.nodeName=..." to scope pods to a node — filter for real here so
	// that handler's test actually exercises its filtering behavior.
	client.PrependReactor("list", "pods", podsFieldSelectorReactor(client))
	// Same gap, same fix, for Events — handleEvents filters by
	// "involvedObject.name"/"involvedObject.kind".
	client.PrependReactor("list", "events", eventsFieldSelectorReactor(client))
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
func (f *fakeManager) ResolveResource(_, resource string) (kube.Resource, error) {
	r, ok := f.gvrs[resource]
	if !ok {
		return kube.Resource{}, fmt.Errorf("fakeManager: no GVR registered for %q", resource)
	}
	return r, nil
}
func (f *fakeManager) ResolveGVK(_ string, gvk schema.GroupVersionKind) (kube.Resource, error) {
	r, ok := testGVKs[gvk.Kind]
	if !ok {
		return kube.Resource{}, fmt.Errorf("fakeManager: no GVR registered for kind %q", gvk.Kind)
	}
	return r, nil
}
func (f *fakeManager) CRDsFor(context.Context, string) ([]apiextensionsv1.CustomResourceDefinition, error) {
	return f.crds, nil
}
func (f *fakeManager) RESTConfigFor(string) (*rest.Config, error) {
	return nil, fmt.Errorf("fakeManager: RESTConfigFor not supported in tests")
}
func (f *fakeManager) RESTMapperFor(string) (apimeta.RESTMapper, error) {
	return nil, fmt.Errorf("fakeManager: RESTMapperFor not supported in tests")
}
func (f *fakeManager) PodWatcherFor(string) (*kube.PodWatcher, error) {
	return nil, fmt.Errorf("fakeManager: PodWatcherFor not supported in tests")
}
func (f *fakeManager) ExecInfoFor(contextName string) (command, profile string, ok bool) {
	info, found := f.execInfo[contextName]
	if !found {
		return "", "", false
	}
	return info[0], info[1], true
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

// newTestServerWithCRDs is newTestServer plus seeded CRDs, for tests of the
// generic CRD-kind discovery endpoint.
func newTestServerWithCRDs(t *testing.T, crds []apiextensionsv1.CustomResourceDefinition, objs ...runtime.Object) *Server {
	t.Helper()
	cfg := config.NewStoreAt(filepath.Join(t.TempDir(), "config.json"))
	return NewServer(newFakeManager(objs...).withCRDs(crds...), cfg, "")
}

// fakeDynamic extracts the underlying *dynamicfake.FakeDynamicClient from a
// server built by newTestServer, for tests that need to patch in a reactor —
// e.g. to simulate DryRun, which the fake tracker otherwise ignores.
func fakeDynamic(t *testing.T, s *Server) *dynamicfake.FakeDynamicClient {
	t.Helper()
	fm, ok := s.mgr.(*fakeManager)
	if !ok {
		t.Fatal("expected a *fakeManager")
	}
	dyn, ok := fm.dynamic.(*dynamicfake.FakeDynamicClient)
	if !ok {
		t.Fatal("expected a *dynamicfake.FakeDynamicClient")
	}
	return dyn
}

// fakeClient extracts the underlying *kubernetesfake.Clientset from a server
// built by newTestServer, for tests that need to patch in a reactor — e.g. a
// proxy reactor, since the fake tracker has no ProxyGet behavior by default
// (it returns a nil ResponseWrapper, which panics on .DoRaw()).
func fakeClient(t *testing.T, s *Server) *kubernetesfake.Clientset {
	t.Helper()
	fm, ok := s.mgr.(*fakeManager)
	if !ok {
		t.Fatal("expected a *fakeManager")
	}
	client, ok := fm.client.(*kubernetesfake.Clientset)
	if !ok {
		t.Fatal("expected a *kubernetesfake.Clientset")
	}
	return client
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
