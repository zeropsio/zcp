//go:build darwin

package workflow

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// processStartTime returns a stable, world-readable start-time string for pid
// (the kern.proc.pid kinfo_proc P_starttime). Combined with the PID it uniquely
// identifies a process for the host's lifetime, so a recycled PID (different
// start-time) is distinguished from the original. Returns ("", false) when the
// process is gone or unreadable.
func processStartTime(pid int) (string, bool) {
	if pid <= 0 {
		return "", false
	}
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil || kp == nil {
		return "", false
	}
	tv := kp.Proc.P_starttime
	return fmt.Sprintf("%d.%06d", tv.Sec, int64(tv.Usec)), true
}
