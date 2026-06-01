//go:build linux

package workflow

import (
	"fmt"
	"os"
	"strings"
)

// processStartTime returns the process start-time (field 22 of /proc/<pid>/stat,
// in clock ticks since boot) — world-readable, stable, and unique with the PID
// for the host's lifetime. Returns ("", false) when the process is gone or the
// stat line can't be parsed.
func processStartTime(pid int) (string, bool) {
	if pid <= 0 {
		return "", false
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "", false
	}
	s := string(data)
	// Field 2 (comm) is parenthesized and may itself contain spaces and ')';
	// every field AFTER the LAST ')' is plain space-separated. starttime is
	// field 22 — index (22-3)=19 in the slice that starts at field 3 (state).
	rparen := strings.LastIndexByte(s, ')')
	if rparen < 0 || rparen+2 >= len(s) {
		return "", false
	}
	fields := strings.Fields(s[rparen+2:])
	const startTimeIdx = 22 - 3 // 19
	if len(fields) <= startTimeIdx {
		return "", false
	}
	return fields[startTimeIdx], true
}
