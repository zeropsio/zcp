// Package mate holds everything shared by the pieces of ZCP that install,
// supervise and publish mate (Zerops Mate, a fork of T3 Code) inside a zcp
// container: the pinned release version, the loopback port and public path prefix
// nginx serves it under, the filesystem paths, the `mate serve` argv, and the
// environment contract the mate server reads to recognise a Zerops project.
//
// mate rides in the zcp binary rather than in the platform's zcp@1 recipe: it is
// installed and supervised by `zcp init` (a `run.init` command) and reached
// through nginx on the container's existing 8080 origin. That is what lets a
// plain service restart — which re-runs the recipe's install.sh and picks up
// the latest zcp release — turn mate on for a container that predates it,
// without a single platform-side change.
//
// Kept to stdlib plus two leaf packages (internal/runtime for the container's
// home rule, internal/schema for the canonical API host) on purpose: the init
// step, the process supervisor and the nginx renderer all read these values,
// and none of them should have to pull in the platform/ops stack for a port
// number.
package mate

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/schema"
)

const (
	// PackageName is the published release asset's npm package name. The fork's
	// workspace package deliberately remains t3; that internal name does not
	// identify the artifact zcp downloads.
	PackageName = "zerops-mate"

	// BinName is the executable the pinned asset links under node_modules/.bin —
	// a property of the release, so it moves only together with PackageName and
	// PinnedVersion.
	BinName = "mate"

	// PinnedVersion names a tag that must exist in zeropsio/mate. It never
	// changes without PinnedSHA256 changing in the same commit.
	PinnedVersion = "0.2.5"

	// ReleaseAssetName and ReleaseURL are derived from the two pins above.
	ReleaseAssetName = PackageName + "-" + PinnedVersion + ".tgz"
	ReleaseURL       = "https://github.com/zeropsio/mate/releases/download/v" + PinnedVersion + "/" + ReleaseAssetName

	// PinnedSHA256 is the locally computed digest of the matching mate GitHub
	// release asset. Reproduce it by fetching and hashing that asset, for example:
	//
	//	version=0.2.5; curl -fL "https://github.com/zeropsio/mate/releases/download/v${version}/zerops-mate-${version}.tgz" | sha256sum
	//
	// The release's SHA256SUMS is also useful for a human cross-check, but this
	// digest compiled into zcp remains the authority. Empty fails closed before
	// any request is made.
	PinnedSHA256 = "52164df2eced650623ea2079485d3bd54db7c6a76b538632f177b5b1e4d9a961"
)

const (
	// ServePort is where the mate server listens. LoopbackHost, not 0.0.0.0:
	// the port is deliberately NOT a declared zcp@1 port, so the only way in
	// is nginx's BasePath location. One door (the Zerops-identity one the mate
	// server owns), never a second.
	ServePort = 3773

	// LoopbackHost is the interface ServePort binds.
	LoopbackHost = "127.0.0.1"

	// BasePath is the public path prefix nginx publishes mate under on the
	// container's 8080 origin, and the value passed to the server as
	// `--base-path` so the URLs it emits (assets, /ws, well-known) carry the
	// prefix. Both the nginx template and ServeArgv read this constant, so
	// moving the prefix is one edit.
	BasePath = "/mate"

	// UnitName is the `zsc unit create` name; systemd renders it as
	// zerops@mate.service. Same primitive as the sshfs mounts.
	UnitName = "mate"

	// UnitFilePath is where `zsc unit create` writes the unit file. Its
	// presence is how `zcp init` tells "already registered" from "first boot"
	// — `zsc unit` has only create and remove, no idempotent upsert.
	UnitFilePath = "/usr/lib/systemd/system/zerops@" + UnitName + ".service"

	// UnitCommand is the ExecStart `zsc unit create` is handed. It stays a
	// bare verb on purpose: a unit's command is frozen at creation and units
	// survive a container restart, so everything that can change between
	// releases (the argv, the environment) must be resolved by this process
	// at start time, never baked into the unit file.
	UnitCommand = "zcp service start " + UnitName

	// WorkspaceDir is the working directory mate bootstraps its project from:
	// the same /var/www every zcp agent already works in, with each mounted
	// dev service under /var/www/<hostname>. mate adapts to zcp's workspace,
	// never the other way round.
	WorkspaceDir = "/var/www"
)

// The environment contract the mate server reads to recognise a Zerops project.
// `zcp init` writes these to EnvFilePath while the full container environment
// is present; `zcp service start mate` merges the file over the unit's own
// environment. Only non-secret identifiers ever go in — never a token.
//
// EnvProjectID set and non-empty is THE signal that this server runs inside a
// Zerops project; nothing else votes. Without it the server keeps its upstream
// pairing behaviour.
const (
	EnvProjectID      = "T3CODE_ZEROPS_PROJECT_ID"
	EnvAPIHost        = "T3CODE_ZEROPS_API_HOST"
	EnvAllowedOrigins = "T3CODE_ZEROPS_ALLOWED_ORIGINS"

	// SourceAllowedOrigins is the service env an operator sets to add browser
	// origins beyond the container's own and http://localhost:*. Absent ⇒
	// EnvAllowedOrigins is not written at all, so the server keeps its default.
	SourceAllowedOrigins = "ZCP_MATE_ALLOWED_ORIGINS"
)

// LiveEnvStorePath is the container's live service-environment snapshot: a
// root-owned, world-readable JSON object of string → string (~200 keys —
// PATH, ZCP_API_KEY, the ZEROPS_* ids, VSCODE_PASSWORD, the agent flags,
// …) that the platform rewrites on every service env change, ~2 s after the
// write, no restart. It is the SAME source a login shell is populated from.
//
// mate's process environment = this store + the T3CODE_* contract file, so the
// agents and `zcp` it spawns see what a login shell sees — the alternative,
// the unit's own os.Environ() under systemd, carries only HOME and PATH. The
// store is read once at unit start (`zcp service start mate`, see
// service.mateExtraEnv); a later service env change needs `sudo systemctl
// restart zerops@mate` (or a future re-read) to reach mate.
const LiveEnvStorePath = "/etc/zerops-zembed/env.json"

// LoadLiveEnv reads the container's live env store at path (see
// LiveEnvStorePath) and renders it as KEY=VALUE lines sorted by key, so a
// caller merging it into a child's environment gets a deterministic order.
// The store is a flat string-to-string object by contract; a value that is
// not a JSON string is rejected, same as a missing file or malformed JSON —
// all three are returned as an error, never silently dropped or coerced. The
// caller decides how to degrade (the mate supervisor logs one line and starts
// with the process environment only — see service.mateExtraEnv).
func LoadLiveEnv(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	values := make(map[string]string, len(raw))
	keys := make([]string, 0, len(raw))
	for key, val := range raw {
		var s string
		if err := json.Unmarshal(val, &s); err != nil {
			return nil, fmt.Errorf("%s: value for %q is not a string: %w", path, key, err)
		}
		values[key] = s
		keys = append(keys, key)
	}
	sort.Strings(keys)

	if len(keys) == 0 {
		return nil, nil
	}
	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, key+"="+values[key])
	}
	return lines, nil
}

// InitMarkerRelPath is where `zcp init` records that it reached the end of its
// step list, relative to its baseDir — the .zcp/state/ convention the guided
// marker already uses. The file's CONTENT is the JSON body nginx serves at
// BasePath+"/healthz", so a client can tell "still initializing" from "broken" before it
// holds any credential.
const InitMarkerRelPath = ".zcp/state/init-complete"

// InitMarkerDir / InitMarkerPath are InitMarkerRelPath's ABSOLUTE production
// forms. nginx has no baseDir to join against, and `zcp init` always runs with
// baseDir "." from a WorkspaceDir cwd in production, so the two agree.
const (
	InitMarkerDir  = WorkspaceDir + "/.zcp/state"
	InitMarkerPath = InitMarkerDir + "/init-complete"
)

// installTimeout bounds all release-download and npm work on the container's
// boot path. Measured live before release delivery: 55 s for the 198-package
// dependency install, 58 s cold end to end. Three minutes leaves headroom for
// a slow registry without stalling a service start behind a hung connection —
// the step degrades when it expires.
const installTimeout = 3 * time.Minute

// helpTimeout bounds the --base-path capability probe (one node startup).
const helpTimeout = 10 * time.Second

// smokeTimeout bounds the post-stage probes EnsureInstalled runs before it lets
// a newly staged version go live: `mate --version` plus the native-addon import.
// Two node startups, same order of magnitude as helpTimeout.
const smokeTimeout = 15 * time.Second

// Prefix is the npm prefix mate lives under. It lives under the service user's
// home so it survives a container restart (a redeploy replaces the container
// and loses it, which is the same fate as the server's own data).
//
// Everything below it is versioned (see VersionsDir/VersionDir/CurrentLink):
// Prefix() itself is never an npm prefix directly, only a version directory is.
func Prefix() string { return filepath.Join(runtime.HomeDir(), ".zcp", "mate") }

// VersionsDir holds one complete npm-install prefix per mate version ever
// activated on this container, each directory named for the npm package
// version it carries. EnsureInstalled stages a new release into its own
// directory here before anything points at it — that is what keeps a failed
// or partial install from ever touching the version that was working.
func VersionsDir() string { return filepath.Join(Prefix(), "versions") }

// VersionDir is one version's complete npm prefix: everything
// `npm install --prefix` writes for that version, isolated from every other
// version so an update can stage, smoke-test and discard one without
// disturbing whichever version is currently live.
func VersionDir(version string) string { return filepath.Join(VersionsDir(), version) }

// CurrentLink is the symlink that names the live version. EnsureInstalled
// repoints it atomically (build under a temp name, os.Rename onto this path),
// so an interrupted activation can never leave it dangling or half-written.
// It is the ONLY thing BinPath() resolves through — nothing outside this
// package should ever read a VersionDir() path directly.
func CurrentLink() string { return filepath.Join(Prefix(), "current") }

// binIn is the bundle entry point npm links inside one already-installed
// prefix directory (a VersionDir(), or historically Prefix() itself) —
// `npm install --prefix <dir>` always lands it at the same relative spot.
func binIn(dir string) string { return filepath.Join(dir, "node_modules", ".bin", BinName) }

// BinPath is the bundle's entry point — what a supervised `mate serve` runs.
// It always resolves through CurrentLink(), so it names whichever version
// EnsureInstalled last activated, with no version check and no network in the
// common case: that is what keeps a warm restart off the network, and what
// makes the hand-delivered dev build (eval/scripts/mate-dev-push.sh, which
// repoints CurrentLink() itself) and a fetched release one code path.
func BinPath() string { return binIn(CurrentLink()) }

// EnvFilePath is where `zcp init` writes the environment contract. Deliberately
// OUTSIDE Prefix(): `npm install --prefix` owns everything under a version
// directory, and the dev loop reinstalls the bundle there.
func EnvFilePath() string { return filepath.Join(runtime.HomeDir(), ".zcp", "mate.env") }

// BaseDir is the mate server's data directory (threads, sessions, auth), passed
// as `--base-dir`. Under the home directory rather than a mount, so a restart
// keeps the history.
func BaseDir() string { return filepath.Join(runtime.HomeDir(), ".t3") }

// InstallArgs is the argv for the npm invocation that installs a downloaded
// release tarball into prefix (argv[0] included).
func InstallArgs(prefix, tarballPath string) []string {
	return []string{
		"npm", "install",
		"--prefix", prefix,
		"--no-audit", "--no-fund", "--loglevel=error",
		tarballPath,
	}
}

// downloadVerified is the download+verify half of installing a release:
// fetch releaseURL into a temporary file and check it against expectedSHA256
// before anything else ever reads it. The caller owns removing the file via
// the returned cleanup, called whether or not err is nil (a mismatch still
// leaves the corrupt download behind unless cleaned up here).
//
// Package-level so EnsureInstalled's tests can substitute a fake tarball with
// no real HTTP round trip; production is defaultDownloadVerified.
var downloadVerified = defaultDownloadVerified

func defaultDownloadVerified(ctx context.Context, client *http.Client, releaseURL, expectedSHA256 string) (tarballPath string, cleanup func(), err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releaseURL, nil)
	if err != nil {
		return "", nil, fmt.Errorf("build mate release request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("download %s: %w", releaseURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", nil, fmt.Errorf("download %s: HTTP %s", releaseURL, resp.Status)
	}

	tarball, err := os.CreateTemp("", PackageName+"-*.tgz")
	if err != nil {
		return "", nil, fmt.Errorf("create temporary mate tarball: %w", err)
	}
	path := tarball.Name()
	cleanup = func() { _ = os.Remove(path) }

	hash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tarball, hash), resp.Body); err != nil {
		_ = tarball.Close()
		cleanup()
		return "", nil, fmt.Errorf("download %s: %w", releaseURL, err)
	}
	if err := tarball.Close(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("close downloaded %s: %w", ReleaseAssetName, err)
	}

	actualSHA256 := fmt.Sprintf("%x", hash.Sum(nil))
	if !strings.EqualFold(actualSHA256, expectedSHA256) {
		cleanup()
		return "", nil, fmt.Errorf("SHA-256 mismatch for %s: expected %s, got %s", ReleaseAssetName, expectedSHA256, actualSHA256)
	}
	return path, cleanup, nil
}

// npmInstallTarball is the npm-invocation half of installing a release, into
// prefix. npm still resolves the tarball's runtime dependencies from its
// registry; downloading the package itself does not make this install
// offline-capable.
//
// Package-level so tests can stub it without a real npm on PATH; production
// is defaultNpmInstallTarball.
var npmInstallTarball = defaultNpmInstallTarball

func defaultNpmInstallTarball(ctx context.Context, prefix, tarballPath string) error {
	args := InstallArgs(prefix, tarballPath)
	cmd := exec.CommandContext(ctx, args[0], args[1:]...) //nolint:gosec // argv is built from package constants plus a caller-controlled prefix/tarball path
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("npm install %s: %w", ReleaseAssetName, err)
	}
	return nil
}

// nativeAddonProbe opens the native addons the server loads through a runtime
// import rather than a static one. Kept as an ESM snippet because one of the
// bundle's packages is ESM-only and refuses `require` outright.
const nativeAddonProbe = `await import("node-pty"); await import("msgpackr-extract");`

// NativeAddonProbeArgs is the argv that runs nativeAddonProbe (argv[0]
// included). It resolves from the working directory the caller sets, which must
// be the version directory `npm install --prefix` wrote.
func NativeAddonProbeArgs() []string {
	return []string{"node", "--input-type=module", "-e", nativeAddonProbe}
}

// smokeTestInstall confirms a freshly staged install both executes and can open
// its native addons, before EnsureInstalled lets it go live.
//
// `<bin> --version` proves the entry point runs and everything it imports
// STATICALLY resolves. It says nothing about node-pty — the PTY behind every
// agent terminal — or msgpackr's native encoder, which the bundle reaches
// through a runtime import. The release manifest declares exactly those addons
// and nothing else (everything else is inlined into the bundle), so a manifest
// that stopped declaring one would install cleanly, answer `--version`, go live,
// and fail the first time somebody opened a terminal. Both halves run here so
// that failure lands on the staging directory, which is discarded, instead of on
// CurrentLink().
//
// Package-level so tests can stub it without a real mate install; production is
// defaultSmokeTestInstall.
var smokeTestInstall = defaultSmokeTestInstall

func defaultSmokeTestInstall(ctx context.Context, versionDir string) error {
	bin := binIn(versionDir)
	if err := exec.CommandContext(ctx, bin, "--version").Run(); err != nil {
		return fmt.Errorf("%s --version: %w", bin, err)
	}

	args := NativeAddonProbeArgs()
	probe := exec.CommandContext(ctx, args[0], args[1:]...) //nolint:gosec // argv is package constants only; the staged directory is the working directory, never an argument
	// node resolves a bare specifier passed to -e from the working directory,
	// so this is what points the probe at the staged install's node_modules.
	probe.Dir = versionDir
	if out, err := probe.CombinedOutput(); err != nil {
		return fmt.Errorf("native addons in %s: %w: %s", versionDir, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// InstallRelease downloads one release tarball, verifies its SHA-256, then
// npm-installs it into prefix. The caller owns ctx; the production caller
// applies installTimeout to both the download and npm.
func InstallRelease(ctx context.Context, client *http.Client, releaseURL, expectedSHA256, prefix string) error {
	if expectedSHA256 == "" {
		return fmt.Errorf("pinned SHA-256 digest is unset for %s", ReleaseAssetName)
	}

	if err := os.MkdirAll(prefix, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", prefix, err)
	}

	tarballPath, cleanup, err := downloadVerified(ctx, client, releaseURL, expectedSHA256)
	if err != nil {
		return err
	}
	defer cleanup()

	return npmInstallTarball(ctx, prefix, tarballPath)
}

// Release names one installable mate build: the version string it reports at
// runtime, the tarball URL to fetch it from, and the digest that must match
// before npm ever sees it.
type Release struct {
	Version string
	URL     string
	SHA256  string
}

// DesiredRelease is the version EnsureInstalled converges this container
// toward. Today it returns exactly the compiled-in pin (PinnedVersion,
// ReleaseURL, PinnedSHA256) — this function is the seam a future "latest
// compatible" resolver lands behind, once one exists.
//
// zcp stays hard-pinned until then on purpose: PinnedSHA256, compiled into
// this binary, is the SOLE integrity authority InstallRelease trusts — there
// is no second signature or registry check behind it. A "latest" resolver
// would have to give that authority up (fetch a digest for whatever it picked
// at install time from somewhere else, which itself would need to be
// trusted) before it could replace a compile-time pin. A release only earns
// that trust once it DECLARES the compatibility contract it satisfies —
// nothing in the fork does yet, so PinnedVersion moves only by a zcp commit
// that changes this constant and PinnedSHA256 together.
func DesiredRelease() Release {
	return Release{Version: PinnedVersion, URL: ReleaseURL, SHA256: PinnedSHA256}
}

// IsDevVersion reports whether v is a semver PRERELEASE — any version
// carrying a "-", e.g. "0.1.0-dev.a1b2c3" (the tag
// eval/scripts/mate-dev-push.sh builds: <package version>-dev.<git sha>). That
// is what distinguishes a hand-pushed dev build from a tagged release:
// EnsureInstalled never silently replaces one with a pinned release.
func IsDevVersion(v string) bool { return strings.Contains(v, "-") }

// InstalledVersion reads the version CurrentLink() resolves to, from npm's
// own package.json record for the installed package — never a side file zcp
// would have to keep honest itself. A missing link, a missing package.json,
// or an unparsable/empty version field is all reported as an error: there is
// no "no version" case that is not also "no usable install".
func InstalledVersion() (string, error) { return installedVersionIn(CurrentLink()) }

func installedVersionIn(dir string) (string, error) {
	path := filepath.Join(dir, "node_modules", PackageName, "package.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	var pkg struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return "", fmt.Errorf("parse %s: %w", path, err)
	}
	if pkg.Version == "" {
		return "", fmt.Errorf("%s: version field is empty", path)
	}
	return pkg.Version, nil
}

// Action names what EnsureInstalled did, so the one caller that logs it
// (enableMate, `zcp mate update`) can print a single honest line without
// re-deriving what happened from Result's other fields.
type Action string

const (
	// ActionNone: nothing changed. Either the desired version was already
	// live (no network reached at all), or a dev build is being kept.
	ActionNone Action = "none"
	// ActionInstalled: nothing usable was live before; the desired release
	// is now live.
	ActionInstalled Action = "installed"
	// ActionUpdated: a different version was live; the desired release
	// replaced it.
	ActionUpdated Action = "updated"
)

// Result reports what EnsureInstalled did. From is the version that was live
// before (empty for ActionInstalled, which starts from nothing usable). For
// ActionNone, From and To are both the version that stayed live.
type Result struct {
	Action Action
	From   string
	To     string
}

// EnsureOptions steers EnsureInstalled's one behavioural choice: whether a
// dev build (see IsDevVersion) may be replaced by a pinned release.
type EnsureOptions struct {
	// Force, when true, lets a dev build be replaced. Default false: a
	// hand-pushed dev build (eval/scripts/mate-dev-push.sh) is never silently
	// clobbered by a routine container boot or an unqualified `zcp mate
	// update` — only an explicit --force does that.
	Force bool
}

// EnsureInstalled converges the installed mate bundle toward DesiredRelease(),
// or leaves it alone, in one pass:
//
//  1. Read the installed and desired versions. Equal ⇒ ActionNone, having
//     made no network request at all — this is what keeps a warm restart off
//     the network.
//  2. The installed version is a dev build and opts.Force is false ⇒
//     ActionNone, keeping the dev build.
//  3. Stage the desired release into its own VersionDir() (clearing any
//     partial leftover there first).
//  4. Smoke-test the staged binary.
//  5. Activate it atomically (repoint CurrentLink()).
//  6. Prune old version directories, always keeping the one just activated.
//
// Any failure in steps 3-5 returns before CurrentLink() is touched, and
// cleans up the half-built version directory — CurrentLink() is left exactly
// where it was, still naming the version that was working. The caller (the
// mate init step, and `zcp mate update`) turns a returned error into a degraded
// step or a non-zero exit, never a torn install.
func EnsureInstalled(opts EnsureOptions) (Result, error) {
	desired := DesiredRelease()
	installed, instErr := InstalledVersion()

	if instErr == nil && installed == desired.Version {
		return Result{Action: ActionNone, From: installed, To: installed}, nil
	}
	if instErr == nil && IsDevVersion(installed) && !opts.Force {
		return Result{Action: ActionNone, From: installed, To: installed}, nil
	}

	if err := stageAndActivate(desired); err != nil {
		return Result{}, err
	}
	pruneOldVersions(desired.Version)

	action := ActionUpdated
	from := installed
	if instErr != nil {
		action = ActionInstalled
		from = ""
	}
	return Result{Action: action, From: from, To: desired.Version}, nil
}

// stageAndActivate installs desired into its own VersionDir(), smoke-tests
// it, then activates it. A failure at any point removes the half-built
// version directory and returns before CurrentLink() is touched.
func stageAndActivate(desired Release) error {
	versionDir := VersionDir(desired.Version)
	if err := os.RemoveAll(versionDir); err != nil {
		return fmt.Errorf("clear partial %s: %w", versionDir, err)
	}

	installCtx, installCancel := context.WithTimeout(context.Background(), installTimeout)
	defer installCancel()
	if err := InstallRelease(installCtx, http.DefaultClient, desired.URL, desired.SHA256, versionDir); err != nil {
		_ = os.RemoveAll(versionDir)
		return err
	}

	smokeCtx, smokeCancel := context.WithTimeout(context.Background(), smokeTimeout)
	defer smokeCancel()
	if err := smokeTestInstall(smokeCtx, versionDir); err != nil {
		_ = os.RemoveAll(versionDir)
		return fmt.Errorf("smoke test %s: %w", versionDir, err)
	}

	return activate(versionDir)
}

// activate atomically repoints CurrentLink() at versionDir: build a
// relative-target symlink under a temporary name in Prefix(), then
// os.Rename it onto CurrentLink(). A crash or failure between those two
// steps leaves either the old link (rename never happened) or the new one
// (rename is atomic on the same filesystem) — never a partially written
// link. A relative target keeps the layout portable if Prefix() itself ever
// moves.
func activate(versionDir string) error {
	target, err := filepath.Rel(Prefix(), versionDir)
	if err != nil {
		return fmt.Errorf("relative path from %s to %s: %w", Prefix(), versionDir, err)
	}

	tmp := CurrentLink() + ".tmp"
	// Best-effort: a stale tmp left by a prior crashed activation must not
	// block this one.
	_ = os.Remove(tmp)
	if err := os.Symlink(target, tmp); err != nil {
		return fmt.Errorf("create symlink %s -> %s: %w", tmp, target, err)
	}
	if err := os.Rename(tmp, CurrentLink()); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("activate %s: %w", versionDir, err)
	}
	return nil
}

// versionEntry names one VersionsDir() entry for pruneOldVersions' sort.
type versionEntry struct {
	name    string
	modTime time.Time
}

// pruneOldVersions removes every VersionsDir() entry except the live one
// (liveVersion, never removed regardless of age) and the single most
// recently modified OTHER entry — two kept in the common case. Best-effort:
// a listing or removal failure is silently skipped, never fatal to the
// install that just succeeded.
func pruneOldVersions(liveVersion string) {
	entries, err := os.ReadDir(VersionsDir())
	if err != nil {
		return
	}

	var versions []versionEntry
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		versions = append(versions, versionEntry{name: e.Name(), modTime: info.ModTime()})
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i].modTime.After(versions[j].modTime) })

	keep := map[string]bool{liveVersion: true}
	for _, v := range versions {
		if len(keep) >= 2 {
			break
		}
		keep[v.name] = true
	}
	for _, v := range versions {
		if !keep[v.name] {
			_ = os.RemoveAll(filepath.Join(VersionsDir(), v.name))
		}
	}
}

// ServeArgv is the supervised command (argv[0] included) for the mate server.
//
// withBasePath is a CAPABILITY, not a preference: the mate CLI treats an unknown
// flag as a fatal parse error, so a bundle that predates --base-path would
// crash-loop the unit at every container boot if the flag were passed blind.
// Dropping it degrades in the safe direction — the server boots and answers
// under BasePath, only its root-absolute assets miss.
//
// The working directory is a POSITIONAL argument and
// --auto-bootstrap-project-from-cwd is a boolean flag (apps/server/src/cli/
// config.ts): the directory must never be written as that flag's value.
func ServeArgv(bin string, withBasePath bool) []string {
	argv := []string{
		bin, "serve",
		"--mode", "web",
		"--host", LoopbackHost,
		"--port", strconv.Itoa(ServePort),
	}
	if withBasePath {
		argv = append(argv, "--base-path", BasePath)
	}
	return append(argv,
		"--base-dir", BaseDir(),
		"--no-browser",
		"--auto-bootstrap-project-from-cwd",
		WorkspaceDir,
	)
}

// SupportsBasePath reports whether the installed bundle advertises
// --base-path, by reading `serve --help`. Any failure (missing binary,
// non-zero exit, timeout) answers false: the argv then omits the flag, which
// still starts.
func SupportsBasePath(bin string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), helpTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, bin, "serve", "--help").CombinedOutput()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "--base-path")
}

// allowedOriginPattern matches one entry of ZCP_MATE_ALLOWED_ORIGINS: scheme,
// host, optional port, nothing else — the exact shape
// T3CODE_ZEROPS_ALLOWED_ORIGINS is matched against (spec-mate.md §3.4,
// "matched exactly"). No bare host, no path, no query, no wildcard, no
// non-https scheme — any of those can never match a real Origin header, so
// writing them would silently configure an allowlist entry that does nothing.
var allowedOriginPattern = regexp.MustCompile(`^https://[a-zA-Z0-9](?:[a-zA-Z0-9-]*[a-zA-Z0-9])?(?:\.[a-zA-Z0-9](?:[a-zA-Z0-9-]*[a-zA-Z0-9])?)*(?::[0-9]{1,5})?$`)

// ValidateAllowedOrigins checks raw — the operator-set ZCP_MATE_ALLOWED_ORIGINS
// value — is well-formed: blank, or a comma-separated list of entries each
// matching allowedOriginPattern. On the first malformed entry it returns it
// trimmed as bad with ok=false, so the caller can name it in one diagnostic
// line and fall back to the server's own default instead of writing a value
// that can never match anything (MD-3's "carries only non-secret
// identifiers" extends here to "or nothing that cannot possibly match").
func ValidateAllowedOrigins(raw string) (bad string, ok bool) {
	if strings.TrimSpace(raw) == "" {
		return "", true
	}
	for entry := range strings.SplitSeq(raw, ",") {
		trimmed := strings.TrimSpace(entry)
		if !allowedOriginPattern.MatchString(trimmed) {
			return trimmed, false
		}
	}
	return "", true
}

// EnvLines renders the environment contract as KEY=VALUE entries. An empty
// allowedOrigins omits its key entirely, so the server keeps its own default
// rather than reading a configured-but-empty allowlist.
func EnvLines(projectID, apiHost, allowedOrigins string) []string {
	lines := []string{
		EnvProjectID + "=" + projectID,
		EnvAPIHost + "=" + apiHost,
	}
	if allowedOrigins != "" {
		lines = append(lines, EnvAllowedOrigins+"="+allowedOrigins)
	}
	return lines
}

// ParseEnvFile reads the KEY=VALUE entries `zcp init` wrote, skipping blank
// lines and comments. A read failure is returned, not swallowed: the caller
// decides (the supervisor logs it and starts mate anyway).
func ParseEnvFile(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		lines = append(lines, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return lines, nil
}

// ResolveAPIHost normalizes ZCP_API_KEY's companion ZCP_API_HOST into the bare
// host the mate server joins its API base from (https://<host>/api/rest/public):
// empty falls back to the canonical host, a scheme and a trailing slash are
// stripped, an explicit port is kept.
func ResolveAPIHost(raw string) string {
	host := strings.TrimSpace(raw)
	if host == "" {
		return schema.CanonicalAPIHost
	}
	host = strings.TrimSuffix(host, "/")
	if idx := strings.Index(host, "://"); idx >= 0 {
		host = host[idx+len("://"):]
	}
	if host == "" {
		return schema.CanonicalAPIHost
	}
	return host
}
