// Package update provides self-update functionality for the ZCP binary.
// It reads the newest GitHub release from the releases/latest redirect, as
// install.sh does, and can download + replace the running binary before the
// MCP server starts.
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	defaultReleasesURL = "https://github.com/zeropsio/zcp/releases"
	checkTimeout       = 5 * time.Second
	lastReadFileName   = "update.json"
	osWindows          = "windows"
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
	CurrentVersion string
	// ReleasesURL is a GitHub releases page: <ReleasesURL>/latest redirects
	// to <ReleasesURL>/tag/<tag> of the newest release, and a release's
	// asset downloads from <ReleasesURL>/download/<tag>/<asset>. Neither is
	// the REST API, whose unauthenticated quota a shared egress spends.
	ReleasesURL string
	// RecordDir holds the record of the latest release last read, which only
	// LastReadLatest reads.
	RecordDir  string
	HTTPClient *http.Client
}

// NewChecker creates a Checker with default settings.
// ZCP_UPDATE_URL, when set, replaces the releases page (testing/private
// deployments).
func NewChecker(currentVersion string) *Checker {
	c := &Checker{
		CurrentVersion: currentVersion,
		ReleasesURL:    defaultReleasesURL,
		RecordDir:      defaultRecordDir(),
		HTTPClient:     &http.Client{Timeout: checkTimeout},
	}
	if base := os.Getenv("ZCP_UPDATE_URL"); base != "" {
		c.ReleasesURL = base
	}
	return c
}

// Check looks for a newer ZCP version, reading the release source on every
// call. An error means the latest release is unknown, never that the current
// version is the latest: each caller decides what an unknown verdict means
// for it.
func (c *Checker) Check(ctx context.Context) (*Info, error) {
	info := &Info{CurrentVersion: c.CurrentVersion}

	tag, err := c.fetchLatestTag(ctx)
	if err != nil {
		return nil, fmt.Errorf("check latest release: %w", err)
	}

	info.LatestVersion = tag
	info.DownloadURL = c.downloadURL(tag)
	info.Available = isNewer(c.CurrentVersion, tag)
	return info, nil
}

// downloadURL is where this platform's binary of release tag downloads from.
func (c *Checker) downloadURL(tag string) string {
	return c.assetURL(tag, assetName(runtime.GOOS, runtime.GOARCH))
}

// assetURL is where asset of release tag downloads from.
func (c *Checker) assetURL(tag, asset string) string {
	return fmt.Sprintf("%s/download/%s/%s", c.ReleasesURL, tag, asset)
}

// LatestAssetURL is the download URL of asset in the newest published
// release, "" when that release does not ship it.
func (c *Checker) LatestAssetURL(ctx context.Context, asset string) (string, error) {
	tag, err := c.fetchLatestTag(ctx)
	if err != nil {
		return "", err
	}
	u := c.assetURL(tag, asset)
	status, _, err := c.head(ctx, u)
	if err != nil {
		return "", fmt.Errorf("probe release asset: %w", err)
	}
	switch {
	case status == http.StatusNotFound:
		return "", nil
	case status < http.StatusBadRequest:
		return u, nil
	}
	return "", fmt.Errorf("release asset %s answered %d", u, status)
}

// lastRead is the record of the latest release last read.
type lastRead struct {
	LatestVersion string `json:"latestVersion"`
}

// fetchLatestTag is the tag <ReleasesURL>/latest redirects to, recorded for
// LastReadLatest.
func (c *Checker) fetchLatestTag(ctx context.Context) (string, error) {
	status, location, err := c.head(ctx, c.ReleasesURL+"/latest")
	if err != nil {
		return "", fmt.Errorf("fetch release: %w", err)
	}
	if status < 300 || status >= 400 {
		return "", fmt.Errorf("releases/latest answered %d, not a redirect", status)
	}
	loc, err := url.Parse(location)
	if err != nil {
		return "", fmt.Errorf("releases/latest redirect: %w", err)
	}
	dir, tag := path.Split(loc.Path)
	if path.Base(dir) != "tag" || tag == "" {
		return "", fmt.Errorf("releases/latest redirects to %s, which names no release tag", loc)
	}
	c.writeLastRead(tag)
	return tag, nil
}

// head is the status and Location header a HEAD request for target answers,
// without following a redirect: a redirect is the answer itself.
func (c *Checker) head(ctx context.Context, target string) (int, string, error) {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, target, nil)
	if err != nil {
		return 0, "", fmt.Errorf("create request: %w", err)
	}
	client := *c.HTTPClient
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return 0, "", fmt.Errorf("head %s: %w", target, err)
	}
	defer resp.Body.Close()
	return resp.StatusCode, resp.Header.Get("Location"), nil
}

// LastReadLatest is the latest release the source last named, without
// network IO. Only `zcp mate status --local` answers from it, for a reader
// that must not wait on the network; no decision reads it. A record older
// than this zcp was read before this zcp was installed, so it says nothing
// about the latest release and reads as none.
func (c *Checker) LastReadLatest() (string, bool) {
	data, err := os.ReadFile(c.lastReadPath())
	if err != nil {
		return "", false
	}
	var entry lastRead
	if err := json.Unmarshal(data, &entry); err != nil || entry.LatestVersion == "" {
		return "", false
	}
	if compareSemver(entry.LatestVersion, c.CurrentVersion) < 0 {
		return "", false
	}
	return entry.LatestVersion, true
}

// writeLastRead records tag for LastReadLatest, best effort: written under a
// temporary name of its own and renamed, so a reader sees the old record or
// the new one, never a part of either.
func (c *Checker) writeLastRead(tag string) {
	data, err := json.Marshal(lastRead{LatestVersion: tag})
	if err != nil {
		return
	}
	if err := os.MkdirAll(c.RecordDir, 0o755); err != nil {
		return
	}
	tmp, err := os.CreateTemp(c.RecordDir, lastReadFileName+".*")
	if err != nil {
		return
	}
	_, err = tmp.Write(data)
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), c.lastReadPath())
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
	}
}

func (c *Checker) lastReadPath() string {
	return filepath.Join(c.RecordDir, lastReadFileName)
}

func defaultRecordDir() string {
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
