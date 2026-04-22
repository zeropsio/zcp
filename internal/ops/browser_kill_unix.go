//go:build !windows

package ops

import "syscall"

// defaultKill sends a signal to a PID on Unix-like systems. Negative
// PID targets a process group — the v27 archive's load-bearing
// mechanism for reaping Chrome + every helper inherited from the
// agent-browser daemon's fork. See browser.go RecoverFork.
func defaultKill(pid int, sig syscall.Signal) error {
	return syscall.Kill(pid, sig)
}

// killSignal is SIGKILL on Unix — the signal RecoverFork issues for
// both the process-group and daemon-direct kill. Split out so
// browser.go compiles on Windows (where syscall.SIGKILL is not
// defined).
const killSignal = syscall.SIGKILL
