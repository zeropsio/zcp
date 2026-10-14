package devsup

import (
	"os"
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
