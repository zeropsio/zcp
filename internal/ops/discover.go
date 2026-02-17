package ops

import (
	"context"

	"github.com/zeropsio/zcp/internal/platform"
)

// DiscoverResult contains project and service information.
type DiscoverResult struct {
	Project  ProjectInfo   `json:"project"`
	Services []ServiceInfo `json:"services"`
	Notes    []string      `json:"notes,omitempty"`
}

// ProjectInfo contains basic project information.
type ProjectInfo struct {
	ID     string           `json:"id"`
	Name   string           `json:"name"`
	Status string           `json:"status"`
	Envs   []map[string]any `json:"envs,omitempty"`
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
		// Fetch full detail (includes CurrentAutoscaling with active config).
		detail, getErr := client.GetService(ctx, svc.ID)
		if getErr != nil {
			return nil, getErr
		}
		info := buildDetailedServiceInfo(detail)
		if includeEnvs {
			attachEnvs(ctx, client, &info, detail.ID)
		}
		result.Services = []ServiceInfo{info}
		addEnvRefNotes(result)
		return result, nil
	}

	result.Services = make([]ServiceInfo, 0, len(services))
	for i := range services {
		if services[i].IsSystem() {
			continue
		}
		info := buildSummaryServiceInfo(&services[i])
		if includeEnvs {
			attachEnvs(ctx, client, &info, services[i].ID)
		}
		result.Services = append(result.Services, info)
	}

	if includeEnvs {
		attachProjectEnvs(ctx, client, &result.Project, projectID)
	}

	addEnvRefNotes(result)
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

	// Prefer CurrentAutoscaling (active config) over CustomAutoscaling (user overrides).
	a := svc.CurrentAutoscaling
	if a == nil {
		a = svc.CustomAutoscaling
	}
	if a != nil {
		info.Resources = buildResourcesMap(a)
		info.Containers = buildContainersMap(a)
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

// buildResourcesMap creates a resources map from autoscaling, omitting zero/empty values.
func buildResourcesMap(a *platform.CustomAutoscaling) map[string]any {
	m := make(map[string]any)
	if a.CPUMode != "" {
		m["cpuMode"] = a.CPUMode
	}
	if a.MinCPU != 0 {
		m["minCpu"] = a.MinCPU
	}
	if a.MaxCPU != 0 {
		m["maxCpu"] = a.MaxCPU
	}
	if a.MinRAM != 0 {
		m["minRam"] = a.MinRAM
	}
	if a.MaxRAM != 0 {
		m["maxRam"] = a.MaxRAM
	}
	if a.MinDisk != 0 {
		m["minDisk"] = a.MinDisk
	}
	if a.MaxDisk != 0 {
		m["maxDisk"] = a.MaxDisk
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

// buildContainersMap creates a containers map from autoscaling, omitting zero values.
func buildContainersMap(a *platform.CustomAutoscaling) map[string]any {
	m := make(map[string]any)
	if a.HorizontalMinCount != 0 {
		m["minContainers"] = a.HorizontalMinCount
	}
	if a.HorizontalMaxCount != 0 {
		m["maxContainers"] = a.HorizontalMaxCount
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

func attachProjectEnvs(ctx context.Context, client platform.Client, info *ProjectInfo, projectID string) {
	envs, err := client.GetProjectEnv(ctx, projectID)
	if err != nil {
		return // silently ignore project env fetch errors
	}
	info.Envs = envVarsToMaps(envs)
}

func attachEnvs(ctx context.Context, client platform.Client, info *ServiceInfo, serviceID string) {
	envs, err := client.GetServiceEnv(ctx, serviceID)
	if err != nil {
		return // silently ignore env fetch errors
	}
	info.Envs = envVarsToMaps(envs)
}

// addEnvRefNotes appends an explanatory note if any service env contains cross-service references.
func addEnvRefNotes(result *DiscoverResult) {
	for _, svc := range result.Services {
		for _, env := range svc.Envs {
			if env["isReference"] == true {
				result.Notes = append(result.Notes,
					"Values showing ${...} are cross-service references — resolved inside the running container, not in the API. Do not restart to resolve them.")
				return
			}
		}
	}
}
