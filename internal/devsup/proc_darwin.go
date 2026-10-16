package devsup

import "golang.org/x/sys/unix"

// processStart is the process's start time in microseconds since the epoch;
// on Linux it is /proc's start ticks. Only equality is ever compared.
func processStart(pid int) (int64, bool) {
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil || kp.Proc.P_pid != int32(pid) {
		return 0, false
	}
	t := kp.Proc.P_starttime
	return t.Sec*1_000_000 + int64(t.Usec), true
}

// groupRSSKB is 0 off Linux: the supervisor runs in Linux containers.
func groupRSSKB(int) int64 { return 0 }

// readTasks knows no task budget off Linux.
func readTasks() (Tasks, bool) { return Tasks{}, false }

// readPreviousRun knows no systemd unit off Linux.
func readPreviousRun() (string, bool) { return "", false }
