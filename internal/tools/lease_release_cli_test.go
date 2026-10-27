// Tests for: tools/lease_release_cli.go — `zcp lease release`'s answer, the
// Mate server's release of a branch when a person archives its conversation
// (docs/spec-workflows.md §8 DP-35): the tool's branch release, never forced,
// refused while another live session shares a lease, idempotent, its JSON
// the contract the Mate decodes.
package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/runtime"
)

// writeLeaseSession records pid as a session that acquired (reused false) or
// reused alias, acquired at acquiredAt, the way an acquire records its own.
func writeLeaseSession(t *testing.T, stateDir, alias, acquiredAt string, pid int, reused bool) {
	t.Helper()
	dir := filepath.Join(stateDir, leaseSessionsDir, alias)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(leaseSession{AcquiredAt: acquiredAt, PID: pid, At: acquiredAt, Reused: reused})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, strconv.Itoa(pid)+".json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

// acquiredAtOf is alias's acquire time as zcp's records keep it.
func acquiredAtOf(t *testing.T, stateDir, alias string) string {
	t.Helper()
	info, ok, err := ops.LeaseTarget(stateDir, alias)
	if err != nil || !ok {
		t.Fatalf("lease %s: %v, %v", alias, ok, err)
	}
	return info.AcquiredAt
}

// TestLeaseReleaseAnswer_Contract: `zcp lease release --json` answers, by its
// exit code, one of three shapes — released (every lease the call released,
// by alias, with what became of its container), refused (each lease that
// keeps the branch: its unsaved files and commits, or how many other live
// sessions share it) or an error — and a branch zcp holds no lease of is
// released already: exit 0, nothing listed.
func TestLeaseReleaseAnswer_Contract(t *testing.T) {
	t.Parallel()
	alive := func(pid int, _ string) bool { return pid == 4242 }
	tests := []struct {
		name     string
		files    map[string]string
		sessions func(t *testing.T, stateDir string)
		branch   string
		repo     string
		wantCode int
		wantJSON string
		wantLeft []string
	}{
		{
			name: "clean", branch: "feat-x", wantCode: LeaseReleaseExitReleased,
			wantJSON: `{"released":[{"lease":"hqpool-3","outcome":"kept-warm"},{"lease":"webpool-7","outcome":"kept-warm"}]}`,
		},
		{
			name: "nothing held of the branch", branch: "gone-already", wantCode: LeaseReleaseExitReleased,
			wantJSON: `{"released":[]}`, wantLeft: []string{"hqpool-3", "webpool-7"},
		},
		{
			name: "uncommitted files in one", branch: "feat-x", files: map[string]string{"webpool-7": "?? notes.txt\n M src/app.ts\n"},
			wantCode: LeaseReleaseExitRefused,
			wantJSON: `{"refused":[{"lease":"webpool-7","unsaved":{"files":["?? notes.txt"," M src/app.ts"],"commits":[]}}]}`,
			wantLeft: []string{"hqpool-3", "webpool-7"},
		},
		{
			name: "another live session reused one", branch: "feat-x",
			sessions: func(t *testing.T, stateDir string) {
				writeLeaseSession(t, stateDir, "hqpool-3", acquiredAtOf(t, stateDir, "hqpool-3"), 4242, true)
			},
			wantCode: LeaseReleaseExitRefused,
			wantJSON: `{"refused":[{"lease":"hqpool-3","inUse":{"sessions":1}}]}`,
			wantLeft: []string{"hqpool-3", "webpool-7"},
		},
		{
			name: "the session that reused it has ended", branch: "feat-x",
			sessions: func(t *testing.T, stateDir string) {
				writeLeaseSession(t, stateDir, "hqpool-3", acquiredAtOf(t, stateDir, "hqpool-3"), 4343, true)
			},
			wantCode: LeaseReleaseExitReleased,
			wantJSON: `{"released":[{"lease":"hqpool-3","outcome":"kept-warm"},{"lease":"webpool-7","outcome":"kept-warm"}]}`,
		},
		{
			name: "a session of an earlier lease under the alias", branch: "feat-x",
			sessions: func(t *testing.T, stateDir string) {
				writeLeaseSession(t, stateDir, "hqpool-3", "2026-01-01T00:00:00Z", 4242, true)
			},
			wantCode: LeaseReleaseExitReleased,
			wantJSON: `{"released":[{"lease":"hqpool-3","outcome":"kept-warm"},{"lease":"webpool-7","outcome":"kept-warm"}]}`,
		},
		{
			name: "the acquirer's own session", branch: "feat-x",
			sessions: func(t *testing.T, stateDir string) {
				writeLeaseSession(t, stateDir, "hqpool-3", acquiredAtOf(t, stateDir, "hqpool-3"), 4242, false)
			},
			wantCode: LeaseReleaseExitReleased,
			wantJSON: `{"released":[{"lease":"hqpool-3","outcome":"kept-warm"},{"lease":"webpool-7","outcome":"kept-warm"}]}`,
		},
		{
			name: "a repo that is no clone URL", branch: "feat-x", repo: "git@github.com:acme/app.git",
			wantCode: LeaseReleaseExitFailed, wantLeft: []string{"hqpool-3", "webpool-7"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv, _, deps := completionLeaseServer(t, &guardSSH{files: tt.files})
			res := callTool(t, srv, "zerops_lease", map[string]any{"action": "acquire", "pools": []string{"webpool", "hqpool"}, "repo": leaseTestRepo, "branch": "feat-x"})
			if res.IsError {
				t.Fatalf("group acquire failed: %s", getTextContent(t, res))
			}
			if tt.sessions != nil {
				tt.sessions(t, deps.StateDir)
			}

			answer, code := releaseLeaseBranchAnswer(t.Context(), deps, tt.repo, tt.branch, alive)

			raw, err := json.Marshal(answer)
			if err != nil {
				t.Fatal(err)
			}
			if code != tt.wantCode {
				t.Fatalf("exit %d, want %d: %s", code, tt.wantCode, raw)
			}
			if tt.wantJSON != "" && string(raw) != tt.wantJSON {
				t.Errorf("answer\n got %s\nwant %s", raw, tt.wantJSON)
			}
			if tt.wantCode == LeaseReleaseExitFailed {
				var shape map[string]any
				if err := json.Unmarshal(raw, &shape); err != nil || shape["error"] == "" || len(shape) != 1 {
					t.Errorf("a failure answers only its error: %s", raw)
				}
			}
			if left := leaseRecordsLeft(t, deps.StateDir); !slices.Equal(left, tt.wantLeft) {
				t.Errorf("leases left = %v, want %v", left, tt.wantLeft)
			}
		})
	}
}

// TestLeaseReleaseAnswer_SecondCallReleasesNothing: the same call again —
// after a restart, or once the first answered — finds the branch released:
// exit 0, nothing listed.
func TestLeaseReleaseAnswer_SecondCallReleasesNothing(t *testing.T) {
	t.Parallel()
	srv, _, deps := completionLeaseServer(t, &guardSSH{})
	if res := callTool(t, srv, "zerops_lease", map[string]any{"action": "acquire", "pools": []string{"webpool", "hqpool"}, "repo": leaseTestRepo, "branch": "feat-x"}); res.IsError {
		t.Fatalf("group acquire failed: %s", getTextContent(t, res))
	}
	never := func(int, string) bool { return false }
	if _, code := releaseLeaseBranchAnswer(t.Context(), deps, "", "feat-x", never); code != LeaseReleaseExitReleased {
		t.Fatalf("first release exit %d", code)
	}
	answer, code := releaseLeaseBranchAnswer(t.Context(), deps, leaseTestRepo, "feat-x", never)
	raw, _ := json.Marshal(answer)
	if code != LeaseReleaseExitReleased || string(raw) != `{"released":[]}` {
		t.Errorf("second release: exit %d, %s; want 0 and nothing listed", code, raw)
	}
}

// TestLeaseTool_Acquire_RecordsItsSession: inside a Mate every acquire records
// this zcp session against each lease it answers — the one it took, and one it
// returned reused — so `zcp lease release` can tell a lease another live
// session shares; outside a Mate nothing is recorded.
func TestLeaseTool_Acquire_RecordsItsSession(t *testing.T) {
	t.Parallel()
	for _, mate := range []bool{true, false} {
		t.Run(strconv.FormatBool(mate), func(t *testing.T) {
			t.Parallel()
			srv, _, stateDir := groupLeaseServerSSHIn(t, &guardSSH{}, runtime.Info{InContainer: true, MateEnabled: mate})
			for range 2 {
				if res := callTool(t, srv, "zerops_lease", map[string]any{"action": "acquire", "pools": []string{"webpool", "hqpool"}, "repo": leaseTestRepo, "branch": "feat-x"}); res.IsError {
					t.Fatalf("acquire failed: %s", getTextContent(t, res))
				}
			}
			for _, alias := range []string{"hqpool-3", "webpool-7"} {
				sessions := readLeaseSessions(stateDir, alias)
				if !mate {
					if len(sessions) != 0 {
						t.Errorf("%s: outside a Mate sessions were recorded: %+v", alias, sessions)
					}
					continue
				}
				if len(sessions) != 1 || sessions[0].PID != os.Getpid() || sessions[0].Reused || sessions[0].AcquiredAt != acquiredAtOf(t, stateDir, alias) {
					t.Errorf("%s: sessions = %+v, want this one session as the acquirer, its reuse keeping that", alias, sessions)
				}
			}
		})
	}
}
