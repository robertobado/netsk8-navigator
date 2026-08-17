package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseVersion(t *testing.T) {
	tests := []struct {
		in   string
		want [3]int
		ok   bool
	}{
		{"1.2.3", [3]int{1, 2, 3}, true},
		{"0.0.10", [3]int{0, 0, 10}, true},
		{"1.2", [3]int{}, false},
		{"1.2.3-rc1", [3]int{}, false},
		{"not-a-version", [3]int{}, false},
	}
	for _, tt := range tests {
		got, ok := parseVersion(tt.in)
		if ok != tt.ok || (ok && got != tt.want) {
			t.Errorf("parseVersion(%q) = %v, %v; want %v, %v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

func TestIsNewerVersion(t *testing.T) {
	tests := []struct {
		latest, current string
		want            bool
	}{
		{"0.0.5", "0.0.4", true},
		{"0.0.4", "0.0.4", false},
		{"0.0.4", "0.0.5", false},
		{"0.0.10", "0.0.9", true},     // numeric, not lexicographic
		{"1.0.0", "0.99.99", true},    // major wins regardless of minor/patch
		{"1.2.3-rc1", "1.2.2", false}, // unparseable latest fails safe to false
	}
	for _, tt := range tests {
		if got := isNewerVersion(tt.latest, tt.current); got != tt.want {
			t.Errorf("isNewerVersion(%q, %q) = %v, want %v", tt.latest, tt.current, got, tt.want)
		}
	}
}

func newLatestReleaseServer(t *testing.T, tagName string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"tag_name": tagName})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestCheckLatestRelease(t *testing.T) {
	t.Run("newer release available", func(t *testing.T) {
		srv := newLatestReleaseServer(t, "v0.0.5")
		got, err := checkLatestRelease(context.Background(), srv.URL, "0.0.4")
		if err != nil {
			t.Fatal(err)
		}
		if !got.Available || got.Latest != "0.0.5" || got.URL == "" {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("already on latest", func(t *testing.T) {
		srv := newLatestReleaseServer(t, "v0.0.5")
		got, err := checkLatestRelease(context.Background(), srv.URL, "0.0.5")
		if err != nil {
			t.Fatal(err)
		}
		if got.Available {
			t.Errorf("got %+v, want Available=false", got)
		}
	})

	t.Run("non-200 response is an error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		t.Cleanup(srv.Close)
		if _, err := checkLatestRelease(context.Background(), srv.URL, "0.0.4"); err == nil {
			t.Error("expected an error for a non-200 response")
		}
	})

	t.Run("malformed request URL is an error", func(t *testing.T) {
		if _, err := checkLatestRelease(context.Background(), "http://[::1]:namedport", "0.0.4"); err == nil {
			t.Error("expected an error building the request for an invalid URL")
		}
	})

	t.Run("unreachable server is an error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		url := srv.URL
		srv.Close() // now nothing is listening — client.Do should fail
		if _, err := checkLatestRelease(context.Background(), url, "0.0.4"); err == nil {
			t.Error("expected an error for an unreachable server")
		}
	})

	t.Run("malformed JSON body is an error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("not json"))
		}))
		t.Cleanup(srv.Close)
		if _, err := checkLatestRelease(context.Background(), srv.URL, "0.0.4"); err == nil {
			t.Error("expected an error for a malformed JSON response body")
		}
	})
}

func TestRunUpdateCheck_SuccessUpdatesCachedResult(t *testing.T) {
	s := newTestServer(t)
	srv := newLatestReleaseServer(t, "v0.0.9")
	s.runUpdateCheck(context.Background(), srv.URL, "0.0.4")
	if !s.updateChecker.result.Available || s.updateChecker.result.Latest != "0.0.9" {
		t.Errorf("got %+v, want the fetched result cached", s.updateChecker.result)
	}
}

func TestHandleUpdateCheck(t *testing.T) {
	s := newTestServer(t)
	s.updateChecker.result = updateCheckResult{Available: true, Latest: "0.0.5", URL: "https://example.com"}

	rec := doRequest(t, s, "GET", "/api/update-check", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var out updateCheckResult
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !out.Available || out.Latest != "0.0.5" {
		t.Errorf("got %+v", out)
	}
}

func TestRunUpdateCheck_NetworkFailureKeepsPreviousResult(t *testing.T) {
	s := newTestServer(t)
	s.updateChecker.result = updateCheckResult{Available: true, Latest: "0.0.5", URL: "https://example.com"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	s.runUpdateCheck(context.Background(), srv.URL, "0.0.4")
	if s.updateChecker.result.Latest != "0.0.5" {
		t.Errorf("expected the previous cached result to survive a failed check, got %+v", s.updateChecker.result)
	}
}
