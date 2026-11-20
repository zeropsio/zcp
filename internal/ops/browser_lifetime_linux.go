//go:build linux

package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const (
	// browserLockPath serialises every call in the container. It stays
	// outside the service's directories, which every stop removes: a lock
	// file removed while held would let the next call lock a new one.
	browserLockPath = "/tmp/zcp-browser.lock"
	// browserReleaseTimeout bounds the end-of-call tab cleanup and a stop.
	browserReleaseTimeout = 15 * time.Second
	// browserPresencePath is the running browser's presence of the kept
	// sign-in (browserPresence); it dies with the service.
	browserPresencePath = browserRuntimeDir + "/zcp-signin.json"
	// browserCallPath names the call that holds the browser
	// (publishBrowserCall); beside the stream's port, it dies with the
	// service.
	browserCallPath = browserRuntimeDir + "/default.call"
)

// browserHost is how this process reaches the warm browser (browser_warm.go).
type browserHost struct {
	home   string
	binary string
	env    []string
}

func newBrowserHost() (browserHost, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return browserHost{}, fmt.Errorf("browser home: %w", err)
	}
	binary, err := exec.LookPath("agent-browser")
	if err != nil {
		return browserHost{}, fmt.Errorf("browser binary: %w", err)
	}
	return browserHost{home: home, binary: binary, env: browserEnv(home, os.Getenv("PATH"))}, nil
}

// step runs one agent-browser command against the warm browser's socket.
// Ending ctx ends only this client; the service bounds what the daemon was
// doing, and a cut call recycles the service.
func (h browserHost) step(ctx context.Context, args []string) (string, string, error) {
	cmd := exec.CommandContext(ctx, h.binary, append([]string{"--json"}, args...)...) //nolint:gosec // G204: agent-browser from PATH, args are the walk's own steps
	cmd.Env = h.env
	// One byte past the cap tells runBrowserSteps the answer was cut.
	out := &capBuffer{cap: browserOutputCap + 1}
	errBuf := &capBuffer{cap: browserOutputCap}
	cmd.Stdout, cmd.Stderr = out, errBuf
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	return out.String(), errBuf.String(), err
}

func (execBrowserRunner) Acquire(ctx context.Context, isolation string) (func(bool) (string, error), bool, string, error) {
	unlock, err := waitBrowserLock(ctx, browserLockPath)
	if err != nil {
		return nil, false, "", err
	}
	// The call's turn starts here; a call that died in its own leaves its
	// file behind, which names nobody now.
	since := time.Now()
	withdrawBrowserCall(browserCallPath)
	h, err := newBrowserHost()
	if err != nil {
		unlock()
		return nil, false, "", err
	}
	// stateDir is where the call's browser keeps the sign-in, "" when the
	// call keeps none (docs/spec-mate.md §2.6c).
	stateDir := ""
	if browserKeepsSignIn(isolation, h.home) {
		stateDir = browserStateDir(h.home)
	}
	budget := browserDefaultTimeout
	if deadline, ok := ctx.Deadline(); ok {
		budget = time.Until(deadline)
	}
	warm, state, err := readWarmBrowser(ctx)
	if err != nil {
		unlock()
		return nil, false, "", err
	}
	reused := warm.reusable(isolation, budget)
	// A call that keeps the sign-in reuses only a browser holding it in the
	// Chrome it was restored into; any other gets a fresh browser and a
	// restore.
	if reused && stateDir != "" && !keptSignInLetsReuse(ctx, h.step, browserPresencePath) {
		reused = false
	}
	fresh := false
	note := ""
	// ownTab is the call's own tab, the one its setup opened: the hand-back's
	// save reads its page's origin. ownTarget is its CDP target.
	ownTab, ownTarget := "", ""
	setup := func() (err error) {
		ownTab, ownTarget, err = h.setup(ctx, isolation)
		return err
	}
	ready := func(running bool) error {
		fresh = true
		if running {
			if err := stopBrowser(ctx); err != nil {
				return err
			}
		}
		if err := h.start(ctx, stateDir); err != nil {
			return err
		}
		if stateDir != "" {
			var err error
			if note, err = restoreBrowserSignIn(ctx, h.step, stateDir, browserPresencePath, time.Now()); err != nil {
				return err
			}
		}
		return setup()
	}
	if reused {
		err = setup()
		if err != nil {
			// The warm browser did not answer; one fresh start is the retry.
			err = ready(true)
		}
	} else {
		err = ready(state != "inactive")
	}
	if err != nil {
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), browserReleaseTimeout)
		defer cancel()
		err = errors.Join(err, stopBrowser(rctx))
		unlock()
		return nil, false, "", err
	}
	// Published only now the call has its tab; withdrawn once its tabs
	// closed or the browser stopped, before the next call can take the lock.
	withdraw, err := publishBrowserCall(browserCallPath, browserCallOf(ctx), ownTarget, since)
	if err != nil {
		fmt.Fprintf(os.Stderr, "zerops_browser: %v\n", err)
	}
	saveNote := ""
	handBack := func(rctx context.Context) error {
		var err error
		saveNote, err = handBackBrowser(rctx, h.step, stateDir, browserPresencePath, ownTab, func(mctx context.Context) error {
			w, _, err := readWarmBrowser(mctx)
			if err != nil {
				return err
			}
			return writeBrowserMarker(browserIdleMarker(w.CPU))
		})
		return err
	}
	release := func(reuse bool) (string, error) {
		defer unlock()
		defer withdraw()
		err := releaseBrowser(ctx, reuse, browserReleaseTimeout, handBack, stopBrowser)
		return saveNote, err
	}
	return release, fresh, note, nil
}

// setup marks the browser busy (a caller that dies mid-call leaves it so,
// and the next call recycles), links the Mate's stream endpoint, and opens
// the call's tab, which it answers with its CDP target.
func (h browserHost) setup(ctx context.Context, isolation string) (string, string, error) {
	if err := writeBrowserMarker(browserMarkerBusy); err != nil {
		return "", "", err
	}
	if err := linkBrowserStream(h.home); err != nil {
		return "", "", err
	}
	return readyCallTab(ctx, isolation, h.step)
}

// start launches the warm browser's service, the kept sign-in's folder in
// stateDir readied first ("" when the call keeps none).
func (h browserHost) start(ctx context.Context, stateDir string) error {
	if err := os.MkdirAll(browserTmpDir, 0o700); err != nil {
		return fmt.Errorf("browser profile directory: %w", err)
	}
	if stateDir != "" {
		if err := readyBrowserState(stateDir, time.Now()); err != nil {
			return err
		}
	}
	memory, _ := containerMemoryMax()
	args := browserUnitArgs(h.binary, h.env, os.Getuid(), runtime.GOMAXPROCS(0), memory)
	if out, err := exec.CommandContext(ctx, "sudo", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("start the browser: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// stopBrowser stops the warm browser's service: systemd kills its whole
// cgroup and removes its runtime and profile directories. It returns once
// systemd has unloaded the service: stop answers while killed Chrome
// processes are still exiting, and until its cgroup is empty the service
// stays loaded and a new start of it is refused.
func stopBrowser(ctx context.Context) error {
	out, err := exec.CommandContext(ctx, "sudo", "-n", "systemctl", "stop", browserUnit).CombinedOutput()
	if err != nil {
		if _, state, showErr := readWarmBrowser(ctx); showErr != nil || state != "inactive" {
			return fmt.Errorf("stop the browser: %w: %s", err, strings.TrimSpace(string(out)))
		}
	}
	for {
		load, err := exec.CommandContext(ctx, "systemctl", "show", browserUnit, "--property=LoadState", "--value").Output()
		if err == nil && strings.TrimSpace(string(load)) == "not-found" {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("the stopped browser is still loaded: %w", ctx.Err())
		case <-time.After(browserLockPoll):
		}
	}
}

// readWarmBrowser reads the service's state and the last call's marker.
func readWarmBrowser(ctx context.Context) (warmBrowser, string, error) {
	out, err := exec.CommandContext(ctx, "systemctl", "show", browserUnit,
		"--property=ActiveState", "--property=ActiveEnterTimestampMonotonic", "--property=CPUUsageNSec").Output()
	if err != nil {
		return warmBrowser{}, "", fmt.Errorf("inspect the browser: %w", err)
	}
	var state string
	var enteredUsec int64
	cpu := time.Duration(-1)
	for line := range strings.SplitSeq(string(out), "\n") {
		if v, ok := strings.CutPrefix(line, "CPUUsageNSec="); ok {
			if nsec, err := strconv.ParseInt(v, 10, 64); err == nil {
				cpu = time.Duration(nsec)
			}
		}
		if v, ok := strings.CutPrefix(line, "ActiveState="); ok {
			state = v
		}
		if v, ok := strings.CutPrefix(line, "ActiveEnterTimestampMonotonic="); ok {
			enteredUsec, _ = strconv.ParseInt(v, 10, 64)
		}
	}
	w := warmBrowser{Active: state == "active", CPU: cpu, IdleCPU: -1}
	var now unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_MONOTONIC, &now); err == nil && enteredUsec > 0 {
		w.Age = time.Duration(now.Nano()) - time.Duration(enteredUsec)*time.Microsecond
	}
	if data, err := os.ReadFile(browserMarkerPath); err == nil {
		var endCPU time.Duration
		w.Marker, endCPU = parseBrowserMarker(string(data))
		if info, err := os.Stat(browserMarkerPath); err == nil {
			w.IdleFor = time.Since(info.ModTime())
		}
		if endCPU >= 0 && cpu >= endCPU {
			w.IdleCPU = cpu - endCPU
		}
	}
	return w, state, nil
}

func writeBrowserMarker(value string) error {
	if err := os.WriteFile(browserMarkerPath, []byte(value+"\n"), 0o600); err != nil {
		return fmt.Errorf("browser marker: %w", err)
	}
	return nil
}

// linkBrowserStream keeps the Mate's Browser panel endpoint pointing into
// the warm browser's runtime directory; it dangles, reading as no stream,
// while no browser runs. A file a raw session put there is left alone.
func linkBrowserStream(home string) error {
	stream := filepath.Join(home, ".agent-browser", "default.stream")
	target := filepath.Join(browserRuntimeDir, "default.stream")
	if current, err := os.Readlink(stream); err == nil && current == target {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(stream), 0o700); err != nil {
		return fmt.Errorf("browser stream directory: %w", err)
	}
	if err := os.Symlink(target, stream); err != nil && !os.IsExist(err) {
		return fmt.Errorf("browser stream link: %w", err)
	}
	return nil
}

func (execBrowserRunner) Run(ctx context.Context, stdin string, timeout time.Duration) (string, string, bool, error) {
	var steps [][]string
	if err := json.Unmarshal([]byte(stdin), &steps); err != nil {
		return "", "", false, fmt.Errorf("browser batch: %w", err)
	}
	h, err := newBrowserHost()
	if err != nil {
		return "", "", false, err
	}
	rctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	stdout, stderr, truncated, walkErr := runCallSteps(rctx, steps, h.step)
	// Inside the call's lock: the origins its open steps ended on are now
	// ones a save reads.
	if err := recordBrowserOpened(browserPresencePath, stdout); err != nil {
		logBrowserSignIn(err)
	}
	return stdout, stderr, truncated, walkErr
}

// readyCallTab runs a call's setup steps (browserSetupSteps) and answers the
// call's own tab, the one its last step opened, and its CDP target, "" when
// the answer names none (agent-browser 0.39.1's window new).
func readyCallTab(ctx context.Context, isolation string, run browserStepExec) (string, string, error) {
	steps := browserSetupSteps(isolation)
	if err := walkBrowser(ctx, steps[:len(steps)-1], run); err != nil {
		return "", "", err
	}
	opened := steps[len(steps)-1]
	out, _, err := run(ctx, opened)
	var answer struct {
		Success bool `json:"success"`
		Data    struct {
			TabID    string `json:"tabId"`
			TargetID string `json:"targetId"`
		} `json:"data"`
		Error *string `json:"error"`
	}
	if jsonErr := json.Unmarshal([]byte(strings.TrimSpace(out)), &answer); jsonErr != nil || !answer.Success || answer.Data.TabID == "" {
		reason := "no tab id in its answer"
		if answer.Error != nil {
			reason = *answer.Error
		}
		return "", "", fmt.Errorf("browser step %q: %s: %w", strings.Join(opened, " "), reason, errors.Join(err, jsonErr))
	}
	return answer.Data.TabID, answer.Data.TargetID, nil
}
