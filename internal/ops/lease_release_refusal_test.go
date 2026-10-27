// Tests for: a branch release refused for unsaved work carries, beside its
// message, each lease's own unsaved work — files and commits — so a caller
// that is no agent (`zcp lease release`, the HQ pass) can name it without
// parsing prose (docs/spec-workflows.md §8 DP-4, DP-23, DP-35); a released
// view names what became of its container; the branch's group is readable
// before its release.
package ops

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// answerGuardWithCommits is answerGuard, with the commits on no remote branch
// each lease lists (git log), one "<sha> <subject>" line each.
func (gf *groupFixture) answerGuardWithCommits(files map[string]string, commits map[string][]string) {
	gf.ssh.setHandler(func(host, command string) ([]byte, error) {
		switch {
		case strings.Contains(command, "status --porcelain"):
			return []byte(files[host]), nil
		case strings.Contains(command, "rev-list --count"):
			return []byte(strconv.Itoa(len(commits[host])) + "\n"), nil
		case strings.Contains(command, " log "):
			return []byte(strings.Join(commits[host], "\n") + "\n"), nil
		}
		return nil, nil
	})
}

// TestLeaseReleaseGroup_UnsavedRefusal_NamesEachLeasesFilesAndCommits: a
// branch release refused for unsaved work answers a LeaseUnsavedError —
// still the PREREQUISITE_MISSING refusal it always was — listing each lease
// that holds work, by alias, with its files and its commits on no remote
// branch (each "<sha> <subject>", read only for the refusal), and no lease
// that holds none. A refusal that is not about unsaved work — a lease that
// cannot be read — is no LeaseUnsavedError.
func TestLeaseReleaseGroup_UnsavedRefusal_NamesEachLeasesFilesAndCommits(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		files   map[string]string
		commits map[string][]string
		readErr string
		want    []LeaseUnsavedWork
	}{
		{
			name:    "files in one, commits in the other",
			files:   map[string]string{"webpool-7": "?? notes.txt\n M src/app.ts\n"},
			commits: map[string][]string{"hqpool-3": {"abc1234 wire the login", "def5678 fix the form"}},
			want: []LeaseUnsavedWork{
				{Alias: "hqpool-3", Commits: []string{"abc1234 wire the login", "def5678 fix the form"}, CommitCount: 2},
				{Alias: "webpool-7", Files: []string{"?? notes.txt", " M src/app.ts"}, FileCount: 2},
			},
		},
		{
			name:  "files in one only",
			files: map[string]string{"hqpool-3": "?? scratch.md\n"},
			want:  []LeaseUnsavedWork{{Alias: "hqpool-3", Files: []string{"?? scratch.md"}, FileCount: 1}},
		},
		{
			name:    "one cannot be read",
			readErr: "webpool-7",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gf := newGroupFixture(t)
			gf.heldGroup(t)
			gf.answerGuardWithCommits(tt.files, tt.commits)
			if tt.readErr != "" {
				clean := gf.ssh.fn
				gf.ssh.setHandler(func(host, command string) ([]byte, error) {
					if host == tt.readErr && strings.Contains(command, "status --porcelain") {
						out := "fatal: not a git repository (or any of the parent directories): .git"
						return []byte(out), sshFailure(out, exitError(t, 128))
					}
					return clean(host, command)
				})
			}

			_, err := LeaseReleaseGroupChange(context.Background(), gf.deps, "", "feat", false)
			var pe *platform.PlatformError
			if !errors.As(err, &pe) || pe.Code != platform.ErrPrerequisiteMissing {
				t.Fatalf("err = %v; want the PREREQUISITE_MISSING refusal", err)
			}
			var refusal *LeaseUnsavedError
			got := errors.As(err, &refusal)
			if tt.want == nil {
				if got {
					t.Errorf("a refusal over a lease that cannot be read is no unsaved-work refusal: %+v", refusal.Leases)
				}
				return
			}
			if !got {
				t.Fatalf("err = %v (%T); want a LeaseUnsavedError", err, err)
			}
			if !slices.EqualFunc(refusal.Leases, tt.want, func(a, b LeaseUnsavedWork) bool {
				return a.Alias == b.Alias && slices.Equal(a.Files, b.Files) && a.FileCount == b.FileCount &&
					slices.Equal(a.Commits, b.Commits) && a.CommitCount == b.CommitCount
			}) {
				t.Errorf("leases = %+v, want %+v", refusal.Leases, tt.want)
			}
			if refusal.Error() != pe.Error() {
				t.Errorf("Error() = %q, want the refusal's own %q", refusal.Error(), pe.Error())
			}
			if recs := gf.records(t); len(recs) != 2 {
				t.Errorf("records = %v, want both kept", recs)
			}
		})
	}
}

// TestLeaseReleaseGroup_OneMemberUnsaved_TypedAsItsOwnRelease: a branch of
// one lease is refused as that lease's own release is, and the refusal still
// names its work.
func TestLeaseReleaseGroup_OneMemberUnsaved_TypedAsItsOwnRelease(t *testing.T) {
	t.Parallel()
	gf := newGroupFixture(t)
	if _, err := LeaseAcquire(context.Background(), gf.deps, groupHQPool, testRepo, "feat"); err != nil {
		t.Fatal(err)
	}
	gf.answerGuardWithCommits(map[string]string{"hqpool-3": "?? notes.txt\n"}, map[string][]string{"hqpool-3": {"abc1234 wip"}})

	_, err := LeaseReleaseGroupChange(context.Background(), gf.deps, "", "feat", false)
	var refusal *LeaseUnsavedError
	if !errors.As(err, &refusal) {
		t.Fatalf("err = %v (%T); want a LeaseUnsavedError", err, err)
	}
	want := LeaseUnsavedWork{Alias: "hqpool-3", Files: []string{"?? notes.txt"}, FileCount: 1, Commits: []string{"abc1234 wip"}, CommitCount: 1}
	if len(refusal.Leases) != 1 || refusal.Leases[0].Alias != want.Alias || !slices.Equal(refusal.Leases[0].Files, want.Files) ||
		!slices.Equal(refusal.Leases[0].Commits, want.Commits) || refusal.Leases[0].CommitCount != 1 {
		t.Errorf("leases = %+v, want [%+v]", refusal.Leases, want)
	}
}

// TestLeaseView_ReleaseOutcome: a released view names what became of its
// container — kept as the pool's warm container, removed (with its lease, or
// deleted because it could not be reset), reset and left in the pool, or
// lost (no longer zcp's, nothing released); a view nothing released names
// nothing.
func TestLeaseView_ReleaseOutcome(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		view LeaseView
		want string
	}{
		{"kept warm", LeaseView{State: LeaseStateReleased, keptWarm: true}, LeaseOutcomeKeptWarm},
		{"removed with its lease", LeaseView{State: LeaseStateReleased, containerRemoved: true}, LeaseOutcomeRemoved},
		{"deleted, it could not be reset", LeaseView{State: LeaseStateReleased, containerDeleted: true}, LeaseOutcomeRemoved},
		{"reset, left in the pool", LeaseView{State: LeaseStateReleased}, LeaseOutcomeReset},
		{"lost", LeaseView{State: LeaseStateLost}, LeaseOutcomeLost},
		{"held, its release failed", LeaseView{State: LeaseStateHeld, Error: "refused"}, ""},
	}
	for _, tt := range tests {
		if got := tt.view.ReleaseOutcome(); got != tt.want {
			t.Errorf("%s: ReleaseOutcome() = %q, want %q", tt.name, got, tt.want)
		}
	}
}

// TestLeaseBranchGroup_IsWhatABranchReleaseTakes: the group a branch release
// takes is readable before it — each lease's alias and acquire time, from
// zcp's records only — and refused the way the release refuses it.
func TestLeaseBranchGroup_IsWhatABranchReleaseTakes(t *testing.T) {
	t.Parallel()
	gf := newGroupFixture(t)
	gf.heldGroup(t)

	group, err := LeaseBranchGroup(gf.stateDir, "", "feat")
	if err != nil {
		t.Fatalf("LeaseBranchGroup: %v", err)
	}
	aliases := make([]string, 0, len(group))
	for _, l := range group {
		aliases = append(aliases, l.Alias)
		if l.AcquiredAt == "" || l.Branch != "feat" {
			t.Errorf("%s: acquiredAt %q, branch %q", l.Alias, l.AcquiredAt, l.Branch)
		}
	}
	slices.Sort(aliases)
	if !slices.Equal(aliases, []string{"hqpool-3", "webpool-7"}) {
		t.Errorf("group = %v, want both leases of the branch", aliases)
	}
	if again, err := LeaseBranchGroup(gf.stateDir, testRepo, "feat"); err != nil || len(again) != 2 {
		t.Errorf("group of the named repository = %+v, %v", again, err)
	}
	var pe *platform.PlatformError
	if _, err := LeaseBranchGroup(gf.stateDir, "", "nothing-here"); !errors.As(err, &pe) || pe.Code != platform.ErrInvalidParameter {
		t.Errorf("a branch zcp holds no lease of: err = %v, want the release's refusal", err)
	}
}
