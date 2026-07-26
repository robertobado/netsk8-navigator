package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"sigs.k8s.io/yaml"
)

// resolveHelmChart downloads (or reuses the local cache of) a chart tgz from
// a repo URL and loads it. Fetching by RepoURL directly — rather than
// requiring the repo to be "added" locally — works even right after a repo's
// index was refreshed, no re-caching step needed.
func resolveHelmChart(repoURL, chartName, version string) (*chart.Chart, error) {
	co := action.ChartPathOptions{RepoURL: repoURL, Version: version}
	path, err := co.LocateChart(chartName, helmSettings)
	if err != nil {
		return nil, err
	}
	return loader.Load(path)
}

// chartDefaultValuesYAML re-marshals a chart's parsed default values. Loading
// then re-marshaling (rather than reading the raw values.yaml file) keeps the
// shape consistent with what actually gets used as the install's base values.
func chartDefaultValuesYAML(c *chart.Chart) string {
	data, err := yaml.Marshal(c.Values)
	if err != nil {
		return ""
	}
	return string(data)
}

// chartReadme returns the chart's README, if it ships one.
func chartReadme(c *chart.Chart) string {
	for _, f := range c.Files {
		if strings.EqualFold(f.Name, "README.md") {
			return string(f.Data)
		}
	}
	return ""
}

// helmInstallRequest is the shared body shape for install and upgrade — the
// chart is always identified by an already-added repo's name (not a raw URL),
// so the same repositories.yaml the search/browse UI reads from is the only
// source of truth for where a chart comes from.
type helmInstallRequest struct {
	Repo        string `json:"repo"`
	Chart       string `json:"chart"`
	Version     string `json:"version"`
	ReleaseName string `json:"releaseName"`
	Namespace   string `json:"namespace"`
	Values      string `json:"values"` // YAML
}

func decodeHelmInstallRequest(r *http.Request) (*helmInstallRequest, map[string]any, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
	if err != nil {
		return nil, nil, err
	}
	var payload helmInstallRequest
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, nil, err
	}
	if payload.Repo == "" || payload.Chart == "" {
		return nil, nil, fmt.Errorf("repo and chart are required")
	}
	values := map[string]any{}
	if strings.TrimSpace(payload.Values) != "" {
		if err := yaml.Unmarshal([]byte(payload.Values), &values); err != nil {
			return nil, nil, fmt.Errorf("invalid values YAML: %w", err)
		}
	}
	return &payload, values, nil
}

// handleHelmReleaseInstall: POST /api/contexts/{ctx}/helm/releases
func (s *Server) handleHelmReleaseInstall(w http.ResponseWriter, r *http.Request) {
	payload, values, err := decodeHelmInstallRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if payload.ReleaseName == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("releaseName is required"))
		return
	}
	ns := orDefaultNS(payload.Namespace)

	repoURL, err := resolveHelmRepoURL(payload.Repo)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	chrt, err := resolveHelmChart(repoURL, payload.Chart, payload.Version)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}

	cfg, err := s.newHelmConfig(r.PathValue("ctx"), ns)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	install := action.NewInstall(cfg)
	install.ReleaseName = payload.ReleaseName
	install.Namespace = ns
	install.CreateNamespace = true

	audit(r, "helm-install", "release", payload.ReleaseName, "namespace", ns, "chart", payload.Chart, "version", payload.Version)
	rel, err := install.Run(chrt, values)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusCreated, toHelmReleaseView(rel))
}

// handleHelmReleaseUpgrade: PUT /api/contexts/{ctx}/helm/releases/{namespace}/{name}
func (s *Server) handleHelmReleaseUpgrade(w http.ResponseWriter, r *http.Request) {
	ns, name := r.PathValue("namespace"), r.PathValue("name")
	payload, values, err := decodeHelmInstallRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	repoURL, err := resolveHelmRepoURL(payload.Repo)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	chrt, err := resolveHelmChart(repoURL, payload.Chart, payload.Version)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}

	cfg, err := s.newHelmConfig(r.PathValue("ctx"), ns)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	upgrade := action.NewUpgrade(cfg)
	upgrade.Namespace = ns

	audit(r, "helm-upgrade", "release", name, "namespace", ns, "chart", payload.Chart, "version", payload.Version)
	rel, err := upgrade.Run(name, chrt, values)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, toHelmReleaseView(rel))
}
