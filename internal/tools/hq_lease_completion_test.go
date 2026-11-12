// Tests for: tools/hq_lease_completion.go — a Mate's HQ pass releases the
// leases of a branch whose change in HQ reads merged or closed, exactly as
// `zerops_lease action=release branch=<branch> repo=<repo>` would and never
// forced; unsaved work keeps them and the pass says so in one line
// (docs/spec-workflows.md §8 DP-34).
package tools

import (
	"context"
	"encoding/json"
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

func (f *fakeBranchRelease) release(_ context.Context, repo, branch string, confirm func([]ops.LeaseTargetInfo) bool) (ops.LeaseChange, error) {
	group, err := ops.LeaseBranchGroup(f.stateDir, repo, branch)
	if err != nil || !confirm(group) {
		return ops.LeaseChange{}, err
	}
	f.mu.Lock()
	f.calls = append(f.calls, releaseCall{repo, branch})
	f.mu.Unlock()
	if f.refuse != nil {
		return ops.LeaseChange{}, f.refuse
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

func TestFollowLeaseCompletion_Decisions_KeepLease(t *testing.T) {
	// non-parallel: subtests reset one HQ server and the same lease records.
	app := "app-1"
	merged := hq.MateChange{Repo: "appdev", Number: 4, State: hq.ChangeMerged, Branch: "fix-login", UpdatedAt: "2026-10-26T10:00:00Z"}
	stateDir := t.TempDir()
	framePath := filepath.Join(stateDir, "state.json")
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected network request: %s %s", r.Method, r.URL.Path)
		http.Error(w, "unexpected request", http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	repo := srv.URL + "/git/org-1/" + app + "/appdev"
	fake := &fakeBranchRelease{t: t, stateDir: stateDir}
	d := devGitDeps{httpClient: srv.Client(), stateDir: stateDir, release: fake.release,
		rt: runtime.Info{InContainer: true, MateEnabled: true, OrgID: "org-1", HQ: &runtime.HQLink{Origin: srv.URL, ProjectID: "mate-1", Key: "test-key", StatePath: framePath}},
	}
	writeState := func(state hq.MateState) {
		t.Helper()
		changes := state.Changes
		if changes == nil {
			changes = []hq.MateChange{}
		}
		raw, err := json.Marshal(map[string]any{"link": "linked", "mate": map[string]any{
			"rev": 1, "projectId": "mate-1", "appId": state.AppID, "repos": []any{}, "changes": changes, "chief": nil, "openPerBranch": state.OpenPerBranch,
		}})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(framePath, raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	reads := 0
	checkKept := func(t *testing.T, frame hq.MateChange, fresh hq.MateState, status int, wantRead bool) {
		t.Helper()
		writeHQLeaseRecord(t, stateDir, "hqpool", 4, repo, "fix-login")
		fake.mu.Lock()
		fake.calls = nil
		fake.mu.Unlock()
		reads = 0
		writeState(hq.MateState{AppID: &app, OpenPerBranch: true, Changes: []hq.MateChange{frame}})
		d.release = func(ctx context.Context, repo, branch string, confirm func([]ops.LeaseTargetInfo) bool) (ops.LeaseChange, error) {
			reads++
			if status != 0 {
				_ = os.Remove(framePath)
			} else {
				writeState(fresh)
			}
			return fake.release(ctx, repo, branch, confirm)
		}
		lines := followLeaseCompletion(t.Context(), d)
		if got := fake.called(); len(got) != 0 {
			t.Errorf("releases = %+v, want the branch kept", got)
		}
		if left := leaseRecordsLeft(t, stateDir); !slices.Equal(left, []string{"hqpool-4"}) {
			t.Errorf("leases left = %v, want hqpool-4", left)
		}
		if len(lines) != 0 {
			t.Errorf("lines = %q, want no release reported", lines)
		}
		if wantRead && reads != 1 {
			t.Errorf("state checks = %d, want one", reads)
		}
	}

	t.Run("a lease is kept when HQ names no settle time for its branch's change", func(t *testing.T) {
		for _, tc := range []struct{ name, updatedAt string }{{"missing", ""}, {"unparseable", "not-a-time"}} {
			t.Run(tc.name, func(t *testing.T) {
				change := merged
				change.UpdatedAt = tc.updatedAt
				fresh := hq.MateState{AppID: &app, OpenPerBranch: true, Changes: []hq.MateChange{change}}
				t.Run("file has no time", func(t *testing.T) { checkKept(t, change, fresh, 0, false) })
				t.Run("file has a time", func(t *testing.T) { checkKept(t, merged, fresh, 0, true) })
			})
		}
	})
	t.Run("a lease is kept when HQ now lists an open change on its branch, whatever the changes file says", func(t *testing.T) {
		for _, number := range []int{3, 5} {
			t.Run(fmt.Sprintf("open change #%d", number), func(t *testing.T) {
				open := merged
				open.Number, open.State = number, hq.ChangeOpen
				fresh := hq.MateState{AppID: &app, OpenPerBranch: true, Changes: []hq.MateChange{open, merged}}
				checkKept(t, merged, fresh, 0, true)
			})
		}
	})
	t.Run("no current evidence keeps the lease", func(t *testing.T) {
		otherApp := "app-2"
		for _, tc := range []struct {
			name   string
			fresh  hq.MateState
			status int
		}{
			{name: "HQ unavailable", status: http.StatusServiceUnavailable},
			{name: "Mate moved to another application", fresh: hq.MateState{AppID: &otherApp, Changes: []hq.MateChange{merged}}},
			{name: "HQ no longer lists the change", fresh: hq.MateState{AppID: &app}},
			{name: "HQ does not list every open change", fresh: hq.MateState{AppID: &app, Changes: []hq.MateChange{merged}}},
		} {
			t.Run(tc.name, func(t *testing.T) { checkKept(t, merged, tc.fresh, tc.status, true) })
		}
	})
	t.Run("each group is checked after the previous release", func(t *testing.T) {
		other := merged
		other.Number, other.Branch = 7, "other-work"
		writeHQLeaseRecord(t, stateDir, "hqpool", 4, repo, "fix-login")
		writeHQLeaseRecord(t, stateDir, "boxpool", 6, repo, "other-work")
		fake.mu.Lock()
		fake.calls = nil
		fake.mu.Unlock()
		current := hq.MateState{AppID: &app, OpenPerBranch: true, Changes: []hq.MateChange{merged, other}}
		reads = 0
		writeState(current)
		d.release = func(ctx context.Context, repo, branch string, confirm func([]ops.LeaseTargetInfo) bool) (ops.LeaseChange, error) {
			reads++
			writeState(current)
			result, err := fake.release(ctx, repo, branch, confirm)
			open := other
			open.Number, open.State = 8, hq.ChangeOpen
			current.Changes = append(current.Changes, open)
			return result, err
		}

		followLeaseCompletion(t.Context(), d)

		if got := fake.called(); !slices.Equal(got, []releaseCall{{repo, "fix-login"}}) {
			t.Errorf("releases = %+v, want only fix-login", got)
		}
		if left := leaseRecordsLeft(t, stateDir); !slices.Equal(left, []string{"boxpool-6"}) {
			t.Errorf("leases left = %v, want boxpool-6", left)
		}
		if reads != 2 {
			t.Errorf("state checks = %d, want one before each group", reads)
		}
	})
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
	landed := closed
	landed.LandedWith = 5
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
		{name: "landed with another change", changes: []hq.MateChange{landed},
			want: []releaseCall{{completionRepo, "fix-login"}}, wantLine: []string{"change #4", "was merged as part of #5", `all of its work on "main"`, "hqpool-4", "boxpool-6", "released"}},
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

			lines := releaseCompletedLeases(t.Context(), stateDir, completionHQ, state.AppID, state.Changes, keepBranch, completionState(state), fake.release)

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
		{Repo: "appdev", Number: 4, State: hq.ChangeMerged, Branch: "fix-login", UpdatedAt: "2026-10-26T10:00:00Z"},
	}}

	for pass := 1; pass <= 2; pass++ {
		lines := releaseCompletedLeases(t.Context(), stateDir, completionHQ, state.AppID, state.Changes, keepBranch, completionState(state), fake.release)
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
	state := hq.MateState{AppID: &app, OpenPerBranch: true, Changes: []hq.MateChange{{Repo: "appdev", Number: 4, State: hq.ChangeClosed, Branch: "fix-login", UpdatedAt: "2026-10-26T10:00:00Z"}}}

	lines := releaseCompletedLeases(t.Context(), stateDir, completionHQ, state.AppID, state.Changes, keepBranch, completionState(state), fake.release)

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

// completionState returns the state HQ names when the completion pass asks.
func completionState(state hq.MateState) func(context.Context) (hq.MateState, error) {
	return func(context.Context) (hq.MateState, error) {
		return hq.MateState{AppID: state.AppID, Changes: state.Changes, OpenPerBranch: state.OpenPerBranch}, nil
	}
}

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
	state := hq.MateState{AppID: &app, OpenPerBranch: true, Changes: []hq.MateChange{{Repo: "appdev", Number: 4, State: hq.ChangeMerged, Branch: "feat-x", UpdatedAt: "2099-10-26T10:00:00Z"}}}
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

			lines := releaseCompletedLeases(t.Context(), deps.StateDir, completionHQ, state.AppID, state.Changes, keepBranch, completionState(state), func(ctx context.Context, repo, branch string, confirm func([]ops.LeaseTargetInfo) bool) (ops.LeaseChange, error) {
				return ops.LeaseReleaseGroupConfirmed(ctx, deps, repo, branch, confirm)
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

func TestReleaseCompletedLeases_LeaseJoiningDuringHQReadIsKept(t *testing.T) {
	t.Parallel()
	ssh := &guardSSH{}
	srv, orchs, deps := completionLeaseServer(t, ssh)
	acquire := func(pool string) {
		t.Helper()
		res := callTool(t, srv, "zerops_lease", map[string]any{
			"action": "acquire", "pools": []string{pool}, "repo": completionRepo, "branch": "feat-x",
		})
		if res.IsError {
			t.Fatalf("acquire %s: %s", pool, getTextContent(t, res))
		}
	}
	acquire("hqpool")
	app := "app-1"
	change := hq.MateChange{Repo: "appdev", Number: 4, State: hq.ChangeMerged, Branch: "feat-x", UpdatedAt: "2099-10-26T10:00:00Z"}
	readStarted, resumeRead, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var resume sync.Once
	unblock := func() { resume.Do(func() { close(resumeRead) }) }
	defer func() { unblock(); <-finished }()
	read := func(ctx context.Context) (hq.MateState, error) {
		close(readStarted)
		select {
		case <-resumeRead:
			return hq.MateState{AppID: &app, OpenPerBranch: true, Changes: []hq.MateChange{change}}, nil
		case <-ctx.Done():
			return hq.MateState{}, ctx.Err()
		}
	}
	go func() {
		defer close(finished)
		releaseCompletedLeases(t.Context(), deps.StateDir, completionHQ, &app, []hq.MateChange{change}, keepBranch, read,
			func(ctx context.Context, repo, branch string, confirm func([]ops.LeaseTargetInfo) bool) (ops.LeaseChange, error) {
				return ops.LeaseReleaseGroupConfirmed(ctx, deps, repo, branch, confirm)
			})
	}()
	<-readStarted
	acquire("webpool")
	unblock()
	<-finished

	if left := leaseRecordsLeft(t, deps.StateDir); !slices.Equal(left, []string{"webpool-7"}) {
		t.Errorf("leases left = %v, want the lease acquired during the read kept", left)
	}
	if counts := releaseCounts(orchs); counts["hqpool"] != 1 || counts["webpool"] != 0 {
		t.Errorf("orchestrator releases = %v, want only the pre-read lease released", counts)
	}
}

func TestReleaseCompletedLeases_AcquisitionNotProvenBeforeSettlement_IsKept(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, acquired, settled string
	}{
		{name: "same second", acquired: "2026-10-26T10:00:00Z", settled: "2026-10-26T10:00:00Z"},
		{name: "fractional settlement in acquisition second", acquired: "2026-10-26T10:00:00Z", settled: "2026-10-26T10:00:00.5Z"},
		{name: "acquisition time missing", settled: "2026-10-26T10:00:00Z"},
		{name: "acquisition time invalid", acquired: "not-a-time", settled: "2026-10-26T10:00:00Z"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stateDir := t.TempDir()
			writeHQLeaseRecord(t, stateDir, "hqpool", 4, completionRepo, "feat-x")
			setLeaseAcquiredAt(t, stateDir, "hqpool-4", tc.acquired)
			app := "app-1"
			state := hq.MateState{AppID: &app, OpenPerBranch: true, Changes: []hq.MateChange{
				{Repo: "appdev", Number: 4, State: hq.ChangeMerged, Branch: "feat-x", UpdatedAt: tc.settled},
			}}
			fake := &fakeBranchRelease{t: t, stateDir: stateDir}
			releaseCompletedLeases(t.Context(), stateDir, completionHQ, &app, state.Changes, keepBranch, completionState(state), fake.release)
			if got := fake.called(); len(got) != 0 {
				t.Errorf("releases = %+v, want the lease kept", got)
			}
			if left := leaseRecordsLeft(t, stateDir); !slices.Equal(left, []string{"hqpool-4"}) {
				t.Errorf("leases left = %v, want hqpool-4", left)
			}
		})
	}
}

func TestCompletionWords_RewireFailureNamesRecovery(t *testing.T) {
	t.Parallel()
	change := hq.MateChange{Number: 4, State: hq.ChangeMerged}
	group := leaseGroupOnHQ{branch: "feat-x", leases: []ops.LeaseTargetInfo{{Alias: "hqpool-3"}}}
	released := ops.LeaseChange{
		Leases:  []ops.LeaseView{{Alias: "hqpool-3", State: ops.LeaseStateReleased}},
		Rewired: []ops.LeaseRewire{{Alias: "webpool-7", Note: "webpool-7 could not be rewired; zerops_dev_server action=restart hostname=webpool-7 sends it again"}},
	}
	for _, tc := range []struct {
		name string
		err  error
	}{
		{name: "release succeeded"},
		{name: "unsaved work refused release", err: unsavedRefusalOf()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			line := completionWords(change, group, released, tc.err)
			if !strings.Contains(line, "webpool-7 could not be rewired") || !strings.Contains(line, "zerops_dev_server action=restart") {
				t.Errorf("completion line %q omitted the sibling's recovery", line)
			}
		})
	}
}

func TestReleaseCompletedLeases_InspectionFailureIsReported(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	writeHQLeaseRecord(t, stateDir, "hqpool", 4, completionRepo, "feat-x")
	app := "app-1"
	change := hq.MateChange{Repo: "appdev", Number: 4, State: hq.ChangeMerged, Branch: "feat-x", UpdatedAt: "2026-10-26T10:00:00Z"}
	lines := releaseCompletedLeases(t.Context(), stateDir, completionHQ, &app, []hq.MateChange{change}, keepBranch,
		func(context.Context) (hq.MateState, error) {
			t.Error("HQ read after failed lease inspection")
			return hq.MateState{}, nil
		},
		func(context.Context, string, string, func([]ops.LeaseTargetInfo) bool) (ops.LeaseChange, error) {
			return ops.LeaseChange{}, errors.New("lease records could not be read")
		})
	if len(lines) != 1 || !strings.Contains(lines[0], "feat-x") || !strings.Contains(lines[0], "lease records could not be read") || strings.Contains(lines[0], "is merged") {
		t.Errorf("lines = %q, want the inspection failure without claiming fresh HQ confirmation", lines)
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

// stateFrame is the Mate's server taking a `state` frame from HQ over its
// link: it writes the frame's changes — the MateChanges fields of the
// Mate's state as HQ has it now — to the file the container's link names, in
// one rename, as the server does on every frame.
func (l *hqLab) stateFrame() {
	l.t.Helper()
	l.snapshotMate(l.t.Context())
}

// TestMateService_MergedChangesLeasesReleasedWithNoAgentPass: the Mate's
// service reads finished changes from the link's last `state` frame, the file
// the Mate's server writes, while zcp holds leases of its repositories, so a
// change a person merges while no agent runs a pass gets its leases released
// all the same — at once when the server renames a frame's file into place,
// with no tick of the service's loop between, and never by reading the file
// on a timer. The service confirms the frame's completed change with HQ:
// a merge no frame has brought yet releases nothing, and no file is no
// finished change.
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
	lab.stateFrame()
	fake := &fakeBranchRelease{t: t, stateDir: lab.stateDir}
	released := make(chan struct{}, 4)
	// The service's looks write no frame: only the server does.
	deps := func(context.Context) (devGitDeps, bool) {
		d := devGitDeps{client: lab.mock, httpClient: lab.hq.srv.Client(), ssh: lab.ssh, rt: lab.rt, stateDir: lab.stateDir}
		d.release = func(ctx context.Context, repo, branch string, confirm func([]ops.LeaseTargetInfo) bool) (ops.LeaseChange, error) {
			accepted := false
			result, err := fake.release(ctx, repo, branch, func(leases []ops.LeaseTargetInfo) bool {
				accepted = confirm(leases)
				return accepted
			})
			if accepted {
				released <- struct{}{}
			}
			return result, err
		}
		return d, true
	}
	const selfRead = "GET /api/mate/self"
	changed, err := hq.WatchReplacedFile(t.Context(), lab.rt.HQ.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	ticks := make(chan time.Time)
	done := make(chan struct{})
	go func() {
		defer close(done)
		keepMateDevGit(t.Context(), deps, start, ticks, changed, func(string) {})
	}()

	// Each send returns once the look before it is over, while the look it
	// starts may still run: the last send only waits for the ticks' looks
	// after the merge to be over before the frame comes.
	ticks <- start.Add(10 * time.Second)
	selfReads := lab.hq.callCount(selfRead)
	lab.hq.mergeChange("appdev", 1)
	ticks <- start.Add(20 * time.Minute)
	ticks <- start.Add(40 * time.Minute)
	ticks <- start.Add(40*time.Minute + 10*time.Second)
	if got := fake.called(); len(got) != 0 {
		t.Fatalf("releases = %+v before any frame said the change is merged, want none", got)
	}
	lab.stateFrame()
	select {
	case <-released:
	case <-done:
		t.Fatal("the service ended before the frame's file woke it")
	}
	close(ticks)
	<-done

	if got := fake.called(); len(got) != 1 || got[0].branch != "fix-login" {
		t.Errorf("releases = %+v, want the merged change's branch once", got)
	}
	if left := leaseRecordsLeft(t, lab.stateDir); len(left) != 0 {
		t.Errorf("leases left = %v, want none", left)
	}
	if n := lab.hq.callCount(selfRead); n != selfReads {
		t.Errorf("the service's looks asked HQ for the Mate's state %d times, want none", n-selfReads)
	}
}
