package api

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"helm.sh/helm/v3/pkg/helmpath"
	"helm.sh/helm/v3/pkg/repo"
)

// withHelmTestSettings points the shared helmSettings (repositories.yaml +
// index cache) at a temp dir for the duration of the test, so tests never
// touch the developer's real ~/.config/helm or ~/.cache/helm.
func withHelmTestSettings(t *testing.T) {
	t.Helper()
	origConfig, origCache := helmSettings.RepositoryConfig, helmSettings.RepositoryCache
	dir := t.TempDir()
	helmSettings.RepositoryConfig = filepath.Join(dir, "repositories.yaml")
	helmSettings.RepositoryCache = filepath.Join(dir, "cache")
	t.Cleanup(func() {
		helmSettings.RepositoryConfig = origConfig
		helmSettings.RepositoryCache = origCache
	})
}

// minimalChartTgz builds a valid, minimal Helm chart archive in memory —
// enough for loader.Load to accept it.
func minimalChartTgz(t *testing.T, name, version string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	files := map[string]string{
		name + "/Chart.yaml":  fmt.Sprintf("apiVersion: v2\nname: %s\nversion: %s\n", name, version),
		name + "/values.yaml": "replicas: 1\n",
	}
	for path, content := range files {
		if err := tw.WriteHeader(&tar.Header{Name: path, Mode: 0o644, Size: int64(len(content))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// newHelmIndexServer serves an index.yaml listing one chart/version (whose
// tgz is a real minimal archive at the referenced URL) plus the tgz itself.
func newHelmIndexServer(t *testing.T, chartName, version string) *httptest.Server {
	t.Helper()
	tgz := minimalChartTgz(t, chartName, version)
	mux := http.NewServeMux()
	mux.HandleFunc("/index.yaml", func(w http.ResponseWriter, r *http.Request) {
		index := fmt.Sprintf(`apiVersion: v1
entries:
  %s:
  - name: %s
    version: %s
    appVersion: "4.5.6"
    description: A test chart
    urls:
    - %s-%s.tgz
generated: "2024-01-01T00:00:00Z"
`, chartName, chartName, version, chartName, version)
		_, _ = w.Write([]byte(index))
	})
	mux.HandleFunc(fmt.Sprintf("/%s-%s.tgz", chartName, version), func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(tgz)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestHandleAddAndListHelmRepo(t *testing.T) {
	withHelmTestSettings(t)
	srv := newHelmIndexServer(t, "mychart", "1.2.3")
	s := newTestServer(t)

	rec := doRequest(t, s, "POST", "/api/helm/repos", fmt.Sprintf(`{"name":"test-repo","url":%q}`, srv.URL))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}

	rec2 := doRequest(t, s, "GET", "/api/helm/repos", "")
	var repos []helmRepoView
	if err := json.Unmarshal(rec2.Body.Bytes(), &repos); err != nil {
		t.Fatal(err)
	}
	if len(repos) != 1 || repos[0].Name != "test-repo" || repos[0].URL != srv.URL {
		t.Errorf("got %+v", repos)
	}
}

func TestHandleAddHelmRepo_UnreachableFails(t *testing.T) {
	withHelmTestSettings(t)
	s := newTestServer(t)
	rec := doRequest(t, s, "POST", "/api/helm/repos", `{"name":"bad","url":"http://127.0.0.1:1"}`)
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502 (unreachable repo)", rec.Code)
	}
	rec2 := doRequest(t, s, "GET", "/api/helm/repos", "")
	var repos []helmRepoView
	_ = json.Unmarshal(rec2.Body.Bytes(), &repos)
	if len(repos) != 0 {
		t.Errorf("an unreachable repo must not be persisted, got %+v", repos)
	}
}

func TestHandleRemoveHelmRepo(t *testing.T) {
	withHelmTestSettings(t)
	srv := newHelmIndexServer(t, "mychart", "1.2.3")
	s := newTestServer(t)
	doRequest(t, s, "POST", "/api/helm/repos", fmt.Sprintf(`{"name":"test-repo","url":%q}`, srv.URL))

	rec := doRequest(t, s, "DELETE", "/api/helm/repos/test-repo", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	rec2 := doRequest(t, s, "GET", "/api/helm/repos", "")
	var repos []helmRepoView
	_ = json.Unmarshal(rec2.Body.Bytes(), &repos)
	if len(repos) != 0 {
		t.Errorf("expected no repos left, got %+v", repos)
	}
}

func TestHandleRemoveHelmRepo_NotFound(t *testing.T) {
	withHelmTestSettings(t)
	s := newTestServer(t)
	rec := doRequest(t, s, "DELETE", "/api/helm/repos/missing", "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestHandleRefreshHelmRepo(t *testing.T) {
	withHelmTestSettings(t)
	srv := newHelmIndexServer(t, "mychart", "1.2.3")
	s := newTestServer(t)
	doRequest(t, s, "POST", "/api/helm/repos", fmt.Sprintf(`{"name":"test-repo","url":%q}`, srv.URL))

	rec := doRequest(t, s, "POST", "/api/helm/repos/test-repo/refresh", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleHelmSearch(t *testing.T) {
	withHelmTestSettings(t)
	srv := newHelmIndexServer(t, "mychart", "1.2.3")
	s := newTestServer(t)
	doRequest(t, s, "POST", "/api/helm/repos", fmt.Sprintf(`{"name":"test-repo","url":%q}`, srv.URL))

	rec := doRequest(t, s, "GET", "/api/helm/search?q=mychart", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var results []helmChartSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &results); err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Repo != "test-repo" || results[0].Name != "mychart" || results[0].Version != "1.2.3" {
		t.Errorf("got %+v", results)
	}
}

func TestHandleHelmSearch_NoQueryListsEverything(t *testing.T) {
	withHelmTestSettings(t)
	srv := newHelmIndexServer(t, "mychart", "1.2.3")
	s := newTestServer(t)
	doRequest(t, s, "POST", "/api/helm/repos", fmt.Sprintf(`{"name":"test-repo","url":%q}`, srv.URL))

	rec := doRequest(t, s, "GET", "/api/helm/search", "")
	var results []helmChartSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &results); err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Errorf("expected the one indexed chart, got %+v", results)
	}
}

func TestHandleHelmSearch_NoReposYet(t *testing.T) {
	withHelmTestSettings(t)
	s := newTestServer(t)
	rec := doRequest(t, s, "GET", "/api/helm/search?q=anything", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var results []helmChartSummary
	_ = json.Unmarshal(rec.Body.Bytes(), &results)
	if len(results) != 0 {
		t.Errorf("expected no results with no repos added, got %+v", results)
	}
}

func TestHandleHelmChartDetail(t *testing.T) {
	withHelmTestSettings(t)
	srv := newHelmIndexServer(t, "mychart", "1.2.3")
	s := newTestServer(t)
	doRequest(t, s, "POST", "/api/helm/repos", fmt.Sprintf(`{"name":"test-repo","url":%q}`, srv.URL))

	rec := doRequest(t, s, "GET", "/api/helm/charts/test-repo/mychart", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var detail helmChartDetail
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if len(detail.Versions) != 1 || detail.Versions[0] != "1.2.3" {
		t.Errorf("versions = %+v", detail.Versions)
	}
	if detail.DefaultValues == "" {
		t.Error("expected the chart's default values.yaml to be returned")
	}
}

func TestHandleHelmChartDetail_UnknownRepo(t *testing.T) {
	withHelmTestSettings(t)
	s := newTestServer(t)
	rec := doRequest(t, s, "GET", "/api/helm/charts/nope/mychart", "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestHandleHelmChartDetail_UnknownChart(t *testing.T) {
	withHelmTestSettings(t)
	srv := newHelmIndexServer(t, "mychart", "1.2.3")
	s := newTestServer(t)
	doRequest(t, s, "POST", "/api/helm/repos", fmt.Sprintf(`{"name":"test-repo","url":%q}`, srv.URL))

	rec := doRequest(t, s, "GET", "/api/helm/charts/test-repo/nope", "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestHandleHelmChartDetail_UnknownVersion(t *testing.T) {
	withHelmTestSettings(t)
	srv := newHelmIndexServer(t, "mychart", "1.2.3")
	s := newTestServer(t)
	doRequest(t, s, "POST", "/api/helm/repos", fmt.Sprintf(`{"name":"test-repo","url":%q}`, srv.URL))

	rec := doRequest(t, s, "GET", "/api/helm/charts/test-repo/mychart?version=9.9.9", "")
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502 (version not in the index)", rec.Code)
	}
}

func TestHandleHelmChartDetail_IndexNotCached(t *testing.T) {
	withHelmTestSettings(t)
	srv := newHelmIndexServer(t, "mychart", "1.2.3")
	s := newTestServer(t)
	doRequest(t, s, "POST", "/api/helm/repos", fmt.Sprintf(`{"name":"test-repo","url":%q}`, srv.URL))

	// The repo is registered, but its cached index.yaml (written as a side
	// effect of the add above) is gone — e.g. the cache directory got pruned.
	cachePath := filepath.Join(helmSettings.RepositoryCache, helmpath.CacheIndexFile("test-repo"))
	if err := os.Remove(cachePath); err != nil {
		t.Fatal(err)
	}

	rec := doRequest(t, s, "GET", "/api/helm/charts/test-repo/mychart", "")
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502 (index not cached)", rec.Code)
	}
}

// breakRepositoryConfigForReading points helmSettings.RepositoryConfig at a
// directory rather than a file, so loadHelmRepoFile's os.ReadFile fails with
// something other than os.ErrNotExist — its only handled case, and otherwise
// unreachable since every other caller only ever sees "file doesn't exist
// yet".
func breakRepositoryConfigForReading(t *testing.T) {
	t.Helper()
	helmSettings.RepositoryConfig = t.TempDir()
}

func TestLoadHelmRepoFile_ReadError(t *testing.T) {
	withHelmTestSettings(t)
	breakRepositoryConfigForReading(t)
	if _, err := loadHelmRepoFile(); err == nil {
		t.Error("expected an error reading a repositories.yaml path that is actually a directory")
	}
}

func TestWriteHelmRepoFile_MkdirError(t *testing.T) {
	withHelmTestSettings(t)
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	// blocker is a regular file, so MkdirAll can't create a directory in its
	// place for the config file's parent.
	helmSettings.RepositoryConfig = filepath.Join(blocker, "repositories.yaml")
	if err := writeHelmRepoFile(repo.NewFile()); err == nil {
		t.Error("expected an error when the parent directory can't be created")
	}
}

func TestHandleHelmRepos_LoadError(t *testing.T) {
	withHelmTestSettings(t)
	breakRepositoryConfigForReading(t)
	s := newTestServer(t)
	rec := doRequest(t, s, "GET", "/api/helm/repos", "")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestHandleAddHelmRepo_BodyReadError(t *testing.T) {
	withHelmTestSettings(t)
	s := newTestServer(t)
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/helm/repos", errReader{})
	s.Routes().ServeHTTP(rec, r)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestHandleAddHelmRepo_MalformedBody(t *testing.T) {
	withHelmTestSettings(t)
	s := newTestServer(t)
	rec := doRequest(t, s, "POST", "/api/helm/repos", `not json`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestHandleAddHelmRepo_MissingFields(t *testing.T) {
	withHelmTestSettings(t)
	s := newTestServer(t)
	for _, body := range []string{`{"name":"","url":"http://example.com"}`, `{"name":"x","url":""}`, `{}`} {
		rec := doRequest(t, s, "POST", "/api/helm/repos", body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("body=%s: status = %d, want 400", body, rec.Code)
		}
	}
}

func TestHandleAddHelmRepo_UnsupportedScheme(t *testing.T) {
	withHelmTestSettings(t)
	s := newTestServer(t)
	rec := doRequest(t, s, "POST", "/api/helm/repos", `{"name":"bad","url":"ftp://example.com/repo"}`)
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502 (no protocol handler registered for ftp)", rec.Code)
	}
}

func TestHandleAddHelmRepo_LoadHelmRepoFileError(t *testing.T) {
	withHelmTestSettings(t)
	srv := newHelmIndexServer(t, "mychart", "1.2.3")
	// Break RepositoryConfig only after the index server exists: the
	// download step (which must succeed first) doesn't touch this path at
	// all, so this isolates loadHelmRepoFile's own failure.
	breakRepositoryConfigForReading(t)
	s := newTestServer(t)
	rec := doRequest(t, s, "POST", "/api/helm/repos", fmt.Sprintf(`{"name":"test-repo","url":%q}`, srv.URL))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleRemoveHelmRepo_LoadError(t *testing.T) {
	withHelmTestSettings(t)
	breakRepositoryConfigForReading(t)
	s := newTestServer(t)
	rec := doRequest(t, s, "DELETE", "/api/helm/repos/whatever", "")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestHandleRefreshHelmRepo_LoadError(t *testing.T) {
	withHelmTestSettings(t)
	breakRepositoryConfigForReading(t)
	s := newTestServer(t)
	rec := doRequest(t, s, "POST", "/api/helm/repos/whatever/refresh", "")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestHandleRefreshHelmRepo_NotFound(t *testing.T) {
	withHelmTestSettings(t)
	s := newTestServer(t)
	rec := doRequest(t, s, "POST", "/api/helm/repos/missing/refresh", "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestHandleRefreshHelmRepo_UnreachableFails(t *testing.T) {
	withHelmTestSettings(t)
	srv := newHelmIndexServer(t, "mychart", "1.2.3")
	s := newTestServer(t)
	doRequest(t, s, "POST", "/api/helm/repos", fmt.Sprintf(`{"name":"test-repo","url":%q}`, srv.URL))

	// The repo was reachable at add time; take it down before refreshing so
	// only the refresh's own downloadHelmRepoIndex call fails.
	srv.Close()

	rec := doRequest(t, s, "POST", "/api/helm/repos/test-repo/refresh", "")
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rec.Code)
	}
}

func TestHandleHelmSearch_LoadError(t *testing.T) {
	withHelmTestSettings(t)
	breakRepositoryConfigForReading(t)
	s := newTestServer(t)
	rec := doRequest(t, s, "GET", "/api/helm/search", "")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

// makeHelmRepoConfigFileReadOnly chmods the already-written
// helmSettings.RepositoryConfig file itself (not its directory) to
// read-only, so a subsequent load still finds the same persisted data (a
// read doesn't need write permission) but writeHelmRepoFile's os.WriteFile
// call to overwrite it is denied — isolating a write failure from a load
// that must succeed against real, already-persisted repo data. Assumes the
// test process isn't running as root, which would bypass the permission
// check.
func makeHelmRepoConfigFileReadOnly(t *testing.T) {
	t.Helper()
	path := helmSettings.RepositoryConfig
	if err := os.Chmod(path, 0o400); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
}

func TestHandleAddHelmRepo_WriteError(t *testing.T) {
	withHelmTestSettings(t)
	// Establish repositories.yaml with content first, so the write that
	// fails below is an overwrite, not the initial create (which
	// writeHelmRepoFile handles as an os.MkdirAll, a different failure mode
	// already covered by TestWriteHelmRepoFile_MkdirError).
	if err := writeHelmRepoFile(repo.NewFile()); err != nil {
		t.Fatal(err)
	}
	makeHelmRepoConfigFileReadOnly(t)

	srv := newHelmIndexServer(t, "mychart", "1.2.3")
	s := newTestServer(t)
	rec := doRequest(t, s, "POST", "/api/helm/repos", fmt.Sprintf(`{"name":"test-repo","url":%q}`, srv.URL))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleRemoveHelmRepo_WriteError(t *testing.T) {
	withHelmTestSettings(t)
	srv := newHelmIndexServer(t, "mychart", "1.2.3")
	s := newTestServer(t)
	doRequest(t, s, "POST", "/api/helm/repos", fmt.Sprintf(`{"name":"test-repo","url":%q}`, srv.URL))

	makeHelmRepoConfigFileReadOnly(t)
	rec := doRequest(t, s, "DELETE", "/api/helm/repos/test-repo", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleHelmChartDetail_ResolveRepoURLLoadError(t *testing.T) {
	withHelmTestSettings(t)
	breakRepositoryConfigForReading(t)
	s := newTestServer(t)
	rec := doRequest(t, s, "GET", "/api/helm/charts/whatever/mychart", "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 (resolveHelmRepoURL surfaces loadHelmRepoFile's error the same as \"not found\")", rec.Code)
	}
}
