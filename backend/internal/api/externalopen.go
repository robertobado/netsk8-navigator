package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// SetExternalOpener installs the desktop app's native "open this URL in the
// user's real default browser" hook — Wails' runtime.BrowserOpenURL, called
// from Go, never from JS (see appevents.go's doc comment for why the JS
// bridge isn't an option here). Left nil in the plain server/browser binary,
// where POST /api/open-external always 501s and the frontend's
// openExternal() falls back to window.open, exactly like a normal web page.
func (s *Server) SetExternalOpener(f func(url string)) {
	s.opener = f
}

func (s *Server) handleOpenExternal(w http.ResponseWriter, r *http.Request) {
	if s.opener == nil {
		writeError(w, http.StatusNotImplemented, fmt.Errorf("no native browser opener available (plain server/browser build)"))
		return
	}
	var body struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	parsed, err := url.Parse(body.URL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		writeError(w, http.StatusBadRequest, fmt.Errorf("url must be an absolute http(s) URL"))
		return
	}
	s.opener(body.URL)
	audit(r, "open-external", "url", body.URL)
	w.WriteHeader(http.StatusNoContent)
}
