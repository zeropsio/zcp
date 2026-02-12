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
			DestructiveHint: boolPtr(true),
		},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ScaleInput) (*mcp.CallToolResult, any, error) {
		if input.ServiceHostname == "" {
			return convertError(platform.NewPlatformError(
				platform.ErrServiceRequired, "Service hostname is required",
				"Provide serviceHostname parameter")), nil, nil
		}

		result, err := ops.Scale(ctx, client, projectID, input.ServiceHostname, buildScaleParams(input))
		if err != nil {
			return convertError(err), nil, nil
		}
		return jsonResult(result), nil, nil
	})
}

func buildScaleParams(input ScaleInput) ops.ScaleParams {
	p := ops.ScaleParams{}
	if input.CPUMode != nil {
		p.CPUMode = *input.CPUMode
	}
	if input.MinCPU != nil {
		p.MinCPU = *input.MinCPU
	}
	if input.MaxCPU != nil {
		p.MaxCPU = *input.MaxCPU
	}
	if input.MinRAM != nil {
		p.MinRAM = *input.MinRAM
	}
	if input.MaxRAM != nil {
		p.MaxRAM = *input.MaxRAM
	}
	if input.MinDisk != nil {
		p.MinDisk = *input.MinDisk
	}
	if input.MaxDisk != nil {
		p.MaxDisk = *input.MaxDisk
	}
	if input.StartContainers != nil {
		p.StartContainers = *input.StartContainers
	}
	if input.MinContainers != nil {
		p.MinContainers = *input.MinContainers
	}
	if input.MaxContainers != nil {
		p.MaxContainers = *input.MaxContainers
	}
	return p
}
