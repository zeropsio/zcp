package init

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/runtime"
)

// mateEnsureInstalled converges the installed mate bundle toward the stable
// release manifest — or leaves it alone, when the desired version is already live or a
// dev build is being kept. Package-level so tests can stub the one step that
// may reach the network; production is mate.EnsureInstalled (bounded timeouts
// throughout).
var mateEnsureInstalled = mate.EnsureInstalled

// mateWaitReady proves the installed release started healthy; package-level so tests
// need no loopback Mate.
var mateWaitReady = mate.WaitReady

// mateBootReadyWait bounds cold observation without rolling back an unproved boot.
const mateBootReadyWait = 30 * time.Second

// defaultMateLockWait bounds init's wait on the install lock the unit's own
// start may hold (mate.LockInstall): longer than an install's own bounds, so
// a live install is waited out and a hung one is not.
const defaultMateLockWait = 4 * time.Minute

var mateLockWait = defaultMateLockWait

// mateUnitFilePath is where `zsc unit create` lands the unit. Package-level so
// tests can point the existence check at a temp path instead of /usr/lib.
var mateUnitFilePath = mate.UnitFilePath

// mateUnitFileExists reports whether a previous `zcp init` registered the
// unit — the same check ensureMateUnit uses to tell "already registered" from
// "first boot". init.go's step-registration gate reads this so a container
// that never had mate (flag off, no unit ever created) prints no step line at
// all, while a container that DOES carry a leftover unit still gets a chance
// to reconcile it away even with the flag off.
func mateUnitFileExists() bool {
	_, err := os.Stat(mateUnitFilePath)
	return err == nil
}

// reconcileMate converges this container toward the state rt.MateEnabled asks
// for — a two-directional step, unlike every other init step:
//
//   - enabled: make Zerops Mate available — a bundle at the prefix, the
//     identity contract on disk, and a supervised unit (unchanged from
//     before the flag existed; see the enable-path documentation below).
//   - disabled: undo exactly what the enabled direction sets up — stop and
//     remove the unit, and drop the identity contract. mate.Prefix() (the
//     downloaded/dev-pushed bundle) is deliberately left alone: re-enabling
//     later must cost no network, only a `zcp init`.
//
// The step is BEST-EFFORT (see step.degraded): `zcp init` is a run.init
// command, so a container with no bundle, no reachable release/registry, or
// a stubborn unit must still start.
func reconcileMate(_ string, rt runtime.Info) error {
	if !rt.MateEnabled {
		return disableMate()
	}
	return enableMate(rt)
}

// disableMate undoes an enabled container's install steps for a container whose
// flag has since been turned off:
//
//  1. If the unit file is absent, there is nothing to reconcile — return nil
//     having done nothing at all (this is the common case: most containers
//     never had mate enabled in the first place).
//  2. Stop the unit. Best-effort: the unit may already be stopped (a prior
//     crash, a prior partial disable), and refusing to continue over that
//     would strand the unit registered forever.
//  3. Remove the unit via `zsc unit remove` — the one real failure this
//     function returns, because a removal that silently fails leaves a
//     server the operator believes is gone still running.
//  4. Remove the identity contract file. A missing file (the common case —
//     the flag might have been off since before the file was ever written)
//     is not an error.
//
// mate.Prefix() is never touched: the bundle stays on disk by design.
func disableMate() error {
	if !mateUnitFileExists() {
		return nil
	}

	unit := "zerops@" + mate.UnitName + ".service"
	fmt.Fprintf(os.Stderr, "    → stopping %s\n", unit)
	if err := commandRunner("sudo", "systemctl", "stop", unit); err != nil {
		fmt.Fprintf(os.Stderr, "    ! systemctl stop %s failed (continuing — the unit may already be stopped): %v\n", unit, err)
	}

	fmt.Fprintf(os.Stderr, "    → removing unit zerops@%s\n", mate.UnitName)
	if err := commandRunner("sudo", "-E", "zsc", "unit", "remove", mate.UnitName); err != nil {
		return fmt.Errorf("zsc unit remove %s: %w", mate.UnitName, err)
	}

	if err := os.Remove(mate.EnvFilePath()); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove %s: %w", mate.EnvFilePath(), err)
	}
	return nil
}

// enableMate makes Zerops Mate available on this container: a bundle at
// mate.CurrentLink(), the identity contract on disk, and a supervised unit.
//
// The bundle itself is mateEnsureInstalled's job in full — same version already
// live (the common warm-restart case) costs only the manifest read, and a
// hand-pushed dev build at or above MinimumMateVersion is kept unless forced.
// See mate.EnsureInstalled for the pass in detail.
//
// When no bundle is available, a first init does not register a unit whose
// ExecStart cannot resolve and would crash-loop when started.
func enableMate(rt runtime.Info) (retErr error) {
	commitLastGood := false
	// restart is set when the unit runs but its Mate does not answer
	// (mate.ErrNotAnswering): no server admitted work, so the unit restarts
	// onto the installed bundle and environment instead of a no-op start.
	restart := false
	// Always request a start of a usable installed bundle, even if this init
	// step fails. The install lock is released first by the later defer; the
	// unit's own start also needs that lock. Starting an active unit is a no-op.
	defer func() {
		if !mateUnitFileExists() {
			return
		}
		installed, err := mate.InstalledVersion()
		if err != nil || mate.BelowFloor(installed) {
			return
		}
		unit := "zerops@" + mate.UnitName + ".service"
		verb, line := "start", "ensuring "+unit+" is running"
		if restart {
			verb, line = "restart", "restarting "+unit
		}
		fmt.Fprintf(os.Stderr, "    → %s\n", line)
		if err := commandRunner("sudo", "systemctl", verb, "--no-block", unit); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("%s %s: %w", verb, unit, err))
			return
		}
		if commitLastGood {
			commitBootLastGood()
		}
	}()
	unitExisted := mateUnitFileExists()
	if unitExisted {
		// Existing units may still have a boot-start link from an older init.
		if err := ensureMateUnit(); err != nil {
			return fmt.Errorf("prepare Mate unit: %w", err)
		}
	}
	// This init cannot write a valid identity contract without a project ID.
	// The deferred start still tries a previously registered usable bundle.
	if rt.ProjectID == "" {
		return errors.New("no projectId in the container environment — mate has no Zerops project to bind to")
	}

	// Serialize staging and switching with both the updater and unit startup.
	release, lockErr := mate.LockInstall(mateLockWait)
	if lockErr != nil {
		return fmt.Errorf("mate install postponed: %w", lockErr)
	}
	defer release()
	if !unitExisted {
		// Disabling Mate removes the unit but keeps its bundle. Register that
		// usable bundle first so an update failure can still start it.
		if installed, err := mate.InstalledVersion(); err == nil && !mate.BelowFloor(installed) {
			if _, err := writeMateEnv(rt); err != nil {
				return fmt.Errorf("write Mate environment: %w", err)
			}
			if err := ensureMateUnit(); err != nil {
				return fmt.Errorf("register installed Mate: %w", err)
			}
			unitExisted = true
		}
	}
	// An unfinished updater switch owns startup and readiness. Use the same
	// recovery authority as the installer before changing its environment.
	if skip, err := mate.PrepareUpdateBoot(); err != nil {
		return fmt.Errorf("prepare Mate boot: %w", err)
	} else if skip {
		return nil
	}

	running, err := runningMate(unitExisted)
	if err != nil {
		return fmt.Errorf("determine Mate activity: %w", err)
	}
	var envChanged bool
	opts := mate.EnsureOptions{Refresh: true}
	if running {
		// The candidate must launch with the reconciled environment.
		envChanged, err = writeMateEnv(rt)
		if err != nil {
			return fmt.Errorf("write running Mate environment: %w", err)
		}
		reloadMateUnitIfChanged()
		opts.Activate = func(candidate mate.Manifest) error {
			err := switchRunningMate(candidate, release)
			if errors.Is(err, mate.ErrNotAnswering) {
				// The installer activates the release as for a stopped unit.
				fmt.Fprintf(os.Stderr, "    (mate does not answer; installing %s and restarting the unit)\n", candidate.Version)
				restart = true
			}
			return err
		}
	}

	// A boot reads the release manifest afresh: the cache is an hour old at
	// most, and a restart inside that hour kept the previous Mate build for
	// the rest of it (the owner's runs, 2026-09-17). An unreachable manifest
	// falls back to the cache, then to what is installed.
	result, err := mateEnsureInstalled(opts)
	if err != nil {
		return fmt.Errorf("ensure mate bundle: %w", err)
	}
	logMateEnsureResult(os.Stderr, result)
	// Whatever the install did, a bundle still below the floor is neither
	// registered nor given the env contract that links it to HQ.
	if err := mate.CheckInstalledFloor(); err != nil {
		return err
	}

	bin := mate.BinPath()
	if _, err := os.Stat(bin); err != nil {
		return fmt.Errorf("bundle still absent after %s: %w", result.Action, err)
	}

	if !running {
		envChanged, err = writeMateEnv(rt)
		if err != nil {
			return err
		}
	}
	if !unitExisted {
		if err := ensureMateUnit(); err != nil {
			return fmt.Errorf("register Mate unit: %w", err)
		}
	}
	if !running {
		reloadMateUnitIfChanged()
	}

	if running && !restart && !bundleChanged(result) && envChanged {
		err := switchRunningMate(mate.Manifest{Version: result.To}, release)
		if errors.Is(err, mate.ErrNotAnswering) {
			fmt.Fprintf(os.Stderr, "    (mate does not answer; restarting the unit with the new environment)\n")
			restart = true
		} else if err != nil {
			return fmt.Errorf("switch Mate environment: %w", err)
		}
	}
	if running && !restart {
		// A switch proved its own readiness; an unchanged Mate proves this boot.
		commitLastGood = !bundleChanged(result) && !envChanged
		return nil
	}

	commitLastGood = true
	return nil
}

// runningMate distinguishes an inactive registered unit from a live process.
// Unknown systemd answers postpone init rather than guessing it is safe to switch.
func runningMate(unitExists bool) (bool, error) {
	if !unitExists {
		return false, nil
	}
	out, err := commandOutput("systemctl", "is-active", "zerops@"+mate.UnitName+".service")
	switch strings.TrimSpace(string(out)) {
	case "active", "activating", "reloading", "deactivating":
		return true, nil
	case "inactive", "failed":
		return false, nil
	default:
		if err != nil {
			return false, fmt.Errorf("read Mate unit activity: %w", err)
		}
		return false, fmt.Errorf("unknown Mate unit activity %q", strings.TrimSpace(string(out)))
	}
}

// switchRunningMate uses the updater's idle proof, durable admission fence and
// verified switch; no init restart of a live unit bypasses that owner.
func switchRunningMate(candidate mate.Manifest, release func()) error {
	installed, err := mate.InstalledVersion()
	if err != nil {
		return fmt.Errorf("read running install: %w", err)
	}
	state, err := mate.ReadOptionalUpdateState()
	if err != nil {
		return fmt.Errorf("read init update state: %w", err)
	}
	state = mate.WorkerUpdateState(state)
	state.Phase, state.Candidate, state.RunningVersion = "draining", candidate.Version, installed
	if err := mate.WriteUpdateState(state); err != nil {
		return fmt.Errorf("record init drain: %w", err)
	}
	post := func(path string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return mate.UpdateRequest(ctx, path, []byte("{}"), nil)
	}
	drain, err := mate.DrainUpdate(candidate.Version, false)
	if err != nil {
		var cancelErr error
		if !errors.Is(err, mate.ErrNotAnswering) {
			cancelErr = post("cancel")
		}
		state.Phase, state.WorkerPID, state.WorkerStart = "postponed", 0, ""
		stateErr := mate.WriteUpdateState(state)
		return fmt.Errorf("init Mate switch postponed: %w", errors.Join(err, cancelErr, stateErr))
	}
	var readiness mate.RestartReadiness
	return mate.SwitchUpdate(candidate.Version, drain.BootID, mate.SwitchHooks{
		Restart: func() error {
			release()
			readiness = mate.ObserveRestart(context.Background())
			if err := commandRunner("sudo", "systemctl", "restart", "--no-block", "zerops@"+mate.UnitName+".service"); err != nil {
				return fmt.Errorf("request Mate restart: %w", err)
			}
			return nil
		},
		Ready:  func(version, boot string) error { return readiness.WaitReady(version, boot, mate.UpdateProgressWindow) },
		Commit: func() error { return post("commit") },
		Cancel: func() error { return post("cancel") },
	})
}

// commitBootLastGood moves last-good to the installed release once that release is
// serving healthy, the proof the update worker also requires. A Mate that is not ready
// in time keeps its previous last-good, and the next boot asks again.
func commitBootLastGood() {
	installed, err := mate.InstalledVersion()
	if err != nil {
		return
	}
	if good, err := mate.LastGoodVersion(); err == nil && good == installed {
		return
	}
	if err := mateWaitReady(installed, "", time.Now().Add(mateBootReadyWait)); err != nil {
		fmt.Fprintf(os.Stderr, "    ! mate %s not ready yet, last-good stays where it was: %v\n", installed, err)
		return
	}
	if err := mate.CommitLastGood(installed); err != nil {
		fmt.Fprintf(os.Stderr, "    ! record last-good %s: %v\n", installed, err)
	}
}

// bundleChanged reports whether EnsureInstalled actually replaced the bytes
// the server runs. "none" changed nothing at all — not worth a restart.
func bundleChanged(result mate.Result) bool {
	return result.Action == mate.ActionInstalled || result.Action == mate.ActionUpdated
}

// logMateEnsureResult prints one line naming what mateEnsureInstalled did, so a
// container boot's log tells "nothing changed" from "fetched a new release"
// without having to infer it from the absence of a download line.
func logMateEnsureResult(w io.Writer, result mate.Result) {
	switch {
	case result.Warning != "":
		// An unreachable/invalid release manifest with something already
		// installed (MD-10): the installed version keeps serving, so this
		// is a logged miss, not a degrade — enableMate proceeds normally.
		fmt.Fprintf(w, "    ! %s\n", result.Warning)
	case result.Action == mate.ActionNone:
		fmt.Fprintf(w, "    (mate %s already current)\n", result.To)
	case result.Action == mate.ActionInstalled:
		fmt.Fprintf(w, "    (installed mate %s)\n", result.To)
	case result.Action == mate.ActionUpdated:
		fmt.Fprintf(w, "    (updated mate %s -> %s)\n", result.From, result.To)
	}
}

// writeMateEnv records the environment the mate server needs, for the supervisor
// to merge at launch.
//
// `zcp init` is a run.init command and therefore runs with the container's
// full environment; a unit registered through `zsc unit create` is not
// guaranteed to inherit it. Writing the values here — every boot, so a new key
// never needs the unit re-created — is what makes them deterministic. Only
// non-secret identifiers are written; a token never goes in this file.
//
// Reports whether the file's CONTENT changed, so the caller can restart the
// unit that reads it: the supervisor merges this file at launch, so a service
// env an operator just edited (ZCP_MATE_ALLOWED_ORIGINS, say) reaches the running
// server no other way.
//
// ZCP_MATE_ALLOWED_ORIGINS goes through mate.ValidateAllowedOrigins first: a
// malformed value (a bare host, a path, a wildcard — see spec-mate.md §3.13)
// can never match a real Origin header, so writing it would silently
// configure an allowlist entry that does nothing. Best-effort like the rest
// of this step: one diagnostic line names the bad entry and the key is left
// unwritten, falling back to the server's own default rather than failing the
// container start.
func writeMateEnv(rt runtime.Info) (bool, error) {
	path := mate.EnvFilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}

	allowedOrigins := os.Getenv(mate.SourceAllowedOrigins)
	if bad, ok := mate.ValidateAllowedOrigins(allowedOrigins); !ok {
		fmt.Fprintf(os.Stderr, "    ! ignoring invalid %s entry %q (want a comma-separated list of https://<host>[:port] origins); mate keeps its default allowlist\n", mate.SourceAllowedOrigins, bad)
		allowedOrigins = ""
	}

	lines := mate.EnvLines(
		rt.ProjectID,
		mate.ResolveAPIHost(os.Getenv("ZCP_API_HOST")),
		allowedOrigins,
	)
	body := "# Written by `zcp init` on every container boot. Read by `zcp service start " +
		mate.UnitName + "`.\n" + strings.Join(lines, "\n") + "\n"

	previous, readErr := os.ReadFile(path)
	changed := readErr != nil || string(previous) != body

	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		return false, fmt.Errorf("write %s: %w", path, err)
	}
	return changed, nil
}

// reloadMateUnitIfChanged reloads systemd's units when systemd says the mate
// unit's file changed on disk since it loaded it (NeedDaemonReload) — what it
// warns "run 'systemctl daemon-reload'" about. Until then a restart, this
// step's or a later one, may run the old definition. Asked of systemd, not
// guessed from the file: `zsc unit create` writes the file, and whatever
// rewrote it, systemd's view is the one a restart uses. Best-effort like the
// restart: a reload that fails is named and the step goes on.
func reloadMateUnitIfChanged() {
	unit := "zerops@" + mate.UnitName + ".service"
	out, err := commandOutput("systemctl", "show", "--property=NeedDaemonReload", "--value", unit)
	if err != nil || strings.TrimSpace(string(out)) != "yes" {
		return
	}
	fmt.Fprintf(os.Stderr, "    → the unit file of %s changed on disk; reloading systemd units\n", unit)
	if err := commandRunner("sudo", "systemctl", "daemon-reload"); err != nil {
		fmt.Fprintf(os.Stderr, "    ! systemctl daemon-reload failed (a restart of %s may run its old definition): %v\n", unit, err)
	}
}

// ensureMateUnit registers the supervised unit when absent and disables its
// boot start on every enabled init.
//
// `zsc unit` has only create and remove — no idempotent upsert — and a unit
// created this way survives a container restart, while `zcp init` runs on
// every boot. So the unit file's presence is the check.
func ensureMateUnit() error {
	if !mateUnitFileExists() {
		fmt.Fprintf(os.Stderr, "    → registering unit zerops@%s\n", mate.UnitName)
		if err := commandRunner("sudo", "-E", "zsc", "unit", "create", "--enable=false", "--start=false", mate.UnitName, mate.UnitCommand); err != nil {
			return fmt.Errorf("zsc unit create %s: %w", mate.UnitName, err)
		}
	}
	unit := "zerops@" + mate.UnitName + ".service"
	if err := commandRunner("sudo", "systemctl", "disable", unit); err != nil {
		// A failed disable must not skip the update and env rewrite on every boot.
		fmt.Fprintf(os.Stderr, "    ! systemctl disable %s failed (boot start may stay enabled): %v\n", unit, err)
	}
	return nil
}
