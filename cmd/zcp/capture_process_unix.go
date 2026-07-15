//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func configureCapturedChild(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func captureProcessSignals() []os.Signal { return []os.Signal{os.Interrupt, syscall.SIGTERM} }

func forwardCapturedSignal(cmd *exec.Cmd, received os.Signal) error {
	signal, ok := received.(syscall.Signal)
	if !ok {
		return nil
	}
	if err := syscall.Kill(-cmd.Process.Pid, signal); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
}

func capturedExitCode(exitErr *exec.ExitError) int {
	if waitStatus, ok := exitErr.Sys().(syscall.WaitStatus); ok && waitStatus.Signaled() {
		return 128 + int(waitStatus.Signal())
	}
	return exitErr.ExitCode()
}

func captureControlDir(stateDir string) string {
	return filepath.Join(os.TempDir(), "zcp-capture-"+strconv.Itoa(os.Getuid()))
}

func configureCaptureDaemonCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

func stopStartingCaptureDaemon(cmd *exec.Cmd) error {
	return cmd.Process.Signal(syscall.SIGTERM)
}

func terminateCaptureDaemon(executable string, pid int, captureID string, force bool) error {
	if pid <= 0 || captureID == "" {
		return errors.New("capture daemon process identity is incomplete")
	}
	output, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "command=").Output() //nolint:gosec,noctx // fixed bounded local process query; pid is decimal
	if err != nil {
		return fmt.Errorf("inspect capture daemon pid %d: %w", pid, err)
	}
	command := string(output)
	if !strings.Contains(command, executable) || !strings.Contains(command, "capture daemon") || !strings.Contains(command, "--capture-id "+captureID) {
		return fmt.Errorf("pid %d is not the owned capture daemon", pid)
	}
	signal := syscall.SIGTERM
	if force {
		signal = syscall.SIGKILL
	}
	if err := syscall.Kill(pid, signal); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("signal capture daemon pid %d: %w", pid, err)
	}
	return nil
}

func captureShutdownSignals() []os.Signal { return []os.Signal{os.Interrupt, syscall.SIGTERM} }
