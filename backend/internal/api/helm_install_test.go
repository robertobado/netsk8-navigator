package api

import (
	"net/http"
	"testing"

	"helm.sh/helm/v3/pkg/chart"
)

// resolveHelmChart / chartDefaultValuesYAML / chartReadme are pure enough to
// exercise directly against a fake chart-repo server (same withHelmTestSettings
// + newHelmIndexServer harness helm_repos_test.go already establishes).

func TestResolveHelmChart(t *testing.T) {
	withHelmTestSettings(t)
	srv := newHelmIndexServer(t, "mychart", "1.2.3")

	c, err := resolveHelmChart(srv.URL, "mychart", "1.2.3")
	if err != nil {
		t.Fatalf("resolveHelmChart: %v", err)
	}
	if c.Metadata.Name != "mychart" || c.Metadata.Version != "1.2.3" {
		t.Errorf("got %+v", c.Metadata)
	}
}

func TestResolveHelmChart_UnknownVersion(t *testing.T) {
	withHelmTestSettings(t)
	srv := newHelmIndexServer(t, "mychart", "1.2.3")

	if _, err := resolveHelmChart(srv.URL, "mychart", "9.9.9"); err == nil {
		t.Error("expected an error for a version not in the index")
	}
}

func TestChartDefaultValuesYAML(t *testing.T) {
	c := &chart.Chart{Values: map[string]any{"replicas": 3}}
	got := chartDefaultValuesYAML(c)
	if got != "replicas: 3\n" {
		t.Errorf("got %q", got)
	}
}

func TestChartReadme(t *testing.T) {
	c := &chart.Chart{Files: []*chart.File{{Name: "README.md", Data: []byte("hello")}}}
	if got := chartReadme(c); got != "hello" {
		t.Errorf("got %q", got)
	}
	// Case-insensitive match, per strings.EqualFold.
	c2 := &chart.Chart{Files: []*chart.File{{Name: "readme.md", Data: []byte("hi")}}}
	if got := chartReadme(c2); got != "hi" {
		t.Errorf("got %q", got)
	}
}

func TestChartReadme_None(t *testing.T) {
	c := &chart.Chart{}
	if got := chartReadme(c); got != "" {
		t.Errorf("got %q, want empty string when the chart ships no README", got)
	}
}

// handleHelmReleaseInstall / handleHelmReleaseUpgrade: the eventual
// action.Install{,Upgrade}.Run(...) call always fails fast against
// fakeManager (RESTConfigFor errors synchronously, no live cluster needed —
// see newHelmConfig), so every branch up to and including that failure is
// testable; only the 200/201 success response (a real completed install)
// isn't.

func TestHandleHelmReleaseInstall_MissingReleaseName(t *testing.T) {
	s := newTestServer(t)
	rec := doRequest(t, s, "POST", "/api/contexts/test/helm/releases", `{"repo":"r","chart":"c"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestHandleHelmReleaseInstall_MissingRepoOrChart(t *testing.T) {
	s := newTestServer(t)
	rec := doRequest(t, s, "POST", "/api/contexts/test/helm/releases", `{"releaseName":"web"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestHandleHelmReleaseInstall_MalformedBody(t *testing.T) {
	s := newTestServer(t)
	rec := doRequest(t, s, "POST", "/api/contexts/test/helm/releases", `not json`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestHandleHelmReleaseInstall_InvalidValuesYAML(t *testing.T) {
	s := newTestServer(t)
	rec := doRequest(t, s, "POST", "/api/contexts/test/helm/releases", `{"repo":"r","chart":"c","releaseName":"web","values":"not: [valid"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestHandleHelmReleaseInstall_UnknownRepo(t *testing.T) {
	withHelmTestSettings(t)
	s := newTestServer(t)
	rec := doRequest(t, s, "POST", "/api/contexts/test/helm/releases", `{"repo":"nope","chart":"c","releaseName":"web"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestHandleHelmReleaseInstall_UnknownChart(t *testing.T) {
	withHelmTestSettings(t)
	srv := newHelmIndexServer(t, "mychart", "1.2.3")
	s := newTestServer(t)
	doRequest(t, s, "POST", "/api/helm/repos", `{"name":"test-repo","url":"`+srv.URL+`"}`)

	rec := doRequest(t, s, "POST", "/api/contexts/test/helm/releases", `{"repo":"test-repo","chart":"nope","releaseName":"web"}`)
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502 (chart not found)", rec.Code)
	}
}

func TestHandleHelmReleaseInstall_RunFailsWithoutLiveCluster(t *testing.T) {
	withHelmTestSettings(t)
	srv := newHelmIndexServer(t, "mychart", "1.2.3")
	s := newTestServer(t)
	doRequest(t, s, "POST", "/api/helm/repos", `{"name":"test-repo","url":"`+srv.URL+`"}`)

	rec := doRequest(t, s, "POST", "/api/contexts/test/helm/releases", `{"repo":"test-repo","chart":"mychart","version":"1.2.3","releaseName":"web"}`)
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502 (install.Run needs a live cluster, fakeManager.RESTConfigFor always errors)", rec.Code)
	}
}

func TestHandleHelmReleaseUpgrade_MalformedBody(t *testing.T) {
	s := newTestServer(t)
	rec := doRequest(t, s, "PUT", "/api/contexts/test/helm/releases/prod/web", `not json`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestHandleHelmReleaseUpgrade_UnknownRepo(t *testing.T) {
	withHelmTestSettings(t)
	s := newTestServer(t)
	rec := doRequest(t, s, "PUT", "/api/contexts/test/helm/releases/prod/web", `{"repo":"nope","chart":"c"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestHandleHelmReleaseUpgrade_RunFailsWithoutLiveCluster(t *testing.T) {
	withHelmTestSettings(t)
	srv := newHelmIndexServer(t, "mychart", "1.2.3")
	s := newTestServer(t)
	doRequest(t, s, "POST", "/api/helm/repos", `{"name":"test-repo","url":"`+srv.URL+`"}`)

	rec := doRequest(t, s, "PUT", "/api/contexts/test/helm/releases/prod/web", `{"repo":"test-repo","chart":"mychart","version":"1.2.3"}`)
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502 (upgrade.Run needs a live cluster)", rec.Code)
	}
}
