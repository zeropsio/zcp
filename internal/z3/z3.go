// Package z3 holds everything shared by the pieces of ZCP that install,
// supervise and publish z3 (Zerops Code, a fork of T3 Code) inside a zcp
// container: the pinned release version, the loopback port and public path prefix
// nginx serves it under, the filesystem paths, the `z3 serve` argv, and the
// environment contract the z3 server reads to recognise a Zerops project.
//
// z3 rides in the zcp binary rather than in the platform's zcp@1 recipe: it is
// installed and supervised by `zcp init` (a `run.init` command) and reached
// through nginx on the container's existing 8080 origin. That is what lets a
// plain service restart — which re-runs the recipe's install.sh and picks up
// the latest zcp release — turn z3 on for a container that predates it,
// without a single platform-side change.
//
// Kept to stdlib plus two leaf packages (internal/runtime for the container's
// home rule, internal/schema for the canonical API host) on purpose: the init
// step, the process supervisor and the nginx renderer all read these values,
// and none of them should have to pull in the platform/ops stack for a port
// number.
package z3

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
	PackageName = "zerops-code"

	// PinnedVersion names a tag that must exist in zeropsio/z3. It never
	// changes without PinnedSHA256 changing in the same commit.
	PinnedVersion = "0.1.0"

	// ReleaseAssetName and ReleaseURL are derived from the two pins above.
	ReleaseAssetName = PackageName + "-" + PinnedVersion + ".tgz"
	ReleaseURL       = "https://github.com/zeropsio/z3/releases/download/v" + PinnedVersion + "/" + ReleaseAssetName

	// PinnedSHA256 is the locally computed digest of the matching z3 GitHub
	// release asset. Reproduce it by fetching and hashing that asset, for example:
	//
	//	version=0.1.0; curl -fL "https://github.com/zeropsio/z3/releases/download/v${version}/zerops-code-${version}.tgz" | sha256sum
	//
	// The release's SHA256SUMS is also useful for a human cross-check, but this
	// digest compiled into zcp remains the authority. Empty fails closed before
	// any request is made.
	PinnedSHA256 = "e40c9407bcf373265508bbf887dd284389f7ee94de89dcd8b62c7429174d57ca"
)

const (
	// ServePort is where the z3 server listens. LoopbackHost, not 0.0.0.0:
	// the port is deliberately NOT a declared zcp@1 port, so the only way in
	// is nginx's BasePath location. One door (the Zerops-identity one the z3
	// server owns), never a second.
	ServePort = 3773

	// LoopbackHost is the interface ServePort binds.
	LoopbackHost = "127.0.0.1"

	// BasePath is the public path prefix nginx publishes z3 under on the
	// container's 8080 origin, and the value passed to the server as
	// `--base-path` so the URLs it emits (assets, /ws, well-known) carry the
	// prefix. Both the nginx template and ServeArgv read this constant, so
	// moving the prefix is one edit.
	BasePath = "/z3"

	// UnitName is the `zsc unit create` name; systemd renders it as
	// zerops@z3.service. Same primitive as the sshfs mounts.
	UnitName = "z3"

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

	// WorkspaceDir is the working directory z3 bootstraps its project from:
	// the same /var/www every zcp agent already works in, with each mounted
	// dev service under /var/www/<hostname>. z3 adapts to zcp's workspace,
	// never the other way round.
	WorkspaceDir = "/var/www"
)

// The environment contract the z3 server reads to recognise a Zerops project.
// `zcp init` writes these to EnvFilePath while the full container environment
// is present; `zcp service start z3` merges the file over the unit's own
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
	SourceAllowedOrigins = "ZCP_Z3_ALLOWED_ORIGINS"
)

// LiveEnvStorePath is the container's live service-environment snapshot: a
// root-owned, world-readable JSON object of string → string (~200 keys —
// PATH, ZCP_API_KEY, the ZEROPS_* ids, VSCODE_PASSWORD, the agent flags,
// …) that the platform rewrites on every service env change, ~2 s after the
// write, no restart. It is the SAME source a login shell is populated from.
//
// z3's process environment = this store + the T3CODE_* contract file, so the
// agents and `zcp` it spawns see what a login shell sees — the alternative,
// the unit's own os.Environ() under systemd, carries only HOME and PATH. The
// store is read once at unit start (`zcp service start z3`, see
// service.z3ExtraEnv); a later service env change needs `sudo systemctl
// restart zerops@z3` (or a future re-read) to reach z3.
const LiveEnvStorePath = "/etc/zerops-zembed/env.json"

// LoadLiveEnv reads the container's live env store at path (see
// LiveEnvStorePath) and renders it as KEY=VALUE lines sorted by key, so a
// caller merging it into a child's environment gets a deterministic order.
// The store is a flat string-to-string object by contract; a value that is
// not a JSON string is rejected, same as a missing file or malformed JSON —
// all three are returned as an error, never silently dropped or coerced. The
// caller decides how to degrade (the z3 supervisor logs one line and starts
// with the process environment only — see service.z3ExtraEnv).
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
// /healthz, so a client can tell "still initializing" from "broken" before it
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

// Prefix is the npm prefix the z3 bundle is installed into. It lives under the
// service user's home so it survives a container restart (a redeploy replaces
// the container and loses it, which is the same fate as the server's own data).
func Prefix() string { return filepath.Join(runtime.HomeDir(), ".zcp", "z3") }

// BinPath is the bundle's entry point — what `npm install --prefix` links.
// Its presence is the whole local-bundle-first rule: present ⇒ used as-is with
// no version check and no network, which is what makes the hand-delivered dev
// build and the fetched release one code path.
func BinPath() string { return filepath.Join(Prefix(), "node_modules", ".bin", "z3") }

// EnvFilePath is where `zcp init` writes the environment contract. Deliberately
// OUTSIDE Prefix(): `npm install --prefix` owns everything under it, and the
// dev loop reinstalls the bundle there.
func EnvFilePath() string { return filepath.Join(runtime.HomeDir(), ".zcp", "z3.env") }

// BaseDir is the z3 server's data directory (threads, sessions, auth), passed
// as `--base-dir`. Under the home directory rather than a mount, so a restart
// keeps the history.
func BaseDir() string { return filepath.Join(runtime.HomeDir(), ".t3") }

// InstallArgs is the argv for the npm invocation that installs a downloaded
// release tarball (argv[0] included). Only reached when BinPath() is absent.
func InstallArgs(tarballPath string) []string {
	return []string{
		"npm", "install",
		"--prefix", Prefix(),
		"--no-audit", "--no-fund", "--loglevel=error",
		tarballPath,
	}
}

// InstallRelease downloads one release tarball, verifies its SHA-256, then
// installs it into Prefix(). The caller owns ctx; the production caller applies
// installTimeout to both the download and npm.
func InstallRelease(ctx context.Context, client *http.Client, releaseURL, expectedSHA256 string) error {
	if expectedSHA256 == "" {
		return fmt.Errorf("pinned SHA-256 digest is unset for %s", ReleaseAssetName)
	}

	if err := os.MkdirAll(Prefix(), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", Prefix(), err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releaseURL, nil)
	if err != nil {
		return fmt.Errorf("build z3 release request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", releaseURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("download %s: HTTP %s", releaseURL, resp.Status)
	}

	tarball, err := os.CreateTemp("", PackageName+"-*.tgz")
	if err != nil {
		return fmt.Errorf("create temporary z3 tarball: %w", err)
	}
	tarballPath := tarball.Name()
	defer func() { _ = os.Remove(tarballPath) }()

	hash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tarball, hash), resp.Body); err != nil {
		_ = tarball.Close()
		return fmt.Errorf("download %s: %w", releaseURL, err)
	}
	if err := tarball.Close(); err != nil {
		return fmt.Errorf("close downloaded %s: %w", ReleaseAssetName, err)
	}

	actualSHA256 := fmt.Sprintf("%x", hash.Sum(nil))
	if !strings.EqualFold(actualSHA256, expectedSHA256) {
		return fmt.Errorf("SHA-256 mismatch for %s: expected %s, got %s", ReleaseAssetName, expectedSHA256, actualSHA256)
	}

	// npm still resolves the tarball's runtime dependencies from its registry;
	// downloading the package itself does not make this install offline-capable.
	args := InstallArgs(tarballPath)
	cmd := exec.CommandContext(ctx, args[0], args[1:]...) //nolint:gosec // argv is built from package constants
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("npm install %s: %w", ReleaseAssetName, err)
	}
	return nil
}

// Install fetches the pinned bundle into Prefix(). The caller is the init step,
// which turns any failure into a degraded step — never a failed container
// start.
func Install() error {
	ctx, cancel := context.WithTimeout(context.Background(), installTimeout)
	defer cancel()
	return InstallRelease(ctx, http.DefaultClient, ReleaseURL, PinnedSHA256)
}

// ServeArgv is the supervised command (argv[0] included) for the z3 server.
//
// withBasePath is a CAPABILITY, not a preference: the z3 CLI treats an unknown
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
// decides (the supervisor logs it and starts z3 anyway).
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
// host the z3 server joins its API base from (https://<host>/api/rest/public):
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
