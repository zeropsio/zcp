// Package service provides exec wrappers for container services.
// Each wrapper starts the service as a child process and waits for it,
// forwarding signals so the service can shut down gracefully.
package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/z3"
)

// ErrZ3Disabled is returned by Start("z3") when ZCP_Z3_ENABLED is off. Named
// so a unit that outlived a failed removal (internal/init's disableZ3, on a
// real `zsc unit remove` failure) cannot resurrect the server just by still
// existing — every launch re-checks the flag.
var ErrZ3Disabled = errors.New("z3 is disabled: set ZCP_Z3_ENABLED=1 and re-run `zcp init`")

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
	// non-nil error aborts Start without running anything. Only z3 sets one —
	// nginx and vscode always launch.
	guard func() error
}

// services returns the exec configuration of every supervised service.
//
// A function, not a package-level map: z3's paths are derived from the service
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
		// z3 (Zerops Code) — the agent server nginx publishes under
		// z3.BasePath on the container's existing 8080 origin. It runs the
		// bundle `zcp init` installed into the prefix, never `npx`: resolving
		// the package at every container start cost 58 s on an image-fresh
		// container, and it is what a hand-delivered dev build replaces.
		"z3": {
			binary:     z3.BinPath(),
			argsFn:     z3Argv,
			extraEnvFn: z3ExtraEnv,
			guard:      z3Guard,
		},
	}
}

// z3Guard refuses to start z3 when ZCP_Z3_ENABLED is off. `zcp service start
// z3` is the unit's own ExecStart, invoked directly by systemd rather than
// through `zcp init`'s runtime.Info — so the guard reads the live environment
// itself, the same way `zcp init` does, rather than trusting that the unit it
// is running under should exist at all.
func z3Guard() error {
	if runtime.Detect().Z3Enabled {
		return nil
	}
	return ErrZ3Disabled
}

// z3Argv builds the serve command for the bundle actually installed here.
//
// --base-path is a capability, not a preference: the z3 CLI rejects an unknown
// flag fatally, so a bundle predating it would crash-loop this unit at every
// boot. The probe costs one node startup at launch, and the omission is logged
// because a base-path-less z3 answers but serves root-absolute assets that the
// cookie gate then redirects — a failure that otherwise looks like "the page
// loads but nothing works".
func z3Argv(binary string) []string {
	supported := z3.SupportsBasePath(binary)
	if !supported {
		fmt.Fprintf(os.Stderr, "[zcp] service z3: installed bundle does not advertise --base-path; omitting it (z3 answers under %s/ but its assets will not resolve)\n", z3.BasePath)
	}
	return z3.ServeArgv(binary, supported)
}

// z3ExtraEnv builds z3's process environment: the container's live env store
// (z3.LiveEnvStorePath) over the unit's own inherited environment, then the
// Zerops identity contract `zcp init` wrote (z3.EnvFilePath, the T3CODE_*
// lines) over that. So z3's process environment = the container's live env
// store + the T3CODE_* file, so the agents and `zcp` it spawns see what a
// login shell sees; the store is read at unit start — a change to the
// service env needs `sudo systemctl restart zerops@z3` (or a future re-read).
//
// A missing or unreadable store or env file is reported (non-fatal, on
// stderr) and z3 starts anyway: the server falls back to its upstream
// pairing behaviour, which is a diagnosable state. A unit that refuses to
// launch is not.
func z3ExtraEnv() []string {
	return mergeZ3Env(z3.LiveEnvStorePath, z3.EnvFilePath())
}

// mergeZ3Env reads and merges the live env store and the T3CODE_* env file
// from the given paths, so the precedence logic is testable without touching
// the real, root-owned z3.LiveEnvStorePath — tests pass a temp path instead.
// z3ExtraEnv is the thin production wrapper that calls this with the real
// constants.
func mergeZ3Env(storePath, envFilePath string) []string {
	store, err := z3.LoadLiveEnv(storePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[zcp] service z3: live env store %s: %v (starting with the process environment only)\n", storePath, err)
	}

	file, err := z3.ParseEnvFile(envFilePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[zcp] service z3: %v (starting without the Zerops environment — re-run `zcp init`)\n", err)
	}

	return mergeEnvLines(store, file)
}

// mergeEnvLines combines KEY=VALUE lines from the live env store with the
// T3CODE_* env file, keeping exactly one line per key. A key present in both
// takes the file's value — z3.env is the more specific, more recently
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

// runFunc starts a service and waits for it to exit. Tests override this.
var runFunc = runCommand

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
