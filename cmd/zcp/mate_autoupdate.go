package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"slices"
	"strconv"
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
func launchMateUpdateWorker(args []string) error {
	binary, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find updater executable: %w", err)
	}
	currentUser, err := user.Current()
	if err != nil {
		return fmt.Errorf("find updater user: %w", err)
	}
	argv := []string{"systemd-run", "--collect", "--unit=zcp-mate-update", "--property=Type=exec", "--property=ExecStopPost=" + strconv.Quote(binary) + " mate update --recover --worker", "--uid=" + currentUser.Uid, "--setenv=HOME=" + runtime.HomeDir()}
	if override := os.Getenv("ZCP_MATE_MANIFEST_URL"); override != "" {
		argv = append(argv, "--setenv=ZCP_MATE_MANIFEST_URL="+override)
	}
	argv = append(argv, binary, "mate", "update", "--worker")
	if slices.Contains(args, "--automatic") {
		argv = append(argv, "--automatic")
	}
	if slices.Contains(args, "--force") {
		argv = append(argv, "--force")
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
	ctx, cancel := context.WithTimeout(context.Background(), statusManifestTimeout)
	desired, err := mate.DesiredRelease(ctx, http.DefaultClient, mate.ManifestOptions{Refresh: true})
	cancel()
	if err != nil {
		return failMateUpdate(false, err.Error())
	}
	automatic := slices.Contains(args, "--automatic")
	if !slices.Contains(args, "--force") && state.FailedVersion != "" && !mate.VersionOlder(state.FailedVersion, desired.Version) {
		return 0
	}
	if automatic && !mate.AutomaticUpdateAllowed(installed, state.FailedVersion, desired) {
		return 0
	}
	if installed == desired.Version || mate.IsDevVersion(installed) && !slices.Contains(args, "--force") {
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
	if err := mate.StageRelease(desired); err != nil {
		return postpone(err)
	}
	state.Phase = "draining"
	if err := mate.WriteUpdateState(state); err != nil {
		return failMateUpdate(false, err.Error())
	}
	body, err := json.Marshal(struct {
		Version    string `json:"version"`
		DeadlineMS int    `json:"deadlineMs"`
		Automatic  bool   `json:"automatic"`
	}{desired.Version, 600000, automatic})
	if err != nil {
		return postpone(err)
	}
	var drain mate.UpdateReadiness
	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Minute+time.Second)
	err = mate.UpdateRequest(ctx, "drain", body, &drain)
	cancel()
	if err != nil || drain.Protocol != 1 || !drain.Drained || drain.BootID == "" {
		_ = mateUpdatePost("cancel")
		if err == nil {
			err = fmt.Errorf("mate did not prove idle before drain deadline")
		}
		return postpone(err)
	}
	current, currentErr := mate.InstalledVersion()
	if currentErr != nil || current != installed || (automatic && !mate.AutomaticUpdateAllowed(current, state.FailedVersion, desired)) {
		_ = mateUpdatePost("cancel")
		if currentErr == nil {
			currentErr = fmt.Errorf("installed version or compatibility changed while draining")
		}
		return postpone(currentErr)
	}
	// The durable switch record excludes installers during restart. The lock
	// must be released: the new unit takes the same lock before its launch.

	var readinessDeadline time.Time
	err = mate.SwitchUpdate(desired.Version, drain.BootID, mate.SwitchHooks{
		PreventRollback: !mate.RollbackAllowed(installed, desired),
		Restart: func() error {
			if locked {
				unlock()
				locked = false
			}
			readinessDeadline = time.Now().Add(15 * time.Second)
			ctx, cancel := context.WithDeadline(context.Background(), readinessDeadline)
			defer cancel()
			cmd := exec.CommandContext(ctx, "sudo", "systemctl", "restart", "--no-block", "zerops@mate.service")
			cmd.Stdout = os.Stderr
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("request Mate restart: %w", err)
			}
			return nil
		},
		Ready: func(version, boot string) error { return mate.WaitReady(version, boot, readinessDeadline) }, Commit: func() error { return mateUpdatePost("commit") }, Cancel: func() error { return mateUpdatePost("cancel") },
	})
	if err != nil {
		return failMateUpdate(false, err.Error())
	}
	return 0
}
func mateUpdatePost(path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return mate.UpdateRequest(ctx, path, []byte("{}"), nil)
}
func runMateUpdateRecovery() int {
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
	var deadline time.Time
	err = mate.RecoverStoppedUpdate(mate.SwitchHooks{
		Restart: func() error {
			if locked {
				unlock()
				locked = false
			}
			deadline = time.Now().Add(15 * time.Second)
			ctx, cancel := context.WithDeadline(context.Background(), deadline)
			defer cancel()
			cmd := exec.CommandContext(ctx, "sudo", "systemctl", "restart", "--no-block", "zerops@mate.service")
			cmd.Stdout = os.Stderr
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("recovery unit restart: %w", err)
			}
			return nil
		},
		Ready:  func(version, boot string) error { return mate.WaitReady(version, boot, deadline) },
		Commit: func() error { return mateUpdatePost("commit") }, Cancel: func() error { return mateUpdatePost("cancel") },
	})
	if err != nil {
		return failMateUpdate(false, err.Error())
	}
	return 0
}
