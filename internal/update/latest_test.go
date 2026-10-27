// Tests for: internal/update — Checker.Latest, the newest published release
// as `zcp mate status` reads it (cache first, --refresh, --local), and
// Behind/IsRelease, which tell a release build that a restart would replace
// from a build nobody publishes.

package update

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestLatest_CacheRefreshAndCacheOnly_AnswersOrUnknown(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		cached    string        // tag a cache entry holds before the call ("" = no entry)
		cacheAge  time.Duration // how old that entry is
		sourceTag string        // tag the release source answers ("" = it fails)
		opts      LatestOptions
		want      string
		wantErr   bool
		wantCalls int32
	}{
		{"fresh cache answers without the network", "v9.226.0", time.Minute, "v9.227.0", LatestOptions{}, "v9.226.0", false, 0},
		{"stale cache asks the release source", "v9.226.0", 2 * time.Hour, "v9.227.0", LatestOptions{}, "v9.227.0", false, 1},
		{"no cache asks the release source", "", 0, "v9.227.0", LatestOptions{}, "v9.227.0", false, 1},
		{"refresh skips a fresh cache", "v9.226.0", time.Minute, "v9.227.0", LatestOptions{Refresh: true}, "v9.227.0", false, 1},
		{"refresh that fails keeps a fresh cache", "v9.226.0", time.Minute, "", LatestOptions{Refresh: true}, "v9.226.0", false, 1},
		{"refresh that fails with a stale cache is unknown", "v9.226.0", 2 * time.Hour, "", LatestOptions{Refresh: true}, "", true, 1},
		{"source failing with nothing cached is unknown", "", 0, "", LatestOptions{}, "", true, 1},
		{"cache only reads a fresh cache", "v9.226.0", time.Minute, "v9.227.0", LatestOptions{CacheOnly: true}, "v9.226.0", false, 0},
		{"cache only never reaches the network", "", 0, "v9.227.0", LatestOptions{CacheOnly: true}, "", true, 0},
		{"cache only ignores a stale cache", "v9.226.0", 2 * time.Hour, "v9.227.0", LatestOptions{CacheOnly: true}, "", true, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				if tt.sourceTag == "" {
					http.Error(w, "rate limited", http.StatusForbidden)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(githubRelease{TagName: tt.sourceTag})
			}))
			defer srv.Close()

			c := &Checker{
				CurrentVersion: "v9.220.0",
				GitHubAPIURL:   srv.URL,
				CacheDir:       t.TempDir(),
				CacheTTL:       time.Hour,
				HTTPClient:     srv.Client(),
			}
			if tt.cached != "" {
				c.writeCache(cacheEntry{CheckedAt: time.Now().Add(-tt.cacheAge), LatestVersion: tt.cached, DownloadURL: "https://example.com/cached"})
			}

			got, err := c.Latest(t.Context(), tt.opts)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Latest() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("Latest() = %q, want %q", got, tt.want)
			}
			if n := calls.Load(); n != tt.wantCalls {
				t.Errorf("release source asked %d times, want %d", n, tt.wantCalls)
			}
		})
	}
}

// A release Latest fetched is cached the way Check caches one, download URL
// included, so the self-update path reading the same file finds a usable
// entry, and the next status reads it without the network.
func TestLatest_Fetched_CachedForTheNextReader(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(githubRelease{TagName: "v9.226.0"})
	}))
	defer srv.Close()

	c := &Checker{
		CurrentVersion: "v9.220.0",
		GitHubAPIURL:   srv.URL,
		CacheDir:       t.TempDir(),
		CacheTTL:       time.Hour,
		HTTPClient:     srv.Client(),
	}
	if _, err := c.Latest(t.Context(), LatestOptions{}); err != nil {
		t.Fatalf("Latest() error = %v", err)
	}
	entry, ok := c.readCache()
	if !ok || entry.LatestVersion != "v9.226.0" || entry.DownloadURL == "" {
		t.Fatalf("cache = %+v (ok %v), want v9.226.0 with a download URL", entry, ok)
	}
	if info := c.Check(t.Context()); !info.Available || info.LatestVersion != "v9.226.0" || calls.Load() != 1 {
		t.Errorf("Check after Latest = %+v with %d calls, want the cached v9.226.0 and one call", info, calls.Load())
	}
}

func TestBehind_ReleaseAndDevBuilds_OnlyAnOlderReleaseIsBehind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		installed string
		latest    string
		want      bool
	}{
		{"older release is behind", "v9.220.0", "v9.226.0", true},
		{"equal release is not behind", "v9.226.0", "v9.226.0", false},
		{"newer release is not behind", "v9.227.0", "v9.226.0", false},
		{"v prefix is optional", "9.220.0", "v9.226.0", true},
		{"components compare as numbers", "v9.99.0", "v9.100.0", true},
		{"dev build is never behind", "dev", "v9.226.0", false},
		{"git-describe build is never behind", "v9.220.0-3-gabc1234-dirty", "v9.226.0", false},
		{"bare commit build is never behind", "abc1234", "v9.226.0", false},
		{"unknown latest is not behind", "v9.220.0", "", false},
		{"unparsable latest is not behind", "v9.220.0", "latest", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := Behind(tt.installed, tt.latest); got != tt.want {
				t.Errorf("Behind(%q, %q) = %v, want %v", tt.installed, tt.latest, got, tt.want)
			}
		})
	}
}

func TestIsRelease_VersionShapes_OnlyMajorMinorPatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		version string
		want    bool
	}{
		{"v9.226.0", true},
		{"9.226.0", true},
		{"dev", false},
		{"", false},
		{"v9.226", false},
		{"v9.226.0-rc.1", false},
		{"v9.220.0-3-gabc1234-dirty", false},
		{"abc1234", false},
		{"v9.x.0", false},
	}
	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			t.Parallel()
			if got := IsRelease(tt.version); got != tt.want {
				t.Errorf("IsRelease(%q) = %v, want %v", tt.version, got, tt.want)
			}
		})
	}
}
