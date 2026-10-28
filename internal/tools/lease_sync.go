package tools

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// leaseSyncToolResult is what zerops_lease action=sync returns
// (docs/spec-workflows.md §8 DP-24). It never carries the lease string.
type leaseSyncToolResult struct {
	Action      string                  `json:"action"`
	Sync        ops.LeaseSyncView       `json:"sync"`
	NextActions string                  `json:"nextActions,omitempty"`
	Envelope    *workflow.StateEnvelope `json:"envelope,omitempty"`
}

// leaseSyncInputRefusal refuses a sync call that is not one lease alone: the
// branch and its other leases are the lease's own, so pool, branch and repo
// have no part in it, and a sync never forces. nil when the call is one.
func leaseSyncInputRefusal(input LeaseInput) error {
	switch {
	case input.Lease == "":
		return platform.NewPlatformError(platform.ErrInvalidParameter,
			"sync needs lease", "Pass the alias (<pool>-<number>) of the lease you committed in as lease; zerops_lease action=status lists them")
	case input.Pool != "" || input.Branch != "" || input.Repo != "":
		return platform.NewPlatformError(platform.ErrInvalidParameter,
			"sync takes lease only: its branch and the branch's other leases are the lease's own", "Pass only the alias of the lease you committed in as lease")
	}
	if f := input.Force.Ptr(); f != nil && *f {
		return platform.NewPlatformError(platform.ErrInvalidParameter,
			"sync never forces: it pushes plainly and only fast-forwards", "Drop force; a lease sync leaves as it was is named in the result with why and what to do")
	}
	return nil
}

// corePushSyncRefusal is a sync of the lease alias refused as non-fast-forward
// (err) with HQ's own push named as the cause when it is one: the open change
// on the lease's branch of a repository on this Mate's HQ (address), as the
// Mate's state names it now — read once through read, only for that refusal —
// carries HQ's merge commit at its head (hq.CorePush), which origin's branch
// holds and the lease does not. The agent brings it in over ssh in the lease,
// a fast-forward when the lease has no commits of its own since, a merge
// otherwise, never a rebase, and syncs again. err as it is for any other
// refusal, any other lease, no reader, or a state that names no such change.
func corePushSyncRefusal(err error, stateDir, address, alias string, read func() *hq.MateState) error {
	var pe *platform.PlatformError
	if read == nil || !errors.As(err, &pe) || pe.Code != platform.ErrGitPushNonFastForward {
		return err
	}
	leases := hqLeaseBranches(stateDir, address)
	i := slices.IndexFunc(leases, func(l hqLeaseBranch) bool { return l.alias == alias })
	if i < 0 || leases[i].branch == hqBase {
		return err
	}
	l := leases[i]
	state := read()
	if state == nil || state.AppID == nil || *state.AppID != l.appID {
		return err
	}
	for _, c := range state.Changes {
		if c.Repo != l.repo || c.Branch != l.branch || c.State != hq.ChangeOpen || !c.CorePushAtHead() {
			continue
		}
		named := *pe
		named.Suggestion = fmt.Sprintf("HQ brought main into #%d after #%d merged: origin's %s is HQ's merge commit on what this lease had, and %s lacks it. Bring it in over ssh — `ssh %s \"cd /var/www && git fetch origin && git merge --no-edit origin/%s\"`, a fast-forward when the lease has no commits of its own since, a merge otherwise; never rebase, never force-push — then sync again: %s",
			c.Number, c.CorePush.Landed, l.branch, alias, alias, l.branch, ops.LeaseSyncCall(alias))
		return &named
	}
	return err
}

// leaseSyncNext is what a sync's result tells the agent to do next: restart
// each server nothing reloads (a runtime entry) on a lease that moved, and look at
// each lease the sync left as it was (its note says why); a credential a
// lease lacks is the user's to give.
func leaseSyncNext(v ops.LeaseSyncView) string {
	restarts := make([]string, 0, len(v.Members))
	var left []string
	credential := false
	for _, m := range v.Members {
		for _, name := range m.UnwatchedServers {
			restarts = append(restarts, fmt.Sprintf("zerops_dev_server action=restart hostname=%q name=%q", m.Alias, name))
		}
		switch m.Outcome {
		case ops.LeaseSyncFastForwarded, ops.LeaseSyncUpToDate:
		default:
			left = append(left, m.Alias+" ("+m.Outcome+")")
		}
		credential = credential || credentialErrorCodes[m.Code]
	}
	var next []string
	if len(restarts) > 0 {
		next = append(next, "These dev servers are runtime entries no declared watch covers, so they keep the old code until restarted: "+strings.Join(restarts, "; ")+".")
	}
	if len(left) > 0 {
		next = append(next, "Not fast-forwarded, left as they were (each lease's note says why and what to do): "+strings.Join(left, ", ")+".")
	}
	if credential {
		next = append(next, credentialUserOwnedContract)
	}
	if len(v.Members) == 0 {
		next = append(next, "No other lease holds branch "+v.Branch+"; origin has it at "+shortCommit(v.Commit)+".")
	}
	return strings.Join(next, " ")
}

// shortCommit is a commit id's first 12 characters.
func shortCommit(c string) string {
	if len(c) > 12 {
		return c[:12]
	}
	return c
}
