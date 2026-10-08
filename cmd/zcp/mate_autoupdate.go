package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
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

const mateUpdateLoopback = "http://127.0.0.1:3773/api/mate/update/"

type updateReadiness struct {
	Protocol int    `json:"protocol"`
	Version  string `json:"version"`
	BootID   string `json:"bootId"`
	Ready    bool   `json:"ready"`
	Drained  bool   `json:"drained"`
}

func updateRequest(ctx context.Context, path string, body []byte, out *updateReadiness) error {
	method := http.MethodGet
	if body != nil {
		method = http.MethodPost
	}
	req, err := http.NewRequestWithContext(ctx, method, mateUpdateLoopback+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("update request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("update %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return &mateUpdateHTTPError{path: path, status: resp.StatusCode}
	}
	if out == nil {
		_, err := io.Copy(io.Discard, resp.Body)
		return err
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(out); err != nil {
		return fmt.Errorf("update %s payload: %w", path, err)
	}
	return nil
}

type mateUpdateHTTPError struct {
	path   string
	status int
}

func (e *mateUpdateHTTPError) Error() string {
	return fmt.Sprintf("update %s: HTTP %d", e.path, e.status)
}
func mateUpdateProbe() (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var r updateReadiness
	if err := updateRequest(ctx, "readiness", nil, &r); err != nil {
		var status *mateUpdateHTTPError
		if errors.As(err, &status) && status.status == http.StatusNotFound {
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
	var drain updateReadiness
	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Minute+time.Second)
	err = updateRequest(ctx, "drain", body, &drain)
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
		Ready: func(version, boot string) error { return mateUpdateWaitReady(version, boot, readinessDeadline) }, Commit: func() error { return mateUpdatePost("commit") }, Cancel: func() error { return mateUpdatePost("cancel") },
	})
	if err != nil {
		return failMateUpdate(false, err.Error())
	}
	return 0
}
func mateUpdatePost(path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return updateRequest(ctx, path, []byte("{}"), nil)
}
func mateUpdateWaitReady(version, oldBoot string, deadline time.Time) error {
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var lastObservation error
	for {
		// One startup connection must not consume the whole readiness budget.
		// The capability probe uses the same transport bound; only a factual
		// exact-version/new-boot response can accept the candidate.
		probeCtx, probeCancel := context.WithTimeout(ctx, time.Second)
		var r updateReadiness
		if err := updateRequest(probeCtx, "readiness", nil, &r); err != nil {
			lastObservation = err
		} else if r.Protocol == 1 && r.Ready && r.Version == version && r.BootID != "" && r.BootID != oldBoot {
			// C-4 remains required alongside engine/database readiness.
			err := mateUpdateEnvironmentReady(probeCtx)
			if err == nil {
				probeCancel()
				return nil
			}
			lastObservation = err
		} else {
			lastObservation = fmt.Errorf("protocol=%d ready=%t version=%q boot=%q", r.Protocol, r.Ready, r.Version, r.BootID)
		}
		probeCancel()
		select {
		case <-ctx.Done():
			return fmt.Errorf("readiness for %s: %w; last observation: %w", version, ctx.Err(), lastObservation)
		case <-ticker.C:
		}
	}
}
func mateUpdateEnvironmentReady(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:3773/.well-known/t3/environment", nil)
	if err != nil {
		return fmt.Errorf("environment request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("environment probe: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var descriptor struct {
		BasePath string `json:"basePath"`
	}
	mediaType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if resp.StatusCode != http.StatusOK || mediaType != "application/json" {
		return fmt.Errorf("environment probe not JSON ready")
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&descriptor); err != nil {
		return fmt.Errorf("environment descriptor: %w", err)
	}
	if descriptor.BasePath != mate.BasePath {
		return fmt.Errorf("environment basePath mismatch")
	}
	return nil
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
		Ready:  func(version, boot string) error { return mateUpdateWaitReady(version, boot, deadline) },
		Commit: func() error { return mateUpdatePost("commit") }, Cancel: func() error { return mateUpdatePost("cancel") },
	})
	if err != nil {
		return failMateUpdate(false, err.Error())
	}
	return 0
}
