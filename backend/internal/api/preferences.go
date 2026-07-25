package api

import (
	"encoding/json"
	"io"
	"net/http"
)

// handleGetAppPrefs: GET /api/preferences
func (s *Server) handleGetAppPrefs(w http.ResponseWriter, r *http.Request) {
	writeRaw(w, s.cfg.App())
}

// handlePutAppPrefs: PUT /api/preferences  (body = the full app prefs JSON)
func (s *Server) handlePutAppPrefs(w http.ResponseWriter, r *http.Request) {
	raw, err := readJSONBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.cfg.SetApp(raw); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeRaw(w, raw)
}

// handleGetClusterPrefs: GET /api/contexts/{ctx}/preferences
func (s *Server) handleGetClusterPrefs(w http.ResponseWriter, r *http.Request) {
	writeRaw(w, s.cfg.Cluster(r.PathValue("ctx")))
}

// handlePutClusterPrefs: PUT /api/contexts/{ctx}/preferences
func (s *Server) handlePutClusterPrefs(w http.ResponseWriter, r *http.Request) {
	raw, err := readJSONBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.cfg.SetCluster(r.PathValue("ctx"), raw); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeRaw(w, raw)
}

// readJSONBody reads and validates a JSON request body (≤256 KiB).
func readJSONBody(r *http.Request) (json.RawMessage, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 256<<10))
	if err != nil {
		return nil, err
	}
	if !json.Valid(body) {
		return nil, errInvalidJSON
	}
	return json.RawMessage(body), nil
}

func writeRaw(w http.ResponseWriter, raw json.RawMessage) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

type jsonError struct{}

func (jsonError) Error() string { return "invalid JSON body" }

var errInvalidJSON = jsonError{}
