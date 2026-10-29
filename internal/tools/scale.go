package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/schema"
)

// ScaleInput is the input type for zerops_scale.
type ScaleInput struct {
	ServiceHostname   string   `json:"serviceHostname"             jsonschema:"Hostname of the service to scale."`
	CPUMode           *string  `json:"cpuMode,omitempty"           jsonschema:"CPU scaling mode: SHARED or DEDICATED."`
	MinCPU            *int     `json:"minCpu,omitempty"            jsonschema:"Minimum CPU cores (autoscaling lower bound)."`
	MaxCPU            *int     `json:"maxCpu,omitempty"            jsonschema:"Maximum CPU cores (autoscaling upper bound)."`
	StartCPU          *int     `json:"startCpu,omitempty"          jsonschema:"Initial CPU cores on service start."`
	MinRAM            *float64 `json:"minRam,omitempty"            jsonschema:"Minimum RAM in GB (autoscaling lower bound)."`
	MaxRAM            *float64 `json:"maxRam,omitempty"            jsonschema:"Maximum RAM in GB (autoscaling upper bound)."`
	MinDisk           *float64 `json:"minDisk,omitempty"           jsonschema:"Minimum disk size in GB (autoscaling lower bound)."`
	MaxDisk           *float64 `json:"maxDisk,omitempty"           jsonschema:"Maximum disk size in GB (autoscaling upper bound)."`
	MinContainers     *int     `json:"minContainers,omitempty"     jsonschema:"Minimum number of containers (autoscaling lower bound)."`
	MaxContainers     *int     `json:"maxContainers,omitempty"     jsonschema:"Maximum number of containers (autoscaling upper bound)."`
	MinFreeRAMGB      *float64 `json:"minFreeRamGB,omitempty"      jsonschema:"Absolute free RAM threshold in GB. Scale-up triggers when free RAM drops below this. Whichever of minFreeRamGB or minFreeRamPercent provides MORE free memory is used. Default: 0.0625 (64 MB). Prevents OOM and preserves kernel disk cache."` //nolint:tagliatelle // matches Zerops import.yaml naming
	MinFreeRAMPercent *float64 `json:"minFreeRamPercent,omitempty" jsonschema:"Free RAM threshold as percentage of granted RAM (0-100). Scales proportionally — e.g. 5%% of 12 GB = 600 MB buffer. Whichever of minFreeRamGB or minFreeRamPercent provides MORE free memory is used. Default: 0 (disabled)."`
	MinFreeCPUCores   *float64 `json:"minFreeCpuCores,omitempty"   jsonschema:"Free CPU threshold as fraction of one core (0.0-1.0). Value 0.2 means scale-up when less than 20%% of one core is free. DEDICATED CPU mode only — ignored in SHARED mode. Default: 0.1 (10%%)."`
	MinFreeCPUPercent *float64 `json:"minFreeCpuPercent,omitempty" jsonschema:"Free CPU threshold as percentage of total capacity across ALL cores (0-100). DEDICATED CPU mode only — ignored in SHARED mode. Default: 0 (disabled)."`
	// ConfirmDestructive acknowledges that lowering a pool's maxContainers
	// deletes the leased containers a first call named (diagnose-before-
	// destruct, the zerops_import override pattern).
	ConfirmDestructive *DestructiveAck `json:"confirmDestructive,omitempty" jsonschema:"Pool maxContainers lowering over a lease: echo wouldDestroy.operation and .targets from the DIAGNOSIS_REQUIRED refusal."`
}

// poolShrinkOperation names the destructive operation of lowering a pool's
// maxContainers over a lease (DiagnosedDestruction.Operation).
const poolShrinkOperation = "pool-shrink"

// ScaleToolOption configures RegisterScale.
type ScaleToolOption func(*scaleToolConfig)

type scaleToolConfig struct {
	stateDir string
	swarm    ops.SwarmFactory
}

// WithPoolOrchestratorsForScale lets zerops_scale ask a pool's orchestrator,
// through swarm, which containers lowering its maxContainers deletes, and
// tell zcp's own leases (records under stateDir) from other holders'.
// Without it zcp cannot check, and every lowering of a pool's maximum needs
// an acknowledgment.
func WithPoolOrchestratorsForScale(stateDir string, swarm ops.SwarmFactory) ScaleToolOption {
	return func(c *scaleToolConfig) { c.stateDir, c.swarm = stateDir, swarm }
}

// GroupRecipeSteer is what a tool that changed a service's scale asks once
// the change landed: the group recipe's steer for that host
// (groupRecipeScalingSteer), "" when there is nothing to say. Nil outside a
// Mate.
type GroupRecipeSteer func(ctx context.Context, host string) string

// NewGroupRecipeSteer is the steer for a Mate's container; nil anywhere else.
func NewGroupRecipeSteer(client platform.Client, httpClient ops.HTTPDoer, rt runtime.Info, stateDir string, schemas *schema.Cache) GroupRecipeSteer {
	if !rt.MateEnabled || httpClient == nil {
		return nil
	}
	return func(ctx context.Context, host string) string {
		return groupRecipeScalingSteer(ctx, client, httpClient, rt, stateDir, host, schemas)
	}
}

// scaleResponse is the scale result and, once it landed, the group recipe's
// steer for the host.
type scaleResponse struct {
	*ops.ScaleResult
	GroupRecipe string `json:"groupRecipe,omitempty"`
}

// RegisterScale registers the zerops_scale tool. steer, when set, is asked
// once a scale change landed (GroupRecipeSteer). Lowering a pool's
// maxContainers is checked against its orchestrator first, and a lowering
// that would delete a leased container — or that cannot be checked — needs
// confirmDestructive (spec-workflows §8 PS-7).
func RegisterScale(srv *mcp.Server, client platform.Client, projectID string, steer GroupRecipeSteer, opts ...ScaleToolOption) {
	var cfg scaleToolConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	poolDeps := ops.LeaseDeps{Client: client, ProjectID: projectID, StateDir: cfg.stateDir, Swarm: cfg.swarm}
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_scale",
		Description: "Scale a service: adjust CPU, RAM, disk, and container autoscaling (verticalAutoscaling only — does NOT set the PostgreSQL/Valkey `profile`/`profileOverrides`). Blocks until completion (FINISHED/FAILED). Constraints: deployment variant (`:single`/`:ha`) immutable; Docker has no autoscaling; CPU mode changeable once/hour; managed services support vertical only, container count fixed by variant (`:single`=1, `:ha`=3); pool counts (0-10) cap orchestrator leases; Box takes none. See zerops_knowledge query=\"scaling\".",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Scale a service",
			IdempotentHint:  true,
			DestructiveHint: boolPtr(false),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, input ScaleInput) (*mcp.CallToolResult, any, error) {
		if input.ServiceHostname == "" {
			return convertError(platform.NewPlatformError(
				platform.ErrServiceRequired, "Service hostname is required",
				"Provide serviceHostname parameter")), nil, nil
		}

		var refused *DiagnosedDestruction
		guard := ops.WithPoolShrinkGuard(poolDeps, func(plan ops.PoolShrink) error {
			expected := poolShrinkDestruction(plan, input)
			if err := ValidateDestructiveAck(input.ConfirmDestructive, expected); err != nil {
				refused = &expected
				return err
			}
			return nil
		})
		result, err := ops.Scale(ctx, client, projectID, input.ServiceHostname, ops.ScaleParams{
			CPUMode:           input.CPUMode,
			MinCPU:            input.MinCPU,
			MaxCPU:            input.MaxCPU,
			StartCPU:          input.StartCPU,
			MinRAM:            input.MinRAM,
			MaxRAM:            input.MaxRAM,
			MinDisk:           input.MinDisk,
			MaxDisk:           input.MaxDisk,
			MinContainers:     input.MinContainers,
			MaxContainers:     input.MaxContainers,
			MinFreeRAMGB:      input.MinFreeRAMGB,
			MinFreeRAMPercent: input.MinFreeRAMPercent,
			MinFreeCPUCores:   input.MinFreeCPUCores,
			MinFreeCPUPercent: input.MinFreeCPUPercent,
		}, guard)
		if err != nil {
			if refused != nil {
				return convertError(err, WithWouldDestroy(refused)), nil, nil
			}
			return convertError(err), nil, nil
		}

		if result.Process != nil {
			onProgress := buildProgressCallback(ctx, req)
			var timedOut bool
			result.Process, timedOut = pollManageProcess(ctx, client, result.Process, onProgress)
			result.TimedOut = timedOut
		}
		resp := scaleResponse{ScaleResult: result}
		if result.TimedOut {
			result.NextActions = "Scaling did not confirm within the poll window — verify with zerops_discover; the change may still be applying."
		} else {
			result.NextActions = nextActionScaleSuccess
			// Only a change that landed is read back and is the recipe's
			// business: a failed or canceled process changed nothing.
			if result.Process == nil || result.Process.Status == platform.ProcessStatusFinished {
				if err := ops.ReadBackScale(ctx, client, result); err != nil {
					result.Message = err.Error()
				}
			}
			if steer != nil && result.Process != nil && result.Process.Status == platform.ProcessStatusFinished {
				resp.GroupRecipe = steer(ctx, input.ServiceHostname)
			}
		}
		return jsonResult(resp), nil, nil
	})
}

// poolShrinkDestruction is what lowering a pool's maxContainers would erase,
// as the diagnose-before-destruct gate states it: the leased containers that
// go (each with whose lease it is), or — when the orchestrator could not be
// asked — the pool itself, since zcp cannot name them. Retry is the same call
// with the acknowledgment added.
func poolShrinkDestruction(plan ops.PoolShrink, input ScaleInput) DiagnosedDestruction {
	d := DiagnosedDestruction{Operation: poolShrinkOperation}
	if plan.Unread != "" {
		d.Targets = []string{plan.Pool}
		d.Diagnoses = []TargetDiagnosis{{Hostname: plan.Pool, Cause: "zcp cannot check which containers go: " + plan.Unread}}
		d.Then = "zerops_lease action=status lists the pool's leases"
	} else {
		var yours []string
		for _, c := range plan.LeasedDeletes() {
			d.Targets = append(d.Targets, c.Alias)
			d.Diagnoses = append(d.Diagnoses, TargetDiagnosis{Hostname: c.Alias, Cause: c.Whose()})
			if c.Yours {
				yours = append(yours, "zerops_lease action=release lease="+c.Alias)
			}
		}
		keep := fmt.Sprintf("keep maxContainers %d or higher, which deletes no lease", plan.LowestSafe)
		if len(yours) > 0 {
			d.Then = fmt.Sprintf("Release the leases of yours you no longer need first (%s; a release refuses while a lease holds unpushed work), or %s", strings.Join(yours, ", "), keep)
		} else {
			d.Then = "Only a lease's holder can release it; " + keep
		}
	}
	for _, c := range plan.Deletes {
		d.Loss.Containers = append(d.Loss.Containers, c.Alias)
	}
	args := map[string]any{}
	if raw, err := json.Marshal(input); err == nil {
		_ = json.Unmarshal(raw, &args)
	}
	args["confirmDestructive"] = map[string]any{"operation": poolShrinkOperation, "acknowledgedTargets": append([]string{}, d.Targets...)}
	d.Retry = &RetryCall{Tool: "zerops_scale", Args: args}
	return d
}
