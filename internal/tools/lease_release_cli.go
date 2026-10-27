package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/workflow"
)

// `zcp lease release --branch <branch> [--repo <repo URL>] --json` is how the
// Mate's server releases a conversation's branch when the person archives it
// (docs/spec-workflows.md §8 DP-35, docs/spec-mate.md MA-6): the tool's branch
// release (ReleaseLeaseBranch), never forced, refused while another live zcp
// session shares one of the branch's leases (leaseSessionsInUse). Its answer
// is a contract the Mate decodes by exit code: 0 released (every lease the
// call released, by alias — a branch holding none is released already), 3
// refused (nothing released: each lease that keeps the branch, with its
// unsaved work or the other sessions using it), 1 anything else.

// The exit codes of `zcp lease release`.
const (
	LeaseReleaseExitReleased = 0
	LeaseReleaseExitFailed   = 1
	LeaseReleaseExitRefused  = 3
)

// LeaseReleaseAnswer is `zcp lease release --json`'s answer. Exactly one of
// its shapes is written (MarshalJSON): released, refused, or error — an error
// beside the leases the call did release when the orchestrator would not
// release one of them.
type LeaseReleaseAnswer struct {
	Released []LeaseReleasedEntry
	Refused  []LeaseRefusedEntry
	Error    string
}

// LeaseReleasedEntry is a lease the call released and what became of its
// container (ops.LeaseView.ReleaseOutcome: kept-warm, removed, reset or lost).
type LeaseReleasedEntry struct {
	Lease   string `json:"lease"`
	Outcome string `json:"outcome"`
}

// LeaseRefusedEntry is a lease that keeps its branch: the unsaved work a
// release would lose, or how many other live sessions use it.
type LeaseRefusedEntry struct {
	Lease   string             `json:"lease"`
	Unsaved *LeaseUnsavedEntry `json:"unsaved,omitempty"`
	InUse   *LeaseInUseEntry   `json:"inUse,omitempty"`
}

// LeaseUnsavedEntry is a lease's unsaved work: its uncommitted and untracked
// files (`git status --porcelain` lines) and its commits on no remote branch
// ("<short sha> <subject>"), the first 20 of each.
type LeaseUnsavedEntry struct {
	Files   []string `json:"files"`
	Commits []string `json:"commits"`
}

// LeaseInUseEntry is how many live zcp sessions other than the lease's
// acquirer reused it.
type LeaseInUseEntry struct {
	Sessions int `json:"sessions"`
}

// MarshalJSON writes the answer's one shape: {"error": …} (with "released"
// when some leases went), {"refused": […]}, or {"released": […]} — an empty
// list when the branch held no lease.
func (a LeaseReleaseAnswer) MarshalJSON() ([]byte, error) {
	switch {
	case a.Error != "":
		out := struct {
			Error    string               `json:"error"`
			Released []LeaseReleasedEntry `json:"released,omitempty"`
		}{a.Error, a.Released}
		return json.Marshal(out)
	case len(a.Refused) > 0:
		return json.Marshal(struct {
			Refused []LeaseRefusedEntry `json:"refused"`
		}{a.Refused})
	}
	released := a.Released
	if released == nil {
		released = []LeaseReleasedEntry{}
	}
	return json.Marshal(struct {
		Released []LeaseReleasedEntry `json:"released"`
	}{released})
}

// ReleaseLeaseBranchAnswer is `zcp lease release`'s release of branch of repo
// over deps, and its exit code.
func ReleaseLeaseBranchAnswer(ctx context.Context, deps ops.LeaseDeps, repo, branch string) (LeaseReleaseAnswer, int) {
	return releaseLeaseBranchAnswer(ctx, deps, repo, branch, workflow.ProcessAlive)
}

// releaseLeaseBranchAnswer is ReleaseLeaseBranchAnswer, alive telling a live
// zcp session from an ended one.
func releaseLeaseBranchAnswer(ctx context.Context, deps ops.LeaseDeps, repo, branch string, alive func(pid int, start string) bool) (LeaseReleaseAnswer, int) {
	group, err := ops.LeaseBranchGroup(deps.StateDir, repo, branch)
	switch {
	case errors.Is(err, ops.ErrNoLeaseOfBranch):
		return LeaseReleaseAnswer{}, LeaseReleaseExitReleased
	case err != nil:
		return LeaseReleaseAnswer{Error: err.Error()}, LeaseReleaseExitFailed
	}
	var inUse []LeaseRefusedEntry
	for _, l := range group {
		if n := leaseSessionsInUse(deps.StateDir, l, alive); n > 0 {
			inUse = append(inUse, LeaseRefusedEntry{Lease: l.Alias, InUse: &LeaseInUseEntry{Sessions: n}})
		}
	}
	if len(inUse) > 0 {
		return LeaseReleaseAnswer{Refused: inUse}, LeaseReleaseExitRefused
	}
	change, err := ReleaseLeaseBranch(ctx, deps, repo, branch)
	var unsaved *ops.LeaseUnsavedRefusal
	switch {
	case errors.As(err, &unsaved):
		refused := make([]LeaseRefusedEntry, 0, len(unsaved.Leases))
		for _, l := range unsaved.Leases {
			refused = append(refused, LeaseRefusedEntry{Lease: l.Alias, Unsaved: &LeaseUnsavedEntry{Files: nonNil(l.Files), Commits: nonNil(l.Commits)}})
		}
		return LeaseReleaseAnswer{Refused: refused}, LeaseReleaseExitRefused
	case errors.Is(err, ops.ErrNoLeaseOfBranch):
		return LeaseReleaseAnswer{}, LeaseReleaseExitReleased
	case err != nil:
		return LeaseReleaseAnswer{Error: err.Error()}, LeaseReleaseExitFailed
	}
	var answer LeaseReleaseAnswer
	var failed []string
	for _, v := range change.Leases {
		if v.Error != "" {
			failed = append(failed, fmt.Sprintf("%s: %s", v.Alias, v.Error))
			continue
		}
		answer.Released = append(answer.Released, LeaseReleasedEntry{Lease: v.Alias, Outcome: v.ReleaseOutcome()})
	}
	forgetLeaseSessions(deps.StateDir, releasedAliases(change.Leases))
	if len(failed) > 0 {
		answer.Error = "the orchestrator would not release every lease of branch " + branch + ", which stay held (" + strings.Join(failed, "; ") + "); the same call again releases what is left"
		return answer, LeaseReleaseExitFailed
	}
	return answer, LeaseReleaseExitReleased
}

// nonNil is list, or an empty list for none: the contract's lists are never
// null.
func nonNil(list []string) []string {
	if list == nil {
		return []string{}
	}
	return list
}
