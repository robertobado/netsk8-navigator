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
	s.mcpFlags.applyFromAppPrefs(raw)
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

// handleGetMCPToken: GET /api/mcp/token — the bearer token /mcp requires via
// the X-Netsk8-MCP-Token header (see mcp.go). Same trust boundary as
// everything else under /api/ already exposes (decoded Secret values,
// manifest edits) — no extra gating needed here.
func (s *Server) handleGetMCPToken(w http.ResponseWriter, r *http.Request) {
	token, err := s.cfg.MCPToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": token})
}

// handleRegenerateMCPToken: POST /api/mcp/token/regenerate — invalidates the
// current token, e.g. after a suspected leak. Any client (including the
// currently-connected GUI's own /mcp session) using the old token stops
// working until reconfigured with the new one.
func (s *Server) handleRegenerateMCPToken(w http.ResponseWriter, r *http.Request) {
	token, err := s.cfg.RegenerateMCPToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": token})
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
	_, _ = w.Write(raw) //nolint:gosec // application/json response, not rendered as HTML
}

type jsonError struct{}

func (jsonError) Error() string { return "invalid JSON body" }

var errInvalidJSON = jsonError{}
