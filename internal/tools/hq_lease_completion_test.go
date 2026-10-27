// Tests for: tools/hq_lease_completion.go — a Mate's HQ pass releases the
// leases of a branch whose change in HQ reads merged or closed, exactly as
// `zerops_lease action=release branch=<branch> repo=<repo>` would and never
// forced; unsaved work keeps them and the pass says so in one line
// (docs/spec-workflows.md §8 DP-34).
package tools

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
)

// completionHQ is the HQ address the completion tests' lease records name.
const completionHQ = "https://hq.example"

// completionRepo is the repository appdev of app-1 on completionHQ, as a
// lease records it.
const completionRepo = completionHQ + "/git/org-1/app-1/appdev"

// releaseCall is one branch release a fake releaser was asked for.
type releaseCall struct{ repo, branch string }

// fakeBranchRelease is a releaser the completion tests control: every call is
// recorded; refuse (when set) answers the release's refusal, otherwise the
// branch's records are dropped and each lease answers released, kept warm.
type fakeBranchRelease struct {
	t        *testing.T
	stateDir string
	refuse   error

	mu    sync.Mutex
	calls []releaseCall
}

func (f *fakeBranchRelease) release(_ context.Context, repo, branch string) (ops.LeaseChange, error) {
	f.mu.Lock()
	f.calls = append(f.calls, releaseCall{repo, branch})
	f.mu.Unlock()
	if f.refuse != nil {
		return ops.LeaseChange{}, f.refuse
	}
	group, err := ops.LeaseBranchGroup(f.stateDir, repo, branch)
	if err != nil {
		return ops.LeaseChange{}, err
	}
	var change ops.LeaseChange
	for _, l := range group {
		if err := os.Remove(filepath.Join(f.stateDir, "leases", l.Alias+".json")); err != nil {
			f.t.Fatal(err)
		}
		change.Leases = append(change.Leases, ops.LeaseView{Alias: l.Alias, State: ops.LeaseStateReleased})
	}
	return change, nil
}

func (f *fakeBranchRelease) called() []releaseCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

// leaseRecordsLeft is the aliases zcp still records leases of, sorted.
func leaseRecordsLeft(t *testing.T, stateDir string) []string {
	t.Helper()
	leases, err := ops.LeaseBranches(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(leases))
	for _, l := range leases {
		out = append(out, l.Alias)
	}
	return out
}

// unsavedRefusalOf is the refusal a branch release answers when hqpool-4
// holds work a release would lose.
func unsavedRefusalOf() error {
	return &ops.LeaseUnsavedError{
		Cause: platform.NewPlatformError(platform.ErrPrerequisiteMissing, "Branch fix-login's leases hold work that releasing would lose, so none of them was released", "commit and push it"),
		Leases: []ops.LeaseUnsavedWork{{
			Alias: "hqpool-4", Files: []string{"?? notes.txt"}, FileCount: 1,
			Commits: []string{"abc1234 wip"}, CommitCount: 1,
		}},
	}
}

// TestReleaseCompletedLeases_ByTheBranchsChange: a branch's leases are
// released once its change in HQ reads merged or closed — one release of the
// branch's group, of its repository — and kept while it is open, while
// another open change uses the branch, while HQ lists no change on it, and
// when they were leased again after the change settled. Leases of another
// application's repository, or of a repository outside HQ, are never
// touched.
func TestReleaseCompletedLeases_ByTheBranchsChange(t *testing.T) {
	t.Parallel()
	merged := hq.MateChange{Repo: "appdev", Number: 4, State: hq.ChangeMerged, Branch: "fix-login", UpdatedAt: "2026-10-26T10:00:00Z"}
	closed := merged
	closed.State = hq.ChangeClosed
	open := merged
	open.State, open.Number = hq.ChangeOpen, 6
	tests := []struct {
		name       string
		changes    []hq.MateChange
		acquiredAt string
		want       []releaseCall
		wantLine   []string
	}{
		{name: "merged", changes: []hq.MateChange{merged},
			want: []releaseCall{{completionRepo, "fix-login"}}, wantLine: []string{`repository "appdev"`, "change #4", `"fix-login"`, "merged", "hqpool-4", "boxpool-6", "released"}},
		{name: "closed", changes: []hq.MateChange{closed},
			want: []releaseCall{{completionRepo, "fix-login"}}, wantLine: []string{"change #4", "closed", "hqpool-4", "boxpool-6", "released"}},
		{name: "open", changes: []hq.MateChange{open}},
		{name: "another open change uses the branch", changes: []hq.MateChange{open, merged}},
		{name: "no change on the branch", changes: []hq.MateChange{{Repo: "appdev", Number: 2, State: hq.ChangeMerged, Branch: "unleased-work"}}},
		{name: "leased again after the change settled", changes: []hq.MateChange{merged}, acquiredAt: "2026-10-26T11:00:00Z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stateDir := t.TempDir()
			writeHQLeaseRecord(t, stateDir, "hqpool", 4, completionRepo, "fix-login")
			writeHQLeaseRecord(t, stateDir, "boxpool", 6, completionRepo, "fix-login")
			writeHQLeaseRecord(t, stateDir, "hqpool", 5, completionRepo, "other-work")
			writeHQLeaseRecord(t, stateDir, "webpool", 7, "https://hq.example/git/org-1/app-2/appdev", "fix-login")
			writeHQLeaseRecord(t, stateDir, "gitpool", 8, "https://github.com/acme/appdev", "fix-login")
			if tt.acquiredAt != "" {
				setLeaseAcquiredAt(t, stateDir, "boxpool-6", tt.acquiredAt)
			}
			fake := &fakeBranchRelease{t: t, stateDir: stateDir}
			app := "app-1"
			state := hq.MateState{AppID: &app, OpenPerBranch: true, Changes: tt.changes}

			lines := releaseCompletedLeases(t.Context(), stateDir, completionHQ, state, keepBranch, fake.release)

			if got := fake.called(); !slices.Equal(got, tt.want) {
				t.Fatalf("releases = %+v, want %+v", got, tt.want)
			}
			if tt.want == nil {
				if len(lines) != 0 {
					t.Errorf("nothing released, yet the pass says %q", lines)
				}
				return
			}
			if len(lines) != 1 {
				t.Fatalf("lines = %q, want one per released branch", lines)
			}
			for _, want := range tt.wantLine {
				if !strings.Contains(lines[0], want) {
					t.Errorf("line %q lacks %q", lines[0], want)
				}
			}
			if left := leaseRecordsLeft(t, stateDir); !slices.Equal(left, []string{"gitpool-8", "hqpool-5", "webpool-7"}) {
				t.Errorf("leases left = %v, want every lease of another branch, application or host", left)
			}
		})
	}
}

// TestReleaseCompletedLeases_Unsaved_KeptAndSaidOncePerPass: a lease holding
// work a release would lose refuses the release — never forced — so the
// branch's leases stay, and each pass says so in one line naming the leases
// and what is unsaved; the next pass tries again, the same way.
func TestReleaseCompletedLeases_Unsaved_KeptAndSaidOncePerPass(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	writeHQLeaseRecord(t, stateDir, "hqpool", 4, completionRepo, "fix-login")
	writeHQLeaseRecord(t, stateDir, "boxpool", 6, completionRepo, "fix-login")
	fake := &fakeBranchRelease{t: t, stateDir: stateDir, refuse: unsavedRefusalOf()}
	app := "app-1"
	state := hq.MateState{AppID: &app, OpenPerBranch: true, Changes: []hq.MateChange{
		{Repo: "appdev", Number: 4, State: hq.ChangeMerged, Branch: "fix-login"},
	}}

	for pass := 1; pass <= 2; pass++ {
		lines := releaseCompletedLeases(t.Context(), stateDir, completionHQ, state, keepBranch, fake.release)
		if len(lines) != 1 {
			t.Fatalf("pass %d: lines = %q, want one", pass, lines)
		}
		for _, want := range []string{`repository "appdev"`, "change #4", "merged", "hqpool-4", "boxpool-6", "not released", "notes.txt", "abc1234 wip", "next pass"} {
			if !strings.Contains(lines[0], want) {
				t.Errorf("pass %d: line %q lacks %q", pass, lines[0], want)
			}
		}
		if strings.Contains(lines[0], "force") {
			t.Errorf("pass %d: the line names a forced release: %q", pass, lines[0])
		}
		if got := len(fake.called()); got != pass {
			t.Errorf("pass %d: %d releases asked for, want one per pass", pass, got)
		}
	}
	if left := leaseRecordsLeft(t, stateDir); !slices.Equal(left, []string{"boxpool-6", "hqpool-4"}) {
		t.Errorf("leases left = %v, want both kept", left)
	}
}

// TestReleaseCompletedLeases_OtherFailure_SaidAndKept: a release that fails
// for any other reason — an orchestrator that does not answer — keeps the
// leases and says why, and the next pass tries again.
func TestReleaseCompletedLeases_OtherFailure_SaidAndKept(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	writeHQLeaseRecord(t, stateDir, "hqpool", 4, completionRepo, "fix-login")
	fake := &fakeBranchRelease{t: t, stateDir: stateDir, refuse: errors.New("the pool's orchestrator did not answer")}
	app := "app-1"
	state := hq.MateState{AppID: &app, Changes: []hq.MateChange{{Repo: "appdev", Number: 4, State: hq.ChangeClosed, Branch: "fix-login"}}}

	lines := releaseCompletedLeases(t.Context(), stateDir, completionHQ, state, keepBranch, fake.release)

	if len(lines) != 1 || !strings.Contains(lines[0], "did not answer") || !strings.Contains(lines[0], "hqpool-4") {
		t.Errorf("lines = %q, want one naming the lease and why", lines)
	}
	if left := leaseRecordsLeft(t, stateDir); !slices.Equal(left, []string{"hqpool-4"}) {
		t.Errorf("leases left = %v, want the lease kept", left)
	}
}

// keepBranch is a change's branch as HQ named it (hq.Client.BranchOf for an
// HQ that names every branch).
func keepBranch(_ int, branch string) string { return branch }

// setLeaseAcquiredAt rewrites alias's record with another acquire time.
func setLeaseAcquiredAt(t *testing.T, stateDir, alias, at string) {
	t.Helper()
	path := filepath.Join(stateDir, "leases", alias+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw = []byte(strings.Replace(string(raw), `"acquiredAt":"2026-10-25T10:00:00Z"`, fmt.Sprintf("%q:%q", "acquiredAt", at), 1))
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestReleaseCompletedLeases_TheToolsOwnBranchRelease: the pass's release is
// the tool's — the leases of a merged change's branch go back to their pools
// through their orchestrators, their records dropped, after the same
// read-only guard; one holding uncommitted files keeps the branch's every
// lease.
func TestReleaseCompletedLeases_TheToolsOwnBranchRelease(t *testing.T) {
	t.Parallel()
	app := "app-1"
	state := hq.MateState{AppID: &app, Changes: []hq.MateChange{{Repo: "appdev", Number: 4, State: hq.ChangeMerged, Branch: "feat-x"}}}
	for _, tc := range []struct {
		name     string
		files    map[string]string
		wantLeft []string
		wantLine []string
	}{
		{name: "clean", wantLine: []string{"released", "hqpool-3", "webpool-7"}},
		{name: "uncommitted files in one", files: map[string]string{"webpool-7": "?? notes.txt\n"},
			wantLeft: []string{"hqpool-3", "webpool-7"}, wantLine: []string{"not released", "webpool-7", "notes.txt"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ssh := &guardSSH{files: tc.files}
			srv, orchs, deps := completionLeaseServer(t, ssh)
			res := callTool(t, srv, "zerops_lease", map[string]any{"action": "acquire", "pools": []string{"webpool", "hqpool"}, "repo": completionRepo, "branch": "feat-x"})
			if res.IsError {
				t.Fatalf("group acquire failed: %s", getTextContent(t, res))
			}

			lines := releaseCompletedLeases(t.Context(), deps.StateDir, completionHQ, state, keepBranch, func(ctx context.Context, repo, branch string) (ops.LeaseChange, error) {
				return ReleaseLeaseBranch(ctx, deps, repo, branch)
			})

			if len(lines) != 1 {
				t.Fatalf("lines = %q, want one", lines)
			}
			for _, want := range tc.wantLine {
				if !strings.Contains(lines[0], want) {
					t.Errorf("line %q lacks %q", lines[0], want)
				}
			}
			if left := leaseRecordsLeft(t, deps.StateDir); !slices.Equal(left, tc.wantLeft) {
				t.Errorf("leases left = %v, want %v", left, tc.wantLeft)
			}
			wantReleases := 1
			if tc.wantLeft != nil {
				wantReleases = 0
			}
			if n := releaseCounts(orchs); n["hqpool"] != wantReleases || n["webpool"] != wantReleases {
				t.Errorf("orchestrator releases = %v, want %d per pool", n, wantReleases)
			}
		})
	}
}

// completionLeaseServer is groupLeaseServerSSH with the lease operations'
// deps the tool runs on, for a caller that releases beside the tool.
func completionLeaseServer(t *testing.T, ssh ops.SSHDeployer) (*mcp.Server, map[string]*poolOrchestrator, ops.LeaseDeps) {
	t.Helper()
	orchs := map[string]*poolOrchestrator{
		"hqpool":  {pool: "hqpool", token: "hq-orchestrator-fixture", number: 3},
		"webpool": {pool: "webpool", token: "web-orchestrator-fixture", number: 7},
	}
	urls := map[string]string{}
	for name, o := range orchs {
		hs := httptest.NewServer(o)
		t.Cleanup(hs.Close)
		urls["http://orch."+name+".zerops"] = hs.URL
	}
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "proj"}).
		WithServices([]platform.ServiceStack{
			{ID: "svc-hqpool", Name: "hqpool", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1", ServiceStackTypeCategoryName: "USER"}},
			{ID: "svc-webpool", Name: "webpool", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1", ServiceStackTypeCategoryName: "USER"}},
		}).
		WithServiceEnv("svc-hqpool", []platform.ServiceEnvVar{{Key: "API_TOKEN", Content: orchs["hqpool"].token, Sensitive: true}}).
		WithServiceEnv("svc-webpool", []platform.ServiceEnvVar{{Key: "API_TOKEN", Content: orchs["webpool"].token, Sensitive: true}})
	factory := func(baseURL, token string) ops.SwarmAPI {
		return platform.NewSwarmClient(urls[baseURL], token, http.DefaultClient)
	}
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	stateDir := t.TempDir()
	registerLeaseTool(t, srv, mock, "proj-1", stateDir, factory, nil, ssh, runtime.Info{})
	deps := LeaseDepsFor(mock, "proj-1", stateDir, factory, nil, ssh, runtime.Info{})
	// The warm policy's creates and clones outlive the release that starts
	// them; they finish before the state dir is removed.
	var background sync.WaitGroup
	deps.Warm.Go = background.Go
	t.Cleanup(background.Wait)
	return srv, orchs, deps
}

// TestHQPass_MergedChangesLeasesReleasedByThePass: inside a Mate the
// repository pass releases a merged change's leases itself, through the
// release it is given, and the merged note names no release for the agent to
// run; a lease of another branch stays. Outside a Mate the pass releases
// nothing.
func TestHQPass_MergedChangesLeasesReleasedByThePass(t *testing.T) {
	requireGitForLab(t)
	t.Parallel()
	for _, mate := range []bool{true, false} {
		t.Run(fmt.Sprintf("mate=%v", mate), func(t *testing.T) {
			t.Parallel()
			lab := newHQLab(t)
			lab.hq.keepOpenPerBranch()
			lab.wire()
			lab.pushFromMain("fix-login", map[string]string{"login.js": "the fix\n"})
			if answer, text, isError := lab.openChangeFrom("fix-login", "Fix the login"); isError || !answer.Created {
				t.Fatalf("open: %s", text)
			}
			lab.leaseOf("hqpool", 4, "fix-login")
			lab.leaseOf("boxpool", 6, "fix-login")
			lab.leaseOf("hqpool", 5, "other-work")
			lab.hq.mergeChange("appdev", 1)
			fake := &fakeBranchRelease{t: t, stateDir: lab.stateDir}
			lab.completion = fake.release
			lab.rt.MateEnabled = mate

			report := lab.passReport()

			if !mate {
				if got := fake.called(); len(got) != 0 {
					t.Errorf("outside a Mate the pass released %+v", got)
				}
				return
			}
			hqc, _ := openHQ(lab.hq.srv.Client(), lab.rt)
			repo, err := hqc.RepoURL(labApp, "appdev")
			if err != nil {
				t.Fatal(err)
			}
			if got := fake.called(); len(got) != 1 || got[0].branch != "fix-login" || !topology.SameRepoURL(got[0].repo, repo) {
				t.Fatalf("releases = %+v, want the merged change's branch of %s once", got, repo)
			}
			for _, want := range []string{"change #1 is merged", "hqpool-4", "boxpool-6", "released"} {
				if !strings.Contains(report, want) {
					t.Errorf("the pass lacks %q:\n%s", want, report)
				}
			}
			if strings.Contains(report, ops.LeaseReleaseBranchCall("fix-login", "", false)) {
				t.Errorf("the pass tells the agent to release what it released:\n%s", report)
			}
			if left := leaseRecordsLeft(t, lab.stateDir); !slices.Equal(left, []string{"hqpool-5"}) {
				t.Errorf("leases left = %v, want only the other branch's", left)
			}
			if again := lab.passReport(); strings.Contains(again, "hqpool-4") || len(fake.called()) != 1 {
				t.Errorf("the next pass releases again or says it again:\n%s", again)
			}
		})
	}
}

// TestMateService_MergedChangesLeasesReleasedWithNoAgentPass: the Mate's
// service looks at HQ for finished changes while zcp holds leases of its
// repositories, so a change a person merges while no agent runs a pass gets
// its leases released all the same, at a look no sooner than
// leaseCompletionEvery after the one before.
func TestMateService_MergedChangesLeasesReleasedWithNoAgentPass(t *testing.T) {
	requireGitForLab(t)
	t.Parallel()
	lab := newHQLab(t)
	lab.hq.keepOpenPerBranch()
	lab.wire()
	lab.pushFromMain("fix-login", map[string]string{"login.js": "the fix\n"})
	if answer, text, isError := lab.openChangeFrom("fix-login", "Fix the login"); isError || !answer.Created {
		t.Fatalf("open: %s", text)
	}
	lab.leaseOf("hqpool", 4, "fix-login")
	fake := &fakeBranchRelease{t: t, stateDir: lab.stateDir}
	deps := func(ctx context.Context) (devGitDeps, bool) {
		d, ok := lab.devGitDeps(ctx)
		d.release = fake.release
		return d, ok
	}
	start := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	ticks := make(chan time.Time)
	done := make(chan struct{})
	go func() {
		defer close(done)
		keepMateDevGit(t.Context(), deps, start, ticks, func(string) {})
	}()

	ticks <- start.Add(10 * time.Second)
	lab.hq.mergeChange("appdev", 1)
	ticks <- start.Add(20 * time.Second) // within leaseCompletionEvery of the start's look
	ticks <- start.Add(leaseCompletionEvery + 30*time.Second)
	ticks <- start.Add(leaseCompletionEvery + 40*time.Second) // the look before is over
	close(ticks)
	<-done

	if got := fake.called(); len(got) != 1 || got[0].branch != "fix-login" {
		t.Errorf("releases = %+v, want the merged change's branch once", got)
	}
	if left := leaseRecordsLeft(t, lab.stateDir); len(left) != 0 {
		t.Errorf("leases left = %v, want none", left)
	}
}
