// Package z3 holds everything shared by the pieces of ZCP that install,
// supervise and publish z3 (Zerops Code, a fork of T3 Code) inside a zcp
// container: the pinned npm version, the loopback port and public path prefix
// nginx serves it under, the filesystem paths, the `t3 serve` argv, and the
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
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/schema"
)

// PinnedVersion is the npm-published z3 server version a container installs
// when it has no bundle of its own. Bumping it is a DELIBERATE step, never
// `latest`: the container fetches this on the boot path, and a surprise major
// would take the whole service start with it. Before bumping, live-check that
// `serve`'s flag surface still matches ServeArgv.
const PinnedVersion = "0.0.35"

// PackageSpec is the exact npm spec the install resolves — the single place a
// version bump touches.
const PackageSpec = "t3@" + PinnedVersion

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

// installTimeout bounds the one network step on the container's boot path.
// Measured live: 55 s for the 198-package install, 58 s cold end to end. Three
// minutes leaves headroom for a slow registry without stalling a service start
// behind a hung connection — the step degrades when it expires.
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
func BinPath() string { return filepath.Join(Prefix(), "node_modules", ".bin", "t3") }

// EnvFilePath is where `zcp init` writes the environment contract. Deliberately
// OUTSIDE Prefix(): `npm install --prefix` owns everything under it, and the
// dev loop reinstalls the bundle there.
func EnvFilePath() string { return filepath.Join(runtime.HomeDir(), ".zcp", "z3.env") }

// BaseDir is the z3 server's data directory (threads, sessions, auth), passed
// as `--base-dir`. Under the home directory rather than a mount, so a restart
// keeps the history.
func BaseDir() string { return filepath.Join(runtime.HomeDir(), ".t3") }

// InstallArgs is the argv for the one npm invocation that fetches the pinned
// bundle (argv[0] included). Only reached when BinPath() is absent.
func InstallArgs() []string {
	return []string{
		"npm", "install",
		"--prefix", Prefix(),
		"--no-audit", "--no-fund", "--loglevel=error",
		PackageSpec,
	}
}

// Install fetches the pinned bundle into Prefix(). The caller is the init step,
// which turns a failure into a degraded step — never a failed container start.
func Install() error {
	ctx, cancel := context.WithTimeout(context.Background(), installTimeout)
	defer cancel()

	if err := os.MkdirAll(Prefix(), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", Prefix(), err)
	}
	args := InstallArgs()
	cmd := exec.CommandContext(ctx, args[0], args[1:]...) //nolint:gosec // argv is built from package constants
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("npm install %s: %w", PackageSpec, err)
	}
	return nil
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
