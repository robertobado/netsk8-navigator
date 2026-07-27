package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"helm.sh/helm/v3/cmd/helm/search"
	"helm.sh/helm/v3/pkg/getter"
	"helm.sh/helm/v3/pkg/helmpath"
	"helm.sh/helm/v3/pkg/repo"
)

// Repos aren't per-cluster — they're config/cache on this machine, the same
// ~/.config/helm and ~/.cache/helm (or HELM_* overrides) the `helm` CLI uses —
// so these handlers don't take a {ctx} path segment.

type helmRepoView struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// loadHelmRepoFile reads repositories.yaml, treating "doesn't exist yet" (no
// repo ever added) as an empty file rather than an error.
func loadHelmRepoFile() (*repo.File, error) {
	f, err := repo.LoadFile(helmSettings.RepositoryConfig)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return repo.NewFile(), nil
		}
		return nil, err
	}
	return f, nil
}

func writeHelmRepoFile(f *repo.File) error {
	if err := os.MkdirAll(filepath.Dir(helmSettings.RepositoryConfig), 0o750); err != nil {
		return err
	}
	return f.WriteFile(helmSettings.RepositoryConfig, 0o644)
}

// handleHelmRepos: GET /api/helm/repos
func (s *Server) handleHelmRepos(w http.ResponseWriter, r *http.Request) {
	f, err := loadHelmRepoFile()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	out := make([]helmRepoView, 0, len(f.Repositories))
	for _, e := range f.Repositories {
		out = append(out, helmRepoView{Name: e.Name, URL: e.URL})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleAddHelmRepo: POST /api/helm/repos, body {"name":"...","url":"..."}
// Downloads the repo's index.yaml immediately so a bad URL/unreachable repo
// fails the request instead of silently registering a dead entry.
func (s *Server) handleAddHelmRepo(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var payload struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if payload.Name == "" || payload.URL == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("name and url are required"))
		return
	}

	entry := &repo.Entry{Name: payload.Name, URL: payload.URL}
	if err := downloadHelmRepoIndex(entry); err != nil {
		writeError(w, http.StatusBadGateway, fmt.Errorf("could not reach repo: %w", err))
		return
	}

	f, err := loadHelmRepoFile()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	f.Update(entry)
	if err := writeHelmRepoFile(f); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	audit(r, "helm-repo-add", "name", payload.Name, "url", payload.URL)
	writeJSON(w, http.StatusOK, helmRepoView{Name: payload.Name, URL: payload.URL})
}

// handleRemoveHelmRepo: DELETE /api/helm/repos/{name}
func (s *Server) handleRemoveHelmRepo(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	f, err := loadHelmRepoFile()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if !f.Remove(name) {
		writeError(w, http.StatusNotFound, fmt.Errorf("repo %q not found", name))
		return
	}
	if err := writeHelmRepoFile(f); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	audit(r, "helm-repo-remove", "name", name)
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

// handleRefreshHelmRepo: POST /api/helm/repos/{name}/refresh — re-downloads
// the index (the equivalent of `helm repo update <name>`).
func (s *Server) handleRefreshHelmRepo(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	f, err := loadHelmRepoFile()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	entry := f.Get(name)
	if entry == nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("repo %q not found", name))
		return
	}
	if err := downloadHelmRepoIndex(entry); err != nil {
		writeError(w, http.StatusBadGateway, fmt.Errorf("could not reach repo: %w", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "refreshed"})
}

func downloadHelmRepoIndex(entry *repo.Entry) error {
	chartRepo, err := repo.NewChartRepository(entry, getter.All(helmSettings))
	if err != nil {
		return err
	}
	chartRepo.CachePath = helmSettings.RepositoryCache
	_, err = chartRepo.DownloadIndexFile()
	return err
}

// resolveHelmRepoURL looks up an added repo's URL by name.
func resolveHelmRepoURL(name string) (string, error) {
	f, err := loadHelmRepoFile()
	if err != nil {
		return "", err
	}
	entry := f.Get(name)
	if entry == nil {
		return "", fmt.Errorf("repo %q not found — add it first", name)
	}
	return entry.URL, nil
}

// helmChartSummary is one chart search result.
type helmChartSummary struct {
	Repo        string `json:"repo"`
	Name        string `json:"name"` // chart name only, without the "repo/" prefix
	Version     string `json:"version"`
	AppVersion  string `json:"appVersion"`
	Description string `json:"description"`
}

// buildHelmSearchIndex loads every added repo's cached index.yaml into one
// searchable index. A repo that was added but never successfully cached
// (or whose cache has gone stale/missing) is skipped rather than failing the
// whole search.
func buildHelmSearchIndex() (*search.Index, error) {
	f, err := loadHelmRepoFile()
	if err != nil {
		return nil, err
	}
	idx := search.NewIndex()
	for _, e := range f.Repositories {
		p := filepath.Join(helmSettings.RepositoryCache, helmpath.CacheIndexFile(e.Name))
		ind, err := repo.LoadIndexFile(p)
		if err != nil {
			continue
		}
		idx.AddRepo(e.Name, ind, false)
	}
	return idx, nil
}

// handleHelmSearch: GET /api/helm/search?q= — "" lists every chart across
// every added repo.
func (s *Server) handleHelmSearch(w http.ResponseWriter, r *http.Request) {
	idx, err := buildHelmSearchIndex()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	q := r.URL.Query().Get("q")
	var results []*search.Result
	if q == "" {
		results = idx.All()
	} else if results, err = idx.Search(q, 100, false); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	search.SortScore(results)

	out := make([]helmChartSummary, 0, len(results))
	for _, res := range results {
		repoName, chartName, ok := strings.Cut(res.Name, "/")
		if !ok {
			continue
		}
		out = append(out, helmChartSummary{
			Repo: repoName, Name: chartName,
			Version: res.Chart.Version, AppVersion: res.Chart.AppVersion, Description: res.Chart.Description,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// helmChartDetail is a chart's available versions plus the latest (or
// requested) version's default values.yaml and README, for the install
// dialog's values editor.
type helmChartDetail struct {
	Versions      []string `json:"versions"`
	DefaultValues string   `json:"defaultValues"` // YAML
	Readme        string   `json:"readme"`
}

// handleHelmChartDetail: GET /api/helm/charts/{repo}/{chart}?version=
// version "" resolves to the newest entry in the repo's index.
func (s *Server) handleHelmChartDetail(w http.ResponseWriter, r *http.Request) {
	repoName, chartName := r.PathValue("repo"), r.PathValue("chart")
	repoURL, err := resolveHelmRepoURL(repoName)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}

	p := filepath.Join(helmSettings.RepositoryCache, helmpath.CacheIndexFile(repoName))
	ind, err := repo.LoadIndexFile(p)
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Errorf("repo index not cached yet — refresh the repo first: %w", err))
		return
	}
	versionsList, ok := ind.Entries[chartName]
	if !ok || len(versionsList) == 0 {
		writeError(w, http.StatusNotFound, fmt.Errorf("chart %q not found in repo %q", chartName, repoName))
		return
	}
	versions := make([]string, 0, len(versionsList))
	for _, v := range versionsList {
		versions = append(versions, v.Version)
	}

	version := r.URL.Query().Get("version")
	chrt, err := resolveHelmChart(repoURL, chartName, version)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, helmChartDetail{
		Versions:      versions,
		DefaultValues: chartDefaultValuesYAML(chrt),
		Readme:        chartReadme(chrt),
	})
}
