package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

// MountInput is the input type for zerops_mount.
type MountInput struct {
	Action          string `json:"action"                    jsonschema:"Action to perform: mount, unmount, or status."`
	ServiceHostname string `json:"serviceHostname,omitempty" jsonschema:"Hostname of the service to mount/unmount. Required for mount and unmount actions."`
}

// RegisterMount registers the zerops_mount tool.
// MountOption configures RegisterMount.
type MountOption func(*mountConfig)

type mountConfig struct {
	swarm ops.SwarmFactory
	// vouch replaces the orchestrator's answer (tests).
	vouch leaseVouch
}

// WithPoolOrchestratorsForMount lets a mount of a lease ask the pool's
// orchestrator, through swarm, whether the lease's container is still the
// lease's before sshfs reaches it (docs/spec-workflows.md §8 DP-2). Without
// it a mount of a lease is refused: zcp cannot vouch for the container.
func WithPoolOrchestratorsForMount(swarm ops.SwarmFactory) MountOption {
	return func(c *mountConfig) { c.swarm = swarm }
}

func RegisterMount(srv *mcp.Server, client platform.Client, projectID string, mounter ops.Mounter, rtInfo runtime.Info, stateDir string, engine *workflow.Engine, recipeProbe RecipeSessionProbe, opts ...MountOption) {
	var cfg mountConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	vouch := cfg.vouch
	if vouch == nil {
		vouch = orchestratorVouch(client, projectID, stateDir, cfg.swarm)
	}
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_mount",
		Description: "Mount/unmount service filesystems via SSHFS. Actions: mount (requires active workflow — bootstrap or develop), unmount, status.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Mount/unmount service filesystems",
			IdempotentHint:  true,
			DestructiveHint: boolPtr(false),
		},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input MountInput) (*mcp.CallToolResult, any, error) {
		if mounter == nil {
			return convertError(platform.NewPlatformError(
				platform.ErrNotImplemented,
				"Mount is only available inside a Zerops container",
				"zerops_mount requires SSHFS and zsc (available in Zerops containers)",
			), WithRecoveryStatus()), nil, nil
		}
		if input.Action == "" {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter, "Action is required",
				"Use mount, unmount, or status"), WithRecoveryStatus()), nil, nil
		}

		switch input.Action {
		case "mount":
			// Guard: prevent self-mount when running inside a Zerops container.
			if rtInfo.InContainer && strings.EqualFold(input.ServiceHostname, rtInfo.ServiceName) {
				return convertError(platform.NewPlatformError(
					platform.ErrSelfServiceBlocked,
					fmt.Sprintf("Cannot mount %q — ZCP is running on this service", input.ServiceHostname),
					"Mounting a service into itself is not supported. Mount other services instead.",
				), WithRecoveryStatus()), nil, nil
			}
			// A lease's container is asked about first (DP-2): one that is no
			// longer the lease's — maybe another holder's — or that the
			// orchestrator does not answer about is never mounted. A parked
			// lease's container is stopped (DP-28): an sshfs mount would block
			// for about 42 s and fail, so the refusal naming the unpark comes
			// before any gate. A record that cannot be read leaves the mount
			// as it was before leases existed.
			switch lease, isLease, err := checkedLeaseOf(ctx, stateDir, vouch, input.ServiceHostname, "mounted"); {
			case err != nil:
				return convertError(err, WithRecoveryStatus()), nil, nil
			case isLease && lease.Parked:
				return convertError(ops.LeaseParkedError(lease, "zerops_mount"), WithRecoveryStatus()), nil, nil
			}
			if blocked := requireWorkflowContext(engine, stateDir, recipeProbe); blocked != nil {
				return blocked, nil, nil
			}
			result, err := ops.MountService(ctx, client, projectID, mounter, input.ServiceHostname)
			if err != nil {
				return convertError(err, WithRecoveryStatus()), nil, nil
			}
			_ = workflow.TouchWorkSession(stateDir)
			return jsonResult(mountResponse{
				MountResult: result,
				Envelope:    freshEnvelope(ctx, stateDir, client, projectID, rtInfo),
			}), nil, nil
		case "unmount":
			result, err := ops.UnmountService(ctx, client, projectID, mounter, input.ServiceHostname)
			if err != nil {
				return convertError(err, WithRecoveryStatus()), nil, nil
			}
			return jsonResult(mountResponse{
				MountResult: result,
				Envelope:    freshEnvelope(ctx, stateDir, client, projectID, rtInfo),
			}), nil, nil
		case actionStatus:
			result, err := ops.MountStatus(ctx, client, projectID, mounter, input.ServiceHostname)
			if err != nil {
				return convertError(err, WithRecoveryStatus()), nil, nil
			}
			return jsonResult(mountStatusResponse{
				MountStatusResult: result,
				Envelope:          freshEnvelope(ctx, stateDir, client, projectID, rtInfo),
			}), nil, nil
		default:
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter, "Invalid action '"+input.Action+"'",
				"Use mount, unmount, or status"), WithRecoveryStatus()), nil, nil
		}
	})
}

// mountResponse carries the mount/unmount result verbatim (embedded, so
// every existing field keeps its name and shape) plus the post-mutation
// lifecycle envelope. docs/spec-mate.md §1.3.
type mountResponse struct {
	*ops.MountResult
	// Envelope is the post-mutation lifecycle state (docs/spec-mate.md §1.3).
	// Absent when its computation failed — the rest of the response is
	// unaffected.
	Envelope *workflow.StateEnvelope `json:"envelope,omitempty"`
}

// mountStatusResponse is mountResponse's read-side twin.
type mountStatusResponse struct {
	*ops.MountStatusResult
	// Envelope is the post-mutation lifecycle state (docs/spec-mate.md §1.3).
	// Absent when its computation failed — the rest of the response is
	// unaffected.
	Envelope *workflow.StateEnvelope `json:"envelope,omitempty"`
}
