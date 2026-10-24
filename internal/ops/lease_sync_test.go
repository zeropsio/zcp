// Tests for: ops/lease_sync.go — a sync pushes the branch a lease committed
// on and fast-forwards every other lease of the branch's group, over a
// scripted ssh fake (docs/spec-workflows.md §8 DP-24). The real-git twins are
// in lease_sync_unix_test.go.
package ops

import (
	"context"
	"errors"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/devsup"
	"github.com/zeropsio/zcp/internal/ops/devservertest"
	"github.com/zeropsio/zcp/internal/platform"
)

const (
	syncBranch = "feat-x"
	syncRepo   = "https://github.com/acme/app"
	// syncFrom is the commit the members are on before a sync; syncTo the one
	// the source pushes.
	syncFrom = "1111111111111111111111111111111111111111"
	syncTo   = "2222222222222222222222222222222222222222"
	syncOwn  = "3333333333333333333333333333333333333333"
)

// syncRecord is a lease record of the sync tests' repository.
func syncRecord(pool, svcID string, number int64, branch, acquiredAt string) leaseRecord {
	return leaseRecord{
		PoolHostname: pool, PoolServiceID: svcID, ContainerID: "c-" + pool, ContainerNumber: number,
		ContainerHostname: nodeHostnameOf(pool, number), Lease: pool + "-lease-0123456789abcdef",
		Branch: branch, Repo: syncRepo, AcquiredAt: acquiredAt,
	}
}

func nodeHostnameOf(pool string, number int64) string {
	return "node-id-" + strconv.FormatInt(number, 10) + ".runtime." + pool + ".zerops"
}

// syncRemote answers a sync's commands per container: each lease's
// uncommitted files, the source's push, each member's fetch and
// fast-forward, and each member's dev servers. A member without an answer of
// its own fast-forwards from syncFrom to syncTo; one without a dev-server
// answer has no zcp-devsup.
type syncRemote struct {
	mu     sync.Mutex
	dirty  map[string]string
	push   *syncAnswer
	member map[string]syncAnswer
	devsup map[string]string
	delay  time.Duration
	calls  []sshCall
}

// syncAnswer is what a scripted remote command answers: its output and, for a
// command that failed, the error ssh returns.
type syncAnswer struct {
	out string
	err error
}

func (r *syncRemote) ExecSSH(_ context.Context, host, command string) ([]byte, error) {
	r.mu.Lock()
	r.calls = append(r.calls, sshCall{hostname: host, command: command})
	delay := r.delay
	r.mu.Unlock()
	if delay > 0 {
		time.Sleep(delay)
	}
	switch {
	case strings.Contains(command, "status --porcelain"):
		return []byte(r.dirty[host]), nil
	case strings.Contains(command, "rev-list --count HEAD --not --remotes=origin"):
		return []byte("1\n"), nil
	case strings.Contains(command, "push --porcelain"):
		if r.push == nil {
			return []byte(syncPushed(syncBranch, syncTo)), nil
		}
		return []byte(r.push.out), r.push.err
	case strings.Contains(command, "merge --ff-only"):
		if a, ok := r.member[host]; ok {
			return []byte(a.out), a.err
		}
		return []byte(syncMemberFastForwarded(syncFrom, syncTo)), nil
	case strings.Contains(command, "@@zcp-devsup status"):
		if out, ok := r.devsup[host]; ok {
			return []byte(out), nil
		}
		return []byte("@@zcp-devsup version 127\n@@zcp-devsup listen 0\n@@zcp-devsup declaration 1\n"), nil
	}
	return nil, nil
}

func (r *syncRemote) ExecSSHBackground(ctx context.Context, host, command string, _ time.Duration) ([]byte, error) {
	return r.ExecSSH(ctx, host, command)
}

// sent are the commands sent to host, in order.
func (r *syncRemote) sent(host string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, c := range r.calls {
		if c.hostname == host {
			out = append(out, c.command)
		}
	}
	return out
}

func (r *syncRemote) all() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.calls))
	for _, c := range r.calls {
		out = append(out, c.command)
	}
	return out
}

// syncPushed is the source's push script answering a fast-forward push.
func syncPushed(branch, commit string) string {
	ref := "refs/heads/" + branch
	return "@@zcp-sync commit " + commit + "\nTo " + syncRepo + "\n \t" + ref + ":" + ref + "\t1111111..2222222\nDone\n"
}

// syncMemberFastForwarded is a member's script answering a fast-forward.
func syncMemberFastForwarded(from, to string) string {
	return "@@zcp-sync from " + from + "\n@@zcp-sync origin " + to + "\n@@zcp-sync to " + to + "\n"
}

// newSyncFixture holds two leases of syncBranch: hqpool-3, acquired first, and
// webpool-7; extra records join them.
func newSyncFixture(t *testing.T, branch string, extra ...leaseRecord) (*groupFixture, *syncRemote) {
	t.Helper()
	gf := newGroupFixture(t)
	remote := &syncRemote{dirty: map[string]string{}, member: map[string]syncAnswer{}, devsup: map[string]string{}}
	gf.deps.SSH = remote
	recs := append([]leaseRecord{
		syncRecord(groupHQPool, "svc-hq", 3, branch, "2026-10-24T10:00:00Z"),
		syncRecord(groupWebPool, "svc-web", 7, branch, "2026-10-24T10:00:01Z"),
	}, extra...)
	for _, r := range recs {
		if err := writeLeaseRecord(gf.stateDir, r); err != nil {
			t.Fatal(err)
		}
	}
	return gf, remote
}

func syncMemberOf(t *testing.T, v LeaseSyncView, alias string) LeaseSyncMember {
	t.Helper()
	for _, m := range v.Members {
		if m.Alias == alias {
			return m
		}
	}
	t.Fatalf("sync members %+v lack %s", v.Members, alias)
	return LeaseSyncMember{}
}

// TestLeaseSync_PushesThenFastForwardsEveryOtherLease: a sync of a clean
// source pushes its branch and fast-forwards every other lease of the group —
// never the source itself — and names the branch, the source, the commit
// origin now has and each member's commits before and after.
func TestLeaseSync_PushesThenFastForwardsEveryOtherLease(t *testing.T) {
	t.Parallel()
	gf, remote := newSyncFixture(t, syncBranch, syncRecord("apipool", "svc-api", 2, syncBranch, "2026-10-24T10:00:02Z"))

	v, err := LeaseSync(context.Background(), gf.deps, "hqpool-3")
	if err != nil {
		t.Fatalf("LeaseSync: %v", err)
	}
	if v.Branch != syncBranch || v.Source != "hqpool-3" || v.Commit != syncTo {
		t.Errorf("sync = branch %q source %q commit %q, want %s from hqpool-3 at %s", v.Branch, v.Source, v.Commit, syncBranch, syncTo)
	}
	if v.Push.Outcome != LeaseSyncPushed {
		t.Errorf("push outcome = %q, want %q", v.Push.Outcome, LeaseSyncPushed)
	}
	if len(v.Members) != 2 || v.Members[0].Alias != "webpool-7" || v.Members[1].Alias != "apipool-2" {
		t.Fatalf("members = %+v, want webpool-7 then apipool-2 (earliest acquired first, the source left out)", v.Members)
	}
	for _, m := range v.Members {
		if m.Outcome != LeaseSyncFastForwarded || m.From != syncFrom || m.To != syncTo {
			t.Errorf("member = %+v, want fast-forwarded %s..%s", m, syncFrom, syncTo)
		}
	}
	for _, c := range remote.sent("hqpool-3") {
		if strings.Contains(c, "merge --ff-only") {
			t.Errorf("the source was sent a fast-forward: %s", c)
		}
	}
	for _, host := range []string{"webpool-7", "apipool-2"} {
		for _, c := range remote.sent(host) {
			if strings.Contains(c, "push") {
				t.Errorf("member %s was sent a push: %s", host, c)
			}
		}
	}
}

// TestLeaseSync_SourceUncommitted_RefusedNamingFiles: zcp commits nothing
// (GF-2), so a source with uncommitted or untracked files is refused naming
// them, with the commit to make; nothing is pushed and no member is touched.
func TestLeaseSync_SourceUncommitted_RefusedNamingFiles(t *testing.T) {
	t.Parallel()
	gf, remote := newSyncFixture(t, syncBranch)
	remote.dirty["hqpool-3"] = " M app.txt\n?? notes.txt\n"

	_, err := LeaseSync(context.Background(), gf.deps, "hqpool-3")
	var pe *platform.PlatformError
	if !errors.As(err, &pe) || pe.Code != platform.ErrPrerequisiteMissing {
		t.Fatalf("err = %v, want a PREREQUISITE_MISSING refusal", err)
	}
	for _, want := range []string{"hqpool-3", "app.txt", "notes.txt"} {
		if !strings.Contains(pe.Message, want) {
			t.Errorf("refusal %q lacks %q", pe.Message, want)
		}
	}
	for _, want := range []string{"Commit", "zcp does not commit", "zerops_lease action=sync lease=hqpool-3"} {
		if !strings.Contains(pe.Suggestion, want) {
			t.Errorf("suggestion %q lacks %q", pe.Suggestion, want)
		}
	}
	for _, c := range remote.all() {
		if strings.Contains(c, "push") || strings.Contains(c, "merge --ff-only") || strings.Contains(c, "fetch") {
			t.Errorf("a refused sync ran %s", c)
		}
	}
	if got := remote.sent("webpool-7"); len(got) != 0 {
		t.Errorf("a refused sync reached a member: %v", got)
	}
}

// TestLeaseSync_BranchMain_Refused: main changes through review, never by a
// lease push — a lease of main (an empty branch is main) is refused before
// zcp reaches any container.
func TestLeaseSync_BranchMain_Refused(t *testing.T) {
	t.Parallel()
	for _, branch := range []string{"main", ""} {
		t.Run("branch "+strings.TrimSpace(branch+" "), func(t *testing.T) {
			t.Parallel()
			gf, remote := newSyncFixture(t, branch)
			_, err := LeaseSync(context.Background(), gf.deps, "hqpool-3")
			var pe *platform.PlatformError
			if !errors.As(err, &pe) || pe.Code != platform.ErrInvalidParameter {
				t.Fatalf("err = %v, want an INVALID_PARAMETER refusal", err)
			}
			if !strings.Contains(pe.Message, "main") || !strings.Contains(pe.Message, "hqpool-3") {
				t.Errorf("refusal %q should name the lease and main", pe.Message)
			}
			if !strings.Contains(pe.Suggestion, "zerops_lease action=acquire") {
				t.Errorf("suggestion %q should name the acquire of a branch of its own", pe.Suggestion)
			}
			if got := remote.all(); len(got) != 0 {
				t.Errorf("a sync of main reached a container: %v", got)
			}
		})
	}
}

// TestLeaseSync_NeverForcePushes: the source's push is a plain push of the
// branch to the same branch on origin — never --force, a force flag of any
// kind, a + refspec, a delete or a mirror — and a member's step only fetches
// and fast-forwards: no reset, rebase or checkout.
func TestLeaseSync_NeverForcePushes(t *testing.T) {
	t.Parallel()
	gf, remote := newSyncFixture(t, syncBranch)
	if _, err := LeaseSync(context.Background(), gf.deps, "hqpool-3"); err != nil {
		t.Fatalf("LeaseSync: %v", err)
	}
	var push string
	for _, c := range remote.sent("hqpool-3") {
		if strings.Contains(c, "push --porcelain") {
			push = c
		}
	}
	if push == "" {
		t.Fatalf("no push reached the source; sent %v", remote.sent("hqpool-3"))
	}
	if !strings.Contains(push, "origin 'refs/heads/feat-x:refs/heads/feat-x'") {
		t.Errorf("push %q should push refs/heads/feat-x to the same branch on origin", push)
	}
	forced := regexp.MustCompile(`--force|--mirror|--delete|--prune| -f\b|'\+|\s\+refs`)
	if m := forced.FindString(push); m != "" {
		t.Errorf("push carries %q: %s", m, push)
	}
	var member string
	for _, c := range remote.sent("webpool-7") {
		if strings.Contains(c, "merge --ff-only") {
			member = c
		}
	}
	if member == "" {
		t.Fatalf("no fast-forward reached webpool-7; sent %v", remote.sent("webpool-7"))
	}
	if m := regexp.MustCompile(`git -C '/var/www' (reset|rebase|checkout|switch|restore|commit|push|stash|clean)\b|--force`).FindString(member); m != "" {
		t.Errorf("a member's step carries %q: %s", m, member)
	}
}

// TestLeaseSync_ReadOnlyGitCallsTakeNoOptionalLocks: every git call of a sync
// that only reads a checkout runs with GIT_OPTIONAL_LOCKS=0 (DP-4), so it
// writes nothing there; only the push, the fetch, the fast-forward and the
// upstream it sets write.
func TestLeaseSync_ReadOnlyGitCallsTakeNoOptionalLocks(t *testing.T) {
	t.Parallel()
	gf, remote := newSyncFixture(t, syncBranch)
	if _, err := LeaseSync(context.Background(), gf.deps, "hqpool-3"); err != nil {
		t.Fatalf("LeaseSync: %v", err)
	}
	call := regexp.MustCompile(`(GIT_OPTIONAL_LOCKS=0 )?git -C '/var/www' ([a-z-]+)`)
	writes := map[string]bool{"push": true, "fetch": true, "merge": true, "branch": true}
	seen := 0
	for _, c := range remote.all() {
		for _, m := range call.FindAllStringSubmatch(c, -1) {
			seen++
			if !writes[m[2]] && m[1] == "" {
				t.Errorf("read-only git %s runs without GIT_OPTIONAL_LOCKS=0: %s", m[2], c)
			}
			if writes[m[2]] && m[1] != "" {
				t.Errorf("git %s writes yet claims to read only: %s", m[2], c)
			}
		}
	}
	if seen == 0 {
		t.Fatal("a sync ran no git call in /var/www")
	}
}

// TestLeaseSync_MemberWithOwnCommits_LeftUntouched: a member whose branch has
// commits origin's lacks is never rebased or merged (zcp rewrites no history):
// it is left as it is, its own commits counted, with how to integrate them.
func TestLeaseSync_MemberWithOwnCommits_LeftUntouched(t *testing.T) {
	t.Parallel()
	gf, remote := newSyncFixture(t, syncBranch)
	remote.member["webpool-7"] = syncAnswer{out: "@@zcp-sync from " + syncOwn + "\n@@zcp-sync origin " + syncTo + "\n@@zcp-sync own 2\n"}

	v, err := LeaseSync(context.Background(), gf.deps, "hqpool-3")
	if err != nil {
		t.Fatalf("LeaseSync: %v", err)
	}
	m := syncMemberOf(t, v, "webpool-7")
	if m.Outcome != LeaseSyncHasOwnCommits || m.OwnCommits != 2 || m.From != syncOwn || m.To != "" {
		t.Errorf("member = %+v, want has-own-commits (2) left at %s", m, syncOwn)
	}
	for _, want := range []string{"2 commit(s)", "origin/" + syncBranch, "zerops_lease action=sync lease=webpool-7"} {
		if !strings.Contains(m.Note, want) {
			t.Errorf("note %q lacks %q", m.Note, want)
		}
	}
	for _, c := range remote.sent("webpool-7") {
		if strings.Contains(c, "@@zcp-devsup declaration") {
			t.Errorf("a member left as it was had its dev servers read: %s", c)
		}
	}
}

// TestLeaseSync_LocalChangesInTheWay_LeftUntouched: git refuses a
// fast-forward that would overwrite a local change, so the member is left as
// it is, named with the files the release guard's own read finds there.
func TestLeaseSync_LocalChangesInTheWay_LeftUntouched(t *testing.T) {
	t.Parallel()
	gf, remote := newSyncFixture(t, syncBranch)
	remote.member["webpool-7"] = syncAnswer{out: "@@zcp-sync from " + syncFrom + "\n@@zcp-sync origin " + syncTo + "\n" +
		"error: Your local changes to the following files would be overwritten by merge:\n\tapp.txt\nPlease commit your changes or stash them before you merge.\nAborting\n@@zcp-sync blocked 1\n"}
	remote.dirty["webpool-7"] = " M app.txt\n"

	v, err := LeaseSync(context.Background(), gf.deps, "hqpool-3")
	if err != nil {
		t.Fatalf("LeaseSync: %v", err)
	}
	m := syncMemberOf(t, v, "webpool-7")
	if m.Outcome != LeaseSyncLocalChangesInTheWay || m.From != syncFrom || m.To != "" {
		t.Errorf("member = %+v, want local-changes-in-the-way left at %s", m, syncFrom)
	}
	for _, want := range []string{"1 uncommitted or untracked file(s)", "app.txt", "zerops_lease action=sync lease=hqpool-3"} {
		if !strings.Contains(m.Note, want) {
			t.Errorf("note %q lacks %q", m.Note, want)
		}
	}
	read := false
	for _, c := range remote.sent("webpool-7") {
		if c == leaseUnsavedFilesCommand() {
			read = true
		}
	}
	if !read {
		t.Errorf("the files were not read with the release guard's own read; sent %v", remote.sent("webpool-7"))
	}
}

// A fast-forward git refuses for another reason (a lock another git holds) is
// no local change: the member fails, with what git said.
func TestLeaseSync_MemberFastForwardFailsOtherwise_NamedWithGitsWords(t *testing.T) {
	t.Parallel()
	gf, remote := newSyncFixture(t, syncBranch)
	remote.member["webpool-7"] = syncAnswer{out: "@@zcp-sync from " + syncFrom + "\n@@zcp-sync origin " + syncTo + "\n" +
		"fatal: Unable to create '/var/www/.git/index.lock': File exists.\n@@zcp-sync blocked 128\n"}
	v, err := LeaseSync(context.Background(), gf.deps, "hqpool-3")
	if err != nil {
		t.Fatalf("LeaseSync: %v", err)
	}
	m := syncMemberOf(t, v, "webpool-7")
	if m.Outcome != LeaseSyncFailed || !strings.Contains(m.Note, "index.lock") {
		t.Errorf("member = %+v, want failed naming git's index.lock", m)
	}
}

// TestLeaseSync_MemberNotFastForwarded_NamedNotFailed: every member zcp could
// not fast-forward is named with why, and never fails the sync — the push
// already happened.
func TestLeaseSync_MemberNotFastForwarded_NamedNotFailed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		answer   func(t *testing.T) syncAnswer
		outcome  string
		code     string
		wantNote []string
	}{
		{
			name: "unreachable",
			answer: func(t *testing.T) syncAnswer {
				t.Helper()
				return syncAnswer{err: sshFailure("ssh: connect to host webpool-7: Connection refused", exitError(t, 255))}
			},
			outcome:  LeaseSyncUnreachable,
			wantNote: []string{"over ssh", "zerops_lease action=status"},
		},
		{
			name:     "on another branch",
			answer:   func(*testing.T) syncAnswer { return syncAnswer{out: "@@zcp-sync head other\n"} },
			outcome:  LeaseSyncOnAnotherBranch,
			wantNote: []string{"other", syncBranch, "zerops_lease action=sync lease=hqpool-3"},
		},
		{
			name: "fetch without a credential",
			answer: func(t *testing.T) syncAnswer {
				t.Helper()
				return syncAnswer{err: sshFailure("fatal: could not read Username for 'https://github.com': terminal prompts disabled", exitError(t, 128))}
			},
			outcome:  LeaseSyncFailed,
			code:     platform.ErrGitTokenMissing,
			wantNote: []string{"GIT_TOKEN", "terminal prompts disabled"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gf, remote := newSyncFixture(t, syncBranch)
			remote.member["webpool-7"] = tt.answer(t)
			v, err := LeaseSync(context.Background(), gf.deps, "hqpool-3")
			if err != nil {
				t.Fatalf("LeaseSync: %v", err)
			}
			if v.Push.Outcome != LeaseSyncPushed {
				t.Errorf("push = %+v, want pushed", v.Push)
			}
			m := syncMemberOf(t, v, "webpool-7")
			if m.Outcome != tt.outcome || m.Code != tt.code {
				t.Errorf("member = %+v, want outcome %q code %q", m, tt.outcome, tt.code)
			}
			for _, want := range tt.wantNote {
				if !strings.Contains(m.Note, want) {
					t.Errorf("note %q lacks %q", m.Note, want)
				}
			}
		})
	}
}

// TestLeaseSync_PushRefused_NothingFastForwarded: a push origin refuses —
// origin moved on, or its rules refused the branch — or that cannot reach
// the source refuses the sync naming why, and no member is touched; never a
// retry with force.
func TestLeaseSync_PushRefused_NothingFastForwarded(t *testing.T) {
	t.Parallel()
	ref := "refs/heads/" + syncBranch + ":refs/heads/" + syncBranch
	tests := []struct {
		name      string
		repo      string
		answer    func(t *testing.T) syncAnswer
		code      string
		wantMsg   []string
		wantSugg  []string
		notInSugg string
	}{
		{
			name: "origin moved on",
			answer: func(t *testing.T) syncAnswer {
				t.Helper()
				out := "@@zcp-sync commit " + syncTo + "\nTo " + syncRepo + "\n!\t" + ref + "\t[rejected] (fetch first)\nDone\n"
				return syncAnswer{out: out, err: sshFailure(out, exitError(t, 1))}
			},
			code:      platform.ErrGitPushNonFastForward,
			wantMsg:   []string{"hqpool-3", syncBranch, "fetch first"},
			wantSugg:  []string{"origin/" + syncBranch, "zerops_lease action=sync lease=hqpool-3"},
			notInSugg: "force",
		},
		{
			name: "HQ's rule refused the branch",
			repo: leaseHQRepo,
			answer: func(t *testing.T) syncAnswer {
				t.Helper()
				out := "@@zcp-sync commit " + syncTo + "\nTo " + leaseHQRepo + "\n!\t" + ref + "\t[remote rejected] (pre-receive hook declined)\nDone\n"
				return syncAnswer{out: out, err: sshFailure(out, exitError(t, 1))}
			},
			code:     platform.ErrPermissionDenied,
			wantMsg:  []string{"hqpool-3", "pre-receive hook declined"},
			wantSugg: []string{"HQ", "another Mate's open change"},
		},
		{
			name: "the source does not answer ssh",
			answer: func(t *testing.T) syncAnswer {
				t.Helper()
				return syncAnswer{err: sshFailure("ssh: Could not resolve hostname hqpool-3", exitError(t, 255))}
			},
			code:     platform.ErrSSHDeployFailed,
			wantMsg:  []string{"hqpool-3"},
			wantSugg: []string{"ssh"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gf, remote := newSyncFixture(t, syncBranch)
			if tt.repo != "" {
				gf.deps.HQAddress = func() string { return leaseHQURL }
				for _, alias := range []string{"hqpool-3", "webpool-7"} {
					rec, err := loadLeaseRecord(gf.stateDir, alias)
					if err != nil {
						t.Fatal(err)
					}
					rec.Repo = tt.repo
					if err := writeLeaseRecord(gf.stateDir, rec); err != nil {
						t.Fatal(err)
					}
				}
			}
			a := tt.answer(t)
			remote.push = &a
			_, err := LeaseSync(context.Background(), gf.deps, "hqpool-3")
			var pe *platform.PlatformError
			if !errors.As(err, &pe) || pe.Code != tt.code {
				t.Fatalf("err = %v, want a %s refusal", err, tt.code)
			}
			for _, want := range tt.wantMsg {
				if !strings.Contains(pe.Message, want) {
					t.Errorf("message %q lacks %q", pe.Message, want)
				}
			}
			for _, want := range tt.wantSugg {
				if !strings.Contains(pe.Suggestion, want) {
					t.Errorf("suggestion %q lacks %q", pe.Suggestion, want)
				}
			}
			if tt.notInSugg != "" && strings.Contains(strings.ToLower(pe.Suggestion), tt.notInSugg) {
				t.Errorf("suggestion %q mentions %q", pe.Suggestion, tt.notInSugg)
			}
			if got := remote.sent("webpool-7"); len(got) != 0 {
				t.Errorf("a refused push reached a member: %v", got)
			}
		})
	}
}

// A source whose checkout is not on the lease's branch is refused naming the
// branch it is on: zcp pushes the lease's branch only.
func TestLeaseSync_SourceNotOnItsBranch_Refused(t *testing.T) {
	t.Parallel()
	gf, remote := newSyncFixture(t, syncBranch)
	remote.push = &syncAnswer{out: "@@zcp-sync head other\n"}
	_, err := LeaseSync(context.Background(), gf.deps, "hqpool-3")
	var pe *platform.PlatformError
	if !errors.As(err, &pe) || pe.Code != platform.ErrPrerequisiteMissing {
		t.Fatalf("err = %v, want a PREREQUISITE_MISSING refusal", err)
	}
	for _, want := range []string{"hqpool-3", "other", syncBranch} {
		if !strings.Contains(pe.Message, want) {
			t.Errorf("refusal %q lacks %q", pe.Message, want)
		}
	}
	if got := remote.sent("webpool-7"); len(got) != 0 {
		t.Errorf("a refused sync reached a member: %v", got)
	}
}

// A zcp without ssh into its leases cannot read or push one: refused, and a
// lease zcp holds no record of is refused as release refuses it.
func TestLeaseSync_NoSSHOrNoRecord_Refused(t *testing.T) {
	t.Parallel()
	gf, _ := newSyncFixture(t, syncBranch)
	gf.deps.SSH = nil
	_, err := LeaseSync(context.Background(), gf.deps, "hqpool-3")
	var pe *platform.PlatformError
	if !errors.As(err, &pe) || pe.Code != platform.ErrPrerequisiteMissing || !strings.Contains(pe.Message, "ssh") {
		t.Errorf("without ssh: err = %v, want a PREREQUISITE_MISSING refusal naming ssh", err)
	}
	gf, _ = newSyncFixture(t, syncBranch)
	_, err = LeaseSync(context.Background(), gf.deps, "hqpool-9")
	if !errors.As(err, &pe) || pe.Code != platform.ErrInvalidParameter || !strings.Contains(pe.Message, "hqpool-9") {
		t.Errorf("no record: err = %v, want an INVALID_PARAMETER refusal naming hqpool-9", err)
	}
}

// syncDevServers is a member's dev servers: web watches the code, api and
// worker do not, worker is stopped, and hq belongs to another part.
const syncDevServers = `{"version":"v9.400.0","formats":[1],"capabilities":["env"]}
@@zcp-devsup version 0
{"format":1,"servers":{"web":{"source":"declared","desired":"running","status":"running"},"api":{"source":"declared","desired":"running","status":"running"},"worker":{"source":"declared","desired":"stopped","status":"stopped"}}}
@@zcp-devsup status 0
@@zcp-devsup listen 0
{"version":1,"servers":[{"name":"web","port":3000,"command":"npm run dev","watch":{"paths":["src"]}},{"name":"api","port":3001,"command":"node api.js"},{"name":"worker","command":"node worker.js"},{"name":"hq","port":8080,"command":"go run .","service":"hqpool"}]}
@@zcp-devsup declaration 0
`

// TestLeaseSync_FastForwardedMember_NamesServersWithoutAWatch: a running
// server without a watch keeps the old code until restarted, so a member that
// was fast-forwarded names each, with the restart; a server with a watch sees
// the change on its own, and a member that did not move reads none.
func TestLeaseSync_FastForwardedMember_NamesServersWithoutAWatch(t *testing.T) {
	t.Parallel()
	gf, remote := newSyncFixture(t, syncBranch, syncRecord("apipool", "svc-api", 2, syncBranch, "2026-10-24T10:00:02Z"))
	remote.devsup["webpool-7"] = syncDevServers
	remote.devsup["apipool-2"] = syncDevServers
	remote.member["apipool-2"] = syncAnswer{out: "@@zcp-sync from " + syncTo + "\n@@zcp-sync origin " + syncTo + "\n@@zcp-sync up-to-date\n"}

	v, err := LeaseSync(context.Background(), gf.deps, "hqpool-3")
	if err != nil {
		t.Fatalf("LeaseSync: %v", err)
	}
	web := syncMemberOf(t, v, "webpool-7")
	if len(web.UnwatchedServers) != 1 || web.UnwatchedServers[0] != "api" {
		t.Errorf("webpool-7 unwatched = %v, want [api] (web watches, worker is stopped, hq is hqpool's)", web.UnwatchedServers)
	}
	if !strings.Contains(web.Note, `zerops_dev_server action=restart hostname="webpool-7" name="api"`) {
		t.Errorf("note %q should name api's restart", web.Note)
	}
	api := syncMemberOf(t, v, "apipool-2")
	if api.Outcome != LeaseSyncUpToDate || api.From != syncTo || api.To != syncTo || len(api.UnwatchedServers) != 0 {
		t.Errorf("apipool-2 = %+v, want up-to-date at %s naming no server", api, syncTo)
	}
	for _, c := range remote.sent("apipool-2") {
		if strings.Contains(c, "@@zcp-devsup declaration") {
			t.Errorf("a member that did not move had its dev servers read: %s", c)
		}
	}
}

// TestLeaseSync_ReportsEachStepsMilliseconds: the result carries how long the
// source's check, the push, each member and the whole sync took (the input to
// measuring a crossing).
func TestLeaseSync_ReportsEachStepsMilliseconds(t *testing.T) {
	t.Parallel()
	gf, remote := newSyncFixture(t, syncBranch)
	remote.delay = 15 * time.Millisecond
	v, err := LeaseSync(context.Background(), gf.deps, "hqpool-3")
	if err != nil {
		t.Fatalf("LeaseSync: %v", err)
	}
	if v.CheckMillis < 15 || v.Push.Millis < 15 {
		t.Errorf("check %d ms, push %d ms; want each at least the 15 ms its ssh call took", v.CheckMillis, v.Push.Millis)
	}
	m := syncMemberOf(t, v, "webpool-7")
	if m.Millis < 15 {
		t.Errorf("webpool-7 took %d ms, want at least 15", m.Millis)
	}
	if v.Millis < v.CheckMillis+v.Push.Millis+m.Millis {
		t.Errorf("total %d ms is less than its steps (%d + %d + %d)", v.Millis, v.CheckMillis, v.Push.Millis, m.Millis)
	}
}

// syncDevsupRemote is a syncRemote whose leases in containers run zcp-devsup:
// their supervisor's commands go to the container, every git command to the
// script.
type syncDevsupRemote struct {
	*syncRemote
	containers map[string]*devservertest.Container
}

func (r syncDevsupRemote) ExecSSH(ctx context.Context, host, command string) ([]byte, error) {
	if c, ok := r.containers[host]; ok && strings.Contains(command, "@@zcp-devsup") {
		return c.ExecSSH(ctx, host, command)
	}
	return r.syncRemote.ExecSSH(ctx, host, command)
}

func (r syncDevsupRemote) ExecSSHBackground(ctx context.Context, host, command string, _ time.Duration) ([]byte, error) {
	return r.ExecSSH(ctx, host, command)
}

// TestLeaseSync_FastForwardChangedDeclaration_SendsOverlayThenReloads: a
// fast-forward is zcp's own change to a lease's checkout, as an acquire's
// checkout is, so when it changes .zcp/dev-servers.json zcp has the lease's
// supervisor take it in the same call — the sibling overlay first, when it
// changed, then a reload, which starts an added server — and says so. The
// servers that started or restarted on the new code are not named as keeping
// the old one; a declaration the fast-forward left alone reloads nothing, and
// one that cannot be read is named, never reloaded.
func TestLeaseSync_FastForwardChangedDeclaration_SendsOverlayThenReloads(t *testing.T) {
	t.Parallel()
	const servers = `"servers":[{"name":"web","port":3000,"command":"pnpm dev"},{"name":"admin","port":4200,"command":"pnpm admin"}]}`
	tests := []struct {
		name          string
		decl          string
		changed       bool // the fast-forward changed the declaration
		wantEnv       map[string]string
		wantReloaded  bool
		wantUnwatched []string
		wantNote      []string
	}{
		{name: "siblingEnv and a server added", decl: `{"version":1,"siblingEnv":{"HQ_HOST":"hqpool"},` + servers, changed: true,
			wantEnv: map[string]string{"HQ_HOST": nodeHostnameOf(groupHQPool, 3)}, wantReloaded: true, wantNote: []string{".zcp/dev-servers.json", "HQ_HOST", "admin"}},
		{name: "a server added", decl: `{"version":1,` + servers, changed: true,
			wantReloaded: true, wantUnwatched: []string{"web"}, wantNote: []string{".zcp/dev-servers.json", "admin"}},
		{name: "the declaration left alone", decl: `{"version":1,` + servers, wantUnwatched: []string{"web"}},
		{name: "a declaration that cannot be read", decl: `{"version":1,"servers":[{"name":"web","port":3000}]}`, changed: true,
			wantUnwatched: []string{"web"}, wantNote: []string{".zcp/dev-servers.json", "cannot"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gf, remote := newSyncFixture(t, syncBranch)
			c := devservertest.NewSupervised(tt.decl)
			delete(c.Servers, "admin") // the supervisor still runs the declaration from before the fast-forward
			if c.Servers["web"] == nil {
				c.Servers["web"] = &devsup.ServerState{Source: devsup.SourceDeclared, Desired: devsup.DesiredRunning, Status: devsup.StatusRunning, Pid: 801}
			}
			gf.deps.SSH = syncDevsupRemote{syncRemote: remote, containers: map[string]*devservertest.Container{"webpool-7": c}}
			if tt.changed {
				remote.member["webpool-7"] = syncAnswer{out: syncMemberFastForwarded(syncFrom, syncTo) + "@@zcp-sync declaration\n"}
			}

			v, err := LeaseSync(context.Background(), gf.deps, "hqpool-3")
			if err != nil {
				t.Fatalf("LeaseSync: %v", err)
			}

			m := syncMemberOf(t, v, "webpool-7")
			if m.Outcome != LeaseSyncFastForwarded || m.Reloaded != tt.wantReloaded {
				t.Errorf("member = %+v, want fast-forwarded, reloaded=%v", m, tt.wantReloaded)
			}
			cmds := c.Sent()
			env, reload := firstSent(cmds, "zcp-devsup ctl 'env'"), firstSent(cmds, "zcp-devsup ctl 'reload'")
			switch {
			case tt.wantReloaded && reload < 0:
				t.Errorf("no reload was sent: %v", cmds)
			case !tt.wantReloaded && reload >= 0:
				t.Errorf("a reload was sent: %v", cmds)
			case tt.wantEnv != nil && (env < 0 || env > reload):
				t.Errorf("ctl env (%d) must come before the reload (%d): %v", env, reload, cmds)
			case tt.wantEnv == nil && env >= 0:
				t.Errorf("ctl env was sent with no overlay change: %v", cmds)
			}
			if tt.wantEnv != nil && !maps.Equal(c.Overlay(), tt.wantEnv) {
				t.Errorf("overlay = %v, want %v", c.Overlay(), tt.wantEnv)
			}
			if tt.wantReloaded && c.Status("admin") != devsup.StatusRunning {
				t.Errorf("admin = %q, want started by the reload", c.Status("admin"))
			}
			if !slices.Equal(m.UnwatchedServers, tt.wantUnwatched) {
				t.Errorf("unwatched = %v, want %v", m.UnwatchedServers, tt.wantUnwatched)
			}
			for _, want := range tt.wantNote {
				if !strings.Contains(m.Note, want) {
					t.Errorf("note %q lacks %q", m.Note, want)
				}
			}
		})
	}
}
