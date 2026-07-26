package api

import (
	"fmt"
	"io"
	"testing"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	kubefake "helm.sh/helm/v3/pkg/kube/fake"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage"
	"helm.sh/helm/v3/pkg/storage/driver"
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
