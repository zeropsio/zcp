package devsup

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// processStart is the process's start time in clock ticks since boot
// (/proc/<pid>/stat field 22).
func processStart(pid int) (int64, bool) {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0, false
	}
	// pid (comm) state ppid pgrp …; comm may hold spaces and parentheses, so
	// count from the last ')': field 3 (state) is index 0, field 22 index 19.
	s := string(b)
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return 0, false
	}
	f := strings.Fields(s[i+1:])
	if len(f) < 20 {
		return 0, false
	}
	v, err := strconv.ParseInt(f[19], 10, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// groupRSSKB sums the resident memory (KiB) of every process in the group,
// read from /proc/<pid>/stat (pgrp, field 5) and /proc/<pid>/statm (resident
// pages, field 2).
func groupRSSKB(pgid int) int64 {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}
	var pages int64
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		b, err := os.ReadFile("/proc/" + e.Name() + "/stat")
		if err != nil {
			continue
		}
		s := string(b)
		i := strings.LastIndexByte(s, ')')
		if i < 0 {
			continue
		}
		f := strings.Fields(s[i+1:])
		if len(f) < 3 || f[2] != strconv.Itoa(pgid) {
			continue
		}
		m, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/statm")
		if err != nil {
			continue
		}
		mf := strings.Fields(string(m))
		if len(mf) < 2 {
			continue
		}
		if n, err := strconv.ParseInt(mf[1], 10, 64); err == nil {
			pages += n
		}
	}
	return pages * int64(os.Getpagesize()) / 1024
}

// ownCgroup is the cgroup v2 path this supervisor runs in, which every server
// it starts shares (/proc/self/cgroup's "0::" line); "" when there is none.
func ownCgroup() string {
	b, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return ""
	}
	for line := range strings.SplitSeq(string(b), "\n") {
		if path, ok := strings.CutPrefix(line, "0::"); ok {
			return path
		}
	}
	return ""
}

// readTasks reads the task budget of the cgroup this supervisor runs in.
func readTasks() (Tasks, bool) {
	path := ownCgroup()
	if path == "" {
		return Tasks{}, false
	}
	return readTasksAt(filepath.Join("/sys/fs/cgroup", path))
}

// readPreviousRun reads systemd's result for the previous run of the unit
// this supervisor runs in (its cgroup's last element, zerops@zerops.service on
// Zerops) from systemd's journal lines about the unit. Only root and the
// systemd-journal group read the journal; Zerops' zerops user reads it
// through passwordless sudo.
func readPreviousRun() (string, bool) {
	unit := filepath.Base(ownCgroup())
	if !strings.HasSuffix(unit, ".service") {
		return "", false
	}
	args := []string{"journalctl", "UNIT=" + unit, "--output=json", "--lines=200", "--no-pager", "--quiet"}
	if os.Geteuid() != 0 {
		args = append([]string{"sudo", "-n"}, args...)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, args[0], args[1:]...).Output()
	if err != nil {
		return "", false
	}
	return previousRunResult(out)
}
