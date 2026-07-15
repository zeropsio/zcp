//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/windows"
)

func configureCapturedChild(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
}

func captureProcessSignals() []os.Signal { return []os.Signal{os.Interrupt} }

func forwardCapturedSignal(cmd *exec.Cmd, _ os.Signal) error {
	if err := windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(cmd.Process.Pid)); err == nil {
		return nil
	}
	return cmd.Process.Kill()
}

func capturedExitCode(exitErr *exec.ExitError) int {
	if code := exitErr.ExitCode(); code >= 0 {
		return code
	}
	return 1
}

func captureControlDir(stateDir string) string {
	return filepath.Join(stateDir, "control")
}

func configureCaptureDaemonCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
}

func stopStartingCaptureDaemon(cmd *exec.Cmd) error { return cmd.Process.Kill() }

func terminateCaptureDaemon(_ context.Context, _ string, pid int, captureID string, _ bool) error {
	if pid <= 0 || captureID == "" {
		return errors.New("capture daemon process identity is incomplete")
	}
	// Windows does not expose another process's full command line through a
	// stable stdlib API. Never kill a potentially reused PID without proving
	// the capture ID; control-socket shutdown remains primary and the manager
	// retains BROKEN ownership state if it is unavailable.
	return fmt.Errorf("identity-checked capture daemon signal fallback is unavailable on windows for pid %d", pid)
}

func captureShutdownSignals() []os.Signal { return []os.Signal{os.Interrupt} }
