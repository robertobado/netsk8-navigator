package api

import (
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	kubefake "helm.sh/helm/v3/pkg/kube/fake"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage"
	"helm.sh/helm/v3/pkg/storage/driver"
	helmtime "helm.sh/helm/v3/pkg/time"
	"k8s.io/client-go/kubernetes"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
)

func seedHelmRelease(t *testing.T, client kubernetes.Interface, name, ns string) {
	t.Helper()
	d := driver.NewSecrets(client.CoreV1().Secrets(ns))
	rel := &release.Release{
		Name:      name,
		Namespace: ns,
		Version:   1,
		Info:      &release.Info{Status: release.StatusDeployed},
		Chart:     &chart.Chart{Metadata: &chart.Metadata{Name: name, Version: "1.0.0"}},
	}
	if err := d.Create(fmt.Sprintf("%s.v1", name), rel); err != nil {
		t.Fatal(err)
	}
}

// TestHelmListAllNamespaces_RequiresEmptyInitNamespace guards a regression in
// handleHelmReleases: action.Configuration.Init must be given an *empty*
// namespace (not a default like "default") for List.AllNamespaces to
// actually see releases outside that one namespace — the secret/configmap
// storage driver lists within whatever namespace Init was given regardless
// of the AllNamespaces flag. This is exactly what `helm list
// --all-namespaces` itself does (cmd/helm/list.go re-inits with "").
//
// This can't go through newHelmConfig/cfg.Init end-to-end (that needs a real
// genericclioptions.RESTClientGetter, which the test fakeManager deliberately
// doesn't support — same untestable-without-a-live-cluster category as
// podexec/watch), so it builds the storage layer directly against a fake
// clientset instead, pinning down the Helm-side behavior handleHelmReleases
// relies on.
func TestHelmListAllNamespaces_RequiresEmptyInitNamespace(t *testing.T) {
	client := kubernetesfake.NewSimpleClientset()
	seedHelmRelease(t, client, "web", "prod")
	seedHelmRelease(t, client, "cache", "staging")

	newCfg := func(namespace string) *action.Configuration {
		return &action.Configuration{
			Releases:   storage.Init(driver.NewSecrets(client.CoreV1().Secrets(namespace))),
			KubeClient: &kubefake.PrintingKubeClient{Out: io.Discard, LogOutput: io.Discard},
			Log:        func(string, ...any) {},
		}
	}

	t.Run("initialized with a concrete namespace only sees that namespace", func(t *testing.T) {
		list := action.NewList(newCfg("default"))
		list.AllNamespaces = true
		results, err := list.Run()
		if err != nil {
			t.Fatal(err)
		}
		if len(results) != 0 {
			t.Errorf("expected 0 releases when initialized with \"default\" (releases live in prod/staging), got %d", len(results))
		}
	})

	t.Run("initialized with an empty namespace sees every namespace", func(t *testing.T) {
		list := action.NewList(newCfg(""))
		list.AllNamespaces = true
		results, err := list.Run()
		if err != nil {
			t.Fatal(err)
		}
		if len(results) != 2 {
			t.Errorf("expected 2 releases across every namespace, got %d", len(results))
		}
	})
}

func TestToHelmReleaseView(t *testing.T) {
	deployed := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	full := &release.Release{
		Name:      "web",
		Namespace: "prod",
		Version:   3,
		Chart:     &chart.Chart{Metadata: &chart.Metadata{Name: "nginx", Version: "1.2.3", AppVersion: "1.25.0"}},
		Info:      &release.Info{Status: release.StatusDeployed, LastDeployed: helmtime.Time{Time: deployed}},
	}
	v := toHelmReleaseView(full)
	if v.Name != "web" || v.Namespace != "prod" || v.Revision != 3 {
		t.Errorf("got %+v", v)
	}
	if v.Chart != "nginx-1.2.3" || v.AppVersion != "1.25.0" {
		t.Errorf("chart/appVersion = %q/%q", v.Chart, v.AppVersion)
	}
	if v.Status != "deployed" {
		t.Errorf("status = %q", v.Status)
	}
	if v.Updated != deployed.Format(time.RFC3339) {
		t.Errorf("updated = %q", v.Updated)
	}
}

func TestToHelmReleaseView_NilChartAndInfo(t *testing.T) {
	v := toHelmReleaseView(&release.Release{Name: "bare", Namespace: "default", Version: 1})
	if v.Chart != "" || v.AppVersion != "" || v.Status != "" || v.Updated != "" {
		t.Errorf("expected every optional field zero-valued, got %+v", v)
	}
}

func TestOrDefaultNS(t *testing.T) {
	if got := orDefaultNS(""); got != "default" {
		t.Errorf("orDefaultNS(\"\") = %q, want \"default\"", got)
	}
	if got := orDefaultNS("prod"); got != "prod" {
		t.Errorf("orDefaultNS(\"prod\") = %q, want passthrough", got)
	}
}

// The remaining handlers all reach s.newHelmConfig successfully against
// fakeManager (Init never dials out), then fail fast the moment the
// returned action.Configuration is actually used — the same
// RESTConfigFor-always-errors path exercised in helm_install_test.go. Only
// each handler's 200 success body (real release data) is untestable here.

func TestHandleHelmReleases_RunFailsWithoutLiveCluster(t *testing.T) {
	s := newTestServer(t)
	for _, path := range []string{
		"/api/contexts/test/helm/releases",
		"/api/contexts/test/helm/releases?namespace=prod",
	} {
		rec := doRequest(t, s, "GET", path, "")
		if rec.Code != http.StatusBadGateway {
			t.Errorf("GET %s: status = %d, want 502", path, rec.Code)
		}
	}
}

func TestHandleHelmReleaseStatus_RunFailsWithoutLiveCluster(t *testing.T) {
	s := newTestServer(t)
	rec := doRequest(t, s, "GET", "/api/contexts/test/helm/releases/prod/web", "")
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rec.Code)
	}
}

func TestHandleHelmReleaseManifest_RunFailsWithoutLiveCluster(t *testing.T) {
	s := newTestServer(t)
	rec := doRequest(t, s, "GET", "/api/contexts/test/helm/releases/prod/web/manifest", "")
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rec.Code)
	}
}

func TestHandleHelmReleaseHistory_RunFailsWithoutLiveCluster(t *testing.T) {
	s := newTestServer(t)
	rec := doRequest(t, s, "GET", "/api/contexts/test/helm/releases/prod/web/history", "")
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rec.Code)
	}
}

func TestHandleHelmReleaseRollback_MalformedBody(t *testing.T) {
	s := newTestServer(t)
	rec := doRequest(t, s, "POST", "/api/contexts/test/helm/releases/prod/web/rollback", `not json`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestHandleHelmReleaseRollback_NonPositiveRevision(t *testing.T) {
	s := newTestServer(t)
	for _, body := range []string{`{"revision":0}`, `{"revision":-1}`, `{}`} {
		rec := doRequest(t, s, "POST", "/api/contexts/test/helm/releases/prod/web/rollback", body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("body=%s: status = %d, want 400", body, rec.Code)
		}
	}
}

func TestHandleHelmReleaseRollback_RunFailsWithoutLiveCluster(t *testing.T) {
	s := newTestServer(t)
	rec := doRequest(t, s, "POST", "/api/contexts/test/helm/releases/prod/web/rollback", `{"revision":2}`)
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rec.Code)
	}
}

func TestHandleHelmReleaseUninstall_RunFailsWithoutLiveCluster(t *testing.T) {
	s := newTestServer(t)
	rec := doRequest(t, s, "DELETE", "/api/contexts/test/helm/releases/prod/web", "")
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rec.Code)
	}
}
