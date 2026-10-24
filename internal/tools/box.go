package tools

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

const (
	boxActionSetup  = "setup"
	boxActionStatus = "status"
)

// importCreateAction is the import process that creates a service; a Box's
// setup runs once it FINISHED (a capacity rollback ends it FAILED).
const importCreateAction = "stack.create"

// BoxInput is the input type for zerops_box.
type BoxInput struct {
	Action          string `json:"action"                    jsonschema:"setup: finish the Box's setup (idempotent; waits for a pull still running). status: read the orchestrator, bases, domains and subdomain state; changes nothing."`
	ServiceHostname string `json:"serviceHostname,omitempty" jsonschema:"Hostname of the Zerops Box (ubuntu/zbox@1) service. Required."`
}

// boxToolResult is what zerops_box returns: the action and its block. It
// never carries an access token or the workspace key.
type boxToolResult struct {
	Action    string               `json:"action"`
	BoxSetup  *ops.BoxSetupResult  `json:"boxSetup,omitempty"`
	BoxStatus *ops.BoxStatusResult `json:"boxStatus,omitempty"`
}

// boxAction runs one action of zerops_box on the Box hostname.
type boxAction func(ctx context.Context, deps ops.BoxDeps, hostname string) (boxToolResult, error)

// boxActions is zerops_box's dispatch: one entry per action, so a later
// action (a sandbox's) is one more entry.
func boxActions() map[string]boxAction {
	return map[string]boxAction{
		boxActionSetup: func(ctx context.Context, deps ops.BoxDeps, hostname string) (boxToolResult, error) {
			r, err := ops.BoxSetup(ctx, deps, hostname)
			if err != nil {
				return boxToolResult{}, fmt.Errorf("box setup: %w", err)
			}
			return boxToolResult{BoxSetup: &r}, nil
		},
		boxActionStatus: func(ctx context.Context, deps ops.BoxDeps, hostname string) (boxToolResult, error) {
			r, err := ops.BoxStatus(ctx, deps, hostname)
			if err != nil {
				return boxToolResult{}, fmt.Errorf("box status: %w", err)
			}
			return boxToolResult{BoxStatus: &r}, nil
		},
	}
}

// RegisterBox registers zerops_box: setup and status of a Zerops Box's
// workspace (docs/spec-workflows.md §8 PS-11..PS-14), reached through zproxy
// with zcp's own Zerops token; box builds the workspace API client.
func RegisterBox(srv *mcp.Server, client platform.Client, projectID string, box ops.BoxFactory) {
	registerBox(srv, ops.BoxDeps{Client: client, ProjectID: projectID, Box: box})
}

// registerBox is RegisterBox over deps (tests set its budgets).
func registerBox(srv *mcp.Server, deps ops.BoxDeps) {
	actions := boxActions()
	names := make([]string, 0, len(actions))
	for name := range actions {
		names = append(names, name)
	}
	slices.Sort(names)
	mcp.AddTool(srv, &mcp.Tool{
		Name: "zerops_box",
		Description: "Finish or read the setup of a Zerops Box (ubuntu/zbox@1). zerops_import already sets up a Box it creates; " +
			"action=setup finishes one later (a pull still running, subdomain access enabled afterwards): it pulls the default sandbox base only when none is READY " +
			"and adds the 8081 sandbox domain only when missing. action=status changes nothing.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Set up a Zerops Box",
			IdempotentHint:  true,
			DestructiveHint: boolPtr(false),
		},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input BoxInput) (*mcp.CallToolResult, any, error) {
		run, ok := actions[input.Action]
		if !ok {
			return convertError(platform.NewPlatformError(platform.ErrInvalidParameter,
				fmt.Sprintf("Unknown action %q", input.Action),
				"zerops_box takes action="+strings.Join(names, " or "))), nil, nil
		}
		if strings.TrimSpace(input.ServiceHostname) == "" {
			return convertError(platform.NewPlatformError(platform.ErrInvalidParameter,
				"serviceHostname is required", "Pass the Box's hostname as serviceHostname; zerops_discover lists the project's services")), nil, nil
		}
		if deps.Box == nil {
			return convertError(platform.NewPlatformError(platform.ErrNotImplemented,
				"This zcp has no Box client", "Run zcp as an MCP server (zcp serve)")), nil, nil
		}
		res, err := run(ctx, deps, input.ServiceHostname)
		if err != nil {
			return convertError(err), nil, nil
		}
		res.Action = input.Action
		return jsonResult(res), nil, nil
	})
}

// ImportOption configures zerops_import.
type ImportOption func(*importConfig)

// importConfig is what the options set.
type importConfig struct {
	// box finishes the setup of each Zerops Box an import creates; nil skips
	// it.
	box *ops.BoxDeps
}

// WithBoxSetup makes zerops_import finish the setup of every Zerops Box it
// creates before it returns (docs/spec-workflows.md §8 PS-11); box builds the
// workspace API client.
func WithBoxSetup(box ops.BoxFactory) ImportOption {
	return func(c *importConfig) { c.box = &ops.BoxDeps{Box: box} }
}

// withBoxSetupDeps is WithBoxSetup over whole deps (tests set its budgets).
func withBoxSetupDeps(deps ops.BoxDeps) ImportOption {
	return func(c *importConfig) { c.box = &deps }
}

// boxSetupAfterImport finishes the setup of every Zerops Box the import
// created — each Box of the YAML whose stack.create FINISHED — side by side,
// and answers their blocks in import order. A Box whose create failed (a
// capacity rollback) gets none: its process's FAILED reason says it. A setup
// that fails is named in its block and never fails the import.
func boxSetupAfterImport(ctx context.Context, deps ops.BoxDeps, input ImportInput, result *ops.ImportResult) []ops.BoxSetupResult {
	boxes, err := ops.ImportedBoxServices(input.Content, input.FilePath)
	if err != nil || len(boxes) == 0 {
		return nil
	}
	type created struct{ hostname, serviceID string }
	var targets []created
	for _, p := range result.Processes {
		if p.ActionName != importCreateAction || p.Status != statusFinished || p.ServiceID == "" || !slices.Contains(boxes, p.Service) {
			continue
		}
		if !slices.ContainsFunc(targets, func(c created) bool { return c.serviceID == p.ServiceID }) {
			targets = append(targets, created{p.Service, p.ServiceID})
		}
	}
	blocks := make([]*ops.BoxSetupResult, len(targets))
	var wg sync.WaitGroup
	for i, c := range targets {
		wg.Go(func() { blocks[i] = ops.BoxSetupImported(ctx, deps, c.hostname, c.serviceID) })
	}
	wg.Wait()
	var out []ops.BoxSetupResult
	for _, b := range blocks {
		if b != nil {
			out = append(out, *b)
		}
	}
	return out
}

// refuseBoxOutsideItsRegion refuses an import declaring a Zerops Box into a
// project whose region cannot hold one, before anything is created — a Box
// exists only in a project in prg1 (docs/spec-workflows.md §8 PS-16). The
// project is read only when the YAML declares a Box; a document that does
// not read is left to the import's own error.
func refuseBoxOutsideItsRegion(ctx context.Context, client platform.Client, projectID string, input ImportInput) *mcp.CallToolResult {
	boxes, err := ops.ImportedBoxServices(input.Content, input.FilePath)
	if err != nil {
		return nil
	}
	if pe := ops.BoxRegionRefusal(ctx, client, projectID, boxes, "Nothing was imported."); pe != nil {
		return convertError(pe)
	}
	return nil
}

// explicitPlanPreflight runs the reads an explicit bootstrap plan passes
// before the engine validates it: the repository's parts
// (repoPartsPlanError), then the region of every Box it would create
// (boxRegionPlanError). The first refusal wins.
func explicitPlanPreflight(ctx context.Context, client platform.Client, projectID string, plan []workflow.BootstrapTarget, ssh ops.SSHDeployer, rt runtime.Info) *platform.PlatformError {
	if pe := repoPartsPlanError(ctx, client, projectID, plan, ssh, rt); pe != nil {
		return pe
	}
	return boxRegionPlanError(ctx, client, projectID, plan)
}

// boxRegionPlanError refuses a bootstrap plan that would create a Zerops Box
// — a dependency of a Box type with resolution CREATE — in a project whose
// region cannot hold one, before the plan is taken (docs/spec-workflows.md
// §8 PS-16). A Box the plan only names (EXISTS, SHARED) is never checked: it
// exists already.
func boxRegionPlanError(ctx context.Context, client platform.Client, projectID string, plan []workflow.BootstrapTarget) *platform.PlatformError {
	var boxes []string
	for _, target := range plan {
		for _, dep := range target.Dependencies {
			if topology.IsSandboxType(dep.Type) && strings.EqualFold(strings.TrimSpace(dep.Resolution), workflow.ResolutionCreate) &&
				!slices.Contains(boxes, dep.Hostname) {
				boxes = append(boxes, dep.Hostname)
			}
		}
	}
	return ops.BoxRegionRefusal(ctx, client, projectID, boxes, "The plan was not taken.")
}
