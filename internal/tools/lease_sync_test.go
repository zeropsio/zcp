// Tests for: tools/lease.go — zerops_lease action=sync pushes the branch a
// lease committed on and fast-forwards every other lease of it
// (docs/spec-workflows.md §8 DP-24).
package tools

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

const (
	syncToolFrom = "1111111111111111111111111111111111111111"
	syncToolTo   = "2222222222222222222222222222222222222222"
)

// syncToolSSH is the leased containers a sync reaches: dirty names the
// aliases whose checkout holds uncommitted files, rejected has origin refuse
// the push, own names the members with commits of their own; every other
// member fast-forwards, and webpool-7 runs api without a watch (its command's
// own watcher reloads it) and crew-ada, a runtime entry nothing reloads.
type syncToolSSH struct {
	mu       sync.Mutex
	dirty    map[string]string
	rejected bool
	own      map[string]bool
	pushes   []string // hosts a push was sent to
	ffs      []string // hosts a fast-forward was sent to
}

func (s *syncToolSSH) ExecSSH(_ context.Context, host, command string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ref := "refs/heads/feat-x:refs/heads/feat-x"
	switch {
	case strings.Contains(command, "status --porcelain"):
		return []byte(s.dirty[host]), nil
	case strings.Contains(command, "rev-list --count HEAD --not --remotes=origin"):
		return []byte("1\n"), nil
	case strings.Contains(command, "push --porcelain"):
		s.pushes = append(s.pushes, host)
		if s.rejected {
			out := "@@zcp-sync commit " + syncToolTo + "\nTo " + leaseTestRepo + "\n!\t" + ref + "\t[rejected] (fetch first)\nDone\n"
			return []byte(out), &platform.SSHExecError{Hostname: host, Output: out, Err: errExit1}
		}
		return []byte("@@zcp-sync commit " + syncToolTo + "\n@@zcp-sync upstream\nTo " + leaseTestRepo + "\n*\t" + ref + "\t[new branch]\nDone\n"), nil
	case strings.Contains(command, "merge --ff-only"):
		s.ffs = append(s.ffs, host)
		if s.own[host] {
			return []byte("@@zcp-sync from " + syncToolFrom + "\n@@zcp-sync origin " + syncToolTo + "\n@@zcp-sync own 1\n"), nil
		}
		return []byte("@@zcp-sync from " + syncToolFrom + "\n@@zcp-sync origin " + syncToolTo + "\n@@zcp-sync to " + syncToolTo + "\n"), nil
	case strings.Contains(command, "@@zcp-devsup status") && host == "webpool-7":
		return []byte(`{"version":"v9.400.0","formats":[1],"capabilities":["env"]}
@@zcp-devsup version 0
{"format":1,"servers":{"api":{"source":"declared","desired":"running","status":"running"},"crew-ada":{"source":"runtime","desired":"running","status":"running","command":"bun run dev","cwd":".","port":4100}}}
@@zcp-devsup status 0
@@zcp-devsup listen 0
{"version":1,"servers":[{"name":"api","port":3001,"command":"node api.js"}]}
@@zcp-devsup declaration 0
`), nil
	}
	return nil, nil
}

func (s *syncToolSSH) ExecSSHBackground(ctx context.Context, host, command string, _ time.Duration) ([]byte, error) {
	return s.ExecSSH(ctx, host, command)
}

// errExit1 stands for a remote command that exited 1.
var errExit1 = &syncExitError{}

type syncExitError struct{}

func (*syncExitError) Error() string { return "exit status 1" }

// syncToolResult is zerops_lease action=sync's result.
type syncToolResult struct {
	Action      string                  `json:"action"`
	Sync        ops.LeaseSyncView       `json:"sync"`
	NextActions string                  `json:"nextActions"`
	Envelope    *workflow.StateEnvelope `json:"envelope"`
}

// TestLeaseTool_Sync_PushesAndFastForwardsTheBranchsOtherLeases: sync with a
// lease pushes its branch from it and fast-forwards every other lease of the
// branch, the result naming the branch, the source, the commit, the push and
// each member's outcome with how long each step took — never a lease or a
// bearer — and nextActions naming each server nothing zcp knows of reloads (a
// runtime entry), never a declared one, which reloads by itself, and each
// lease left as it was.
func TestLeaseTool_Sync_PushesAndFastForwardsTheBranchsOtherLeases(t *testing.T) {
	t.Parallel()
	ssh := &syncToolSSH{}
	srv, orchs, _ := groupLeaseServerSSH(t, ssh)
	acquireFeatX(t, srv)

	res := callTool(t, srv, "zerops_lease", map[string]any{"action": "sync", "lease": "hqpool-3"})
	text := getTextContent(t, res)
	if res.IsError {
		t.Fatalf("sync failed: %s", text)
	}
	for _, o := range orchs {
		for _, secret := range []string{o.lease(), o.token} {
			if strings.Contains(text, secret) {
				t.Errorf("result carries a secret: %s", text)
			}
		}
	}
	var got syncToolResult
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("result is not JSON: %v\n%s", err, text)
	}
	s := got.Sync
	if got.Action != "sync" || s.Branch != "feat-x" || s.Source != "hqpool-3" || s.Commit != syncToolTo {
		t.Errorf("sync = %+v, want feat-x from hqpool-3 at %s", s, syncToolTo)
	}
	if s.Push.Outcome != ops.LeaseSyncNewBranch || !s.Push.SetUpstream {
		t.Errorf("push = %+v, want new-branch with its upstream set", s.Push)
	}
	if len(s.Members) != 1 || s.Members[0].Alias != "webpool-7" || s.Members[0].Outcome != ops.LeaseSyncFastForwarded ||
		s.Members[0].From != syncToolFrom || s.Members[0].To != syncToolTo {
		t.Fatalf("members = %+v, want webpool-7 fast-forwarded", s.Members)
	}
	if len(s.Members[0].UnwatchedServers) != 1 || s.Members[0].UnwatchedServers[0] != "crew-ada" {
		t.Errorf("webpool-7 unwatched = %v, want [crew-ada]: api's own watcher reloads it", s.Members[0].UnwatchedServers)
	}
	for _, key := range []string{`"checkMs":`, `"ms":`, `"push":{`, `"members":[`} {
		if !strings.Contains(text, key) {
			t.Errorf("result lacks %s: %s", key, text)
		}
	}
	if !strings.Contains(got.NextActions, `zerops_dev_server action=restart hostname="webpool-7" name="crew-ada"`) {
		t.Errorf("nextActions %q should name crew-ada's restart on webpool-7", got.NextActions)
	}
	if strings.Contains(got.NextActions, `name="api"`) {
		t.Errorf("nextActions %q names a restart of api, which reloads by itself", got.NextActions)
	}
	if got.Envelope == nil {
		t.Errorf("no envelope in %s", text)
	}
	ssh.mu.Lock()
	defer ssh.mu.Unlock()
	if strings.Join(ssh.pushes, ",") != "hqpool-3" || strings.Join(ssh.ffs, ",") != "webpool-7" {
		t.Errorf("pushes %v, fast-forwards %v; want the push from hqpool-3 and the fast-forward of webpool-7", ssh.pushes, ssh.ffs)
	}
}

// A member left as it was is named in nextActions; the sync still succeeds.
func TestLeaseTool_Sync_MemberLeftAsItWas_NamedInNextActions(t *testing.T) {
	t.Parallel()
	ssh := &syncToolSSH{own: map[string]bool{"webpool-7": true}}
	srv, _, _ := groupLeaseServerSSH(t, ssh)
	acquireFeatX(t, srv)

	res := callTool(t, srv, "zerops_lease", map[string]any{"action": "sync", "lease": "hqpool-3"})
	text := getTextContent(t, res)
	if res.IsError {
		t.Fatalf("sync failed: %s", text)
	}
	var got syncToolResult
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("result is not JSON: %v\n%s", err, text)
	}
	if m := got.Sync.Members; len(m) != 1 || m[0].Outcome != ops.LeaseSyncHasOwnCommits || m[0].OwnCommits != 1 {
		t.Fatalf("members = %+v, want webpool-7 has-own-commits", m)
	}
	if !strings.Contains(got.NextActions, "webpool-7") || !strings.Contains(got.NextActions, ops.LeaseSyncHasOwnCommits) {
		t.Errorf("nextActions %q should name webpool-7 and why it was left as it was", got.NextActions)
	}
}

// TestLeaseTool_Sync_Refused_TypedError: a dirty source and a push origin
// refuses are typed refusals naming why, and nothing reaches a member.
func TestLeaseTool_Sync_Refused_TypedError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		ssh      *syncToolSSH
		wantCode string
		wantText []string
	}{
		{"uncommitted files in the source", &syncToolSSH{dirty: map[string]string{"hqpool-3": "?? notes.txt\n"}}, platform.ErrPrerequisiteMissing, []string{"notes.txt", "zcp does not commit"}},
		{"origin moved on", &syncToolSSH{rejected: true}, platform.ErrGitPushNonFastForward, []string{"fetch first", "origin/feat-x"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv, _, _ := groupLeaseServerSSH(t, tt.ssh)
			acquireFeatX(t, srv)
			res := callTool(t, srv, "zerops_lease", map[string]any{"action": "sync", "lease": "hqpool-3"})
			text := getTextContent(t, res)
			if !res.IsError || !strings.Contains(text, tt.wantCode) {
				t.Fatalf("result = (error %v) %s, want %s", res.IsError, text, tt.wantCode)
			}
			for _, want := range tt.wantText {
				if !strings.Contains(text, want) {
					t.Errorf("refusal %s lacks %q", text, want)
				}
			}
			tt.ssh.mu.Lock()
			defer tt.ssh.mu.Unlock()
			if len(tt.ssh.ffs) != 0 {
				t.Errorf("a refused sync fast-forwarded %v", tt.ssh.ffs)
			}
		})
	}
}

// TestLeaseTool_Sync_BadInput_TypedRefusal: sync takes one lease and nothing
// else — the branch and its leases are the lease's own, and sync never
// forces.
func TestLeaseTool_Sync_BadInput_TypedRefusal(t *testing.T) {
	t.Parallel()
	ssh := &syncToolSSH{}
	srv, _, _ := groupLeaseServerSSH(t, ssh)
	// Read once every case, run in parallel, has ended.
	t.Cleanup(func() {
		ssh.mu.Lock()
		defer ssh.mu.Unlock()
		if len(ssh.pushes)+len(ssh.ffs) != 0 {
			t.Errorf("bad input reached a container: pushes %v, fast-forwards %v", ssh.pushes, ssh.ffs)
		}
	})
	for _, tt := range []struct {
		name     string
		args     map[string]any
		wantText string
	}{
		{"no lease", map[string]any{"action": "sync"}, "sync needs lease"},
		{"a branch", map[string]any{"action": "sync", "lease": "hqpool-3", "branch": "feat-x"}, "sync takes lease only"},
		{"a pool", map[string]any{"action": "sync", "lease": "hqpool-3", "pool": "hqpool"}, "sync takes lease only"},
		{"a repo", map[string]any{"action": "sync", "lease": "hqpool-3", "repo": leaseTestRepo}, "sync takes lease only"},
		{"force", map[string]any{"action": "sync", "lease": "hqpool-3", "force": true}, "never forces"},
		{"pools", map[string]any{"action": "sync", "lease": "hqpool-3", "pools": []string{"hqpool"}}, "pools"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res := callTool(t, srv, "zerops_lease", tt.args)
			text := getTextContent(t, res)
			if !res.IsError || !strings.Contains(text, platform.ErrInvalidParameter) || !strings.Contains(text, tt.wantText) {
				t.Errorf("result = (error %v) %s, want INVALID_PARAMETER mentioning %q", res.IsError, text, tt.wantText)
			}
		})
	}
}
