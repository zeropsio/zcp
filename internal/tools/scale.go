package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// ScaleInput is the input type for zerops_scale.
type ScaleInput struct {
	ServiceHostname string   `json:"serviceHostname"`
	CPUMode         *string  `json:"cpuMode,omitempty"`
	MinCPU          *int     `json:"minCpu,omitempty"`
	MaxCPU          *int     `json:"maxCpu,omitempty"`
	MinRAM          *float64 `json:"minRam,omitempty"`
	MaxRAM          *float64 `json:"maxRam,omitempty"`
	MinDisk         *float64 `json:"minDisk,omitempty"`
	MaxDisk         *float64 `json:"maxDisk,omitempty"`
	StartContainers *int     `json:"startContainers,omitempty"`
	MinContainers   *int     `json:"minContainers,omitempty"`
	MaxContainers   *int     `json:"maxContainers,omitempty"`
	StartCPU        *int     `json:"startCpu,omitempty"`
}

// RegisterScale registers the zerops_scale tool.
func RegisterScale(srv *mcp.Server, client platform.Client, projectID string) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_scale",
		Description: "Scale a service: adjust CPU, RAM, disk, and container autoscaling parameters.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Scale a service",
			IdempotentHint:  true,
			DestructiveHint: boolPtr(true),
		},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ScaleInput) (*mcp.CallToolResult, any, error) {
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
		return jsonResult(result), nil, nil
	})
}
