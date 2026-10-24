//go:build unix

// Tests for: ops/lease_sync.go — a sync over real git: the source's branch
// pushed to a local bare origin, the other leases' checkouts fast-forwarded,
// and every lease zcp cannot fast-forward left exactly as it was
// (docs/spec-workflows.md §8 DP-24).

package ops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// localLeasesSSH runs each lease's commands in its own local checkout, the way
// localCheckoutSSH runs one; a host it has no checkout for does not answer
// (ssh's 255).
type localLeasesSSH struct {
	hosts       map[string]*localCheckoutSSH
	unreachable error
}

func (s *localLeasesSSH) ExecSSH(ctx context.Context, host, command string) ([]byte, error) {
	c, ok := s.hosts[host]
	if !ok {
		return nil, &platform.SSHExecError{Hostname: host, Output: "ssh: Could not resolve hostname " + host, Err: s.unreachable}
	}
	return c.ExecSSH(ctx, host, command)
}

func (s *localLeasesSSH) ExecSSHBackground(ctx context.Context, host, command string, _ time.Duration) ([]byte, error) {
	return s.ExecSSH(ctx, host, command)
}

// syncClones is a bare origin holding main, and the checkouts of three leases
// of the branch feat-new — hqpool-3 (acquired first), webpool-7 and apipool-2
// — each cut from origin's main by the lease checkout (feat-new is new to
// origin, so none tracks anything yet), with zcp's records of them.
type syncClones struct {
	origin, mainSHA string
	dirs            map[string]string
	deps            LeaseDeps
}

const syncNewBranch = "feat-new"

func newSyncClones(t *testing.T) *syncClones {
	t.Helper()
	origin, mainSHA, _ := originWithBranches(t)
	sc := &syncClones{origin: origin, mainSHA: mainSHA, dirs: map[string]string{}}
	ssh := &localLeasesSSH{hosts: map[string]*localCheckoutSSH{}, unreachable: exitError(t, 255)}
	gf := newGroupFixture(t)
	recs := []leaseRecord{
		syncRecord(groupHQPool, "svc-hq", 3, syncNewBranch, "2026-10-24T10:00:00Z"),
		syncRecord(groupWebPool, "svc-web", 7, syncNewBranch, "2026-10-24T10:00:01Z"),
		syncRecord("apipool", "svc-api", 2, syncNewBranch, "2026-10-24T10:00:02Z"),
	}
	holdSyncLeases(t, gf, recs)
	for i, r := range recs {
		dir := t.TempDir()
		base := ""
		if i > 0 {
			base = mainSHA // the group's base: every member cut from one main commit
		}
		if out, err := runCheckoutFrom(t, dir, origin, syncNewBranch, base); err != nil {
			t.Fatalf("checkout %s: %v\n%s", r.alias(), err, out)
		}
		if err := writeLeaseRecord(gf.stateDir, r); err != nil {
			t.Fatal(err)
		}
		sc.dirs[r.alias()] = dir
		ssh.hosts[r.alias()] = &localCheckoutSSH{dir: dir}
	}
	gf.deps.SSH = ssh
	sc.deps = gf.deps
	return sc
}

// commit commits a file with body in the lease's checkout and returns HEAD.
func (sc *syncClones) commit(t *testing.T, alias, file, body string) string {
	t.Helper()
	dir := sc.dirs[alias]
	if err := os.WriteFile(filepath.Join(dir, file), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-q", "-m", file)
	return gitIn(t, dir, "rev-parse", "HEAD")
}

func (sc *syncClones) head(t *testing.T, alias string) string {
	t.Helper()
	return gitIn(t, sc.dirs[alias], "rev-parse", "HEAD")
}

// upstream is what the lease's branch tracks, "" for nothing.
func (sc *syncClones) upstream(t *testing.T, alias string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(sc.dirs[alias], ".git", "config"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `[branch "`+syncNewBranch+`"]`) {
		return ""
	}
	return gitIn(t, sc.dirs[alias], "rev-parse", "--symbolic-full-name", syncNewBranch+"@{upstream}")
}

// originHas is the commit origin's feat-new is at, "" when origin has none.
func (sc *syncClones) originHas(t *testing.T) string {
	t.Helper()
	out := gitIn(t, filepath.Dir(sc.origin), "ls-remote", sc.origin, "refs/heads/"+syncNewBranch)
	if out == "" {
		return ""
	}
	return strings.Fields(out)[0]
}

// syncFromHQ syncs from hqpool-3, the lease the agent commits in.
func (sc *syncClones) syncFromHQ(t *testing.T) LeaseSyncView {
	t.Helper()
	v, err := LeaseSync(context.Background(), sc.deps, "hqpool-3")
	if err != nil {
		t.Fatalf("LeaseSync hqpool-3: %v", err)
	}
	return v
}

// TestLeaseSyncScript_RealGit_FastForwardsTwoClones: after the agent commits
// in hqpool-3, one sync pushes feat-new — with -u the first time, so the
// source tracks it — and leaves webpool-7 and apipool-2 at the same commit,
// each tracking origin/feat-new; the next commit is pushed plainly and
// fast-forwards them again; a sync with nothing new changes nothing.
func TestLeaseSyncScript_RealGit_FastForwardsTwoClones(t *testing.T) {
	t.Parallel()
	sc := newSyncClones(t)

	c1 := sc.commit(t, "hqpool-3", "one.txt", "1")
	v := sc.syncFromHQ(t)
	if v.Push.Outcome != LeaseSyncNewBranch || !v.Push.SetUpstream || v.Commit != c1 {
		t.Errorf("first push = %+v (commit %s), want new-branch, upstream set, at %s", v.Push, v.Commit, c1)
	}
	if got := sc.originHas(t); got != c1 {
		t.Errorf("origin's %s = %q, want %s", syncNewBranch, got, c1)
	}
	for _, alias := range []string{"webpool-7", "apipool-2"} {
		m := syncMemberOf(t, v, alias)
		if m.Outcome != LeaseSyncFastForwarded || m.From != sc.mainSHA || m.To != c1 {
			t.Errorf("%s = %+v, want fast-forwarded %s..%s", alias, m, sc.mainSHA, c1)
		}
		if got := sc.head(t, alias); got != c1 {
			t.Errorf("%s HEAD = %s, want %s", alias, got, c1)
		}
	}
	for _, alias := range []string{"hqpool-3", "webpool-7", "apipool-2"} {
		if got := sc.upstream(t, alias); got != "refs/remotes/origin/"+syncNewBranch {
			t.Errorf("%s tracks %q, want origin/%s", alias, got, syncNewBranch)
		}
	}

	c2 := sc.commit(t, "hqpool-3", "two.txt", "2")
	v = sc.syncFromHQ(t)
	if v.Push.Outcome != LeaseSyncPushed || v.Push.SetUpstream {
		t.Errorf("second push = %+v, want pushed, no upstream to set", v.Push)
	}
	for _, alias := range []string{"webpool-7", "apipool-2"} {
		if m := syncMemberOf(t, v, alias); m.Outcome != LeaseSyncFastForwarded || m.From != c1 || m.To != c2 || sc.head(t, alias) != c2 {
			t.Errorf("%s = %+v (HEAD %s), want fast-forwarded %s..%s", alias, m, sc.head(t, alias), c1, c2)
		}
	}
	if _, err := os.Stat(filepath.Join(sc.dirs["webpool-7"], "two.txt")); err != nil {
		t.Errorf("webpool-7's working tree lacks the pushed file: %v", err)
	}

	v = sc.syncFromHQ(t)
	if v.Push.Outcome != LeaseSyncUpToDate {
		t.Errorf("third push = %+v, want up-to-date", v.Push)
	}
	for _, alias := range []string{"webpool-7", "apipool-2"} {
		if m := syncMemberOf(t, v, alias); m.Outcome != LeaseSyncUpToDate || m.From != c2 || m.To != c2 {
			t.Errorf("%s = %+v, want up-to-date at %s", alias, m, c2)
		}
	}
}

// TestLeaseSyncScript_RealGit_MemberWithOwnCommits_LeftUntouched: a member
// that committed on its own is neither rebased nor merged — its HEAD and
// files stay — while the other member is fast-forwarded.
func TestLeaseSyncScript_RealGit_MemberWithOwnCommits_LeftUntouched(t *testing.T) {
	t.Parallel()
	sc := newSyncClones(t)
	own := sc.commit(t, "webpool-7", "mine.txt", "web's own")
	c1 := sc.commit(t, "hqpool-3", "one.txt", "1")

	v := sc.syncFromHQ(t)
	m := syncMemberOf(t, v, "webpool-7")
	if m.Outcome != LeaseSyncHasOwnCommits || m.OwnCommits != 1 || m.From != own {
		t.Errorf("webpool-7 = %+v, want has-own-commits (1) at %s", m, own)
	}
	if got := sc.head(t, "webpool-7"); got != own {
		t.Errorf("webpool-7 HEAD = %s, want its own commit %s, untouched", got, own)
	}
	if _, err := os.Stat(filepath.Join(sc.dirs["webpool-7"], "one.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("webpool-7 got the pushed file although it was left untouched (stat: %v)", err)
	}
	if got := gitIn(t, sc.dirs["webpool-7"], "status", "--porcelain"); got != "" {
		t.Errorf("webpool-7's checkout changed: %q", got)
	}
	if api := syncMemberOf(t, v, "apipool-2"); api.Outcome != LeaseSyncFastForwarded || sc.head(t, "apipool-2") != c1 {
		t.Errorf("apipool-2 = %+v, want fast-forwarded to %s", api, c1)
	}
}

// TestLeaseSyncScript_RealGit_LocalChangesInTheWay_LeftUntouched: git refuses
// a fast-forward that would overwrite a member's uncommitted change or an
// untracked file the push adds, so the member stays at its commit with its
// files as they were, named; a change the fast-forward does not touch rides
// along.
func TestLeaseSyncScript_RealGit_LocalChangesInTheWay_LeftUntouched(t *testing.T) {
	t.Parallel()
	sc := newSyncClones(t)
	web, api := sc.dirs["webpool-7"], sc.dirs["apipool-2"]
	if err := os.WriteFile(filepath.Join(web, "app.txt"), []byte("web's edit"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(api, "README"), []byte("api's unrelated edit"), 0o644); err != nil {
		t.Fatal(err)
	}
	c1 := sc.commit(t, "hqpool-3", "app.txt", "hq's change")

	v := sc.syncFromHQ(t)
	m := syncMemberOf(t, v, "webpool-7")
	if m.Outcome != LeaseSyncLocalChangesInTheWay || m.From != sc.mainSHA {
		t.Errorf("webpool-7 = %+v, want local-changes-in-the-way at %s", m, sc.mainSHA)
	}
	if !strings.Contains(m.Note, "app.txt") {
		t.Errorf("note %q should name app.txt", m.Note)
	}
	if got := sc.head(t, "webpool-7"); got != sc.mainSHA {
		t.Errorf("webpool-7 HEAD = %s, want %s, untouched", got, sc.mainSHA)
	}
	if raw, err := os.ReadFile(filepath.Join(web, "app.txt")); err != nil || string(raw) != "web's edit" {
		t.Errorf("webpool-7's app.txt = %q (%v), want its own edit kept", raw, err)
	}
	if a := syncMemberOf(t, v, "apipool-2"); a.Outcome != LeaseSyncFastForwarded || sc.head(t, "apipool-2") != c1 {
		t.Errorf("apipool-2 = %+v, want fast-forwarded to %s with its unrelated edit", a, c1)
	}
	if raw, err := os.ReadFile(filepath.Join(api, "README")); err != nil || string(raw) != "api's unrelated edit" {
		t.Errorf("apipool-2's README = %q (%v), want its edit kept", raw, err)
	}

	// An untracked file the pushed commit adds is in the way too.
	if err := os.WriteFile(filepath.Join(api, "three.txt"), []byte("api's own file"), 0o644); err != nil {
		t.Fatal(err)
	}
	sc.commit(t, "hqpool-3", "three.txt", "3")
	v = sc.syncFromHQ(t)
	if a := syncMemberOf(t, v, "apipool-2"); a.Outcome != LeaseSyncLocalChangesInTheWay || !strings.Contains(a.Note, "three.txt") || sc.head(t, "apipool-2") != c1 {
		t.Errorf("apipool-2 = %+v (HEAD %s), want local-changes-in-the-way naming three.txt at %s", a, sc.head(t, "apipool-2"), c1)
	}
}

// TestLeaseSyncScript_RealGit_DirtySource_NothingPushed: a source with an
// untracked file is refused before anything reaches origin or a member.
func TestLeaseSyncScript_RealGit_DirtySource_NothingPushed(t *testing.T) {
	t.Parallel()
	sc := newSyncClones(t)
	sc.commit(t, "hqpool-3", "one.txt", "1")
	if err := os.WriteFile(filepath.Join(sc.dirs["hqpool-3"], "notes.txt"), []byte("n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LeaseSync(context.Background(), sc.deps, "hqpool-3")
	var pe *platform.PlatformError
	if !errors.As(err, &pe) || pe.Code != platform.ErrPrerequisiteMissing || !strings.Contains(pe.Message, "notes.txt") {
		t.Fatalf("err = %v, want a refusal naming notes.txt", err)
	}
	if got := sc.originHas(t); got != "" {
		t.Errorf("origin has %s at %s after a refused sync", syncNewBranch, got)
	}
	if got := sc.head(t, "webpool-7"); got != sc.mainSHA {
		t.Errorf("webpool-7 moved to %s after a refused sync", got)
	}
}

// TestLeaseSyncScript_RealGit_OriginMovedOn_RefusedNothingForced: when origin's
// branch has a commit the source lacks, git rejects the push; the sync says
// so and changes nothing — origin keeps the other commit and no member moves.
func TestLeaseSyncScript_RealGit_OriginMovedOn_RefusedNothingForced(t *testing.T) {
	t.Parallel()
	sc := newSyncClones(t)
	theirs := sc.commit(t, "apipool-2", "api.txt", "a")
	gitIn(t, sc.dirs["apipool-2"], "push", "-q", "origin", syncNewBranch)
	sc.commit(t, "hqpool-3", "one.txt", "1")

	_, err := LeaseSync(context.Background(), sc.deps, "hqpool-3")
	var pe *platform.PlatformError
	if !errors.As(err, &pe) || pe.Code != platform.ErrGitPushNonFastForward {
		t.Fatalf("err = %v, want a GIT_PUSH_NON_FAST_FORWARD refusal", err)
	}
	if got := sc.originHas(t); got != theirs {
		t.Errorf("origin's %s = %s, want the other commit %s kept", syncNewBranch, got, theirs)
	}
	if got := sc.head(t, "webpool-7"); got != sc.mainSHA {
		t.Errorf("webpool-7 moved to %s after a refused push", got)
	}
}

// A lease of a group of one is pushed alone: the result lists no member.
func TestLeaseSyncScript_RealGit_GroupOfOne_PushesOnly(t *testing.T) {
	t.Parallel()
	sc := newSyncClones(t)
	for _, alias := range []string{"webpool-7", "apipool-2"} {
		if err := os.Remove(filepath.Join(leaseDir(sc.deps.StateDir), alias+".json")); err != nil {
			t.Fatal(err)
		}
	}
	c1 := sc.commit(t, "hqpool-3", "one.txt", "1")
	v := sc.syncFromHQ(t)
	if len(v.Members) != 0 || v.Members == nil || sc.originHas(t) != c1 {
		t.Errorf("sync = %+v (origin at %s), want no member and origin at %s", v, sc.originHas(t), c1)
	}
}

// memberAnswers records what each member's sync step answered.
type memberAnswers struct {
	SSHDeployer
	mu  sync.Mutex
	got map[string]string
}

func (m *memberAnswers) ExecSSH(ctx context.Context, host, command string) ([]byte, error) {
	out, err := m.SSHDeployer.ExecSSH(ctx, host, command)
	if strings.Contains(command, "merge --ff-only") {
		m.mu.Lock()
		m.got[host] = string(out)
		m.mu.Unlock()
	}
	return out, err
}

func (m *memberAnswers) answer(host string) leaseSyncAnswer {
	m.mu.Lock()
	defer m.mu.Unlock()
	return parseLeaseSyncAnswer(m.got[host])
}

// TestLeaseSyncScript_RealGit_DeclarationChange_Marked: a member's step says
// when the fast-forward changed .zcp/dev-servers.json — and only then — so
// zcp has its supervisor take the new one.
func TestLeaseSyncScript_RealGit_DeclarationChange_Marked(t *testing.T) {
	t.Parallel()
	sc := newSyncClones(t)
	answers := &memberAnswers{SSHDeployer: sc.deps.SSH, got: map[string]string{}}
	sc.deps.SSH = answers
	if err := os.MkdirAll(filepath.Join(sc.dirs["hqpool-3"], ".zcp"), 0o755); err != nil {
		t.Fatal(err)
	}
	sc.commit(t, "hqpool-3", ".zcp/dev-servers.json", `{"version":1,"servers":[]}`+"\n")

	sc.syncFromHQ(t)
	for _, alias := range []string{"webpool-7", "apipool-2"} {
		if a := answers.answer(alias); !hasMark(a, "to") || !hasMark(a, "declaration") {
			t.Errorf("%s answered %v, want fast-forwarded with the declaration changed", alias, a.marks)
		}
	}

	sc.commit(t, "hqpool-3", "README.md", "readme\n")
	sc.syncFromHQ(t)
	for _, alias := range []string{"webpool-7", "apipool-2"} {
		if a := answers.answer(alias); !hasMark(a, "to") || hasMark(a, "declaration") {
			t.Errorf("%s answered %v, want fast-forwarded with the declaration left alone", alias, a.marks)
		}
	}
}
