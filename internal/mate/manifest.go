package mate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	// SupportedContract is the zcp<->mate contract number this build drives
	// (spec-mate.md §2.8, C-1...C-6). A manifest declaring a different
	// number is refused before any download — the one case where an old zcp
	// deliberately stays on the mate it has.
	SupportedContract = 1

	// MinimumMateVersion is the oldest mate release this zcp still drives.
	// It moves only when a contract fact changes, never for a routine
	// release.
	MinimumMateVersion = "0.8.1"

	// defaultManifestURL is GitHub's own "latest release" redirect for the
	// fork's stable release asset. The fork's release workflow triggers only
	// on stable v<major>.<minor>.<patch> tags, so this is the stable
	// channel — a nightly never produces a "latest release".
	defaultManifestURL = "https://github.com/zeropsio/mate/releases/latest/download/stable.json"

	// manifestURLEnvOverride lets a test or a private deploy point
	// DesiredRelease at a different manifest.
	manifestURLEnvOverride = "ZCP_MATE_MANIFEST_URL"

	// manifestFetchTimeout bounds the manifest GET — one small JSON file,
	// nowhere near installTimeout's budget for the tarball + npm install.
	manifestFetchTimeout = 5 * time.Second

	// manifestCacheTTL bounds how long a fetched-and-validated manifest is
	// trusted without asking again — a warm restart and `zcp mate status`
	// reach the network at most this often.
	manifestCacheTTL = time.Hour

	manifestCacheFileName = "manifest.json"
)

// Manifest names one installable mate release, as published in the fork's
// stable.json release asset (spec-mate.md §2.1c).
type Manifest struct {
	Version     string    `json:"version"`
	Asset       string    `json:"asset"`
	URL         string    `json:"url"`
	SHA256      string    `json:"sha256"`
	Size        int64     `json:"size"`
	Contract    int       `json:"contract"`
	PublishedAt time.Time `json:"publishedAt"`
}

// ManifestURL is where DesiredRelease fetches the release manifest from.
// ZCP_MATE_MANIFEST_URL overrides it — tests and private deploys.
func ManifestURL() string {
	if v := strings.TrimSpace(os.Getenv(manifestURLEnvOverride)); v != "" {
		return v
	}
	return defaultManifestURL
}

// manifestCachePath is where DesiredRelease caches the last fetched,
// already-validated manifest: ~/.zcp/mate/manifest.json, the same shape as
// zcp's own internal/update cache (a fetch timestamp plus the payload,
// TTL-gated).
func manifestCachePath() string { return filepath.Join(Prefix(), manifestCacheFileName) }

type manifestCacheEntry struct {
	FetchedAt time.Time `json:"fetchedAt"`
	Manifest  Manifest  `json:"manifest"`
}

// ManifestOptions steers DesiredRelease's one behavioural choice: whether
// the on-disk cache may answer instead of a network fetch.
type ManifestOptions struct {
	// Refresh bypasses the cache and always fetches fresh. `zcp mate update`
	// always sets it; `zcp init` and `zcp mate status` do not, so a warm
	// restart or a routine status check reach the network at most hourly.
	Refresh bool
}

// ContractMismatchError reports a manifest declaring a contract number this
// zcp build does not know — refused before any download (spec-mate.md
// §2.1c, §2.8).
type ContractMismatchError struct {
	Manifest  int
	Supported int
}

func (e *ContractMismatchError) Error() string {
	return fmt.Sprintf("mate release manifest declares contract %d, this zcp build supports contract %d", e.Manifest, e.Supported)
}

// VersionTooOldError reports a manifest version below MinimumMateVersion,
// the oldest release this zcp still drives.
type VersionTooOldError struct {
	Version string
	Minimum string
}

func (e *VersionTooOldError) Error() string {
	return fmt.Sprintf("mate release manifest version %s is older than the minimum this zcp drives (%s)", e.Version, e.Minimum)
}

var sha256HexPattern = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)

// validateManifest checks a freshly fetched manifest before it is trusted or
// cached (spec-mate.md §2.1c "what zcp checks, and what it does not"): the
// contract this build knows, a version floor, a well-formed digest, and an
// https URL. There is no deeper trust chain than this — see the spec section
// for why.
func validateManifest(m Manifest) error {
	if m.Contract != SupportedContract {
		return &ContractMismatchError{Manifest: m.Contract, Supported: SupportedContract}
	}
	cmp, err := compareSemver(m.Version, MinimumMateVersion)
	if err != nil {
		return fmt.Errorf("mate release manifest: %w", err)
	}
	if cmp < 0 {
		return &VersionTooOldError{Version: m.Version, Minimum: MinimumMateVersion}
	}
	if !sha256HexPattern.MatchString(m.SHA256) {
		return fmt.Errorf("mate release manifest: sha256 %q is not 64 hex characters", m.SHA256)
	}
	if !strings.HasPrefix(m.URL, "https://") {
		return fmt.Errorf("mate release manifest: url %q is not https", m.URL)
	}
	return nil
}

// DesiredRelease is the release EnsureInstalled converges this container
// toward: the fork's current stable release, read from ManifestURL()
// (spec-mate.md §2.1c) rather than a version compiled into zcp. There is no
// registry-package fallback — an unreachable or invalid manifest is
// returned as an error and the caller decides how to degrade (MD-10).
func DesiredRelease(ctx context.Context, client *http.Client, opts ManifestOptions) (Manifest, error) {
	if !opts.Refresh {
		if cached, ok := readManifestCache(); ok {
			return cached, nil
		}
	}

	if client == nil {
		client = http.DefaultClient
	}
	fetchCtx, cancel := context.WithTimeout(ctx, manifestFetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, ManifestURL(), nil)
	if err != nil {
		return Manifest{}, fmt.Errorf("build mate manifest request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return Manifest{}, fmt.Errorf("fetch mate release manifest: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return Manifest{}, fmt.Errorf("fetch mate release manifest: HTTP %s", resp.Status)
	}

	var m Manifest
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return Manifest{}, fmt.Errorf("parse mate release manifest: %w", err)
	}

	if err := validateManifest(m); err != nil {
		return Manifest{}, err
	}

	writeManifestCache(m)
	return m, nil
}

func readManifestCache() (Manifest, bool) {
	data, err := os.ReadFile(manifestCachePath())
	if err != nil {
		return Manifest{}, false
	}
	var entry manifestCacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return Manifest{}, false
	}
	if time.Since(entry.FetchedAt) > manifestCacheTTL {
		return Manifest{}, false
	}
	return entry.Manifest, true
}

func writeManifestCache(m Manifest) {
	data, err := json.Marshal(manifestCacheEntry{FetchedAt: time.Now(), Manifest: m})
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(manifestCachePath()), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(manifestCachePath(), data, 0o600)
}

// semver holds a parsed plain "major.minor.patch" version.
type semver struct {
	major, minor, patch int
}

// compare returns -1, 0 or 1 for s relative to other.
func (s semver) compare(other semver) int {
	switch {
	case s.major != other.major:
		return cmpInt(s.major, other.major)
	case s.minor != other.minor:
		return cmpInt(s.minor, other.minor)
	default:
		return cmpInt(s.patch, other.patch)
	}
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// compareSemver compares two plain "major.minor.patch" semver strings —
// never a "v" prefix, never a prerelease suffix, since the fork's release
// workflow publishes stable.json only for stable v<major>.<minor>.<patch>
// tags. Returns -1, 0 or 1; an error names whichever side fails to parse.
func compareSemver(a, b string) (int, error) {
	am, err := parseSemver(a)
	if err != nil {
		return 0, fmt.Errorf("version %q: %w", a, err)
	}
	bm, err := parseSemver(b)
	if err != nil {
		return 0, fmt.Errorf("version %q: %w", b, err)
	}
	return am.compare(bm), nil
}

// VersionOlder reports whether installed names a strictly older release
// than latest, by plain "major.minor.patch" comparison — what `zcp mate
// status` answers updateAvailable from. A version that fails to parse on
// either side answers false rather than guessing.
func VersionOlder(installed, latest string) bool {
	cmp, err := compareSemver(installed, latest)
	return err == nil && cmp < 0
}

func parseSemver(v string) (semver, error) {
	parts := strings.SplitN(v, ".", 3)
	if len(parts) != 3 {
		return semver{}, fmt.Errorf("not major.minor.patch")
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil || major < 0 {
		return semver{}, fmt.Errorf("component %q is not a non-negative integer", parts[0])
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil || minor < 0 {
		return semver{}, fmt.Errorf("component %q is not a non-negative integer", parts[1])
	}
	patch, err := strconv.Atoi(parts[2])
	if err != nil || patch < 0 {
		return semver{}, fmt.Errorf("component %q is not a non-negative integer", parts[2])
	}
	return semver{major: major, minor: minor, patch: patch}, nil
}
