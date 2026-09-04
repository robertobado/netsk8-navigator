package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"k8s.io/client-go/discovery"

	"github.com/robertobado/netsk8-navigator/backend/internal/kubeconfig"
)

// kubeconfigUnavailable writes a 501 and returns true when this server has
// no kubeconfig.Editor wired up (in-cluster mode, or the file couldn't be
// read at startup) — used to gate every /api/kubeconfig/* handler.
func (s *Server) kubeconfigUnavailable(w http.ResponseWriter) bool {
	if s.kcfg != nil {
		return false
	}
	writeError(w, http.StatusNotImplemented, fmt.Errorf("kubeconfig editing unavailable (no kubeconfig file — running with in-cluster credentials)"))
	return true
}

// reloadAfterWrite re-reads the kubeconfig into the live Manager so
// subsequent /api/contexts/* calls immediately reflect a write that already
// succeeded on disk. A failure here is logged but never turned into an HTTP
// error — the write itself is already durable; only the in-memory cache is
// stale, and it'll catch up on the next natural reload or restart.
func (s *Server) reloadAfterWrite() {
	if err := s.mgr.Reload(); err != nil {
		log.Printf("kubeconfig write succeeded but reloading the live context list failed: %v", err)
	}
}

func (s *Server) handleKubeconfigView(w http.ResponseWriter, r *http.Request) {
	if s.kubeconfigUnavailable(w) {
		return
	}
	view, err := s.kcfg.View()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleKubeconfigSetCurrentContext(w http.ResponseWriter, r *http.Request) {
	if s.kubeconfigUnavailable(w) {
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.kcfg.SetCurrentContext(body.Name); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.reloadAfterWrite()
	audit(r, "kubeconfig-set-current-context", "context", body.Name)
	writeJSON(w, http.StatusOK, map[string]string{"currentContext": body.Name})
}

func (s *Server) handleKubeconfigEditContext(w http.ResponseWriter, r *http.Request) {
	if s.kubeconfigUnavailable(w) {
		return
	}
	name := r.PathValue("name")
	var body struct {
		NewName   *string `json:"newName"`
		Namespace *string `json:"namespace"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.kcfg.EditContext(name, body.NewName, body.Namespace); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.reloadAfterWrite()
	audit(r, "kubeconfig-edit-context", "context", name)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleKubeconfigCreateContext(w http.ResponseWriter, r *http.Request) {
	if s.kubeconfigUnavailable(w) {
		return
	}
	var body struct {
		Name      string `json:"name"`
		Cluster   string `json:"cluster"`
		User      string `json:"user"`
		Namespace string `json:"namespace"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.kcfg.CreateContext(body.Name, body.Cluster, body.User, body.Namespace); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.reloadAfterWrite()
	audit(r, "kubeconfig-create-context", "context", body.Name)
	writeJSON(w, http.StatusCreated, map[string]bool{"ok": true})
}

func (s *Server) handleKubeconfigDeleteContext(w http.ResponseWriter, r *http.Request) {
	if s.kubeconfigUnavailable(w) {
		return
	}
	name := r.PathValue("name")
	orphanedCluster, orphanedUser, err := s.kcfg.DeleteContext(name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.reloadAfterWrite()
	audit(r, "kubeconfig-delete-context", "context", name)
	writeJSON(w, http.StatusOK, map[string]string{"orphanedCluster": orphanedCluster, "orphanedUser": orphanedUser})
}

func (s *Server) handleKubeconfigCreateUser(w http.ResponseWriter, r *http.Request) {
	if s.kubeconfigUnavailable(w) {
		return
	}
	var body struct {
		Name                  string `json:"name"`
		Token                 string `json:"token"`
		Username              string `json:"username"`
		Password              string `json:"password"`
		ClientCertificateData string `json:"clientCertificateData"`
		ClientKeyData         string `json:"clientKeyData"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	spec := kubeconfig.UserAuthSpec{
		Token: body.Token, Username: body.Username, Password: body.Password,
		ClientCertificateData: body.ClientCertificateData, ClientKeyData: body.ClientKeyData,
	}
	if err := s.kcfg.CreateUser(body.Name, spec); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.reloadAfterWrite()
	// Never the secret itself — same reasoning as every other kubeconfig
	// audit entry below (edit/delete-context, reveal-secret): name and
	// shape only.
	audit(r, "kubeconfig-create-user", "user", body.Name)
	writeJSON(w, http.StatusCreated, map[string]bool{"ok": true})
}

func (s *Server) handleKubeconfigEditUser(w http.ResponseWriter, r *http.Request) {
	if s.kubeconfigUnavailable(w) {
		return
	}
	name := r.PathValue("name")
	var body struct {
		NewName string `json:"newName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.kcfg.EditUser(name, body.NewName); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.reloadAfterWrite()
	audit(r, "kubeconfig-edit-user", "user", name)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleKubeconfigDeleteUser(w http.ResponseWriter, r *http.Request) {
	if s.kubeconfigUnavailable(w) {
		return
	}
	name := r.PathValue("name")
	if err := s.kcfg.DeleteUser(name); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.reloadAfterWrite()
	audit(r, "kubeconfig-delete-user", "user", name)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleKubeconfigImportPreview(w http.ResponseWriter, r *http.Request) {
	if s.kubeconfigUnavailable(w) {
		return
	}
	var body struct {
		YAML string `json:"yaml"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	preview, err := s.kcfg.PreviewImport([]byte(body.YAML))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

func (s *Server) handleKubeconfigImportCommit(w http.ResponseWriter, r *http.Request) {
	if s.kubeconfigUnavailable(w) {
		return
	}
	var body struct {
		YAML      string   `json:"yaml"`
		Overwrite []string `json:"overwrite"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.kcfg.CommitImport([]byte(body.YAML), body.Overwrite); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.reloadAfterWrite()
	audit(r, "kubeconfig-import-commit")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// revealFields are the only field names Editor.Reveal accepts — validated
// here too (not just inside Editor) so an invalid field never even reaches
// the audit log as if it were a real reveal.
var revealFields = map[string]bool{"token": true, "password": true, "clientKeyData": true, "clientCertificateData": true}

func (s *Server) handleKubeconfigRevealSecret(w http.ResponseWriter, r *http.Request) {
	if s.kubeconfigUnavailable(w) {
		return
	}
	name := r.PathValue("name")
	field := r.URL.Query().Get("field")
	if !revealFields[field] {
		writeError(w, http.StatusBadRequest, fmt.Errorf("unknown field %q", field))
		return
	}
	value, err := s.kcfg.Reveal(name, field)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	audit(r, "kubeconfig-reveal-secret", "user", name, "field", field)
	writeJSON(w, http.StatusOK, map[string]string{"value": value})
}

// handleKubeconfigPingContext does a short-timeout connectivity probe
// against a context — real value: kubeconfig entries silently go stale
// (rotated tokens, decommissioned clusters) with no way today to spot one
// short of hitting an error mid-task elsewhere in the app. A discovery
// client with its own Timeout is built here (rather than reusing
// ClientFor's cached clientset) specifically so a slow/unreachable server
// can't hang past 5s regardless of that clientset's own configuration.
func (s *Server) handleKubeconfigPingContext(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	restCfg, err := s.mgr.RESTConfigFor(name)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"reachable": false, "error": err.Error()})
		return
	}
	cfgCopy := *restCfg
	cfgCopy.Timeout = 5 * time.Second
	disco, err := discovery.NewDiscoveryClientForConfig(&cfgCopy)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"reachable": false, "error": err.Error()})
		return
	}
	start := time.Now()
	_, err = disco.ServerVersion()
	latencyMs := time.Since(start).Milliseconds()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"reachable": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"reachable": true, "latencyMs": latencyMs})
}
