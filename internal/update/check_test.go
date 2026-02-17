// Tests for: internal/update/check.go — version check and cache logic.

package update

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCheck_NewerVersionAvailable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		currentVersion string
		latestTag      string
		wantAvailable  bool
	}{
		{
			name:           "newer version available",
			currentVersion: "0.1.0",
			latestTag:      "v0.2.0",
			wantAvailable:  true,
		},
		{
			name:           "already latest",
			currentVersion: "0.2.0",
			latestTag:      "v0.2.0",
			wantAvailable:  false,
		},
		{
			name:           "current newer than remote",
			currentVersion: "0.3.0",
			latestTag:      "v0.2.0",
			wantAvailable:  false,
		},
		{
			name:           "dev version always updates",
			currentVersion: "dev",
			latestTag:      "v0.1.0",
			wantAvailable:  true,
		},
		{
			name:           "current with v prefix",
			currentVersion: "v0.2.0",
			latestTag:      "v0.2.0",
			wantAvailable:  false,
		},
		{
			name:           "patch update",
			currentVersion: "0.2.0",
			latestTag:      "v0.2.1",
			wantAvailable:  true,
		},
		{
			name:           "major update",
			currentVersion: "1.0.0",
			latestTag:      "v2.0.0",
			wantAvailable:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				resp := githubRelease{TagName: tt.latestTag}
				w.Header().Set("Content-Type", "application/json")
				if err := json.NewEncoder(w).Encode(resp); err != nil {
					t.Errorf("encode response: %v", err)
				}
			}))
			defer srv.Close()

			cacheDir := t.TempDir()
			c := &Checker{
				CurrentVersion: tt.currentVersion,
				GitHubAPIURL:   srv.URL,
				CacheDir:       cacheDir,
				CacheTTL:       0, // force fresh check
				HTTPClient:     srv.Client(),
			}

			info := c.Check(t.Context())
			if info.Available != tt.wantAvailable {
				t.Errorf("Available = %v, want %v", info.Available, tt.wantAvailable)
			}
			if tt.wantAvailable && info.LatestVersion == "" {
				t.Error("expected LatestVersion to be set")
			}
		})
	}
}

func TestCheck_CacheRespected(t *testing.T) {
	t.Parallel()

	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		resp := githubRelease{TagName: "v0.2.0"}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer srv.Close()

	cacheDir := t.TempDir()
	c := &Checker{
		CurrentVersion: "0.1.0",
		GitHubAPIURL:   srv.URL,
		CacheDir:       cacheDir,
		CacheTTL:       time.Hour,
		HTTPClient:     srv.Client(),
	}

	// First check hits API.
	info1 := c.Check(t.Context())
	if !info1.Available {
		t.Fatal("first check should find update")
	}
	if calls != 1 {
		t.Fatalf("expected 1 API call, got %d", calls)
	}

	// Second check uses cache.
	info2 := c.Check(t.Context())
	if !info2.Available {
		t.Fatal("cached check should still find update")
	}
	if calls != 1 {
		t.Fatalf("expected still 1 API call (cached), got %d", calls)
	}
}

func TestCheck_CacheExpired(t *testing.T) {
	t.Parallel()

	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		resp := githubRelease{TagName: "v0.2.0"}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer srv.Close()

	cacheDir := t.TempDir()

	// Write an expired cache entry.
	entry := cacheEntry{
		CheckedAt:     time.Now().Add(-2 * time.Hour),
		LatestVersion: "v0.1.5",
		DownloadURL:   "https://example.com/old",
	}
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, cacheFileName), data, 0o644); err != nil {
		t.Fatal(err)
	}

	c := &Checker{
		CurrentVersion: "0.1.0",
		GitHubAPIURL:   srv.URL,
		CacheDir:       cacheDir,
		CacheTTL:       time.Hour,
		HTTPClient:     srv.Client(),
	}

	info := c.Check(t.Context())
	if !info.Available {
		t.Fatal("should find update after cache expiry")
	}
	if calls != 1 {
		t.Fatalf("expected 1 API call after cache expiry, got %d", calls)
	}
	if info.LatestVersion != "v0.2.0" {
		t.Errorf("expected v0.2.0, got %s", info.LatestVersion)
	}
}

func TestCheck_NetworkError_GracefulFallback(t *testing.T) {
	t.Parallel()

	c := &Checker{
		CurrentVersion: "0.1.0",
		GitHubAPIURL:   "http://127.0.0.1:1", // connection refused
		CacheDir:       t.TempDir(),
		CacheTTL:       0,
		HTTPClient:     &http.Client{Timeout: time.Second},
	}

	info := c.Check(t.Context())
	if info.Available {
		t.Error("should not report available on network error")
	}
}

func TestCheck_InvalidJSON_GracefulFallback(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	c := &Checker{
		CurrentVersion: "0.1.0",
		GitHubAPIURL:   srv.URL,
		CacheDir:       t.TempDir(),
		CacheTTL:       0,
		HTTPClient:     srv.Client(),
	}

	info := c.Check(t.Context())
	if info.Available {
		t.Error("should not report available on invalid JSON")
	}
}

func TestCompareSemver(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		current string
		latest  string
		want    int // -1 = current < latest, 0 = equal, 1 = current > latest
	}{
		{"equal", "0.1.0", "0.1.0", 0},
		{"patch newer", "0.1.0", "0.1.1", -1},
		{"minor newer", "0.1.0", "0.2.0", -1},
		{"major newer", "0.1.0", "1.0.0", -1},
		{"current newer", "0.2.0", "0.1.0", 1},
		{"with v prefix", "v0.1.0", "v0.1.0", 0},
		{"mixed prefix", "0.1.0", "v0.1.0", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := compareSemver(tt.current, tt.latest)
			if got != tt.want {
				t.Errorf("compareSemver(%q, %q) = %d, want %d", tt.current, tt.latest, got, tt.want)
			}
		})
	}
}

func TestDownloadURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		tag  string
		goos string
		arch string
		want string
	}{
		{
			name: "darwin arm64",
			tag:  "v0.2.0",
			goos: "darwin",
			arch: "arm64",
			want: "https://github.com/zeropsio/zcp/releases/download/v0.2.0/zcp-darwin-arm64",
		},
		{
			name: "linux amd64",
			tag:  "v0.2.0",
			goos: "linux",
			arch: "amd64",
			want: "https://github.com/zeropsio/zcp/releases/download/v0.2.0/zcp-linux-amd64",
		},
		{
			name: "linux 386",
			tag:  "v0.2.0",
			goos: "linux",
			arch: "386",
			want: "https://github.com/zeropsio/zcp/releases/download/v0.2.0/zcp-linux-386",
		},
		{
			name: "windows amd64",
			tag:  "v0.2.0",
			goos: "windows",
			arch: "amd64",
			want: "https://github.com/zeropsio/zcp/releases/download/v0.2.0/zcp-win-x64.exe",
		},
		{
			name: "darwin amd64",
			tag:  "v0.2.0",
			goos: "darwin",
			arch: "amd64",
			want: "https://github.com/zeropsio/zcp/releases/download/v0.2.0/zcp-darwin-amd64",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := buildDownloadURL(tt.tag, tt.goos, tt.arch)
			if got != tt.want {
				t.Errorf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestAssetName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		goos string
		arch string
		want string
	}{
		{"linux amd64", "linux", "amd64", "zcp-linux-amd64"},
		{"darwin arm64", "darwin", "arm64", "zcp-darwin-arm64"},
		{"windows amd64", "windows", "amd64", "zcp-win-x64.exe"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := assetName(tt.goos, tt.arch)
			if got != tt.want {
				t.Errorf("assetName(%s, %s) = %q, want %q", tt.goos, tt.arch, got, tt.want)
			}
		})
	}
}

func TestCheck_DownloadBaseURL(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := githubRelease{TagName: "v0.3.0"}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer srv.Close()

	c := &Checker{
		CurrentVersion:  "0.1.0",
		GitHubAPIURL:    srv.URL,
		CacheDir:        t.TempDir(),
		CacheTTL:        0,
		HTTPClient:      srv.Client(),
		DownloadBaseURL: srv.URL + "/download",
	}

	info := c.Check(t.Context())
	if !info.Available {
		t.Fatal("should find update")
	}

	wantPrefix := srv.URL + "/download/v0.3.0/zcp-"
	if !strings.HasPrefix(info.DownloadURL, wantPrefix) {
		t.Errorf("DownloadURL = %q, want prefix %q", info.DownloadURL, wantPrefix)
	}
}

func TestNewChecker_ZCPUpdateURL(t *testing.T) {
	t.Setenv("ZCP_UPDATE_URL", "http://mock.local")

	c := NewChecker("0.1.0")
	if c.GitHubAPIURL != "http://mock.local/repos/zeropsio/zcp/releases/latest" {
		t.Errorf("GitHubAPIURL = %q, want mock URL", c.GitHubAPIURL)
	}
	if c.DownloadBaseURL != "http://mock.local/download" {
		t.Errorf("DownloadBaseURL = %q, want mock download URL", c.DownloadBaseURL)
	}
}
