package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"slices"
	"time"

	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/runtime"
)

func mateUpdateProbe() (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var r mate.UpdateReadiness
	if err := mate.UpdateRequest(ctx, "readiness", nil, &r); err != nil {
		var status *mate.UpdateHTTPError
		if errors.As(err, &status) && status.Status == http.StatusNotFound {
			return false, nil
		}
		return false, fmt.Errorf("cannot prove Mate update capability; update postponed: %w", err)
	}
	if r.Protocol != 1 {
		return false, fmt.Errorf("unknown Mate updater protocol %d; update postponed", r.Protocol)
	}
	return true, nil
}

// startMateUpdateWorker starts the transient update worker unit.
func startMateUpdateWorker(flags ...string) error {
	argv, err := mate.UpdateWorkerArgv(flags...)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sudo", argv...)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("launch independent update worker: %w", err)
	}
	return nil
}

func runMateUpdateWorker(args []string) int {
	rt := runtime.DetectFrom(mate.LiveLookup(mate.LiveEnvStorePath))
	if !rt.InContainer || !rt.MateEnabled {
		return failMateUpdate(false, "update worker requires enabled Mate container")
	}
	if slices.Contains(args, "--recover") {
		return runMateUpdateRecovery()
	}
	unlock, err := mate.LockInstall(0)
	if err != nil {
		return failMateUpdate(false, err.Error())
	}
	locked := true
	defer func() {
		if locked {
			unlock()
		}
	}()
	installed, err := mate.InstalledVersion()
	if err != nil {
		return failMateUpdate(false, err.Error())
	}
	state, stateErr := mate.ReadOptionalUpdateState()
	if stateErr != nil {
		return failMateUpdate(false, stateErr.Error())
	}
	if pending, _, err := mate.PendingSwitch(); err != nil || pending {
		// An unfinished switch is its owner's; the worker writes nothing over
		// it, and one whose owner is gone is recovered by this unit's
		// ExecStopPost (mate.RecoverStoppedUpdate) as it exits.
		return failMateUpdate(false, "an unfinished Mate switch is pending; update postponed")
	}
	automatic, force := slices.Contains(args, "--automatic"), slices.Contains(args, "--force")
	ctx, cancel := context.WithTimeout(context.Background(), statusManifestTimeout)
	desired, err := mate.DesiredRelease(ctx, http.DefaultClient, mate.ManifestOptions{Refresh: true})
	cancel()
	switch {
	case err == nil && updateWanted(installed, state.FailedVersion, desired, automatic, force):
	case slices.Contains(args, "--restart"):
		// Init asks for a restart of the running version (its environment
		// changed): the same drain and switch, with the installed version as
		// the candidate.
		desired = mate.Manifest{Version: installed}
	case err != nil:
		return failMateUpdate(false, err.Error())
	default:
		return 0
	}
	state = mate.WorkerUpdateState(state)
	state.Phase = "staging"
	state.Candidate = desired.Version
	state.RunningVersion = installed
	if err := mate.WriteUpdateState(state); err != nil {
		return failMateUpdate(false, err.Error())
	}
	postpone := func(reason error) int {
		state.Phase = "postponed"
		_ = mate.WriteUpdateState(state)
		return failMateUpdate(false, reason.Error())
	}
	// A restart of the running version has nothing to stage: it is current.
	if desired.Version != installed {
		if err := mate.StageRelease(desired); err != nil {
			return postpone(err)
		}
	}
	state.Phase = "draining"
	if err := mate.WriteUpdateState(state); err != nil {
		return failMateUpdate(false, err.Error())
	}
	drain, err := mate.DrainUpdate(desired.Version, automatic)
	if errors.Is(err, mate.ErrNotAnswering) {
		// A server answers nothing before command readiness: a silent one may
		// be booting. It is drained once ready; only a launch that ended or
		// stopped progressing is no running Mate. An attended switch needs no
		// drain then; an automatic switch still needs the server's policy.
		if waitErr := mate.WaitRunningBoot(installed); waitErr != nil {
			if !automatic {
				fmt.Fprintf(os.Stderr, "mate update: no running Mate to drain (%v); switching\n", waitErr)
				drain, err = mate.UpdateReadiness{}, nil
			}
		} else {
			drain, err = mate.DrainUpdate(desired.Version, automatic)
		}
	}
	if err != nil {
		if !errors.Is(err, mate.ErrNotAnswering) {
			_ = mate.PostUpdate("cancel")
		}
		return postpone(err)
	}
	current, currentErr := mate.InstalledVersion()
	if currentErr != nil || current != installed {
		_ = mate.PostUpdate("cancel")
		if currentErr == nil {
			currentErr = fmt.Errorf("installed version changed while draining")
		}
		return postpone(currentErr)
	}
	// The durable switch record excludes installers during restart. The lock
	// must be released: the new unit takes the same lock before its launch.

	var readiness mate.RestartReadiness
	err = mate.SwitchUpdate(desired.Version, drain.BootID, mate.SwitchHooks{
		Restart: func() error {
			if locked {
				unlock()
				locked = false
			}
			readiness = mate.ObserveRestart(context.Background())
			ctx, cancel := context.WithTimeout(context.Background(), restartTimeout)
			defer cancel()
			cmd := exec.CommandContext(ctx, "sudo", "systemctl", "restart", "--no-block", "zerops@mate.service")
			cmd.Stdout = os.Stderr
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("request Mate restart: %w", err)
			}
			return nil
		},
		Ready: func(version, boot string) error {
			return readiness.WaitReady(version, boot, mate.UpdateProgressWindow)
		}, Commit: func() error { return mate.PostUpdate("commit") }, Cancel: func() error { return mate.PostUpdate("cancel") },
	})
	if err != nil {
		return failMateUpdate(false, err.Error())
	}
	return 0
}

// updateWanted says whether the worker installs desired over installed: never
// the installed version again; with --automatic only what
// AutomaticUpdateAllowed allows; without --force never the remembered failed
// release, never over a dev build and never an older release (an install
// never moves the Mate back).
func updateWanted(installed, failed string, desired mate.Manifest, automatic, force bool) bool {
	switch {
	case installed == desired.Version:
		return false
	case automatic && !mate.AutomaticUpdateAllowed(installed, failed, desired):
		return false
	case force:
		return true
	case failed != "" && !mate.VersionOlder(failed, desired.Version):
		return false
	case mate.IsDevVersion(installed) || mate.VersionOlder(desired.Version, installed):
		return false
	}
	return true
}

// runMateUpdateRecovery waits for the install lock as a start does: the unit's
// start command holds it while it decides the state, and a recovery refused
// then would leave a fenced server with nobody to settle it.
func runMateUpdateRecovery() int {
	unlock, err := mate.LockInstall(mateUpdateLockWait)
	if err != nil {
		return failMateUpdate(false, err.Error())
	}
	locked := true
	defer func() {
		if locked {
			unlock()
		}
	}()
	var readiness mate.RestartReadiness
	err = mate.RecoverStoppedUpdate(mate.SwitchHooks{
		Restart: func() error {
			if locked {
				unlock()
				locked = false
			}
			readiness = mate.ObserveRestart(context.Background())
			ctx, cancel := context.WithTimeout(context.Background(), restartTimeout)
			defer cancel()
			cmd := exec.CommandContext(ctx, "sudo", "systemctl", "restart", "--no-block", "zerops@mate.service")
			cmd.Stdout = os.Stderr
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("recovery unit restart: %w", err)
			}
			return nil
		},
		Ready:  func(version, boot string) error { return readiness.WaitReady(version, boot, mate.UpdateProgressWindow) },
		Commit: func() error { return mate.PostUpdate("commit") }, Cancel: func() error { return mate.PostUpdate("cancel") },
	})
	if err != nil {
		return failMateUpdate(false, err.Error())
	}
	return 0
}
