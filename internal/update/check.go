// Package update provides self-update functionality for the ZCP binary.
// It checks GitHub releases for newer versions and can download + replace
// the running binary before the MCP server starts.
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	defaultGitHubAPI = "https://api.github.com/repos/zeropsio/zcp/releases/latest"
	defaultCacheTTL  = 24 * time.Hour
	checkTimeout     = 5 * time.Second
	cacheFileName    = "update.json"
	osWindows        = "windows"
)

// Info holds the result of an update check.
type Info struct {
	Available      bool
	CurrentVersion string
	LatestVersion  string
	DownloadURL    string
}

// Checker performs version checks against GitHub releases.
type Checker struct {
	CurrentVersion  string
	GitHubAPIURL    string
	DownloadBaseURL string // override download URL base (testing/private deployments)
	CacheDir        string
	CacheTTL        time.Duration
	HTTPClient      *http.Client
}

// NewChecker creates a Checker with default settings.
// If ZCP_UPDATE_URL is set, it overrides both the API and download base URLs.
func NewChecker(currentVersion string) *Checker {
	c := &Checker{
		CurrentVersion: currentVersion,
		GitHubAPIURL:   defaultGitHubAPI,
		CacheDir:       defaultCacheDir(),
		CacheTTL:       defaultCacheTTL,
		HTTPClient:     &http.Client{Timeout: checkTimeout},
	}
	if base := os.Getenv("ZCP_UPDATE_URL"); base != "" {
		c.GitHubAPIURL = base + "/repos/zeropsio/zcp/releases/latest"
		c.DownloadBaseURL = base + "/download"
	}
	return c
}

// Check looks for a newer ZCP version. Returns Info with Available=false
// on any error — the MCP server should always start regardless.
func (c *Checker) Check(ctx context.Context) *Info {
	info := &Info{CurrentVersion: c.CurrentVersion}

	// Try cache first.
	if cached, ok := c.readCache(); ok {
		info.LatestVersion = cached.LatestVersion
		info.DownloadURL = cached.DownloadURL
		info.Available = isNewer(c.CurrentVersion, cached.LatestVersion)
		return info
	}

	// Fetch from GitHub API.
	release, err := c.fetchLatestRelease(ctx)
	if err != nil {
		return info
	}

	info.LatestVersion = release.TagName
	if c.DownloadBaseURL != "" {
		info.DownloadURL = fmt.Sprintf("%s/%s/%s", c.DownloadBaseURL, release.TagName, assetName(runtime.GOOS, runtime.GOARCH))
	} else {
		info.DownloadURL = buildDownloadURL(release.TagName, runtime.GOOS, runtime.GOARCH)
	}
	info.Available = isNewer(c.CurrentVersion, release.TagName)

	// Write cache (best-effort).
	c.writeCache(cacheEntry{
		CheckedAt:     time.Now(),
		LatestVersion: release.TagName,
		DownloadURL:   info.DownloadURL,
	})

	return info
}

type githubRelease struct {
	TagName string `json:"tag_name"` //nolint:tagliatelle // GitHub API uses snake_case
}

type cacheEntry struct {
	CheckedAt     time.Time `json:"checkedAt"`
	LatestVersion string    `json:"latestVersion"`
	DownloadURL   string    `json:"downloadUrl"`
}

func (c *Checker) fetchLatestRelease(ctx context.Context) (*githubRelease, error) {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.GitHubAPIURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github API returned %d", resp.StatusCode)
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("decode release: %w", err)
	}
	return &release, nil
}

func (c *Checker) readCache() (*cacheEntry, bool) {
	if c.CacheTTL == 0 {
		return nil, false
	}

	data, err := os.ReadFile(c.cachePath())
	if err != nil {
		return nil, false
	}

	var entry cacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, false
	}

	if time.Since(entry.CheckedAt) > c.CacheTTL {
		return nil, false
	}

	return &entry, true
}

func (c *Checker) writeCache(entry cacheEntry) {
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	if err := os.MkdirAll(c.CacheDir, 0o755); err != nil {
		return
	}
	_ = os.WriteFile(c.cachePath(), data, 0o600)
}

func (c *Checker) cachePath() string {
	return filepath.Join(c.CacheDir, cacheFileName)
}

func defaultCacheDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "/" {
		return filepath.Join(os.TempDir(), "zcp-cache")
	}
	return filepath.Join(home, ".cache", "zcp")
}

// isNewer returns true if latest is newer than current.
// "dev" always needs an update.
func isNewer(current, latest string) bool {
	if current == "dev" {
		return true
	}
	return compareSemver(current, latest) < 0
}

// compareSemver compares two semver strings. Returns -1, 0, or 1.
// Strips leading "v" prefix. Non-parseable versions compare as 0.0.0.
func compareSemver(a, b string) int {
	am, ami, ap := parseSemver(a)
	bm, bmi, bp := parseSemver(b)

	if am != bm {
		return cmpInt(am, bm)
	}
	if ami != bmi {
		return cmpInt(ami, bmi)
	}
	return cmpInt(ap, bp)
}

func parseSemver(v string) (major, minor, patch int) {
	v = strings.TrimPrefix(v, "v")
	parts := strings.SplitN(v, ".", 3)
	if len(parts) >= 1 {
		major, _ = strconv.Atoi(parts[0])
	}
	if len(parts) >= 2 {
		minor, _ = strconv.Atoi(parts[1])
	}
	if len(parts) >= 3 {
		patch, _ = strconv.Atoi(parts[2])
	}
	return
}

func cmpInt(a, b int) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

// assetName returns the binary asset filename for a given OS/arch combination.
func assetName(goos, goarch string) string {
	if goos == osWindows && goarch == "amd64" {
		return "zcp-win-x64.exe"
	}
	return fmt.Sprintf("zcp-%s-%s", goos, goarch)
}

// buildDownloadURL constructs the GitHub release asset URL for a given tag and platform.
func buildDownloadURL(tag, goos, goarch string) string {
	return fmt.Sprintf("https://github.com/zeropsio/zcp/releases/download/%s/%s", tag, assetName(goos, goarch))
}
