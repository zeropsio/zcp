package devsup

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
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

// readTasks reads the task budget of the cgroup this supervisor runs in, which
// every server it starts shares (cgroup v2: /proc/self/cgroup's "0::" line).
func readTasks() (Tasks, bool) {
	b, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return Tasks{}, false
	}
	for line := range strings.SplitSeq(string(b), "\n") {
		if path, ok := strings.CutPrefix(line, "0::"); ok {
			return readTasksAt(filepath.Join("/sys/fs/cgroup", path))
		}
	}
	return Tasks{}, false
}
