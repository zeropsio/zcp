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
	// browserLockPath serialises every call in the container.
	browserLockPath = "/tmp/zcp-browser-owned/lock"
	// browserReleaseTimeout bounds the end-of-call tab cleanup and a stop.
	browserReleaseTimeout = 15 * time.Second
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

// walk runs housekeeping steps and fails unless every one succeeded.
func (h browserHost) walk(ctx context.Context, steps [][]string) error {
	stdout, stderr, _, err := runBrowserSteps(ctx, steps, h.step)
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr))
	}
	var results []BrowserStepResult
	if err := json.Unmarshal([]byte(stdout), &results); err != nil {
		return fmt.Errorf("browser steps: %w", err)
	}
	for _, r := range results {
		if !r.Success {
			reason := "failed"
			if r.Error != nil {
				reason = *r.Error
			}
			return fmt.Errorf("browser step %q: %s", strings.Join(r.Command, " "), reason)
		}
	}
	return nil
}

func (execBrowserRunner) Acquire(ctx context.Context, isolation string) (func(bool) error, error) {
	unlock, err := waitBrowserLock(ctx, browserLockPath)
	if err != nil {
		return nil, err
	}
	h, err := newBrowserHost()
	if err != nil {
		unlock()
		return nil, err
	}
	budget := browserDefaultTimeout
	if deadline, ok := ctx.Deadline(); ok {
		budget = time.Until(deadline)
	}
	warm, state, err := readWarmBrowser(ctx)
	if err != nil {
		unlock()
		return nil, err
	}
	reused := isolation != BrowserIsolationNewBrowser && warm.reusable(budget)
	ready := func(running bool) error {
		if running {
			if err := stopBrowser(ctx); err != nil {
				return err
			}
		}
		if err := h.start(ctx); err != nil {
			return err
		}
		return h.setup(ctx, isolation)
	}
	if reused {
		err = h.setup(ctx, isolation)
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
		return nil, err
	}
	release := func(reuse bool) error {
		defer unlock()
		// Release runs after a cancel or deadline too; its own bound.
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), browserReleaseTimeout)
		defer cancel()
		if reuse {
			err := h.closeCallTabs(rctx)
			if err == nil {
				err = writeBrowserMarker(browserMarkerIdle)
			}
			if err == nil {
				return nil
			}
			return errors.Join(fmt.Errorf("hand the browser back: %w", err), stopBrowser(rctx))
		}
		return stopBrowser(rctx)
	}
	return release, nil
}

// setup marks the browser busy (a caller that dies mid-call leaves it so,
// and the next call recycles), links the Mate's stream endpoint, and opens
// the call's tab.
func (h browserHost) setup(ctx context.Context, isolation string) error {
	if err := writeBrowserMarker(browserMarkerBusy); err != nil {
		return err
	}
	if err := linkBrowserStream(h.home); err != nil {
		return err
	}
	return h.walk(ctx, browserSetupSteps(isolation))
}

// closeCallTabs closes every tab but the base one.
func (h browserHost) closeCallTabs(ctx context.Context) error {
	list, _, err := h.step(ctx, []string{"tab", "list"})
	if err != nil {
		return fmt.Errorf("tab list: %w", err)
	}
	steps, err := browserCleanupSteps(list)
	if err != nil {
		return err
	}
	return h.walk(ctx, steps)
}

// start launches the warm browser's service.
func (h browserHost) start(ctx context.Context) error {
	if err := os.MkdirAll(browserTmpDir, 0o700); err != nil {
		return fmt.Errorf("browser profile directory: %w", err)
	}
	args := browserUnitArgs(h.binary, h.env, os.Getuid(), runtime.GOMAXPROCS(0))
	if out, err := exec.CommandContext(ctx, "sudo", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("start the browser: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// stopBrowser stops the warm browser's service: systemd kills its whole
// cgroup and removes its runtime and profile directories.
func stopBrowser(ctx context.Context) error {
	out, err := exec.CommandContext(ctx, "sudo", "-n", "systemctl", "stop", browserUnit).CombinedOutput()
	if err == nil {
		return nil
	}
	if _, state, showErr := readWarmBrowser(ctx); showErr == nil && state == "inactive" {
		return nil // already gone (collected after it ended)
	}
	return fmt.Errorf("stop the browser: %w: %s", err, strings.TrimSpace(string(out)))
}

// readWarmBrowser reads the service's state and the last call's marker.
func readWarmBrowser(ctx context.Context) (warmBrowser, string, error) {
	out, err := exec.CommandContext(ctx, "systemctl", "show", browserUnit,
		"--property=ActiveState", "--property=ActiveEnterTimestampMonotonic").Output()
	if err != nil {
		return warmBrowser{}, "", fmt.Errorf("inspect the browser: %w", err)
	}
	var state string
	var enteredUsec int64
	for line := range strings.SplitSeq(string(out), "\n") {
		if v, ok := strings.CutPrefix(line, "ActiveState="); ok {
			state = v
		}
		if v, ok := strings.CutPrefix(line, "ActiveEnterTimestampMonotonic="); ok {
			enteredUsec, _ = strconv.ParseInt(v, 10, 64)
		}
	}
	w := warmBrowser{Active: state == "active"}
	var now unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_MONOTONIC, &now); err == nil && enteredUsec > 0 {
		w.Age = time.Duration(now.Nano()) - time.Duration(enteredUsec)*time.Microsecond
	}
	if data, err := os.ReadFile(browserMarkerPath); err == nil {
		w.Marker = strings.TrimSpace(string(data))
		if info, err := os.Stat(browserMarkerPath); err == nil {
			w.IdleFor = time.Since(info.ModTime())
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
	return runBrowserSteps(rctx, steps, h.step)
}
