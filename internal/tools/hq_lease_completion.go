package tools

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/schema"
)

// A branch's leases are the person's work in progress on that branch
// (docs/spec-workflows.md §8 DP-17); once the branch's change in HQ is merged
// or closed that work is finished, and nobody should have to release them by
// hand. A Mate's HQ pass — the repository reconcile bootstrap, adopt and
// develop's start run, and the Mate service's look — releases them itself
// (DP-34): exactly the tool's branch release, never forced, so the release's
// own guard (DP-4) keeps any lease holding uncommitted or untracked files or
// commits on no remote branch, and the pass says so in one line. It is
// completion that releases, never idleness (docs/spec-mate.md §0 rule 4,
// DP-25).

// leaseBranchRelease releases every lease zcp holds, by its records, of
// branch of repo — the tool's `zerops_lease action=release branch=<branch>
// repo=<repo>`, unforced (ReleaseLeaseBranch over the deps the tool runs on).
type leaseBranchRelease func(ctx context.Context, repo, branch string) (ops.LeaseChange, error)

// LeaseDepsFor is what zerops_lease runs its operations on in the container
// rt describes, and so what every release beside the tool runs on — the HQ
// pass's (DP-34) and `zcp lease release`'s (DP-35): the lease operations over
// client and swarm, a lease's ssh alias and mount through mounter (nil: no
// host side), git in its container through ssh, each pool's warm container
// (DP-27), the HQ a repository is recognised on, and — inside a Mate — the
// Mate's Git credential for a pool's leases of an HQ repository (DP-16).
func LeaseDepsFor(client platform.Client, projectID, stateDir string, swarm ops.SwarmFactory, mounter ops.Mounter, ssh ops.SSHDeployer, rt runtime.Info) ops.LeaseDeps {
	return leaseDepsIn(leaseDeps(client, projectID, stateDir, swarm, mounter, ssh), rt)
}

// leaseDepsIn is deps in the container rt describes: the HQ a repository is
// recognised on and, inside a Mate, the pool's HQ credential (DP-12, DP-16).
func leaseDepsIn(deps ops.LeaseDeps, rt runtime.Info) ops.LeaseDeps {
	// The HQ a repository is recognised on is the one the container rt
	// presents itself to, read at each acquire, as git-push-setup reads it
	// for a dev service.
	deps.HQAddress = func() string { return hqAddress(rt) }
	if rt.MateEnabled {
		// Inside a Mate an acquire of an HQ repository puts the Mate
		// credential on the pool (ops.PoolHQCredentialEnvKey), as
		// git-push-setup puts it on a wired dev service (DP-16); outside one
		// zcp writes nothing, and the pool's GIT_TOKEN is the user's always.
		deps.PoolCredential = ops.PoolHQCredential{Read: hqCredential(rt), Kept: pairKeptPoolCredential(deps.StateDir)}
	}
	return deps
}

// ReleaseLeaseBranch is the tool's release of a branch, unforced: every lease
// zcp holds of branch of repo (repo empty: the one repository zcp leases the
// branch for), all or none, refused while any holds work a release would lose
// (ops.LeaseUnsavedError) — the one code path the tool, the HQ pass and
// `zcp lease release` share.
func ReleaseLeaseBranch(ctx context.Context, deps ops.LeaseDeps, repo, branch string) (ops.LeaseChange, error) {
	return releaseLeaseBranch(ctx, deps, repo, branch, false)
}

// releaseLeaseBranch is zerops_lease action=release branch=<branch>: the
// group's release (ops.LeaseReleaseGroupChange), git's stderr from an ssh
// failure put into the refusal (withLeaseSSHStderr), a refusal for unsaved
// work kept as it is so its typed reader still finds it.
func releaseLeaseBranch(ctx context.Context, deps ops.LeaseDeps, repo, branch string, force bool) (ops.LeaseChange, error) {
	change, err := ops.LeaseReleaseGroupChange(ctx, deps, repo, branch, force)
	var unsaved *ops.LeaseUnsavedError
	if err != nil && !errors.As(err, &unsaved) {
		err = withLeaseSSHStderr(err)
	}
	return change, err
}

// leaseBranchReleaseFor is the HQ pass's release in the container rt
// describes: ReleaseLeaseBranch over the deps zerops_lease runs on, the pools'
// orchestrators reached as the tool reaches them.
func leaseBranchReleaseFor(client platform.Client, projectID, stateDir string, mounter ops.Mounter, ssh ops.SSHDeployer, rt runtime.Info) leaseBranchRelease {
	if client == nil || stateDir == "" || !rt.InContainer || !rt.MateEnabled {
		return nil
	}
	deps := LeaseDepsFor(client, projectID, stateDir, ops.NewSwarmFactory(&http.Client{Timeout: ops.LeaseCallTimeout}), mounter, ssh, rt)
	return func(ctx context.Context, repo, branch string) (ops.LeaseChange, error) {
		return ReleaseLeaseBranch(ctx, deps, repo, branch)
	}
}

// hqPassOption configures a repository pass (reconcileHQRepositories).
type hqPassOption func(*hqPassConfig)

// hqPassConfig is what a pass's options set.
type hqPassConfig struct {
	schemas *schema.Cache
	// release releases a finished change's leases (DP-34); nil releases
	// none.
	release leaseBranchRelease
	// recipeFollows: the recipe reconcile runs right after the pass
	// (reconcileMateRepositories), so the pass leaves a conflicting
	// import-files change to it (recipeChangeTurn).
	recipeFollows bool
}

// withRecipeFollowing says the recipe reconcile runs right after the pass.
func withRecipeFollowing() hqPassOption {
	return func(c *hqPassConfig) { c.recipeFollows = true }
}

// withLeaseCompletion lets a Mate's pass release the leases of every branch
// whose change in HQ is merged or closed through release (DP-34).
func withLeaseCompletion(release leaseBranchRelease) hqPassOption {
	return func(c *hqPassConfig) { c.release = release }
}

// hqPassConfigOf is the config opts set.
func hqPassConfigOf(opts []hqPassOption) hqPassConfig {
	var cfg hqPassConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

// leaseGroupOnHQ is one group of leases zcp holds (DP-17) of a repository of
// the application HQ holds the Mate in: the repository's name there, its
// clone URL as the records keep it, the branch and the leases.
type leaseGroupOnHQ struct {
	repo, repoURL, branch string
	leases                []ops.LeaseTargetInfo
}

// releaseCompletedLeases releases, through release, every group of leases
// zcp holds of a repository of the application HQ holds the Mate in (appID,
// nil for none) whose branch's change in changes — the Mate's changes there —
// reads merged or closed (settledChangeOn), and still does in a fresh read of
// the Mate's state in HQ — and says
// what became of each in one line, "repository <name>: …", by repository and
// branch. A group leased again after its change settled is a new start on the
// branch and stays. Leases of another application's repository, or of a
// repository outside HQ (address), are never touched. release refusing for
// unsaved work keeps every lease of the group, and the line names the leases
// and what each holds; the next pass tries again. branchOf is the branch of a
// change by its number and the branch HQ names (hq.Client.BranchOf). Nothing
// when release or readState is nil.
func releaseCompletedLeases(ctx context.Context, stateDir, address string, appID *string, changes []hq.MateChange, branchOf func(int, string) string, readState func(context.Context) (hq.MateState, error), release leaseBranchRelease) []string {
	if release == nil || readState == nil || appID == nil {
		return nil
	}
	var lines []string
	for _, group := range leaseGroupsOnHQ(stateDir, address, *appID) {
		if _, settled := settledChangeOn(changes, group.repo, group.branch, branchOf); !settled {
			continue
		}
		readCtx, cancel := context.WithTimeout(ctx, hqChangesReadBound)
		state, err := readState(readCtx)
		cancel()
		// An older HQ's limited history may omit an open change, so only a
		// state that lists every open change can establish it is settled.
		if err != nil || state.AppID == nil || *state.AppID != *appID || !state.OpenPerBranch {
			continue
		}
		change, settled := settledChangeOn(state.Changes, group.repo, group.branch, branchOf)
		if !settled || leasedAgainSince(group.leases, change.UpdatedAt) {
			continue
		}
		released, err := release(ctx, group.repoURL, group.branch)
		lines = append(lines, fmt.Sprintf("repository %q: %s", group.repo, completionWords(change, group, released, err)))
	}
	return lines
}

// leaseGroupsOnHQ is every group of leases zcp holds of a repository of appID
// on this Mate's HQ (address), by repository and branch.
func leaseGroupsOnHQ(stateDir, address, appID string) []leaseGroupOnHQ {
	leases, _ := ops.LeaseBranches(stateDir)
	var groups []leaseGroupOnHQ
	for _, l := range leases {
		app, repo, ok := hq.RepoOfURL(address, l.Repo)
		if !ok || app != appID {
			continue
		}
		i := slices.IndexFunc(groups, func(g leaseGroupOnHQ) bool { return g.repo == repo && g.branch == l.Branch })
		if i < 0 {
			groups = append(groups, leaseGroupOnHQ{repo: repo, repoURL: l.Repo, branch: l.Branch})
			i = len(groups) - 1
		}
		groups[i].leases = append(groups[i].leases, l)
	}
	slices.SortFunc(groups, func(a, b leaseGroupOnHQ) int {
		return cmp.Or(cmp.Compare(a.repo, b.repo), cmp.Compare(a.branch, b.branch))
	})
	return groups
}

// settledChangeOn is the change of branch in repo as changes name it once it
// has settled: the newest change listed on the branch, merged or closed,
// while none on it is open — another change still using the branch keeps its
// leases. false while one is open, and when none is listed on the branch:
// absence is no word that the work is finished.
func settledChangeOn(changes []hq.MateChange, repo, branch string, branchOf func(int, string) string) (hq.MateChange, bool) {
	var newest *hq.MateChange
	for i := range changes {
		c := &changes[i]
		if c.Repo != repo || branchOf(c.Number, c.Branch) != branch {
			continue
		}
		if c.State == hq.ChangeOpen {
			return hq.MateChange{}, false
		}
		if newest == nil || c.Number > newest.Number {
			newest = c
		}
	}
	if newest == nil || (newest.State != hq.ChangeMerged && newest.State != hq.ChangeClosed) {
		return hq.MateChange{}, false
	}
	return *newest, true
}

// leasedAgainSince reports whether any of leases was acquired after settled,
// when HQ last changed its record of the settled change: the branch was
// leased again for new work, which that change's end does not finish. A time
// HQ does not name, or that does not parse, keeps the group; an acquire time
// the record does not name, or that does not parse, reads as before.
func leasedAgainSince(leases []ops.LeaseTargetInfo, settled string) bool {
	at, err := time.Parse(time.RFC3339, settled)
	if err != nil {
		return true
	}
	return slices.ContainsFunc(leases, func(l ops.LeaseTargetInfo) bool {
		acquired, err := time.Parse(time.RFC3339, l.AcquiredAt)
		return err == nil && acquired.After(at)
	})
}

// completionWords is what became of group's release once change settled:
// released (each lease with what became of its container, a lease the
// orchestrator would not release named with why), or not released — its
// leases kept, with the unsaved work each holds, or why.
func completionWords(change hq.MateChange, group leaseGroupOnHQ, released ops.LeaseChange, err error) string {
	ended := "merged"
	if change.State == hq.ChangeClosed {
		ended = "closed without merging"
	}
	head := fmt.Sprintf("change #%d on %q is %s", change.Number, group.branch, ended)
	aliases := make([]string, 0, len(group.leases))
	for _, l := range group.leases {
		aliases = append(aliases, l.Alias)
	}
	held := strings.Join(aliases, ", ")
	var refusal *ops.LeaseUnsavedError
	switch {
	case errors.As(err, &refusal):
		return fmt.Sprintf("%s, but its leases %s were not released: %s — commit it on a branch of your own and push it from the lease, and the next pass releases them; nothing unsaved is discarded unasked",
			head, held, unsavedWorkWords(refusal))
	case err != nil:
		return fmt.Sprintf("%s, but its leases %s were not released (%s); the next pass tries again", head, held, err.Error())
	}
	var went, stayed []string
	for _, v := range released.Leases {
		if v.Error != "" {
			stayed = append(stayed, fmt.Sprintf("%s (%s)", v.Alias, v.Error))
			continue
		}
		went = append(went, v.Alias+releaseOutcomeWords(v.ReleaseOutcome()))
	}
	words := fmt.Sprintf("%s, so zcp released its leases: %s", head, strings.Join(went, ", "))
	if len(stayed) > 0 {
		words += fmt.Sprintf("; the orchestrator would not release %s, which stays held — the next pass tries again", strings.Join(stayed, ", "))
	}
	return words
}

// unsavedWorkWords names what each lease a refused release read holds: its
// files (the first ones, all counted) and its commits on no remote branch.
func unsavedWorkWords(refusal *ops.LeaseUnsavedError) string {
	parts := make([]string, 0, len(refusal.Leases)+1)
	for _, l := range refusal.Leases {
		var holds []string
		if l.FileCount > 0 {
			holds = append(holds, fmt.Sprintf("%d uncommitted or untracked file(s) (%s)", l.FileCount, strings.Join(l.Files, ", ")))
		}
		if l.CommitCount > 0 {
			commits := fmt.Sprintf("%d commit(s) on no remote branch", l.CommitCount)
			if len(l.Commits) > 0 {
				commits += " (" + strings.Join(l.Commits, "; ") + ")"
			}
			holds = append(holds, commits)
		}
		parts = append(parts, l.Alias+" holds "+strings.Join(holds, " and "))
	}
	if len(refusal.Unread) > 0 {
		parts = append(parts, strings.Join(refusal.Unread, ", ")+" could not be checked")
	}
	return strings.Join(parts, "; ")
}

// releaseOutcomeWords says what a release did with a lease's container, as a
// suffix of its alias.
func releaseOutcomeWords(outcome string) string {
	switch outcome {
	case ops.LeaseOutcomeKeptWarm:
		return " (kept as its pool's warm container)"
	case ops.LeaseOutcomeRemoved:
		return " (its container removed)"
	case ops.LeaseOutcomeLost:
		return " (no longer zcp's, so nothing was released)"
	case ops.LeaseOutcomeReset:
		return " (reset)"
	}
	return ""
}

// followLeaseCompletion is the Mate's service's look for finished changes
// (DP-34), run when the Mate's server put a new `state` frame's changes in
// place (keepMateDevGit): only while zcp holds a lease of a repository on
// this Mate's HQ, it reads the changes the link's last `state` frame brought
// (hq.ReadMateChanges, the file the Mate's server writes) and releases what
// releaseCompletedLeases confirms with HQ. No file, or one that cannot be
// read, is no finished change.
func followLeaseCompletion(ctx context.Context, d devGitDeps) []string {
	if d.release == nil {
		return nil
	}
	hqc, linked := openHQ(d.httpClient, d.rt)
	if !linked || len(hqLeaseBranches(d.stateDir, hqc.Address())) == 0 {
		return nil
	}
	frame, err := hq.ReadMateChanges(hqIdentity(d.rt).ChangesPath)
	if err != nil {
		return nil
	}
	return releaseCompletedLeases(ctx, d.stateDir, hqc.Address(), frame.AppID, frame.Changes, hqc.BranchOf, hqc.Once().Self, d.release)
}

// withRecipeSchemas shares the server schema cache with recipe conflict passes.
func withRecipeSchemas(schemas *schema.Cache) hqPassOption {
	return func(c *hqPassConfig) { c.schemas = schemas }
}
