package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// ManageInput is the input type for zerops_manage.
type ManageInput struct {
	Action          string   `json:"action"`
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

// RegisterManage registers the zerops_manage tool.
func RegisterManage(srv *mcp.Server, client platform.Client, projectID string) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_manage",
		Description: "Manage service lifecycle: start, stop, restart, or scale a service.",
		Annotations: &mcp.ToolAnnotations{
			DestructiveHint: boolPtr(true),
		},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ManageInput) (*mcp.CallToolResult, any, error) {
		if input.Action == "" {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter, "Action is required",
				"Use start, stop, restart, or scale")), nil, nil
		}
		if input.ServiceHostname == "" {
			return convertError(platform.NewPlatformError(
				platform.ErrServiceRequired, "Service hostname is required",
				"Provide serviceHostname parameter")), nil, nil
		}

		switch input.Action {
		case "start":
			proc, err := ops.Start(ctx, client, projectID, input.ServiceHostname)
			if err != nil {
				return convertError(err), nil, nil
			}
			return jsonResult(proc), nil, nil
		case "stop":
			proc, err := ops.Stop(ctx, client, projectID, input.ServiceHostname)
			if err != nil {
				return convertError(err), nil, nil
			}
			return jsonResult(proc), nil, nil
		case "restart":
			proc, err := ops.Restart(ctx, client, projectID, input.ServiceHostname)
			if err != nil {
				return convertError(err), nil, nil
			}
			return jsonResult(proc), nil, nil
		case "scale":
			result, err := ops.Scale(ctx, client, projectID, input.ServiceHostname, buildScaleParams(input))
			if err != nil {
				return convertError(err), nil, nil
			}
			return jsonResult(result), nil, nil
		default:
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter, "Invalid action '"+input.Action+"'",
				"Use start, stop, restart, or scale")), nil, nil
		}
	})
}

func buildScaleParams(input ManageInput) ops.ScaleParams {
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
