package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
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
	MinFreeRAMGB      *float64 `json:"minFreeRamGB,omitempty"      jsonschema:"Absolute free RAM threshold in GB. Scale-up triggers when free RAM drops below this. Whichever of minFreeRamGB or minFreeRamPercent provides MORE free memory is used. Default: 0.0625 (64 MB). Prevents OOM and preserves kernel disk cache."` //nolint:tagliatelle // matches Zerops import.yml naming
	MinFreeRAMPercent *float64 `json:"minFreeRamPercent,omitempty" jsonschema:"Free RAM threshold as percentage of granted RAM (0-100). Scales proportionally — e.g. 5%% of 12 GB = 600 MB buffer. Whichever of minFreeRamGB or minFreeRamPercent provides MORE free memory is used. Default: 0 (disabled)."`
	MinFreeCPUCores   *float64 `json:"minFreeCpuCores,omitempty"   jsonschema:"Free CPU threshold as fraction of one core (0.0-1.0). Value 0.2 means scale-up when less than 20%% of one core is free. DEDICATED CPU mode only — ignored in SHARED mode. Default: 0.1 (10%%)."`
	MinFreeCPUPercent *float64 `json:"minFreeCpuPercent,omitempty" jsonschema:"Free CPU threshold as percentage of total capacity across ALL cores (0-100). DEDICATED CPU mode only — ignored in SHARED mode. Default: 0 (disabled)."`
}

// RegisterScale registers the zerops_scale tool.
func RegisterScale(srv *mcp.Server, client platform.Client, projectID string) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_scale",
		Description: "Scale a service: adjust CPU, RAM, disk, and container autoscaling parameters. Blocks until completion (FINISHED/FAILED). Constraints: HA mode immutable after creation; Docker has no autoscaling; CPU mode changeable once/hour; managed services (DB/cache) support vertical only, container count fixed by mode (NON_HA=1, HA=3). Use zerops_knowledge query=\"scaling\" for detailed scaling mechanics and strategy presets.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Scale a service",
			IdempotentHint:  true,
			DestructiveHint: boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, input ScaleInput) (*mcp.CallToolResult, any, error) {
		if input.ServiceHostname == "" {
			return convertError(platform.NewPlatformError(
				platform.ErrServiceRequired, "Service hostname is required",
				"Provide serviceHostname parameter")), nil, nil
		}

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
		})
		if err != nil {
			return convertError(err), nil, nil
		}

		if result.Process != nil {
			onProgress := buildProgressCallback(ctx, req)
			result.Process, _ = pollManageProcess(ctx, client, result.Process, onProgress)
		}
		result.NextActions = nextActionScaleSuccess
		return jsonResult(result), nil, nil
	})
}
