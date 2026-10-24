package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

const (
	leaseActionAcquire = "acquire"
	leaseActionStatus  = "status"
	leaseActionRelease = "release"
	leaseActionRollout = "rollout"
	leaseActionSync    = "sync"
)

// LeaseInput is the input type for zerops_lease.
type LeaseInput struct {
	Action string `json:"action"`
	Pool   string `json:"pool,omitempty"`
	// Pools is a group acquire: one lease per pool on the branch (DP-17).
	Pools  []string  `json:"pools,omitempty"`
	Branch string    `json:"branch,omitempty"`
	Repo   string    `json:"repo,omitempty"`
	Lease  string    `json:"lease,omitempty"`
	Force  *FlexBool `json:"force,omitempty"`
}

// leaseInputSchema is explicit because force is a FlexBool (an agent that
// stringifies booleans must not be bounced by the schema).
func leaseInputSchema() *jsonschema.Schema {
	return objectSchema(map[string]*jsonschema.Schema{
		"action": {Type: "string", Description: "acquire, status, release, rollout or sync."},
		"pool":   {Type: "string", Description: "Pool service hostname. acquire (or pools), rollout: required. status: optional (default every pool)."},
		"pools":  {Type: "array", Items: &jsonschema.Schema{Type: "string"}, Description: "acquire: one lease per pool on the branch, all or none."},
		"branch": {Type: "string", Description: "acquire: branch to check out (default main); a new one is cut from main. release: every lease of it, all or none."},
		"repo":   {Type: "string", Description: "acquire: https clone URL; the first one on a pool becomes its default. release: the branch's, if several."},
		"lease":  {Type: "string", Description: "release, sync: the lease alias <pool>-<number> from acquire or status (not a secret)."},
		"force":  flexBoolSchema("release: discard unpushed commits and uncommitted files (default false: refused while any exist)."),
	}, "action")
}

// leaseDeps wires the lease operations: with a mounter (container mode) a
// lease gets its ssh alias and mount (ops.NewLeaseHost); without one the host
// side does nothing. The HQ a repository is recognised on is read from the
// enrollment at each acquire, as git-push-setup reads it for a dev service.
func leaseDeps(client platform.Client, projectID, stateDir string, swarm ops.SwarmFactory, mounter ops.Mounter, ssh ops.SSHDeployer) ops.LeaseDeps {
	deps := ops.LeaseDeps{Client: client, ProjectID: projectID, StateDir: stateDir, Swarm: swarm, SSH: ssh, HQAddress: hqAddress}
	if mounter != nil {
		deps.Host = ops.NewLeaseHost(mounter)
	}
	return deps
}

// leaseToolResult is what zerops_lease returns. It never carries the lease
// string itself.
type leaseToolResult struct {
	Action string          `json:"action"`
	Leases []ops.LeaseView `json:"leases"`
	// UnreadPools are the pools a status of every pool could not read, each
	// with why; their leases are listed from zcp's records, unverified.
	UnreadPools []ops.LeasePoolUnread   `json:"unreadPools,omitempty"`
	NextActions string                  `json:"nextActions,omitempty"`
	Envelope    *workflow.StateEnvelope `json:"envelope,omitempty"`
}

// leaseRolloutToolResult is what zerops_lease action=rollout returns.
type leaseRolloutToolResult struct {
	Action   string                  `json:"action"`
	Rollout  ops.LeaseRolloutView    `json:"rollout"`
	Envelope *workflow.StateEnvelope `json:"envelope,omitempty"`
}

// RegisterLease registers the zerops_lease tool: acquire, status and release
// of a container of a Swarm pool, and rollout of the pool, through the pool's
// orchestrator, and sync of a lease's branch to the branch's other leases
// (DP-24). swarm
// builds the orchestrator client (tests inject a fake or a stub transport).
// mounter (container mode; nil elsewhere) gives a lease its ssh alias and
// mount; ssh runs git inside the leased container (nil: no checkout, no
// release guard and no sync). Every successful result carries the lifecycle envelope,
// computed after the operation so it lists the new lease set (spec-mate §1.3);
// rt is the runtime that envelope is computed for.
func RegisterLease(srv *mcp.Server, client platform.Client, projectID, stateDir string, swarm ops.SwarmFactory, mounter ops.Mounter, ssh ops.SSHDeployer, rt runtime.Info) {
	deps := leaseDeps(client, projectID, stateDir, swarm, mounter, ssh)
	if rt.MateEnabled {
		// Inside a Mate an acquire of an HQ repository puts the Mate
		// credential on the pool's GIT_TOKEN, as git-push-setup puts it on a
		// wired dev service (DP-16); outside one the secret stays the user's.
		deps.PoolCredential = ops.PoolHQCredential{Read: hqCredential}
	}
	// envelope is the result's lifecycle envelope; read are the views this
	// call read checkouts for, whose activity and unsaved work ride leases[]
	// (the envelope's own lease reader never reads a checkout).
	envelope := func(ctx context.Context, read ...ops.LeaseView) *workflow.StateEnvelope {
		var extra []workflow.EnvelopeOption
		if swarm != nil {
			extra = append(extra, workflow.WithLeaseReader(leaseReaderWith(client, projectID, stateDir, swarm, deps.Host, leaseStatusTimeout)))
		}
		if reads := leaseReads(read); len(reads) > 0 {
			extra = append(extra, workflow.WithLeaseActivity(reads))
		}
		return freshEnvelope(ctx, stateDir, client, projectID, rt, extra...)
	}
	mcp.AddTool(srv, &mcp.Tool{
		Name: "zerops_lease",
		Description: "Lease a Swarm pool (swarm@1) container for one branch. acquire takes a clean running container named <pool>-<number> with the branch checked out in /var/www; " +
			"status lists your leases; release resets and returns it, refusing on unpushed commits or uncommitted files unless force=true; " +
			"sync pushes a lease's commits and fast-forwards the branch's other leases; rollout replaces free outdated containers. In-project only.",
		InputSchema: leaseInputSchema(),
		Annotations: &mcp.ToolAnnotations{
			Title:           "Lease a pool container",
			DestructiveHint: boolPtr(true),
		},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input LeaseInput) (*mcp.CallToolResult, any, error) {
		if stateDir == "" {
			return convertError(platform.NewPlatformError(platform.ErrPrerequisiteMissing,
				"No state directory: leases are recorded under .zcp/state/leases", "Run zcp from the project's working directory")), nil, nil
		}
		if input.Pools != nil && input.Action != leaseActionAcquire {
			return convertError(platform.NewPlatformError(platform.ErrInvalidParameter,
				"pools is for acquire only", "Address one lease by its alias as lease, or one pool as pool")), nil, nil
		}
		switch input.Action {
		case leaseActionAcquire:
			pools := input.Pools
			switch {
			case input.Pool != "" && pools != nil:
				return convertError(platform.NewPlatformError(platform.ErrInvalidParameter,
					"acquire takes pool or pools, not both", "Pass one pool as pool, or every pool of the branch as pools")), nil, nil
			case input.Pool != "":
				pools = []string{input.Pool}
			case len(pools) == 0:
				return convertError(platform.NewPlatformError(platform.ErrInvalidParameter,
					"acquire needs pool", "Pass the pool service's hostname as pool, or several as pools")), nil, nil
			}
			views, err := ops.LeaseAcquireGroup(ctx, deps, pools, input.Repo, input.Branch)
			if err != nil {
				return convertError(withLeaseSSHStderr(err)), nil, nil
			}
			return jsonResult(leaseToolResult{
				Action: input.Action, Leases: views, NextActions: leaseAcquireNext(views), Envelope: envelope(ctx),
			}), nil, nil
		case leaseActionStatus:
			status, err := ops.LeaseStatus(ctx, deps, input.Pool)
			if err != nil {
				return convertError(err), nil, nil
			}
			views := status.Leases
			if views == nil {
				views = []ops.LeaseView{}
			}
			return jsonResult(leaseToolResult{Action: input.Action, Leases: views, UnreadPools: status.Unread, Envelope: envelope(ctx, views...)}), nil, nil
		case leaseActionRelease:
			force := false
			if f := input.Force.Ptr(); f != nil {
				force = *f
			}
			switch {
			case input.Lease != "" && input.Branch != "":
				return convertError(platform.NewPlatformError(platform.ErrInvalidParameter,
					"release takes lease or branch, not both", "Pass one lease by its alias as lease, or every lease of a branch as branch")), nil, nil
			case input.Branch != "":
				views, err := ops.LeaseReleaseGroup(ctx, deps, input.Repo, input.Branch, force)
				if err != nil {
					return convertError(withLeaseSSHStderr(err)), nil, nil
				}
				return jsonResult(leaseToolResult{
					Action: input.Action, Leases: views, NextActions: leaseReleaseBranchNext(views, input.Branch, input.Repo, force), Envelope: envelope(ctx),
				}), nil, nil
			case input.Lease == "":
				return convertError(platform.NewPlatformError(platform.ErrInvalidParameter,
					"release needs lease or branch", "Pass the lease alias (<pool>-<number>) as lease, or a branch as branch to release every lease of it; zerops_lease action=status lists them")), nil, nil
			}
			view, err := ops.LeaseRelease(ctx, deps, input.Lease, force)
			if err != nil {
				return convertError(withLeaseSSHStderr(err)), nil, nil
			}
			return jsonResult(leaseToolResult{Action: input.Action, Leases: []ops.LeaseView{view}, Envelope: envelope(ctx)}), nil, nil
		case leaseActionSync:
			if refusal := leaseSyncInputRefusal(input); refusal != nil {
				return convertError(refusal), nil, nil
			}
			view, err := ops.LeaseSync(ctx, deps, input.Lease)
			if err != nil {
				return convertError(withLeaseSSHStderr(err)), nil, nil
			}
			return jsonResult(leaseSyncToolResult{Action: input.Action, Sync: view, NextActions: leaseSyncNext(view), Envelope: envelope(ctx)}), nil, nil
		case leaseActionRollout:
			if input.Pool == "" {
				return convertError(platform.NewPlatformError(platform.ErrInvalidParameter,
					"rollout needs pool", "Pass the pool service's hostname as pool")), nil, nil
			}
			view, err := ops.LeaseRollout(ctx, deps, input.Pool)
			if err != nil {
				return convertError(err), nil, nil
			}
			return jsonResult(leaseRolloutToolResult{Action: input.Action, Rollout: view, Envelope: envelope(ctx)}), nil, nil
		default:
			return convertError(platform.NewPlatformError(platform.ErrInvalidParameter,
				fmt.Sprintf("Unknown action %q", input.Action), "action is acquire, status, release, rollout or sync")), nil, nil
		}
	})
}

// leaseAcquireNext is what an acquire's result tells the agent to do next:
// release each lease when done, and that a reused one is the lease already
// held for the branch, not a second container.
func leaseAcquireNext(views []ops.LeaseView) string {
	aliases := make([]string, 0, len(views))
	var reused []string
	for _, v := range views {
		aliases = append(aliases, v.Alias)
		if v.Reused {
			reused = append(reused, v.Alias)
		}
	}
	var next string
	if len(views) == 1 {
		next = fmt.Sprintf("The container is yours until you release it: zerops_lease action=release lease=%s.", views[0].Alias)
	} else {
		next = fmt.Sprintf("The containers are yours until you release them, each by its alias: zerops_lease action=release lease=<alias> for %s.", strings.Join(aliases, ", "))
	}
	if len(reused) > 0 {
		next += fmt.Sprintf(" %s was already held for this branch (reused): the same container, not a second one.", strings.Join(reused, ", "))
	}
	return next + " zerops_lease action=status shows every lease."
}

// leaseReleaseBranchNext is what a release of a branch tells the agent when
// a lease of it could not be released: which, and that the same call again
// releases what is left (a released lease has no record left); "" when every
// lease went.
func leaseReleaseBranchNext(views []ops.LeaseView, branch, repo string, force bool) string {
	var failed []string
	for _, v := range views {
		if v.Error != "" {
			failed = append(failed, v.Alias)
		}
	}
	if len(failed) == 0 {
		return ""
	}
	next := fmt.Sprintf("Not released, still held and recorded (each lease's error says why): %s.", strings.Join(failed, ", "))
	if len(failed) < len(views) {
		next += " Every other lease of the branch was released."
	}
	return next + " " + ops.LeaseReleaseBranchCall(branch, repo, force) + " again releases what is left of the branch."
}

// leaseReads is what a call read of each lease's checkout, keyed by alias, for
// the envelope (workflow.WithLeaseActivity): a view that carries no activity
// and no unsaved work was not read and is left out.
func leaseReads(views []ops.LeaseView) map[string]workflow.LeaseRead {
	reads := map[string]workflow.LeaseRead{}
	for _, v := range views {
		if v.Activity != nil || v.Unsaved != nil {
			reads[v.Alias] = workflow.LeaseRead{Activity: v.Activity, Unsaved: v.Unsaved}
		}
	}
	return reads
}

// withLeaseSSHStderr puts git's stderr from an ssh failure into the error's
// message (withSSHStderr: SSHExecError.Error() drops it). Errors that carry no
// ssh failure pass through.
func withLeaseSSHStderr(err error) error {
	var pe *platform.PlatformError
	var sshErr *platform.SSHExecError
	if !errors.As(err, &pe) || !errors.As(err, &sshErr) {
		return err
	}
	out := *pe
	out.Message = withSSHStderr(pe.Message, sshErr)
	return &out
}
