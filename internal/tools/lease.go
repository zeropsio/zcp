package tools

import (
	"context"
	"errors"
	"fmt"

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
)

// LeaseInput is the input type for zerops_lease.
type LeaseInput struct {
	Action string    `json:"action"`
	Pool   string    `json:"pool,omitempty"`
	Branch string    `json:"branch,omitempty"`
	Repo   string    `json:"repo,omitempty"`
	Lease  string    `json:"lease,omitempty"`
	Force  *FlexBool `json:"force,omitempty"`
}

// leaseInputSchema is explicit because force is a FlexBool (an agent that
// stringifies booleans must not be bounced by the schema).
func leaseInputSchema() *jsonschema.Schema {
	return objectSchema(map[string]*jsonschema.Schema{
		"action": {Type: "string", Description: "acquire, status, release or rollout."},
		"pool":   {Type: "string", Description: "Pool service hostname. acquire, rollout: required. status: optional (default every pool)."},
		"branch": {Type: "string", Description: "acquire: branch to check out (default main); a new one is cut from main."},
		"repo":   {Type: "string", Description: "acquire: https clone URL; the first one on a pool becomes its default."},
		"lease":  {Type: "string", Description: "release: the lease alias <pool>-<number> from acquire or status (not a secret)."},
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
// orchestrator. swarm
// builds the orchestrator client (tests inject a fake or a stub transport).
// mounter (container mode; nil elsewhere) gives a lease its ssh alias and
// mount; ssh runs git inside the leased container (nil: no checkout and no
// release guard). Every successful result carries the lifecycle envelope,
// computed after the operation so it lists the new lease set (spec-mate §1.3);
// rt is the runtime that envelope is computed for.
func RegisterLease(srv *mcp.Server, client platform.Client, projectID, stateDir string, swarm ops.SwarmFactory, mounter ops.Mounter, ssh ops.SSHDeployer, rt runtime.Info) {
	deps := leaseDeps(client, projectID, stateDir, swarm, mounter, ssh)
	envelope := func(ctx context.Context) *workflow.StateEnvelope {
		var extra []workflow.EnvelopeOption
		if swarm != nil {
			extra = append(extra, workflow.WithLeaseReader(leaseReaderWith(client, projectID, stateDir, swarm, leaseStatusTimeout)))
		}
		return freshEnvelope(ctx, stateDir, client, projectID, rt, extra...)
	}
	mcp.AddTool(srv, &mcp.Tool{
		Name: "zerops_lease",
		Description: "Lease a Swarm pool (swarm@1) container for one branch. acquire takes a clean running container, names it <pool>-<number>, checks the branch out in /var/www; " +
			"status lists your leases; release resets and returns it, refusing on unpushed commits or uncommitted files unless force=true; " +
			"rollout replaces the pool's free outdated containers (leases keep their version). The orchestrator answers only inside the project.",
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
		switch input.Action {
		case leaseActionAcquire:
			if input.Pool == "" {
				return convertError(platform.NewPlatformError(platform.ErrInvalidParameter,
					"acquire needs pool", "Pass the pool service's hostname as pool")), nil, nil
			}
			view, err := ops.LeaseAcquire(ctx, deps, input.Pool, input.Repo, input.Branch)
			if err != nil {
				return convertError(withLeaseSSHStderr(err)), nil, nil
			}
			return jsonResult(leaseToolResult{
				Action: input.Action, Leases: []ops.LeaseView{view},
				NextActions: fmt.Sprintf("The container is yours until you release it: zerops_lease action=release lease=%s. zerops_lease action=status shows every lease.", view.Alias),
				Envelope:    envelope(ctx),
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
			return jsonResult(leaseToolResult{Action: input.Action, Leases: views, UnreadPools: status.Unread, Envelope: envelope(ctx)}), nil, nil
		case leaseActionRelease:
			if input.Lease == "" {
				return convertError(platform.NewPlatformError(platform.ErrInvalidParameter,
					"release needs lease", "Pass the lease alias (<pool>-<number>) as lease; zerops_lease action=status lists them")), nil, nil
			}
			force := false
			if f := input.Force.Ptr(); f != nil {
				force = *f
			}
			view, err := ops.LeaseRelease(ctx, deps, input.Lease, force)
			if err != nil {
				return convertError(withLeaseSSHStderr(err)), nil, nil
			}
			return jsonResult(leaseToolResult{Action: input.Action, Leases: []ops.LeaseView{view}, Envelope: envelope(ctx)}), nil, nil
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
				fmt.Sprintf("Unknown action %q", input.Action), "action is acquire, status, release or rollout")), nil, nil
		}
	})
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
