package devsup

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// The supervisor reads the service's task limit from its own cgroup v2
// directory: pids.max, pids.current and the "max" line of pids.events, the
// count of forks the kernel refused at the limit.
func TestReadTasks_ReadsTheLimitTheTasksAndTheRefusedForksOfTheCgroup(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		files map[string]string
		want  Tasks
		ok    bool
	}{
		{
			name:  "the platform unit's limit",
			files: map[string]string{"pids.max": "300\n", "pids.current": "71\n", "pids.events": "max 607\n"},
			want:  Tasks{Max: 300, Current: 71, Refused: 607}, ok: true,
		},
		{
			name:  "no limit",
			files: map[string]string{"pids.max": "max\n", "pids.current": "12\n", "pids.events": "max 0\n"},
			want:  Tasks{Max: 0, Current: 12, Refused: 0}, ok: true,
		},
		{
			name:  "pids controller absent",
			files: map[string]string{},
			ok:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			for name, body := range tt.files {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			got, ok := readTasksAt(dir)
			if ok != tt.ok || (ok && got != tt.want) {
				t.Fatalf("readTasksAt = %+v, %v; want %+v, %v", got, ok, tt.want, tt.ok)
			}
		})
	}
}

// A start the kernel refused with EAGAIN — `fork/exec /bin/sh: resource
// temporarily unavailable` — met the task limit as surely as a crash during
// which the cgroup refused a fork; an exit with neither did not.
func TestMetTaskLimit_AnEAGAINStartOrARefusedForkDuringTheRunMetTheLimit(t *testing.T) {
	t.Parallel()
	eagain := &os.PathError{Op: "fork/exec", Path: "/bin/sh", Err: syscall.EAGAIN}
	before := Tasks{Max: 300, Current: 120, Refused: 4}
	tests := []struct {
		name  string
		err   error
		after Tasks
		ok    bool
		want  bool
	}{
		{"start refused with EAGAIN", eagain, before, true, true},
		{"start refused with EAGAIN, no cgroup to read", eagain, Tasks{}, false, true},
		{"a fork refused while it ran", nil, Tasks{Max: 300, Current: 299, Refused: 5}, true, true},
		{"no fork refused while it ran", nil, before, true, false},
		{"another start failure", errors.New("cwd api is not a directory"), Tasks{Max: 300, Current: 120, Refused: 4}, true, false},
		{"no cgroup to read", nil, Tasks{}, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := metTaskLimit(tt.err, before, tt.after, tt.ok); got != tt.want {
				t.Fatalf("metTaskLimit = %v, want %v", got, tt.want)
			}
		})
	}
}
