package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// ScaleInput is the input type for zerops_scale.
type ScaleInput struct {
	ServiceHostname string   `json:"serviceHostname"           jsonschema:"Hostname of the service to scale."`
	CPUMode         *string  `json:"cpuMode,omitempty"         jsonschema:"CPU scaling mode: SHARED or DEDICATED."`
	MinCPU          *int     `json:"minCpu,omitempty"          jsonschema:"Minimum CPU cores (autoscaling lower bound)."`
	MaxCPU          *int     `json:"maxCpu,omitempty"          jsonschema:"Maximum CPU cores (autoscaling upper bound)."`
	MinRAM          *float64 `json:"minRam,omitempty"          jsonschema:"Minimum RAM in GB (autoscaling lower bound)."`
	MaxRAM          *float64 `json:"maxRam,omitempty"          jsonschema:"Maximum RAM in GB (autoscaling upper bound)."`
	MinDisk         *float64 `json:"minDisk,omitempty"         jsonschema:"Minimum disk size in GB (autoscaling lower bound)."`
	MaxDisk         *float64 `json:"maxDisk,omitempty"         jsonschema:"Maximum disk size in GB (autoscaling upper bound)."`
	StartContainers *int     `json:"startContainers,omitempty" jsonschema:"Initial number of containers on service start."`
	MinContainers   *int     `json:"minContainers,omitempty"   jsonschema:"Minimum number of containers (autoscaling lower bound)."`
	MaxContainers   *int     `json:"maxContainers,omitempty"   jsonschema:"Maximum number of containers (autoscaling upper bound)."`
	StartCPU        *int     `json:"startCpu,omitempty"        jsonschema:"Initial CPU cores on service start."`
}

// RegisterScale registers the zerops_scale tool.
func RegisterScale(srv *mcp.Server, client platform.Client, projectID string) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_scale",
		Description: "Scale a service: adjust CPU, RAM, disk, and container autoscaling parameters. Blocks until the scaling process completes — returns final status (FINISHED/FAILED).",
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
			CPUMode:         input.CPUMode,
			MinCPU:          input.MinCPU,
			MaxCPU:          input.MaxCPU,
			MinRAM:          input.MinRAM,
			MaxRAM:          input.MaxRAM,
			MinDisk:         input.MinDisk,
			MaxDisk:         input.MaxDisk,
			StartContainers: input.StartContainers,
			MinContainers:   input.MinContainers,
			MaxContainers:   input.MaxContainers,
		})
		if err != nil {
			return convertError(err), nil, nil
		}

		if result.Process != nil {
			onProgress := buildProgressCallback(ctx, req)
			result.Process = pollManageProcess(ctx, client, result.Process, onProgress)
		}
		result.NextActions = nextActionScaleSuccess
		return jsonResult(result), nil, nil
	})
}
