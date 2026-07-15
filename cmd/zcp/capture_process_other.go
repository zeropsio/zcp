//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
)

func configureCapturedChild(_ *exec.Cmd) {}

func captureProcessSignals() []os.Signal { return []os.Signal{os.Interrupt} }

func forwardCapturedSignal(cmd *exec.Cmd, signal os.Signal) error {
	return cmd.Process.Signal(signal)
}

func capturedExitCode(exitErr *exec.ExitError) int {
	if code := exitErr.ExitCode(); code >= 0 {
		return code
	}
	return 1
}

func captureControlDir(stateDir string) string { return filepath.Join(stateDir, "control") }

func configureCaptureDaemonCommand(_ *exec.Cmd) {}

func stopStartingCaptureDaemon(cmd *exec.Cmd) error { return cmd.Process.Kill() }

func terminateCaptureDaemon(_ string, _ int, _ string, _ bool) error {
	return errors.New("identity-checked capture daemon signal fallback is unsupported on this platform")
}

func captureShutdownSignals() []os.Signal { return []os.Signal{os.Interrupt} }
