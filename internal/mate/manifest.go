package mate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

const (
	// SupportedContract is the zcp<->mate contract number this build drives
	// (spec-mate.md §2.8, C-1...C-6). A manifest declaring a different
	// number is refused before any download — the one case where an old zcp
	// deliberately stays on the mate it has. Contract 2 is the project key:
	// the Mate links to HQ with it and keeps its Git credential in a file
	// (hq.EnvGitCredential) instead of an enrollment.
	SupportedContract = 2

	// MinimumMateVersion is the oldest mate release this zcp still drives.
	// It moves only when a contract fact changes, never for a routine
	// release. 0.14.0 is the first release that judges a stand-up by its
	// zcp process (PID + start) instead of the heartbeat this zcp no longer
	// writes; it also keeps a link to HQ, so zcp treats a Mate as HQ's
	// whatever its link. 0.15.119 is the first release
	// that accepts the envelope's stages[] and devServers[] (field-order
	// step 2, PartsMateVersion). 0.15.153 is the first release that draws
	// zerops_origin's ask and tells the agent how it ended: an older one
	// shows a plain call, and the agent waits on a card it never draws.
	// 0.16.0 is the first contract-2 release: it links with the project key
	// and its package declares the HQ origin. 0.16.40 still lacks Observe
	// credential delivery; 0.16.41 is required for the frame receiver and
	// file writer used by strict HQ reads. 0.16.54 is the first rev 7
	// release: it answers HQ's tokened tickets; an older Mate is refused
	// every ticket by rev 7 HQ. 0.16.154 keeps the link's Mate state in the
	// file zcp reads (§2.8). 0.16.195 is the first release that keeps no
	// member list: it makes no member search at Zerops and sends HQ no
	// person hint (docs/spec-mate.md §3.9), and no longer passes --refresh.
	MinimumMateVersion = "0.16.195"

	// PartsMateVersion is the first mate release that accepts the envelope's
	// stages[] and devServers[] (docs/spec-mate.md §2.8a.3, field-order
	// step 1). zcp sends them only once MinimumMateVersion reaches it
	// (step 2) — EnvelopeCarriesParts.
	PartsMateVersion = "0.15.119"

	// defaultManifestURL is GitHub's own "latest release" redirect for the
	// fork's stable release asset. The fork's release workflow triggers only
	// on stable v<major>.<minor>.<patch> tags, so this is the stable
	// channel — a nightly never produces a "latest release".
	defaultManifestURL = "https://github.com/zeropsio/mate/releases/latest/download/stable.json"

	// manifestURLEnvOverride lets a test or a private deploy point
	// DesiredRelease at a different manifest.
	manifestURLEnvOverride = "ZCP_MATE_MANIFEST_URL"

	// manifestFetchTimeout bounds the manifest GET — one small JSON file,
	// nowhere near InstallTimeout's budget for the tarball + npm install.
	manifestFetchTimeout = 5 * time.Second

	lastReadFileName = "manifest.json"
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
// ZCP_MATE_MANIFEST_URL overrides it — tests and private deploys — read
// through the live env store (LiveLookup), so status, the start job and the
// keep job read one value whatever environment systemd or the server gave them.
func ManifestURL() string {
	if v := strings.TrimSpace(LiveLookup(LiveEnvStorePath)(manifestURLEnvOverride)); v != "" {
		return v
	}
	return defaultManifestURL
}

// lastReadPath is where DesiredRelease records the manifest it last read and
// validated: ~/.zcp/mate/manifest.json. Only LastReadRelease reads it.
func lastReadPath() string { return filepath.Join(Prefix(), lastReadFileName) }

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
// recorded (spec-mate.md §2.1c "what zcp checks, and what it does not"): the
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

// DesiredRelease is the release list's one release: the fork's current stable
// release, read from ManifestURL() on every call (spec-mate.md §2.1c) rather
// than a version compiled into zcp or a copy read earlier. There is no
// fallback — an unreachable or invalid manifest is returned as an error and
// the caller decides how to degrade (MD-10).
func DesiredRelease(ctx context.Context, client *http.Client) (Manifest, error) {
	m, err := fetchManifest(ctx, client, ManifestURL(), "")
	if err != nil {
		return Manifest{}, err
	}

	if err := validateManifest(m); err != nil {
		return Manifest{}, err
	}

	writeLastRead(m)
	return m, nil
}

// CheckPublishedRelease gates a zcp release on the Mate already published to
// the canonical stable channel. Release authorization never uses the last read
// or the installation URL override: neither proves what new containers fetch.
// A non-empty githubToken signs the request in: anonymous github.com downloads
// from runners can connect and never answer. net/http drops the header when the
// download redirects off github.com.
func CheckPublishedRelease(ctx context.Context, client *http.Client, githubToken string) (Manifest, error) {
	m, err := fetchManifest(ctx, client, defaultManifestURL, githubToken)
	if err != nil {
		return Manifest{}, fmt.Errorf("check published Mate: %w", err)
	}
	if err := validateManifest(m); err != nil {
		return Manifest{}, fmt.Errorf("check published Mate: %w", err)
	}
	return m, nil
}

// fetchManifest reads the manifest from url, unvalidated; an empty githubToken
// keeps the request anonymous.
func fetchManifest(ctx context.Context, client *http.Client, url, githubToken string) (Manifest, error) {
	if client == nil {
		client = http.DefaultClient
	}
	fetchCtx, cancel := context.WithTimeout(ctx, manifestFetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, url, nil)
	if err != nil {
		return Manifest{}, fmt.Errorf("build mate manifest request: %w", err)
	}
	if githubToken != "" {
		req.Header.Set("Authorization", "Bearer "+githubToken)
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
	return m, nil
}

// writeLastRead records m for LastReadRelease, best effort: written under a
// temporary name of its own and renamed, so a reader sees the old record or
// the new one, never a part of either.
func writeLastRead(m Manifest) {
	data, err := json.Marshal(m)
	if err != nil {
		return
	}
	dir := filepath.Dir(lastReadPath())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(lastReadPath())+".*")
	if err != nil {
		return
	}
	_, err = tmp.Write(data)
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), lastReadPath())
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
	}
}

// compareSemver compares full, unprefixed semantic versions, including
// prerelease precedence and build metadata. Private release manifests use the
// same ordering and minimum-version gate as stable releases.
func compareSemver(a, b string) (int, error) {
	av, err := parseSemver(a)
	if err != nil {
		return 0, fmt.Errorf("version %q: %w", a, err)
	}
	bv, err := parseSemver(b)
	if err != nil {
		return 0, fmt.Errorf("version %q: %w", b, err)
	}
	return semver.Compare(av, bv), nil
}

// EnvelopeCarriesParts reports whether this zcp sends stages[] and
// devServers[] in place of stageHostname/stageSetupName (docs/spec-mate.md
// §2.8a.3 field-order step 3): only when every mate it drives accepts them.
func EnvelopeCarriesParts() bool {
	return carriesParts(MinimumMateVersion, PartsMateVersion)
}

func carriesParts(minimum, accepting string) bool {
	cmp, err := compareSemver(minimum, accepting)
	return err == nil && cmp >= 0
}

// VersionOlder reports whether installed names a strictly older release
// than latest, by semantic version precedence — what `zcp mate
// status` answers updateAvailable from. A version that fails to parse on
// either side answers false rather than guessing.
func VersionOlder(installed, latest string) bool {
	cmp, err := compareSemver(installed, latest)
	return err == nil && cmp < 0
}

func parseSemver(v string) (string, error) {
	// x/mod also accepts abbreviated vMAJOR and vMAJOR.MINOR; release
	// manifests require all three components and never a leading v.
	core, _, _ := strings.Cut(v, "-")
	core, _, _ = strings.Cut(core, "+")
	prefixed := "v" + v
	if strings.Count(core, ".") != 2 || !semver.IsValid(prefixed) {
		return "", fmt.Errorf("not a full unprefixed semantic version")
	}
	return prefixed, nil
}

// LastReadRelease is the manifest DesiredRelease last read, without network
// IO, while it is valid for this build: an earlier zcp validated it against
// its own floor and contract. Only `zcp mate status --local` answers from it,
// for a reader that must not wait on the network (spec-mate.md §2.1c); no
// decision about which version runs reads it.
func LastReadRelease() (Manifest, bool) {
	data, err := os.ReadFile(lastReadPath())
	if err != nil {
		return Manifest{}, false
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil || validateManifest(m) != nil {
		return Manifest{}, false
	}
	return m, true
}
