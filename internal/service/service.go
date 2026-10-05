// Package service provides exec wrappers for container services.
// Each wrapper starts the service as a child process and waits for it,
// forwarding signals so the service can shut down gracefully.
package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/matesetup"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
)

// ErrMateDisabled is returned by Start("mate") when ZCP_MATE_ENABLED is off. Named
// so a unit that outlived a failed removal (internal/init's disableMate, on a
// real `zsc unit remove` failure) cannot resurrect the server just by still
// existing — every launch re-checks the flag.
var ErrMateDisabled = errors.New("mate is disabled: set ZCP_MATE_ENABLED=1 and re-run `zcp init`")

type execConfig struct {
	binary string   // binary name (resolved via PATH) or an absolute path
	args   []string // argv including argv[0]
	// argsFn, when set, builds argv (argv[0] included) from the RESOLVED
	// binary path at launch time, replacing args. It exists for a service
	// whose command depends on what is installed on this container right now,
	// which a package-level literal cannot know.
	argsFn func(binary string) []string
	// extraEnvFn, when set, returns KEY=VALUE entries merged over the
	// inherited environment. For a service whose configuration is delivered
	// by `zcp init` rather than by the unit's own environment.
	extraEnvFn func() []string
	// tasksMax, when > 0, is the systemd TasksMax raised on this service's
	// unit (zerops@<name>.service) before launch. 0 = leave the default.
	tasksMax int
	// guard, when set, is checked before the binary is even resolved; a
	// non-nil error aborts Start without running anything. Only mate sets one —
	// nginx and vscode always launch.
	guard func() error
	// prepare runs after guard and before the binary is resolved: what the
	// service needs on disk before it starts.
	prepare func()
}

// services returns the exec configuration of every supervised service.
//
// A function, not a package-level map: mate's paths are derived from the service
// user's home (runtime.HomeDir), which a map literal would freeze at package
// init — before a caller (or a test) has had any chance to set HOME.
func services() map[string]execConfig {
	return map[string]execConfig{
		"nginx": {
			binary: "nginx",
			args:   []string{"nginx", "-g", "daemon off;"},
		},
		"vscode": {
			binary: "code-server",
			args:   []string{"code-server", "--auth", "none", "--bind-addr", "0.0.0.0:8081", "--disable-workspace-trust", "/var/www"},
			// code-server + the in-container AI agents (claude/codex/…) it hosts
			// spawn many subprocesses (language servers, terminals, tool calls);
			// the unit's default TasksMax (300 observed live, ~121 used at idle)
			// is exhausted under real use → `fork: Resource temporarily
			// unavailable`. Sized against the CONTAINER's shared pid budget, not in
			// isolation: the top cgroup pids.max is 2000 for ALL units. Capping
			// vscode at 1600 (80%) reserves ~400 for everything else (nginx, sshfs
			// mounts, the zerops supervisor, sshd sessions, the zcp MCP — ~70 at
			// idle) so a runaway code-server can't exhaust the whole container and
			// lock out the SSH/MCP access you'd need to recover. Still ~13× idle.
			tasksMax: 1600,
		},
		// mate (Zerops Mate) — the agent server nginx publishes under
		// mate.BasePath on the container's existing 8080 origin. It runs the
		// bundle `zcp init` installed into the prefix, never `npx`: resolving
		// the package at every container start cost 58 s on an image-fresh
		// container, and it is what a hand-delivered dev build replaces.
		"mate": {
			binary:     mate.BinPath(),
			argsFn:     mate.ServeArgv,
			extraEnvFn: mateExtraEnv,
			guard:      mateGuard,
			prepare:    mateLaunchSetupThenInstall,
		},
	}
}

// mateStorePath is the container's live env store the guard consults. A var so
// a test can point it at a temp file instead of the real root-owned path.
var mateStorePath = mate.LiveEnvStorePath

// mateGuard refuses to start mate when the container says Zerops Mate is off.
//
// It must NOT read this process's own environment alone. `zcp service start
// mate` IS the unit's ExecStart, and a systemd unit inherits almost nothing —
// HOME and PATH — so the service env carrying ZCP_MATE_ENABLED is simply not
// here. That is the same reason mateExtraEnv exists at all. Reading only
// os.Environ made the guard refuse every start on a container where mate was
// enabled, crash-looping the unit; found live on z3-eval, restart counter 13.
//
// So the flag is resolved from the live env store — the same source a login
// shell is populated from, and the same one this supervisor already merges
// into the child — with this process's own environment as an override for the
// dev loop, where the store may be absent.
func mateGuard() error {
	if mateFlagEnabled(mateStorePath) {
		return nil
	}
	return ErrMateDisabled
}

// mateFlagEnabled answers whether this container has Zerops Mate turned on.
//
// An UNREADABLE store fails OPEN, deliberately: the unit only exists because a
// `zcp init` that saw the flag on created it, and `zcp init` is also what
// removes it when the flag goes off — so on a container whose env store is
// broken, starting is strictly better than crash-looping, and the reconcile on
// the next boot still has the last word.
func mateFlagEnabled(storePath string) bool {
	if runtime.Detect().MateEnabled {
		return true
	}

	store, err := mate.LoadLiveEnv(storePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[zcp] service mate: live env store %s: %v (starting: a registered unit means `zcp init` saw the flag on)\n", storePath, err)
		return true
	}
	for _, line := range store {
		if key, value, ok := strings.Cut(line, "="); ok && key == "ZCP_MATE_ENABLED" {
			return runtime.EnvEnabled(value)
		}
	}
	return false
}

// mateExtraEnv builds mate's process environment: the container's live env store
// (mate.LiveEnvStorePath) over the unit's own inherited environment, then the
// Zerops identity contract `zcp init` wrote (mate.EnvFilePath, the T3CODE_*
// lines) over that. So mate's process environment = the container's live env
// store + the T3CODE_* file, so the agents and `zcp` it spawns see what a
// login shell sees; the store is read at unit start — a change to the
// service env needs `sudo systemctl restart zerops@mate` (or a future re-read).
//
// A missing or unreadable store or env file is reported (non-fatal, on
// stderr) and mate starts anyway: the server falls back to its upstream
// pairing behaviour, which is a diagnosable state. A unit that refuses to
// launch is not.
func mateExtraEnv() []string {
	return append(mergeMateEnv(mate.LiveEnvStorePath, mate.EnvFilePath()), mate.LaunchEnvLines()...)
}

// mergeMateEnv reads and merges the live env store and the T3CODE_* env file
// from the given paths, so the precedence logic is testable without touching
// the real, root-owned mate.LiveEnvStorePath — tests pass a temp path instead.
// mateExtraEnv is the thin production wrapper that calls this with the real
// constants.
func mergeMateEnv(storePath, envFilePath string) []string {
	store, err := mate.LoadLiveEnv(storePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[zcp] service mate: live env store %s: %v (starting with the process environment only)\n", storePath, err)
	}

	file, err := mate.ParseEnvFile(envFilePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[zcp] service mate: %v (starting without the Zerops environment — re-run `zcp init`)\n", err)
	}

	return mergeEnvLines(store, file)
}

// mergeEnvLines combines KEY=VALUE lines from the live env store with the
// T3CODE_* env file, keeping exactly one line per key. A key present in both
// takes the file's value — mate.env is the more specific, more recently
// written source (rewritten by `zcp init` on every boot); a store-only key
// (including PATH — the unit's own PATH under systemd carries none of the
// container's fuller one) passes through unchanged.
func mergeEnvLines(store, file []string) []string {
	fileKeys := make(map[string]bool, len(file))
	for _, line := range file {
		if key, _, ok := strings.Cut(line, "="); ok {
			fileKeys[key] = true
		}
	}

	merged := make([]string, 0, len(store)+len(file))
	for _, line := range store {
		key, _, ok := strings.Cut(line, "=")
		if ok && fileKeys[key] {
			continue
		}
		merged = append(merged, line)
	}
	return append(merged, file...)
}

// mateLaunchSetupThenInstall readies a launch: the status file first, with
// the boot import of a new Mate's runtimes started beside everything that
// follows — it never holds the server's start — then the bundle.
func mateLaunchSetupThenInstall() {
	mateLaunchSetup()
	mateInstallBeforeStart()
}

// mateSetupBoot is the boot import (matesetup.Boot); package-level so tests
// stand in for it.
var mateSetupBoot = matesetup.Boot

// mateLaunchSetup writes the status file the server reads (ZCP_STATUS_FILE,
// mate.LaunchEnvLines), enrolls before seeding the server's absent sign-ins,
// and starts keeping the Mate enrolled with its HQ, and starts the boot import
// when the container carries a runtimes plan. The plan, the Mate's key and its
// project come from the live env store, as the guard's flag does: a unit's
// own environment carries none of them. All run in this process, for as long
// as the server does; a restart cut short finds what it left by looking.
func mateLaunchSetup() {
	lookup := mate.LiveLookup(mateStorePath)
	path := mate.DefaultStatusFilePath()
	planSet := strings.TrimSpace(lookup(matesetup.EnvRuntimes)) != ""
	if err := matesetup.MarkLaunch(path, planSet, time.Now()); err != nil {
		fmt.Fprintf(os.Stderr, "[zcp] service mate: %v\n", err)
	}
	env := func() func(string) string { return mate.LiveLookup(mateStorePath) }
	ctx, cancel := context.WithTimeout(context.Background(), hqCallTimeout)
	err := mateHQPrepare(ctx, lookup)
	cancel()
	if err == nil {
		mateSeedSignIns(context.Background(), lookup)
	} else {
		logHQ("launch enrollment: " + err.Error() + "; starting the Mate without a sign-in seed")
	}
	go mateHQKeep(context.Background(), env)
	if planSet {
		go mateSetupBoot(context.Background(), path, env)
	}
}

// mateHQKeep keeps the Mate enrolled with its org's official HQ (hq.Keep);
// package-level so tests stand in for it.
var mateHQKeep = keepEnrolled

// mateHQPrepare makes one bounded enrollment attempt before the seed. The
// server always starts afterwards; the existing keep loop owns later enrollment.
var mateHQPrepare = prepareEnrollment

// SetMateHQPrepare stands in for launch enrollment; for tests.
func SetMateHQPrepare(fn func(context.Context, func(string) string) error) { mateHQPrepare = fn }

func prepareEnrollment(ctx context.Context, lookup func(string) string) error {
	client, err := apiClientOf(lookup)
	if err != nil {
		return err
	}
	e := mateEnroller(lookup, client)
	if e.ProjectID == "" {
		return errors.New("projectId is not in this container's environment")
	}
	_, err = e.Recheck(ctx)
	if err == nil {
		return nil
	}
	var refused *hq.RefusedError
	if !errors.Is(err, hq.ErrNotEnrolled) && !errors.Is(err, hq.ErrOtherProject) &&
		(!errors.As(err, &refused) || refused.Code != "mate_credential_required") {
		return err
	}
	info, err := client.GetUserInfo(ctx)
	if err != nil {
		return fmt.Errorf("read the key's organization: %w", err)
	}
	e.OrgID = info.ID
	_, err = e.Enroll(ctx)
	if err != nil {
		return fmt.Errorf("enroll the Mate before seeding: %w", err)
	}
	return nil
}

// SetMateHQKeep stands in for the HQ enrollment; for tests.
func SetMateHQKeep(fn func(context.Context, func() func(string) string)) { mateHQKeep = fn }

// hqCallTimeout bounds each call to HQ.
const hqCallTimeout = 15 * time.Second

// keepEnrolled is hq.Keep over the container's environment: each attempt
// builds the client from the live env store as it is then, so a rotated key
// is the one it uses. An enrollment, and the anchor read while the kept HQ
// does not answer, ask the key's own record which org it is in, to read that
// org's member list for the official HQ; a recheck asks the kept
// enrollment's HQ alone (R6). Each attempt that says something about
// this Mate leaves its outcome beside the enrollment.
func keepEnrolled(ctx context.Context, env func() func(string) string) {
	seedInput := ""
	enroller := func(ctx context.Context, withOrg bool) (hq.Enroller, error) {
		lookup := env()
		projectID := lookup("projectId")
		if projectID == "" {
			return hq.Enroller{}, errors.New("projectId is not in this container's environment")
		}
		client, err := apiClientOf(lookup)
		if err != nil {
			return hq.Enroller{}, err
		}
		e := mateEnroller(lookup, client)
		if withOrg {
			info, err := client.GetUserInfo(ctx)
			if err != nil {
				return hq.Enroller{}, fmt.Errorf("read the key's organization: %w", err)
			}
			e.OrgID = info.ID
		}
		return e, nil
	}
	hq.Keep(ctx, func(ctx context.Context) (hq.Result, error) {
		e, err := enroller(ctx, true)
		if err != nil {
			return hq.Result{}, err
		}
		return e.Enroll(ctx)
	}, func(ctx context.Context) (hq.Result, error) {
		e, err := enroller(ctx, false)
		if err != nil {
			return hq.Result{}, err
		}
		return e.Recheck(ctx)
	}, func(ctx context.Context) (bool, error) {
		e, err := enroller(ctx, true)
		if err != nil {
			return false, err
		}
		return e.Moved(ctx)
	}, hq.KeepOptions{
		Log: logHQ,
		// The Mate server says from it why its setup waits (spec-mate §2.8).
		Record: func(err error) {
			o, recorded := hq.OutcomeOf(err, time.Now().UTC())
			if !recorded {
				return
			}
			if err := hq.SaveOutcome(hq.OutcomePath(), o); err != nil {
				logHQ(err.Error())
			}
			if err == nil {
				client, openErr := hq.Open(&http.Client{Timeout: hqCallTimeout}, hq.EnrollmentPath())
				if openErr != nil {
					logHQ("sign-in seed enrollment: " + openErr.Error())
					return
				}
				if input := client.SignersInput(); input != seedInput {
					seedInput = input
					mateSeedSignIns(ctx, env())
				}
			}
		},
	})
}

// mateEnroller is the enroller of the Mate in this container, over its live
// environment: its project, and its own zcp service, which HQ holds as the
// project's one Mate. Its org is an enrollment's to read (keepEnrolled).
func mateEnroller(lookup func(string) string, client hq.Zerops) hq.Enroller {
	return hq.Enroller{
		Zerops:    client,
		HTTP:      &http.Client{Timeout: hqCallTimeout},
		ProjectID: lookup("projectId"),
		ServiceID: lookup("serviceId"),
		Path:      hq.EnrollmentPath(),
	}
}

// mateSeedSignIns seeds the server's sign-ins before it starts
// (seedSignIns); package-level so tests stand in for it.
var mateSeedSignIns = seedSignIns

// SetMateSeedSignIns stands in for the sign-in seed; for tests.
func SetMateSeedSignIns(fn func(context.Context, func(string) string)) { mateSeedSignIns = fn }

// seedSignIns reads the signers with one snapshot of the enrolled credential.
// Failure is retained for that input, reported by mate status, and never fatal
// to launch or the link. A changed enrollment permits a new attempt; removing
// the marker and restarting the unit is the operator's manual "again". HQ
// naming no signers is read once more by the unit's next start.
func seedSignIns(ctx context.Context, lookup func(string) string) {
	client, err := hq.Open(&http.Client{Timeout: hqCallTimeout}, hq.EnrollmentPath())
	if err != nil {
		logHQ("sign-ins waiting for enrollment: " + err.Error())
		return
	}
	_, err = mate.SeedSignInsForEnrollment(mate.SignInsPath(), mate.SignInsSeededPath(), client.SignersInput(), func() (map[string]string, error) {
		ctx, cancel := context.WithTimeout(ctx, hqCallTimeout)
		defer cancel()
		return client.Signers(ctx, lookup("projectId"))
	}, time.Now())
	if err != nil {
		fmt.Fprintf(os.Stderr, "[zcp] service mate: sign-ins: %v; failed input retained in %s (reported by `zcp mate status --json`); a changed enrollment permits one new attempt; to try again manually, remove the marker and start the Mate service again\n", err, mate.SignInsSeededPath())
	}
}

// apiClientOf is a client of the Zerops API over the Mate's key as the live
// env store holds it now, so a rotated key is the one it uses.
func apiClientOf(lookup func(string) string) (*platform.ZeropsClient, error) {
	key := lookup("ZCP_API_KEY")
	if key == "" {
		return nil, errors.New("ZCP_API_KEY is not in this container's environment")
	}
	client, err := platform.NewZeropsClient(key, mate.ResolveAPIHost(lookup("ZCP_API_HOST")))
	if err != nil {
		return nil, fmt.Errorf("build the API client: %w", err)
	}
	return client, nil
}

func logHQ(line string) { fmt.Fprintf(os.Stderr, "[zcp] hq: %s\n", line) }

// SetMateSetupBoot / ResetMateSetupBoot stand in for the boot import; for tests.
func SetMateSetupBoot(fn func(context.Context, string, func() func(string) string)) {
	mateSetupBoot = fn
}
func ResetMateSetupBoot() { mateSetupBoot = matesetup.Boot }

// runFunc starts a service and waits for it to exit. Tests override this.
// mateEnsure brings the installed bundle to the release; package-level so
// tests stub the network.
var mateEnsure = mate.EnsureInstalled

// defaultMateLockWait bounds the wait for `zcp init`'s install: longer than
// an install's own bounds (the manifest fetch, the download and npm install,
// the smoke test), so a live install is waited out and a hung one is not.
const defaultMateLockWait = 4 * time.Minute

var mateLockWait = defaultMateLockWait

// mateInstallBeforeStart brings the bundle to the release before the server
// starts, under the install lock `zcp init` shares (mate.LockInstall). The
// unit starts at boot on its own, before `zcp init` updates the bundle; it
// served the old release and `zcp init` restarted it onto the new one — two
// starts, each ~10 s down. Now whichever runs first installs and the other
// finds nothing to do. Nothing here ever keeps the server down: a lock held
// past the wait, or an install that fails, starts what is installed.
func mateInstallBeforeStart() {
	release, err := mate.LockInstall(mateLockWait)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[zcp] service mate: %v — starting what is installed\n", err)
		return
	}
	defer release()
	result, err := mateEnsure(mate.EnsureOptions{Refresh: true})
	switch {
	case err != nil:
		fmt.Fprintf(os.Stderr, "[zcp] service mate: bring the bundle to the release: %v — starting what is installed\n", err)
	case result.Warning != "":
		fmt.Fprintf(os.Stderr, "[zcp] service mate: %s\n", result.Warning)
	case result.Action == mate.ActionUpdated:
		fmt.Fprintf(os.Stderr, "[zcp] service mate: updated mate %s -> %s before starting\n", result.From, result.To)
	case result.Action == mate.ActionInstalled:
		fmt.Fprintf(os.Stderr, "[zcp] service mate: installed mate %s before starting\n", result.To)
	}
}

// SetMateEnsureFunc stubs the install a mate start runs; for tests.
func SetMateEnsureFunc(fn func(mate.EnsureOptions) (mate.Result, error)) { mateEnsure = fn }

// ResetMateEnsureFunc restores the real install.
func ResetMateEnsureFunc() { mateEnsure = mate.EnsureInstalled }

// SetMateLockWait bounds a mate start's wait on the install lock; for tests.
func SetMateLockWait(d time.Duration) { mateLockWait = d }

// ResetMateLockWait restores the default wait.
func ResetMateLockWait() { mateLockWait = defaultMateLockWait }

var runFunc = runCommand

// SetMateStorePath / ResetMateStorePath point the guard's live-env-store lookup at
// a test file instead of the real root-owned path.
func SetMateStorePath(path string) { mateStorePath = path }
func ResetMateStorePath()          { mateStorePath = mate.LiveEnvStorePath }

// SetRunFunc overrides the run function for testing.
func SetRunFunc(fn func(binary string, args, extraEnv []string) error) { runFunc = fn }

// ResetRunFunc restores the default run function.
func ResetRunFunc() { runFunc = runCommand }

// tuneFunc raises a systemd unit's TasksMax. Tests override this.
var tuneFunc = systemdSetTasksMax

// SetTuneFunc overrides the TasksMax tuner for testing.
func SetTuneFunc(fn func(string, int) error) { tuneFunc = fn }

// ResetTuneFunc restores the default tuner.
func ResetTuneFunc() { tuneFunc = systemdSetTasksMax }

// Start runs the named service as a child process and blocks until it exits.
// Signals (SIGINT, SIGTERM) are forwarded to the child.
func Start(name string) error {
	all := services()
	cfg, ok := all[name]
	if !ok {
		return fmt.Errorf("unknown service %q (available: %s)", name, strings.Join(sortedNames(all), ", "))
	}

	if cfg.guard != nil {
		if err := cfg.guard(); err != nil {
			return err
		}
	}

	if cfg.prepare != nil {
		cfg.prepare()
	}

	// Raise the systemd unit's TasksMax before launching. `zcp service start
	// <name>` is the ExecStart of zerops@<name>.service, so this tunes the
	// launcher's OWN unit; `set-property --runtime` does not survive a restart,
	// hence re-applying on every start. Non-fatal: a tuning failure (not under
	// systemd, no sudo, missing unit) must never block the service.
	if cfg.tasksMax > 0 {
		unit := fmt.Sprintf("zerops@%s.service", name)
		if err := tuneFunc(unit, cfg.tasksMax); err != nil {
			fmt.Fprintf(os.Stderr, "[zcp] service %s: TasksMax tune failed (non-fatal): %v\n", name, err)
		} else {
			fmt.Fprintf(os.Stderr, "[zcp] service %s: raised %s TasksMax=%d\n", name, unit, cfg.tasksMax)
		}
	}

	binary, err := exec.LookPath(cfg.binary)
	if err != nil {
		return fmt.Errorf("find %s: %w", cfg.binary, err)
	}

	args := cfg.args
	if cfg.argsFn != nil {
		args = cfg.argsFn(binary)
	}
	var extraEnv []string
	if cfg.extraEnvFn != nil {
		extraEnv = cfg.extraEnvFn()
	}

	fmt.Fprintf(os.Stderr, "[zcp] service %s: resolved %s → %s\n", name, cfg.binary, binary)
	fmt.Fprintf(os.Stderr, "[zcp] service %s: args=%v\n", name, args)

	err = runFunc(binary, args, extraEnv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[zcp] service %s: exited with error: %v\n", name, err)
	} else {
		fmt.Fprintf(os.Stderr, "[zcp] service %s: exited cleanly (code 0)\n", name)
	}
	return err
}

// runCommand starts a child process and waits for it.
// The context cancels on SIGINT/SIGTERM, which sends SIGKILL to the child.
func runCommand(binary string, args, extraEnv []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cmd := exec.CommandContext(ctx, binary, args[1:]...) //nolint:gosec // binary is resolved from a hardcoded service map via LookPath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	// extraEnv last: a later entry wins in exec's environment, so what `zcp
	// init` resolved for this container overrides whatever the unit inherited.
	cmd.Env = append(os.Environ(), extraEnv...)

	fmt.Fprintf(os.Stderr, "[zcp] exec: %s %v (pid will follow)\n", binary, args[1:])

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start: %w", err)
	}
	fmt.Fprintf(os.Stderr, "[zcp] started pid %d\n", cmd.Process.Pid)

	err := cmd.Wait()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			fmt.Fprintf(os.Stderr, "[zcp] pid %d exited: code=%d state=%s\n",
				cmd.Process.Pid, exitErr.ExitCode(), exitErr.ProcessState)
		}
		return fmt.Errorf("%s exited: %w", args[0], err)
	}
	return nil
}

// systemdSetTasksMax raises a unit's TasksMax via `systemctl set-property
// --runtime`, which applies live to the cgroup (kernel-enforced pids.max)
// without persisting to /etc/systemd — it is re-applied on every Start.
// Verified live on a Zerops container: set-property moves the unit's
// TasksMax + the cgroup's pids.max in lockstep.
func systemdSetTasksMax(unit string, tasksMax int) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	//nolint:gosec // args are derived from the hardcoded service map (unit name + int)
	cmd := exec.CommandContext(ctx, "sudo", "systemctl", "set-property", "--runtime", unit, fmt.Sprintf("TasksMax=%d", tasksMax))
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// List returns the names of all available services.
func List() []string { return sortedNames(services()) }

// sortedNames keeps every name listing (List, the unknown-service error)
// stable — map iteration order is not.
func sortedNames(all map[string]execConfig) []string {
	names := make([]string, 0, len(all))
	for name := range all {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
