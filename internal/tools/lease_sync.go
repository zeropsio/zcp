package tools

import (
	"fmt"
	"strings"

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

// leaseSyncNext is what a sync's result tells the agent to do next: restart
// each server that runs without a watch on a lease that moved, and look at
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
		next = append(next, "These dev servers run without a watch and keep the old code until restarted: "+strings.Join(restarts, "; ")+".")
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
