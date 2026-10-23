package devsup

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// Tasks is the task budget of the cgroup the supervisor and its servers share
// — on Zerops the platform's zerops@zerops.service unit, whose pids.max is
// systemd's default TasksMax (docs/spec-mate.md §2.8a.2).
type Tasks struct {
	// Max is pids.max; 0 when the cgroup sets no limit.
	Max int
	// Current is pids.current: the tasks (processes and threads) alive now.
	Current int
	// Refused is the "max" count of pids.events: the forks the kernel refused
	// because the cgroup was at Max.
	Refused int64
}

// TaskLimit names the task limit a crashed server met: Max is the limit,
// Current the tasks alive when the state was read.
type TaskLimit struct {
	Max     int `json:"max"`
	Current int `json:"current"`
}

// readTasksAt reads the task budget from the cgroup v2 directory dir; false
// when the pids controller is not there.
func readTasksAt(dir string) (Tasks, bool) {
	read := func(name string) (string, bool) {
		b, err := os.ReadFile(filepath.Join(dir, name))
		return strings.TrimSpace(string(b)), err == nil
	}
	maxS, ok1 := read("pids.max")
	curS, ok2 := read("pids.current")
	events, ok3 := read("pids.events")
	if !ok1 || !ok2 || !ok3 {
		return Tasks{}, false
	}
	var t Tasks
	if maxS != "max" {
		t.Max, _ = strconv.Atoi(maxS)
	}
	t.Current, _ = strconv.Atoi(curS)
	for line := range strings.SplitSeq(events, "\n") {
		if k, v, ok := strings.Cut(line, " "); ok && k == "max" {
			t.Refused, _ = strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		}
	}
	return t, true
}

// metTaskLimit reports whether a server's start or run met the task limit:
// its start was refused with EAGAIN (`fork/exec /bin/sh: resource temporarily
// unavailable`), or the cgroup refused a fork between its start (before) and
// its exit (after) — the EAGAIN its own tools crashed on (uv's rayon pool,
// pnpm). ok says after and before were both read.
func metTaskLimit(startErr error, before, after Tasks, ok bool) bool {
	if startErr != nil {
		return errors.Is(startErr, syscall.EAGAIN)
	}
	return ok && after.Refused > before.Refused
}

// limitReason is the restart reason of a start after the task limit was met.
func limitReason(t *Tasks) string {
	if t == nil || t.Max == 0 {
		return "the service's task limit was reached"
	}
	return "the service's task limit (" + strconv.Itoa(t.Max) + ") was reached"
}
