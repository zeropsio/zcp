//go:build windows

package ops

import "syscall"

// defaultKill is a no-op on Windows. agent-browser is a Linux-
// container-only tool (the Zerops ZCP container runs Linux); the
// RecoverFork pidfile-based process-group kill has no Windows
// equivalent, and zerops_browser is disabled at tool-registration
// time via the AgentBrowserAvailable() LookPath gate. The Windows
// build exists so the zcp CLI can run locally on Windows against a
// remote Zerops account for non-browser workflows.
func defaultKill(pid int, sig syscall.Signal) error {
	_ = pid
	_ = sig
	return nil
}

// killSignal is a placeholder on Windows (syscall.SIGKILL is not
// defined in the windows syscall package). defaultKill ignores it
// entirely, so the concrete value is immaterial; the numeric 9
// mirrors the POSIX SIGKILL convention for readability.
const killSignal syscall.Signal = 9
