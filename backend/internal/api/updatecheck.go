package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	updateCheckInterval = 24 * time.Hour
	latestReleaseURL    = "https://api.github.com/repos/robertobado/netsk8-navigator/releases/latest"
	releasesPageURL     = "https://github.com/robertobado/netsk8-navigator/releases/latest"
)

// updateCheckResult is what GET /api/update-check reports — always served
// from cache (updateChecker.result), never blocking the request on GitHub.
type updateCheckResult struct {
	Available bool   `json:"available"`
	Latest    string `json:"latest,omitempty"`
	URL       string `json:"url,omitempty"`
}

// updateChecker polls GitHub's latest-release endpoint once at startup and
// every updateCheckInterval after that, caching the result for
// handleUpdateCheck to serve instantly.
type updateChecker struct {
	mu     sync.RWMutex
	result updateCheckResult
}

// StartUpdateChecker launches the background poll loop. A no-op for a
// `go run .`/dev build (currentVersion == "dev" — see main.go's `version`
// var) since there's no meaningful "newer" to compare against and a
// developer restarting constantly shouldn't hammer GitHub's API.
func (s *Server) StartUpdateChecker(currentVersion string) {
	if currentVersion == "dev" {
		return
	}
	go func() {
		ctx := context.Background()
		s.runUpdateCheck(ctx, latestReleaseURL, currentVersion)
		ticker := time.NewTicker(updateCheckInterval)
		defer ticker.Stop()
		for range ticker.C {
			s.runUpdateCheck(ctx, latestReleaseURL, currentVersion)
		}
	}()
}

// runUpdateCheck fetches the latest release and caches the result. Network
// failures (offline machine, GitHub down, rate-limited) just leave the
// previous cached result in place — never worth surfacing as an error to
// the user for a background convenience check.
func (s *Server) runUpdateCheck(ctx context.Context, url, currentVersion string) {
	result, err := checkLatestRelease(ctx, url, currentVersion)
	if err != nil {
		return
	}
	s.updateChecker.mu.Lock()
	s.updateChecker.result = result
	s.updateChecker.mu.Unlock()
}

// handleUpdateCheck: GET /api/update-check
func (s *Server) handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	s.updateChecker.mu.RLock()
	result := s.updateChecker.result
	s.updateChecker.mu.RUnlock()
	writeJSON(w, http.StatusOK, result)
}

// checkLatestRelease asks GitHub (or, in tests, a stand-in server at url)
// for the latest (non-prerelease, non-draft — GitHub's own /releases/latest
// semantics) release and compares it against currentVersion.
func checkLatestRelease(ctx context.Context, url, currentVersion string) (updateCheckResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return updateCheckResult{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return updateCheckResult{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return updateCheckResult{}, fmt.Errorf("GitHub releases API returned %d", resp.StatusCode)
	}
	var body struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return updateCheckResult{}, err
	}
	latest := strings.TrimPrefix(body.TagName, "v")
	return updateCheckResult{
		Available: isNewerVersion(latest, strings.TrimPrefix(currentVersion, "v")),
		Latest:    latest,
		URL:       releasesPageURL,
	}, nil
}

// isNewerVersion reports whether latest > current, comparing dotted
// major.minor.patch numerically (not as strings, so "0.0.10" > "0.0.9").
// Anything that doesn't parse as three dot-separated integers (a
// pre-release suffix, a malformed tag) is treated as not-newer — fail safe
// to no update notification rather than a false positive.
func isNewerVersion(latest, current string) bool {
	l, ok := parseVersion(latest)
	if !ok {
		return false
	}
	c, ok := parseVersion(current)
	if !ok {
		return false
	}
	for i := range l {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

func parseVersion(v string) ([3]int, bool) {
	var out [3]int
	parts := strings.SplitN(v, ".", 3)
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, true
}
