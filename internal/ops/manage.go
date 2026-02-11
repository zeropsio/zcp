package ops

import (
	"context"
	"fmt"

	"github.com/zeropsio/zcp/internal/platform"
)

// ScaleParams holds scaling configuration for a service.
type ScaleParams struct {
	CPUMode         string
	MinCPU          int
	MaxCPU          int
	MinRAM          float64
	MaxRAM          float64
	MinDisk         float64
	MaxDisk         float64
	StartContainers int
	MinContainers   int
	MaxContainers   int
}

// ScaleResult contains the result of a scale operation.
type ScaleResult struct {
	Process   *platform.Process `json:"process,omitempty"`
	Message   string            `json:"message,omitempty"`
	Hostname  string            `json:"serviceHostname"`
	ServiceID string            `json:"serviceId"`
}

// Start starts a stopped service.
func Start(ctx context.Context, client platform.Client, projectID, hostname string) (*platform.Process, error) {
	svc, err := resolveService(ctx, client, projectID, hostname)
	if err != nil {
		return nil, err
	}
	return client.StartService(ctx, svc.ID)
}

// Stop stops a running service.
func Stop(ctx context.Context, client platform.Client, projectID, hostname string) (*platform.Process, error) {
	svc, err := resolveService(ctx, client, projectID, hostname)
	if err != nil {
		return nil, err
	}
	return client.StopService(ctx, svc.ID)
}

// Restart restarts a running service.
func Restart(ctx context.Context, client platform.Client, projectID, hostname string) (*platform.Process, error) {
	svc, err := resolveService(ctx, client, projectID, hostname)
	if err != nil {
		return nil, err
	}
	return client.RestartService(ctx, svc.ID)
}

// Scale updates the autoscaling parameters for a service.
func Scale(ctx context.Context, client platform.Client, projectID, hostname string, params ScaleParams) (*ScaleResult, error) {
	if err := validateScaleParams(params); err != nil {
		return nil, err
	}

	svc, err := resolveService(ctx, client, projectID, hostname)
	if err != nil {
		return nil, err
	}

	apiParams := buildAutoscalingParams(params)
	proc, err := client.SetAutoscaling(ctx, svc.ID, apiParams)
	if err != nil {
		return nil, err
	}

	result := &ScaleResult{
		Process:   proc,
		Hostname:  svc.Name,
		ServiceID: svc.ID,
	}
	if proc == nil {
		result.Message = "Scaling parameters updated"
	}

	return result, nil
}

// resolveService is a convenience wrapper that fetches services and resolves hostname.
func resolveService(ctx context.Context, client platform.Client, projectID, hostname string) (*platform.ServiceStack, error) {
	services, err := client.ListServices(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return resolveServiceID(services, hostname)
}

func validateScaleParams(p ScaleParams) error {
	if !hasAnyScaleParam(p) {
		return platform.NewPlatformError(platform.ErrInvalidScaling,
			"At least one scaling parameter must be provided",
			"Provide cpuMode, minCpu/maxCpu, minRam/maxRam, minDisk/maxDisk, or minContainers/maxContainers")
	}

	if p.CPUMode != "" && p.CPUMode != "SHARED" && p.CPUMode != "DEDICATED" {
		return platform.NewPlatformError(platform.ErrInvalidScaling,
			fmt.Sprintf("Invalid cpuMode '%s'", p.CPUMode),
			"Use SHARED or DEDICATED")
	}

	if p.MinCPU > 0 && p.MaxCPU > 0 && p.MinCPU > p.MaxCPU {
		return platform.NewPlatformError(platform.ErrInvalidScaling,
			"minCpu must be <= maxCpu", "")
	}
	if p.MinRAM > 0 && p.MaxRAM > 0 && p.MinRAM > p.MaxRAM {
		return platform.NewPlatformError(platform.ErrInvalidScaling,
			"minRam must be <= maxRam", "")
	}
	if p.MinDisk > 0 && p.MaxDisk > 0 && p.MinDisk > p.MaxDisk {
		return platform.NewPlatformError(platform.ErrInvalidScaling,
			"minDisk must be <= maxDisk", "")
	}
	if p.MinContainers > 0 && p.MaxContainers > 0 && p.MinContainers > p.MaxContainers {
		return platform.NewPlatformError(platform.ErrInvalidScaling,
			"minContainers must be <= maxContainers", "")
	}

	return nil
}

func hasAnyScaleParam(p ScaleParams) bool {
	return p.CPUMode != "" || p.MinCPU != 0 || p.MaxCPU != 0 ||
		p.MinRAM != 0 || p.MaxRAM != 0 || p.MinDisk != 0 || p.MaxDisk != 0 ||
		p.StartContainers != 0 || p.MinContainers != 0 || p.MaxContainers != 0
}

func buildAutoscalingParams(p ScaleParams) platform.AutoscalingParams {
	params := platform.AutoscalingParams{}

	if p.CPUMode != "" {
		params.VerticalCPUMode = &p.CPUMode
	}
	if p.MinCPU != 0 {
		v := int32(p.MinCPU)
		params.VerticalMinCPU = &v
	}
	if p.MaxCPU != 0 {
		v := int32(p.MaxCPU)
		params.VerticalMaxCPU = &v
	}
	if p.MinRAM != 0 {
		params.VerticalMinRAM = &p.MinRAM
	}
	if p.MaxRAM != 0 {
		params.VerticalMaxRAM = &p.MaxRAM
	}
	if p.MinDisk != 0 {
		params.VerticalMinDisk = &p.MinDisk
	}
	if p.MaxDisk != 0 {
		params.VerticalMaxDisk = &p.MaxDisk
	}
	if p.StartContainers != 0 {
		v := int32(p.StartContainers)
		params.VerticalStartCPU = &v // mapped to start container count
	}
	if p.MinContainers != 0 {
		v := int32(p.MinContainers)
		params.HorizontalMinCount = &v
	}
	if p.MaxContainers != 0 {
		v := int32(p.MaxContainers)
		params.HorizontalMaxCount = &v
	}

	return params
}
