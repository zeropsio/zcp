package tools

import (
	"context"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

const (
	leaseActionAcquire = "acquire"
	leaseActionStatus  = "status"
	leaseActionRelease = "release"
)

// LeaseInput is the input type for zerops_lease.
type LeaseInput struct {
	Action string    `json:"action"`
	Pool   string    `json:"pool,omitempty"`
	Branch string    `json:"branch,omitempty"`
	Lease  string    `json:"lease,omitempty"`
	Reset  *FlexBool `json:"reset,omitempty"`
}

// leaseInputSchema is explicit because reset is a FlexBool (an agent that
// stringifies booleans must not be bounced by the schema).
func leaseInputSchema() *jsonschema.Schema {
	return objectSchema(map[string]*jsonschema.Schema{
		"action": {Type: "string", Description: "acquire, status or release."},
		"pool":   {Type: "string", Description: "Pool service hostname. acquire: required. status: optional (default every pool)."},
		"branch": {Type: "string", Description: "acquire: the branch this container is for; recorded with the lease."},
		"lease":  {Type: "string", Description: "release: the lease alias <pool>-<number> from acquire or status. It names the lease; it is not a secret."},
		"reset":  flexBoolSchema("release: restore the clean snapshot first (default true; running processes end)."),
	}, "action")
}

// leaseToolResult is what zerops_lease returns. It never carries the lease
// string itself.
type leaseToolResult struct {
	Action      string          `json:"action"`
	Leases      []ops.LeaseView `json:"leases"`
	NextActions string          `json:"nextActions,omitempty"`
}

// RegisterLease registers the zerops_lease tool: acquire, status and release
// of a container of a Swarm pool, through the pool's orchestrator. swarm
// builds the orchestrator client (tests inject a fake or a stub transport).
func RegisterLease(srv *mcp.Server, client platform.Client, projectID, stateDir string, swarm ops.SwarmFactory) {
	deps := ops.LeaseDeps{Client: client, ProjectID: projectID, StateDir: stateDir, Swarm: swarm}
	mcp.AddTool(srv, &mcp.Tool{
		Name: "zerops_lease",
		Description: "Lease a container of a Swarm pool (swarm@1) for one branch. acquire takes a clean running container and names it <pool>-<number>; " +
			"status lists your leases and flags lost or unknown-holder ones; release returns it, restoring the clean snapshot unless reset=false " +
			"(running processes end). The pool's orchestrator answers only inside the project.",
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
			view, err := ops.LeaseAcquire(ctx, deps, input.Pool, input.Branch)
			if err != nil {
				return convertError(err), nil, nil
			}
			return jsonResult(leaseToolResult{
				Action: input.Action, Leases: []ops.LeaseView{view},
				NextActions: fmt.Sprintf("The container is yours until you release it: zerops_lease action=release lease=%s. zerops_lease action=status shows every lease.", view.Alias),
			}), nil, nil
		case leaseActionStatus:
			views, err := ops.LeaseList(ctx, deps, input.Pool)
			if err != nil {
				return convertError(err), nil, nil
			}
			if views == nil {
				views = []ops.LeaseView{}
			}
			return jsonResult(leaseToolResult{Action: input.Action, Leases: views}), nil, nil
		case leaseActionRelease:
			if input.Lease == "" {
				return convertError(platform.NewPlatformError(platform.ErrInvalidParameter,
					"release needs lease", "Pass the lease alias (<pool>-<number>) as lease; zerops_lease action=status lists them")), nil, nil
			}
			reset := true
			if r := input.Reset.Ptr(); r != nil {
				reset = *r
			}
			view, err := ops.LeaseRelease(ctx, deps, input.Lease, reset)
			if err != nil {
				return convertError(err), nil, nil
			}
			return jsonResult(leaseToolResult{Action: input.Action, Leases: []ops.LeaseView{view}}), nil, nil
		default:
			return convertError(platform.NewPlatformError(platform.ErrInvalidParameter,
				fmt.Sprintf("Unknown action %q", input.Action), "action is acquire, status or release")), nil, nil
		}
	})
}
