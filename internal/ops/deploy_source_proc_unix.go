//go:build unix

package ops

import (
	"errors"
	"syscall"
)

// processGone reports a pid no process holds any more (signal 0 answers
// ESRCH).
func processGone(pid int) bool {
	return errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
}
