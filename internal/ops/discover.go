package ops

import (
	"context"

	"github.com/zeropsio/zcp/internal/platform"
)

// DiscoverResult contains project and service information.
type DiscoverResult struct {
	Project  ProjectInfo   `json:"project"`
	Services []ServiceInfo `json:"services"`
}

// ProjectInfo contains basic project information.
type ProjectInfo struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

// ServiceInfo contains service details for the discover response.
type ServiceInfo struct {
	Hostname   string           `json:"hostname"`
	ServiceID  string           `json:"serviceId"`
	Type       string           `json:"type"`
	Status     string           `json:"status"`
	Created    string           `json:"created,omitempty"`
	Containers map[string]any   `json:"containers,omitempty"`
	Resources  map[string]any   `json:"resources,omitempty"`
	Ports      []map[string]any `json:"ports,omitempty"`
	Envs       []map[string]any `json:"envs,omitempty"`
}

// Discover fetches project and service information.
func Discover(
	ctx context.Context,
	client platform.Client,
	projectID string,
	hostname string,
	includeEnvs bool,
) (*DiscoverResult, error) {
	proj, err := client.GetProject(ctx, projectID)
	if err != nil {
		return nil, err
	}

	services, err := client.ListServices(ctx, projectID)
	if err != nil {
		return nil, err
	}

	result := &DiscoverResult{
		Project: ProjectInfo{
			ID:     proj.ID,
			Name:   proj.Name,
			Status: proj.Status,
		},
	}

	if hostname != "" {
		svc, resolveErr := resolveServiceID(services, hostname)
		if resolveErr != nil {
			return nil, resolveErr
		}
		info := buildDetailedServiceInfo(svc)
		if includeEnvs {
			attachEnvs(ctx, client, &info, svc.ID)
		}
		result.Services = []ServiceInfo{info}
		return result, nil
	}

	result.Services = make([]ServiceInfo, len(services))
	for i := range services {
		result.Services[i] = buildSummaryServiceInfo(&services[i])
		if includeEnvs {
			attachEnvs(ctx, client, &result.Services[i], services[i].ID)
		}
	}

	return result, nil
}

func buildSummaryServiceInfo(svc *platform.ServiceStack) ServiceInfo {
	return ServiceInfo{
		Hostname:  svc.Name,
		ServiceID: svc.ID,
		Type:      svc.ServiceStackTypeInfo.ServiceStackTypeVersionName,
		Status:    svc.Status,
	}
}

func buildDetailedServiceInfo(svc *platform.ServiceStack) ServiceInfo {
	info := buildSummaryServiceInfo(svc)
	info.Created = svc.Created

	if svc.CustomAutoscaling != nil {
		a := svc.CustomAutoscaling
		info.Resources = map[string]any{
			"cpuMode": a.CPUMode,
			"minCpu":  a.MinCPU,
			"maxCpu":  a.MaxCPU,
			"minRam":  a.MinRAM,
			"maxRam":  a.MaxRAM,
			"minDisk": a.MinDisk,
			"maxDisk": a.MaxDisk,
		}
		info.Containers = map[string]any{
			"minContainers": a.HorizontalMinCount,
			"maxContainers": a.HorizontalMaxCount,
		}
	}

	if len(svc.Ports) > 0 {
		info.Ports = make([]map[string]any, len(svc.Ports))
		for i, p := range svc.Ports {
			info.Ports[i] = map[string]any{
				"port":     p.Port,
				"protocol": p.Protocol,
				"public":   p.Public,
			}
		}
	}

	return info
}

func attachEnvs(ctx context.Context, client platform.Client, info *ServiceInfo, serviceID string) {
	envs, err := client.GetServiceEnv(ctx, serviceID)
	if err != nil {
		return // silently ignore env fetch errors
	}
	info.Envs = make([]map[string]any, len(envs))
	for i, e := range envs {
		info.Envs[i] = map[string]any{
			"key":   e.Key,
			"value": e.Content,
		}
	}
}
