package ops

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
)

// A branch release refused for unsaved work (docs/spec-workflows.md §8 DP-4,
// DP-23) says so in prose for the agent. A caller that is no agent — the
// Mate's server through `zcp lease release`, the HQ pass releasing a merged or
// closed change's leases (DP-34, DP-35) — names each lease's work from the
// same reads instead, through LeaseUnsavedError: the refusal is still the
// PREREQUISITE_MISSING one, unchanged, and carries the work beside it.

// LeaseUnsavedWork is one lease's unsaved work as the refused release read
// it: the first leaseGuardFileLimit `git status --porcelain` lines and how
// many there are, and the first leaseGuardFileLimit commits on no remote
// branch ("<short sha> <subject>") and how many there are. Commits may list
// fewer than CommitCount when git could not list them; the count is the
// guard's.
type LeaseUnsavedWork struct {
	Alias       string
	Files       []string
	FileCount   int
	Commits     []string
	CommitCount int
}

// LeaseUnsavedError is a branch release refused because a lease holds work
// a release would lose. Cause is the refusal itself — the
// PREREQUISITE_MISSING one — and Error and Unwrap are its own, so every
// reader of that refusal reads it as before. Leases are the leases holding
// work, by alias; Unread are the leases of the branch the release could not
// check (the refusal's message says why).
type LeaseUnsavedError struct {
	Cause  error
	Leases []LeaseUnsavedWork
	Unread []string
}

func (e *LeaseUnsavedError) Error() string { return e.Cause.Error() }

func (e *LeaseUnsavedError) Unwrap() error { return e.Cause }

// unsavedRefusal is cause — a branch release's refusal — as a
// LeaseUnsavedError when a lease of the branch holds unsaved work, each
// such lease's commits listed for it (one read-only ssh call per lease that
// holds any); cause itself when none does, or when cause is no refusal.
func unsavedRefusal(ctx context.Context, d LeaseDeps, members []*releaseMember, cause error) error {
	var pe *platform.PlatformError
	if cause == nil || !errors.As(cause, &pe) {
		return cause
	}
	var leases []LeaseUnsavedWork
	var unread []string
	for _, m := range members {
		switch {
		case m.lost != "":
		case m.err != nil:
			unread = append(unread, m.rec.alias())
		case m.work.any():
			leases = append(leases, unsavedWorkOf(ctx, d, m.rec.alias(), m.work))
		}
	}
	if len(leases) == 0 {
		return cause
	}
	slices.SortFunc(leases, func(a, b LeaseUnsavedWork) int { return cmp.Compare(a.Alias, b.Alias) })
	slices.Sort(unread)
	return &LeaseUnsavedError{Cause: cause, Leases: leases, Unread: unread}
}

// unsavedWorkOf is w, the guard's read of alias's checkout, as a
// LeaseUnsavedWork, its commits on no remote branch listed when it counted
// any.
func unsavedWorkOf(ctx context.Context, d LeaseDeps, alias string, w unsavedWork) LeaseUnsavedWork {
	files := w.files
	if len(files) > leaseGuardFileLimit {
		files = files[:leaseGuardFileLimit]
	}
	out := LeaseUnsavedWork{Alias: alias, Files: slices.Clone(files), FileCount: len(w.files), CommitCount: w.unpushed}
	if w.unpushed > 0 && d.SSH != nil {
		if listed, err := d.SSH.ExecSSH(ctx, alias, leaseUnpushedListCommand()); err == nil {
			out.Commits = parseUnsavedFiles(string(listed))
		}
	}
	return out
}

// leaseUnpushedListCommand lists, read-only, the commits leaseUnpushedCommand
// counts — every local branch and HEAD, none origin's branches reach — newest
// first, the first leaseGuardFileLimit as "<short sha> <subject>".
func leaseUnpushedListCommand() string {
	wd := shellQuote(LeaseWorkDir)
	log := " log --format=" + shellQuote("%h %s") + " -n " + strconv.Itoa(leaseGuardFileLimit)
	return "if " + leaseGitRead + wd + " rev-parse -q --verify HEAD >/dev/null 2>&1; then " +
		leaseGitRead + wd + log + " --branches HEAD --not --remotes=origin; else " +
		leaseGitRead + wd + log + " --branches --not --remotes=origin; fi"
}

// What a release did with a lease's container (LeaseView.ReleaseOutcome).
const (
	// LeaseOutcomeKeptWarm: reset and kept as its pool's warm container
	// (DP-27).
	LeaseOutcomeKeptWarm = "kept-warm"
	// LeaseOutcomeRemoved: removed with its own lease, or deleted because it
	// could not be reset (DP-15, DP-27).
	LeaseOutcomeRemoved = "removed"
	// LeaseOutcomeReset: reset and left in its pool, not counted warm (a
	// stopped or outdated container whose removal was refused, or a release
	// without the warm policy).
	LeaseOutcomeReset = "reset"
	// LeaseOutcomeLost: no longer zcp's — gone, free or another holder's —
	// so nothing was released and the record was dropped (DP-2).
	LeaseOutcomeLost = "lost"
)

// ReleaseOutcome is what the release that answered v did with its container:
// LeaseOutcomeKeptWarm, LeaseOutcomeRemoved, LeaseOutcomeReset or
// LeaseOutcomeLost; "" for a view no release answered — a lease whose
// release failed stays held.
func (v LeaseView) ReleaseOutcome() string {
	switch {
	case v.State == LeaseStateLost:
		return LeaseOutcomeLost
	case v.State != LeaseStateReleased:
		return ""
	case v.containerRemoved || v.containerDeleted:
		return LeaseOutcomeRemoved
	case v.keptWarm:
		return LeaseOutcomeKeptWarm
	}
	return LeaseOutcomeReset
}

// LeaseBranchGroup is what `zerops_lease action=release branch=<branch>`
// (repo optional) would release — every lease zcp holds, by its records, of
// the branch of one repository (DP-17, DP-23), earliest acquired first — read
// from the records alone, never the orchestrator or a container, and refused
// the way that release refuses it: a branch zcp holds no lease of, one it
// leases for several repositories without repo, a repo that is no clone URL.
func LeaseBranchGroup(stateDir, repo, branch string) ([]LeaseTargetInfo, error) {
	if err := validateLeaseBranch(branch); err != nil {
		return nil, err
	}
	group, err := branchLeaseRecords(stateDir, strings.TrimSpace(repo), branch)
	if err != nil {
		return nil, err
	}
	out := make([]LeaseTargetInfo, 0, len(group))
	for _, r := range group {
		info := r.targetInfo()
		info.Repo, info.Branch = recordRepo(stateDir, r), recordBranch(r)
		out = append(out, info)
	}
	return out, nil
}
