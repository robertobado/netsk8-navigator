package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/release"
	"sigs.k8s.io/yaml"
)

// helmReleaseView is the UI projection of a Helm release.
type helmReleaseView struct {
	Name       string `json:"name"`
	Namespace  string `json:"namespace"`
	Chart      string `json:"chart"`      // "nginx-1.2.3" (name-version)
	AppVersion string `json:"appVersion"` // the application's own version, e.g. "1.25.0"
	Revision   int    `json:"revision"`
	Status     string `json:"status"`
	Updated    string `json:"updated"` // RFC3339
}

func toHelmReleaseView(r *release.Release) helmReleaseView {
	v := helmReleaseView{Name: r.Name, Namespace: r.Namespace, Revision: r.Version}
	if r.Chart != nil && r.Chart.Metadata != nil {
		v.Chart = r.Chart.Metadata.Name + "-" + r.Chart.Metadata.Version
		v.AppVersion = r.Chart.Metadata.AppVersion
	}
	if r.Info != nil {
		v.Status = r.Info.Status.String()
		if !r.Info.LastDeployed.IsZero() {
			v.Updated = r.Info.LastDeployed.Format(time.RFC3339)
		}
	}
	return v
}

// handleHelmReleases: GET /api/contexts/{ctx}/helm/releases?namespace=
// "" lists across every namespace.
func (s *Server) handleHelmReleases(w http.ResponseWriter, r *http.Request) {
	ns := r.URL.Query().Get("namespace")
	// Deliberately NOT orDefaultNS here: Helm's own `list --all-namespaces`
	// re-initializes the storage driver with an *empty* namespace too
	// (cmd/helm/list.go) — passing "default" would silently scope every
	// list to that one namespace even with AllNamespaces set, since the
	// secret/configmap driver lists within cfg.Init's namespace regardless.
	cfg, err := s.newHelmConfig(r.PathValue("ctx"), ns)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	list := action.NewList(cfg)
	list.AllNamespaces = ns == ""
	list.All = true // include failed/pending releases, not just successfully deployed
	releases, err := list.Run()
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	out := make([]helmReleaseView, 0, len(releases))
	for _, rel := range releases {
		out = append(out, toHelmReleaseView(rel))
	}
	writeJSON(w, http.StatusOK, out)
}

// orDefaultNS mirrors `helm install`/`upgrade`'s own CLI default: those
// always target one concrete namespace ("default" when --namespace is
// omitted) — unlike list --all-namespaces, there's no "every namespace" mode
// to preserve here.
func orDefaultNS(ns string) string {
	if ns == "" {
		return "default"
	}
	return ns
}

// helmReleaseDetail extends the summary with what only the single-release
// view needs — the notes and the values the user actually supplied (not the
// full computed values tree; `helm get values` without -a shows the same).
type helmReleaseDetail struct {
	helmReleaseView
	Notes  string `json:"notes"`
	Values string `json:"values"` // YAML
}

// handleHelmReleaseStatus: GET /api/contexts/{ctx}/helm/releases/{namespace}/{name}
func (s *Server) handleHelmReleaseStatus(w http.ResponseWriter, r *http.Request) {
	ns, name := r.PathValue("namespace"), r.PathValue("name")
	cfg, err := s.newHelmConfig(r.PathValue("ctx"), ns)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	rel, err := action.NewGet(cfg).Run(name)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	valuesYAML, err := yaml.Marshal(rel.Config)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	notes := ""
	if rel.Info != nil {
		notes = rel.Info.Notes
	}
	writeJSON(w, http.StatusOK, helmReleaseDetail{
		helmReleaseView: toHelmReleaseView(rel),
		Notes:           notes,
		Values:          string(valuesYAML),
	})
}

// handleHelmReleaseManifest: GET /api/contexts/{ctx}/helm/releases/{namespace}/{name}/manifest
func (s *Server) handleHelmReleaseManifest(w http.ResponseWriter, r *http.Request) {
	ns, name := r.PathValue("namespace"), r.PathValue("name")
	cfg, err := s.newHelmConfig(r.PathValue("ctx"), ns)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	rel, err := action.NewGet(cfg).Run(name)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"yaml": rel.Manifest})
}

// handleHelmReleaseHistory: GET /api/contexts/{ctx}/helm/releases/{namespace}/{name}/history
// Most recent revision first.
func (s *Server) handleHelmReleaseHistory(w http.ResponseWriter, r *http.Request) {
	ns, name := r.PathValue("namespace"), r.PathValue("name")
	cfg, err := s.newHelmConfig(r.PathValue("ctx"), ns)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	revs, err := action.NewHistory(cfg).Run(name)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	sort.Slice(revs, func(i, j int) bool { return revs[i].Version > revs[j].Version })
	out := make([]helmReleaseView, 0, len(revs))
	for _, rev := range revs {
		out = append(out, toHelmReleaseView(rev))
	}
	writeJSON(w, http.StatusOK, out)
}

// handleHelmReleaseRollback: POST /api/contexts/{ctx}/helm/releases/{namespace}/{name}/rollback
// body {"revision": N}
func (s *Server) handleHelmReleaseRollback(w http.ResponseWriter, r *http.Request) {
	ns, name := r.PathValue("namespace"), r.PathValue("name")
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var payload struct {
		Revision int `json:"revision"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if payload.Revision <= 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("revision must be a positive integer"))
		return
	}

	cfg, err := s.newHelmConfig(r.PathValue("ctx"), ns)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	rb := action.NewRollback(cfg)
	rb.Version = payload.Revision
	audit(r, "helm-rollback", "release", name, "namespace", ns, "revision", fmt.Sprintf("%d", payload.Revision))
	if err := rb.Run(name); err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "rolled-back"})
}

// handleHelmReleaseUninstall: DELETE /api/contexts/{ctx}/helm/releases/{namespace}/{name}
func (s *Server) handleHelmReleaseUninstall(w http.ResponseWriter, r *http.Request) {
	ns, name := r.PathValue("namespace"), r.PathValue("name")
	cfg, err := s.newHelmConfig(r.PathValue("ctx"), ns)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	audit(r, "helm-uninstall", "release", name, "namespace", ns)
	if _, err := action.NewUninstall(cfg).Run(name); err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "uninstalled"})
}
