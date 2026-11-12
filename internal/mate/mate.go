// Package mate holds everything shared by the pieces of ZCP that install,
// supervise and publish mate (Zerops Mate, a fork of T3 Code) inside a zcp
// container: the release manifest, the loopback port and public path prefix
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
// Kept to stdlib plus internal/runtime (the container's home rule),
// internal/schema (the canonical API host) and internal/hq (HQ identity and
// credential paths) on purpose: the init
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

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/schema"
)

const (
	// PackageName is the published release asset's npm package name. The fork's
	// workspace package deliberately remains t3; that internal name does not
	// identify the artifact zcp downloads.
	PackageName = "zerops-mate"

	// BinName is the executable a release asset links under
	// node_modules/.bin — a property of the release (C-1), constant across
	// versions.
	BinName = "mate"
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
	// container's 8080 origin, and the value the server reads from
	// EnvBasePath so the URLs it emits (assets, /ws, well-known) carry the
	// prefix. Both the nginx template and LaunchEnvLines read this constant,
	// so moving the prefix is one edit.
	BasePath = "/mate"

	// UnitName is the `zsc unit create` name; systemd renders it as
	// zerops@mate.service. Same primitive as the sshfs mounts.
	UnitName = "mate"

	// KeepUnitName names the keep job's systemd units (docs/spec-mate.md
	// §2.1d): the template service zcp-mate-fuse@<way>.service and its timer.
	KeepUnitName = "zcp-mate-fuse"

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

// AgentStateDir is the state directory of the agents a Mate runs: zcp keeps
// it under the working directory it serves from, and every agent works in
// WorkspaceDir — so the pairs' records the zcp of an agent writes are here.
const AgentStateDir = WorkspaceDir + "/.zcp/state"

// InitMarkerDir / InitMarkerPath are InitMarkerRelPath's ABSOLUTE production
// forms. nginx has no baseDir to join against, and `zcp init` always runs with
// baseDir "." from a WorkspaceDir cwd in production, so the two agree.
const (
	InitMarkerDir  = AgentStateDir
	InitMarkerPath = InitMarkerDir + "/init-complete"
)

// InstallTimeout bounds one release download, npm install and smoke test
// where nothing else waits on it: the keep job, and a start with nothing
// installed yet. Measured live before release delivery: 55 s for the
// 198-package dependency install, 58 s cold end to end; three minutes leaves
// headroom for a slow registry. A start that has current uses
// StartStageTimeout instead.
const InstallTimeout = 3 * time.Minute

// Prefix is the npm prefix mate lives under. It lives under the service user's
// home so it survives a container restart (a redeploy replaces the container
// and loses it, which is the same fate as the server's own data).
//
// Everything below it is versioned (see VersionsDir/VersionDir/CurrentLink):
// Prefix() itself is never an npm prefix directly, only a version directory is.
func Prefix() string { return filepath.Join(runtime.HomeDir(), ".zcp", "mate") }

// VersionsDir holds one complete npm-install prefix per mate version ever
// activated on this container, each directory named for the npm package
// version it carries. StageRelease stages a new release into its own
// directory here before anything points at it — that is what keeps a failed
// or partial install from ever touching the version that was working.
func VersionsDir() string { return filepath.Join(Prefix(), "versions") }

// VersionDir is one version's complete npm prefix: everything
// `npm install --prefix` writes for that version, isolated from every other
// version so an update can stage, smoke-test and discard one without
// disturbing whichever version is currently live.
func VersionDir(version string) string { return filepath.Join(VersionsDir(), version) }

// CurrentLink is the symlink that names the version the unit starts. The start
// job repoints it atomically (build under a temp name, os.Rename onto this path),
// so an interrupted activation can never leave it dangling or half-written.
// It is the ONLY thing BinPath() resolves through — nothing outside this
// package should ever read a VersionDir() path directly.
func CurrentLink() string { return filepath.Join(Prefix(), "current") }

// binIn is the bundle entry point npm links inside one already-installed
// prefix directory (a VersionDir(), or historically Prefix() itself) —
// `npm install --prefix <dir>` always lands it at the same relative spot.
func binIn(dir string) string { return filepath.Join(dir, "node_modules", ".bin", BinName) }

// packageIn is the installed package's own directory under a version
// directory: where the server's runtime imports resolve from. Its bundled
// dependencies (node-pty, since mate 0.8.1) live in the package's own
// node_modules, one level below the prefix's, and Node finds both from here.
func packageIn(dir string) string { return filepath.Join(dir, "node_modules", PackageName) }

// BinPath is the bundle's entry point — what a supervised `mate serve` runs.
// It always resolves through CurrentLink(), so it names whichever version
// the start job last activated, with no version check and no network in the
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

func downloadVerified(ctx context.Context, client *http.Client, releaseURL, expectedSHA256 string) (tarballPath string, cleanup func(), err error) {
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
		return "", nil, fmt.Errorf("close downloaded %s: %w", releaseURL, err)
	}

	actualSHA256 := fmt.Sprintf("%x", hash.Sum(nil))
	if !strings.EqualFold(actualSHA256, expectedSHA256) {
		cleanup()
		return "", nil, fmt.Errorf("SHA-256 mismatch for %s: expected %s, got %s", releaseURL, expectedSHA256, actualSHA256)
	}
	return path, cleanup, nil
}

// npmInstallTarball is the npm-invocation half of installing a release, into
// prefix. npm still resolves the tarball's runtime dependencies from its
// registry; downloading the package itself does not make this install
// offline-capable.

func npmInstallTarball(ctx context.Context, prefix, tarballPath string) error {
	args := InstallArgs(prefix, tarballPath)
	cmd := exec.CommandContext(ctx, args[0], args[1:]...) //nolint:gosec // argv is built from package constants plus a caller-controlled prefix/tarball path
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("npm install %s: %w", tarballPath, err)
	}
	return nil
}

// nativeAddonProbe opens the native addons the server loads through a runtime
// import rather than a static one. Kept as an ESM snippet because one of the
// bundle's packages is ESM-only and refuses `require` outright.
const nativeAddonProbe = `await import("node-pty"); await import("msgpackr-extract");`

// NativeAddonProbeArgs is the argv that runs nativeAddonProbe (argv[0]
// included). It resolves from the working directory the caller sets, which must
// be the installed package's directory (packageIn), the same place the server
// resolves its own runtime imports from.
func NativeAddonProbeArgs() []string {
	return []string{"node", "--input-type=module", "-e", nativeAddonProbe}
}

// smokeTestInstall confirms a freshly staged install both executes and can open
// its native addons, before it may go live.
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
// It runs under the caller's deadline, never a clock of its own: how long it
// takes says nothing about the install. Zerops throttles a container whose
// memory is over its cgroup's memory.high (the granted RAM less the minimum
// free RAM, raised by the autoscaler in steps about 10-20 s apart), and then
// every process in it stalls. Measured on a 2-core Mate container
// (2026-11-12): `mate --version` takes 1.2-3.1 s unthrottled and made no
// progress in 60 s over memory.high; on a first boot memory.high started at
// 128 MiB and took 36 s to reach 896 MiB.

func smokeTestInstall(ctx context.Context, versionDir string) error {
	bin := binIn(versionDir)
	if err := exec.CommandContext(ctx, bin, "--version").Run(); err != nil {
		return fmt.Errorf("%s --version: %w", bin, err)
	}

	args := NativeAddonProbeArgs()
	probe := exec.CommandContext(ctx, args[0], args[1:]...) //nolint:gosec // argv is package constants only; the staged directory is the working directory, never an argument
	// node resolves a bare specifier passed to -e from the working directory.
	// Probing from the package directory, not the prefix, sees what the server
	// sees: a bundled dependency sits in the package's own node_modules and is
	// invisible from the prefix root (0.8.1 failed its smoke test that way).
	probe.Dir = packageIn(versionDir)
	if out, err := probe.CombinedOutput(); err != nil {
		return fmt.Errorf("native addons in %s: %w: %s", versionDir, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// InstallRelease downloads one release tarball, verifies its SHA-256, then
// npm-installs it into prefix. The caller owns ctx; the production caller
// bounds the download, npm and the smoke test together (InstallTimeout,
// StartStageTimeout).
func InstallRelease(ctx context.Context, client *http.Client, releaseURL, expectedSHA256, prefix string) error {
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

// IsDevVersion reports whether v is a semver PRERELEASE — any version
// carrying a "-", e.g. "0.1.0-dev.a1b2c3" (the tag
// eval/scripts/mate-dev-push.sh builds: <package version>-dev.<git sha>). That
// is what distinguishes a hand-pushed dev build from a tagged release:
// VersionToRun keeps one unless its base release is below MinimumMateVersion.
func IsDevVersion(v string) bool { return strings.Contains(v, "-") }

// BelowFloor reports whether v, a release or a dev build, is older than
// MinimumMateVersion. A dev build is judged by the release it was cut from
// (its version without the prerelease suffix), so one cut at or above the floor
// stays and one cut below it is no longer a build this zcp drives.
func BelowFloor(v string) bool {
	core, _, _ := strings.Cut(v, "-")
	return VersionOlder(core, MinimumMateVersion)
}

// BelowFloorError reports an installed bundle older than MinimumMateVersion.
type BelowFloorError struct {
	Installed string
	Minimum   string
}

func (e *BelowFloorError) Error() string {
	return fmt.Sprintf("installed mate %s is below the minimum %s this zcp drives", e.Installed, e.Minimum)
}

// InstalledVersion reads the version CurrentLink() resolves to, from npm's
// own package.json record for the installed package — never a side file zcp
// would have to keep honest itself. A missing link, a missing package.json,
// or an unparsable/empty version field is all reported as an error: there is
// no "no version" case that is not also "no usable install".
func InstalledVersion() (string, error) { return installedVersionIn(CurrentLink()) }

func installedVersionIn(dir string) (string, error) {
	pkg, path, err := installedPackageIn(dir)
	if err != nil {
		return "", err
	}
	if pkg.Version == "" {
		return "", fmt.Errorf("%s: version field is empty", path)
	}
	return pkg.Version, nil
}

// installedPackage is what zcp reads from the installed package's own
// package.json.
type installedPackage struct {
	Version string `json:"version"`
	// Mate.HQOrigin is the HQ the server links to unless T3CODE_ZEROPS_HQ_ORIGIN
	// names another; bundles before the declaration carry none.
	Mate struct {
		HQOrigin string `json:"hqOrigin"`
	} `json:"mate"`
}

func installedPackageIn(dir string) (installedPackage, string, error) {
	path := filepath.Join(dir, "node_modules", PackageName, "package.json")
	var pkg installedPackage
	data, err := os.ReadFile(path)
	if err != nil {
		return pkg, path, fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return pkg, path, fmt.Errorf("parse %s: %w", path, err)
	}
	return pkg, path, nil
}

// HQIdentity is the identity hq.IdentityFrom reads from getenv, with HQ's
// origin the installed Mate declares when getenv names none (spec-mate §3.5):
// zcp holds no default of its own, and a process the Mate's server did not
// start — the unit's supervisor, a plain shell — never inherits the origin the
// server publishes to its children.
func HQIdentity(getenv func(string) string) hq.Identity {
	id := hq.IdentityFrom(getenv)
	if id.Origin == "" {
		if pkg, _, err := installedPackageIn(CurrentLink()); err == nil {
			id.Origin = strings.TrimRight(strings.TrimSpace(pkg.Mate.HQOrigin), "/")
		}
	}
	return id
}

// Replacing says what StageRelease does with a directory that holds the
// version but fails its smoke test.
type Replacing bool

const (
	// ReplaceFailing downloads it afresh: the keep job, which has the time.
	ReplaceFailing Replacing = true
	// KeepFailing leaves it as it is and fails: the start job, whose smoke
	// test can time out under boot load on a version keep already verified.
	KeepFailing Replacing = false
)

// StageRelease makes versions/<version> a complete, smoke-tested install of
// release, without moving current. A directory that already holds the version
// and passes the smoke test is kept, so the keep job's download and the start
// job's share one; one that fails it is replaced only under ReplaceFailing.
// Otherwise the release's tarball is downloaded into it afresh, which needs
// release.URL; ctx bounds the download, the npm install and every smoke test
// together. The directory current or last-good names is never replaced. Any
// failure removes the half-built directory.
func StageRelease(ctx context.Context, release Manifest, replace Replacing) error {
	versionDir := VersionDir(release.Version)
	if version, err := installedVersionIn(versionDir); err == nil && version == release.Version {
		err := smokeTestInstall(ctx, versionDir)
		if err == nil {
			return nil
		}
		if replace == KeepFailing {
			return fmt.Errorf("%s fails its smoke test: %w; left for the keep job", versionDir, err)
		}
	}
	for _, link := range []string{CurrentLink(), LastGoodLink()} {
		if dir, err := linkedVersionDir(link); err == nil && filepath.Clean(dir) == filepath.Clean(versionDir) {
			return fmt.Errorf("%s fails its smoke test and is what %s names; not replaced", versionDir, filepath.Base(link))
		}
	}
	if release.URL == "" {
		return fmt.Errorf("mate %s is not downloaded, and the release list does not name it", release.Version)
	}
	if err := os.RemoveAll(versionDir); err != nil {
		return fmt.Errorf("clear partial %s: %w", versionDir, err)
	}

	if err := InstallRelease(ctx, http.DefaultClient, release.URL, release.SHA256, versionDir); err != nil {
		_ = os.RemoveAll(versionDir)
		return err
	}

	if err := smokeTestInstall(ctx, versionDir); err != nil {
		_ = os.RemoveAll(versionDir)
		return fmt.Errorf("smoke test %s: %w", versionDir, err)
	}

	return nil
}

// activate atomically repoints CurrentLink() at versionDir: build a
// relative-target symlink under a temporary name in Prefix(), then
// os.Rename it onto CurrentLink(). A crash or failure between those two
// steps leaves either the old link (rename never happened) or the new one
// (rename is atomic on the same filesystem) — never a partially written
// link. A relative target keeps the layout portable if Prefix() itself ever
// moves.
func activate(versionDir string) error {
	return replaceLink(CurrentLink(), versionDir)
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
	if lastGood, err := os.Readlink(LastGoodLink()); err == nil {
		keep[filepath.Base(lastGood)] = true
	}
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
// It carries no flag whose support depends on the installed bundle: the mate
// CLI treats an unknown flag as a fatal parse error, and a launch may start a
// bundle nothing has vetted — a kept dev build, or what is installed after an
// install failed. The public prefix rides EnvBasePath instead
// (LaunchEnvLines), which every mate release since v0.1.0 reads.
//
// The working directory is a POSITIONAL argument and
// --auto-bootstrap-project-from-cwd is a boolean flag (apps/server/src/cli/
// config.ts): the directory must never be written as that flag's value.
func ServeArgv(bin string) []string {
	return []string{
		bin, "serve",
		"--mode", "web",
		"--host", LoopbackHost,
		"--port", strconv.Itoa(ServePort),
		"--base-dir", BaseDir(),
		"--no-browser",
		"--auto-bootstrap-project-from-cwd",
		WorkspaceDir,
	}
}

// EnvBasePath is the server's public prefix as its environment states it —
// the same setting as the CLI's --base-path, which zcp does not pass. Both
// arrived in one mate release, before v0.1.0.
const EnvBasePath = "T3CODE_BASE_PATH"

// EnvHQGitCredential names, to the server, the file it writes the Git
// credential HQ sends it over the link to (hq.GitCredentialPath). zcp reads
// the file and never writes it; the file may not exist until the Mate is
// linked.
const EnvHQGitCredential = hq.EnvGitCredential

// LaunchEnvLines is what a mate launch adds to the server's environment on
// top of the live env store and the identity contract: its public prefix, the
// status file it reads a new Mate's setup from (status.go), the file it reports
// its boot progress in (BootFilePath), and the files it keeps its Git
// credential and last state in.
func LaunchEnvLines() []string {
	return []string{
		EnvBasePath + "=" + BasePath,
		"ZCP_MATE_BOOT_FILE=" + BootFilePath(),
		EnvStatusFile + "=" + DefaultStatusFilePath(),
		EnvHQGitCredential + "=" + hq.GitCredentialPath(),
		"ZCP_MATE_STATE_FILE=" + hq.MateStatePath(),
	}
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
