//go:build linux

package ops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// managedBrowserCommand owns one transient service, not a PID or process name.
// The lock survives until stop completes; acquiring it proves the previous Go
// owner ended. Stopping the service kills detached descendants in every group.
// Only its private runtime directory is removed; caches and user profiles remain.
func managedBrowserCommand(ctx context.Context) (*exec.Cmd, func() error, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, nil, fmt.Errorf("browser home: %w", err)
	}
	// One lock/unit bounds every managed browser in this container.
	root := "/tmp/zcp-browser-owned"
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, nil, fmt.Errorf("browser directory: %w", err)
	}
	lock, err := os.OpenFile(filepath.Join(root, "lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, nil, fmt.Errorf("browser lock: %w", err)
	}
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = lock.Close()
		return nil, nil, fmt.Errorf("managed browser already owned (retry after its batch ends): %w", err)
	}
	unit := "zcp-browser-owned.service"
	runtimeDir := filepath.Join(root, "runtime")
	stream := filepath.Join(home, ".agent-browser", "default.stream")
	ownedStream := filepath.Join(runtimeDir, "default.stream")
	stop := func() error {
		// Cleanup is independent of the cancelled caller; the deadline bounds I/O,
		// never serves as evidence that the service or its descendants ended.
		cctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		state, err := exec.CommandContext(cctx, "sudo", "-n", "systemctl", "show", unit, "--property=LoadState", "--property=ControlGroup").Output()
		if err != nil {
			return fmt.Errorf("inspect owned browser service: %w", err)
		}
		var group string
		for _, line := range strings.Split(string(state), "\n") {
			if value, ok := strings.CutPrefix(line, "ControlGroup="); ok {
				group = value
			}
		}
		if !strings.Contains(string(state), "LoadState=not-found") {
			if out, err := exec.CommandContext(cctx, "sudo", "-n", "systemctl", "stop", unit).CombinedOutput(); err != nil {
				return fmt.Errorf("stop owned browser service: %w: %s", err, out)
			}
		}
		if group != "" {
			events, err := os.ReadFile(filepath.Join("/sys/fs/cgroup", group, "cgroup.events"))
			if err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("read owned browser cgroup: %w", err)
			}
			if err == nil && !strings.Contains(string(events), "populated 0\n") {
				return fmt.Errorf("owned browser cgroup still populated; residue retained")
			}
		}
		if target, err := os.Readlink(stream); err == nil && target == ownedStream {
			if err := os.Remove(stream); err != nil {
				return fmt.Errorf("remove owned stream link: %w", err)
			}
		}
		if err := os.RemoveAll(runtimeDir); err != nil {
			return fmt.Errorf("remove owned browser residue: %w", err)
		}
		return nil
	}
	release := func() error { err := stop(); return errors.Join(err, lock.Close()) }
	if err := stop(); err != nil {
		_ = lock.Close()
		return nil, nil, err
	}
	if err := os.MkdirAll(runtimeDir, 0700); err != nil {
		_ = lock.Close()
		return nil, nil, fmt.Errorf("browser runtime directory: %w", err)
	}
	binary, err := exec.LookPath("agent-browser")
	if err != nil {
		_ = lock.Close()
		return nil, nil, fmt.Errorf("browser binary: %w", err)
	}
	// Preserve Mate's existing stream endpoint without sharing daemon sockets.
	if err := os.MkdirAll(filepath.Dir(stream), 0700); err != nil {
		_ = lock.Close()
		return nil, nil, fmt.Errorf("browser stream directory: %w", err)
	}
	if err := os.Symlink(ownedStream, stream); err != nil && !os.IsExist(err) {
		_ = lock.Close()
		return nil, nil, fmt.Errorf("browser stream link: %w", err)
	}
	args := []string{"-n", "systemd-run", "--quiet", "--pipe", "--wait", "--collect", "--service-type=exec", "--unit=" + unit, "--uid=" + strconv.Itoa(os.Getuid()), "--property=KillMode=control-group", "--property=TimeoutStopSec=5s", "--property=TasksMax=10%", "--setenv=HOME=" + home, "--setenv=PATH=" + os.Getenv("PATH"), "--setenv=TMPDIR=" + runtimeDir, "--setenv=AGENT_BROWSER_SOCKET_DIR=" + runtimeDir, "--setenv=AGENT_BROWSER_SESSION=default", binary, "batch", "--json"}
	cmd := exec.CommandContext(ctx, "sudo", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// End the admission client before inspecting/stopping its service.
	cmd.Cancel = func() error { return errors.Join(unix.Kill(-cmd.Process.Pid, unix.SIGKILL), stop()) }
	cmd.WaitDelay = time.Second // bound inherited pipe waits if the supervisor cannot stop
	return cmd, release, nil
}
