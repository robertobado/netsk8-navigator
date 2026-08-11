package api

import (
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	fakecorev1 "k8s.io/client-go/kubernetes/typed/core/v1/fake"
	"k8s.io/client-go/rest"
	restfake "k8s.io/client-go/rest/fake"

	"github.com/robertobado/netsk8-navigator/backend/internal/config"
)

// metricsRoundTripper serves canned metrics-server JSON responses keyed by
// exact request path, letting usage.go's raw RESTClient().Get().AbsPath()
// calls be driven from a table instead of a live cluster. An unmapped path
// 404s, the same way a cluster with no metrics-server installed would.
type metricsRoundTripper struct {
	responses map[string]string
}

func (m metricsRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if body, ok := m.responses[req.URL.Path]; ok {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	}
	return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
}

// fakeCoreV1WithREST wraps a fake CoreV1Interface, swapping in a working
// fake RESTClient. The plain kubernetesfake.Clientset's CoreV1().RESTClient()
// always returns a nil *rest.RESTClient (see client-go's generated
// fake_core_client.go), which panics on the raw AbsPath().DoRaw() calls
// usage.go makes against the metrics-server API — there's no typed client
// for metrics.k8s.io in this codebase, so that's the only way to reach it.
type fakeCoreV1WithREST struct {
	*fakecorev1.FakeCoreV1
	rc rest.Interface
}

func (f *fakeCoreV1WithREST) RESTClient() rest.Interface { return f.rc }

type fakeClientsetWithREST struct {
	*kubernetesfake.Clientset
	core corev1client.CoreV1Interface
}

func (f *fakeClientsetWithREST) CoreV1() corev1client.CoreV1Interface { return f.core }

// newFakeClientWithMetrics builds a fake kubernetes.Interface whose typed
// calls (Pods().List(), Nodes().List(), ...) behave exactly like
// kubernetesfake.NewSimpleClientset, and whose CoreV1().RESTClient() replays
// responses (via rt) instead of panicking.
func newFakeClientWithMetrics(t *testing.T, rt metricsRoundTripper, objs ...runtime.Object) kubernetes.Interface {
	t.Helper()
	base := kubernetesfake.NewSimpleClientset(objs...)
	base.PrependReactor("list", "pods", podsFieldSelectorReactor(base))
	rc := &restfake.RESTClient{
		NegotiatedSerializer: scheme.Codecs.WithoutConversion(),
		Client:               restfake.CreateHTTPClient(rt.RoundTrip),
	}
	return &fakeClientsetWithREST{
		Clientset: base,
		core: &fakeCoreV1WithREST{
			FakeCoreV1: base.CoreV1().(*fakecorev1.FakeCoreV1),
			rc:         rc,
		},
	}
}

// newTestServerWithMetrics is newTestServer plus a metrics-server round
// tripper, for tests exercising usage.go's handlers end to end.
func newTestServerWithMetrics(t *testing.T, rt metricsRoundTripper, objs ...runtime.Object) *Server {
	t.Helper()
	fm := &fakeManager{
		client:  newFakeClientWithMetrics(t, rt, objs...),
		dynamic: dynamicfake.NewSimpleDynamicClient(scheme.Scheme, objs...),
		gvrs:    testGVRs,
	}
	cfg := config.NewStoreAt(filepath.Join(t.TempDir(), "config.json"))
	return NewServer(fm, cfg, "")
}
