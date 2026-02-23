package ops

import (
	"context"
	"fmt"

	"github.com/zeropsio/zcp/internal/platform"
)

// ScaleParams holds scaling configuration for a service.
// Pointer fields distinguish "not set" (nil) from zero values.
type ScaleParams struct {
	CPUMode         *string
	MinCPU          *int
	MaxCPU          *int
	MinRAM          *float64
	MaxRAM          *float64
	MinDisk         *float64
	MaxDisk         *float64
	StartContainers *int
	MinContainers   *int
	MaxContainers   *int
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

// Reload reloads a running service. Faster than restart (~4s vs ~14s), sufficient for env var changes.
func Reload(ctx context.Context, client platform.Client, projectID, hostname string) (*platform.Process, error) {
	svc, err := resolveService(ctx, client, projectID, hostname)
	if err != nil {
		return nil, err
	}
	return client.ReloadService(ctx, svc.ID)
}

// ConnectStorage connects a shared-storage volume to a runtime service.
func ConnectStorage(ctx context.Context, client platform.Client, projectID, hostname, storageHostname string) (*platform.Process, error) {
	svc, err := resolveService(ctx, client, projectID, hostname)
	if err != nil {
		return nil, err
	}
	storage, err := resolveService(ctx, client, projectID, storageHostname)
	if err != nil {
		return nil, err
	}
	return client.ConnectSharedStorage(ctx, svc.ID, storage.ID)
}

// DisconnectStorage disconnects a shared-storage volume from a runtime service.
func DisconnectStorage(ctx context.Context, client platform.Client, projectID, hostname, storageHostname string) (*platform.Process, error) {
	svc, err := resolveService(ctx, client, projectID, hostname)
	if err != nil {
		return nil, err
	}
	storage, err := resolveService(ctx, client, projectID, storageHostname)
	if err != nil {
		return nil, err
	}
	return client.DisconnectSharedStorage(ctx, svc.ID, storage.ID)
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
	apiParams.ServiceMode = svc.Mode
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

	if p.CPUMode != nil && *p.CPUMode != "SHARED" && *p.CPUMode != "DEDICATED" {
		return platform.NewPlatformError(platform.ErrInvalidScaling,
			fmt.Sprintf("Invalid cpuMode '%s'", *p.CPUMode),
			"Use SHARED or DEDICATED")
	}

	if p.MinCPU != nil && p.MaxCPU != nil && *p.MinCPU > *p.MaxCPU {
		return platform.NewPlatformError(platform.ErrInvalidScaling,
			"minCpu must be <= maxCpu", "")
	}
	if p.MinRAM != nil && p.MaxRAM != nil && *p.MinRAM > *p.MaxRAM {
		return platform.NewPlatformError(platform.ErrInvalidScaling,
			"minRam must be <= maxRam", "")
	}
	if p.MinDisk != nil && p.MaxDisk != nil && *p.MinDisk > *p.MaxDisk {
		return platform.NewPlatformError(platform.ErrInvalidScaling,
			"minDisk must be <= maxDisk", "")
	}
	if p.MinContainers != nil && p.MaxContainers != nil && *p.MinContainers > *p.MaxContainers {
		return platform.NewPlatformError(platform.ErrInvalidScaling,
			"minContainers must be <= maxContainers", "")
	}

	return nil
}

func hasAnyScaleParam(p ScaleParams) bool {
	return p.CPUMode != nil || p.MinCPU != nil || p.MaxCPU != nil ||
		p.MinRAM != nil || p.MaxRAM != nil || p.MinDisk != nil || p.MaxDisk != nil ||
		p.StartContainers != nil || p.MinContainers != nil || p.MaxContainers != nil
}

func buildAutoscalingParams(p ScaleParams) platform.AutoscalingParams {
	params := platform.AutoscalingParams{}

	if p.CPUMode != nil {
		params.VerticalCPUMode = p.CPUMode
	}
	if p.MinCPU != nil {
		v := int32(*p.MinCPU)
		params.VerticalMinCPU = &v
	}
	if p.MaxCPU != nil {
		v := int32(*p.MaxCPU)
		params.VerticalMaxCPU = &v
	}
	if p.MinRAM != nil {
		params.VerticalMinRAM = p.MinRAM
	}
	if p.MaxRAM != nil {
		params.VerticalMaxRAM = p.MaxRAM
	}
	if p.MinDisk != nil {
		params.VerticalMinDisk = p.MinDisk
	}
	if p.MaxDisk != nil {
		params.VerticalMaxDisk = p.MaxDisk
	}
	if p.StartContainers != nil {
		v := int32(*p.StartContainers)
		params.VerticalStartCPU = &v // mapped to start container count
	}
	if p.MinContainers != nil {
		v := int32(*p.MinContainers)
		params.HorizontalMinCount = &v
	}
	if p.MaxContainers != nil {
		v := int32(*p.MaxContainers)
		params.HorizontalMaxCount = &v
	}

	return params
}
