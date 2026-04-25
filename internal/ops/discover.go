package ops

import (
	"context"
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// DiscoverResult contains project and service information.
type DiscoverResult struct {
	Project  ProjectInfo   `json:"project"`
	Services []ServiceInfo `json:"services"`
	Notes    []string      `json:"notes,omitempty"`
	Warnings []string      `json:"warnings,omitempty"`
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
	Hostname         string           `json:"hostname"`
	ServiceID        string           `json:"serviceId"`
	Type             string           `json:"type"`
	Status           string           `json:"status"`
	Mode             string           `json:"mode,omitempty"`
	ManagedByZCP     bool             `json:"managedByZcp"`
	IsInfrastructure bool             `json:"isInfrastructure"`
	MountPath        string           `json:"mountPath,omitempty"`
	SubdomainEnabled bool             `json:"subdomainEnabled,omitempty"`
	SubdomainURL     string           `json:"subdomainUrl,omitempty"`
	Created          string           `json:"created,omitempty"`
	Containers       map[string]any   `json:"containers,omitempty"`
	Resources        map[string]any   `json:"resources,omitempty"`
	Ports            []map[string]any `json:"ports,omitempty"`
	Envs             []map[string]any `json:"envs,omitempty"`
}

// Discover fetches project and service information.
// When includeEnvs is true, env var keys and annotations are returned.
// When includeEnvValues is also true, actual env var values are included (for troubleshooting).
func Discover(
	ctx context.Context,
	client platform.Client,
	projectID string,
	hostname string,
	includeEnvs bool,
	includeEnvValues bool,
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
		var rawEnvs []platform.EnvVar
		if includeEnvs {
			rawEnvs = attachEnvs(ctx, client, &info, detail.ID, result, includeEnvValues)
		}
		if detail.SubdomainAccess {
			info.SubdomainURL = extractSubdomainURL(ctx, client, detail.ID, rawEnvs)
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
			attachEnvs(ctx, client, &info, services[i].ID, result, includeEnvValues)
		}
		result.Services = append(result.Services, info)
	}

	if includeEnvs {
		attachProjectEnvs(ctx, client, &result.Project, projectID, result, includeEnvValues)
	}

	addEnvRefNotes(result)
	return result, nil
}

func buildSummaryServiceInfo(svc *platform.ServiceStack) ServiceInfo {
	typeVersion := svc.ServiceStackTypeInfo.ServiceStackTypeVersionName
	// Mode (HA/NON_HA) is exposed only for service types where it is
	// load-bearing — managed databases, caches, search engines, messaging
	// brokers, shared-storage. For runtime services the platform accepts
	// the field but actual replica count is governed by
	// containers.minContainers/maxContainers, so surfacing mode here misled
	// agents into "HA on runtime = N replicas always running" reasoning
	// (see ops/discover.go contract in CLAUDE.md). Object-storage is
	// always internally replicated and exposes no mode semantic.
	mode := ""
	if workflow.ServiceSupportsMode(typeVersion) {
		mode = svc.Mode
	}
	return ServiceInfo{
		Hostname:         svc.Name,
		ServiceID:        svc.ID,
		Type:             typeVersion,
		Status:           svc.Status,
		Mode:             mode,
		IsInfrastructure: workflow.IsManagedService(typeVersion),
		SubdomainEnabled: svc.SubdomainAccess,
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
				"port":        p.Port,
				"protocol":    p.Protocol,
				"public":      p.Public,
				"httpSupport": p.HTTPSupport,
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
	if a.StartCPUCoreCount != 0 {
		m["startCpuCoreCount"] = a.StartCPUCoreCount
	}
	if a.MinFreeCPUCores != 0 {
		m["minFreeCpuCores"] = a.MinFreeCPUCores
	}
	if a.MinFreeCPUPercent != 0 {
		m["minFreeCpuPercent"] = a.MinFreeCPUPercent
	}
	if a.MinFreeRAMGB != 0 {
		m["minFreeRamGB"] = a.MinFreeRAMGB
	}
	if a.MinFreeRAMPercent != 0 {
		m["minFreeRamPercent"] = a.MinFreeRAMPercent
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

func attachProjectEnvs(ctx context.Context, client platform.Client, info *ProjectInfo, projectID string, result *DiscoverResult, includeValues bool) {
	envs, err := client.GetProjectEnv(ctx, projectID)
	if err != nil {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("Failed to fetch project env vars: %s", err.Error()))
		return
	}
	info.Envs = envVarsToMaps(envs, includeValues)
}

// attachEnvs fetches service env vars and converts them for JSON output.
// Returns raw envs for internal use (e.g. extractSubdomainURL).
func attachEnvs(ctx context.Context, client platform.Client, info *ServiceInfo, serviceID string, result *DiscoverResult, includeValues bool) []platform.EnvVar {
	envs, err := client.GetServiceEnv(ctx, serviceID)
	if err != nil {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("Failed to fetch env vars for %s: %s", info.Hostname, err.Error()))
		return nil
	}
	info.Envs = envVarsToMaps(envs, includeValues)
	return envs
}

// BuildSubdomainURL constructs a full subdomain URL for a service port.
// Pattern: https://{hostname}-{prefix}-{port}.{rest} (port 80 omits the port suffix).
// Returns "" if subdomainHost has no domain suffix (bare prefix like "1df2").
func BuildSubdomainURL(hostname, subdomainHost string, port int) string {
	prefix, rest, found := strings.Cut(subdomainHost, ".")
	if !found || rest == "" {
		return ""
	}
	if port == 80 {
		return fmt.Sprintf("https://%s-%s.%s", hostname, prefix, rest)
	}
	return fmt.Sprintf("https://%s-%s-%d.%s", hostname, prefix, port, rest)
}

// extractSubdomainURL reads the zeropsSubdomain env var for the URL.
// Checks already-fetched raw envs first (when includeEnvs=true), falls back to API call.
func extractSubdomainURL(ctx context.Context, client platform.Client, serviceID string, rawEnvs []platform.EnvVar) string {
	for _, env := range rawEnvs {
		if env.Key == envKeyZeropsSubdomain {
			return env.Content
		}
	}
	envs, err := client.GetServiceEnv(ctx, serviceID)
	if err != nil {
		return ""
	}
	for _, env := range envs {
		if env.Key == envKeyZeropsSubdomain {
			return env.Content
		}
	}
	return ""
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
