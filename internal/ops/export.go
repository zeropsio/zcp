package ops

import (
	"context"
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/ops/bundle"
	"github.com/zeropsio/zcp/internal/platform"
)

// FetchServiceScaling reads the live autoscaling shape of a service and maps it
// to the bundle's platform-free Scaling projection (R7). Prefers
// CurrentAutoscaling (the active resolved config) over CustomAutoscaling (user
// overrides) — mirroring Discover. Returns nil (not an error) when the service
// exposes no resolved autoscaling, so the export composer emits a "scaling
// unread" warning rather than guessing. The single platform-read site for the
// export bundle's scaling, so caching/instrumentation land here.
func FetchServiceScaling(ctx context.Context, client platform.Client, serviceID string) (*bundle.Scaling, error) {
	detail, err := client.GetService(ctx, serviceID)
	if err != nil {
		return nil, fmt.Errorf("fetch service scaling: %w", err)
	}
	a := detail.CurrentAutoscaling
	if a == nil {
		a = detail.CustomAutoscaling
	}
	if a == nil {
		return nil, nil //nolint:nilnil // not-found sentinel: no resolved autoscaling → composer emits "scaling unread" warning
	}
	return &bundle.Scaling{
		MinContainers: int(a.HorizontalMinCount),
		MaxContainers: int(a.HorizontalMaxCount),
		MinCPU:        int(a.MinCPU),
		MaxCPU:        int(a.MaxCPU),
		MinRAM:        a.MinRAM,
		MaxRAM:        a.MaxRAM,
		MinDisk:       a.MinDisk,
		MaxDisk:       a.MaxDisk,
		CPUMode:       a.CPUMode,
	}, nil
}

// FetchServiceProfile reads the live scaling-tier profile (autoscalingProfileId)
// of a profile-bearing managed service (PostgreSQL/Valkey) from the FULL service
// stack — the lighter ListServices shape (EsServiceStack) does not carry it.
// Returns "" with no error when the service has no profile set. Export's identity
// snapshot (R7) uses it so a re-imported DB keeps its tier instead of reverting
// to the platform default.
func FetchServiceProfile(ctx context.Context, client platform.Client, serviceID string) (string, error) {
	detail, err := client.GetService(ctx, serviceID)
	if err != nil {
		return "", fmt.Errorf("fetch service profile: %w", err)
	}
	return detail.Profile, nil
}

// ExportResult contains the export output for a project.
type ExportResult struct {
	// ExportYAML is the raw YAML from the platform export API (re-importable).
	ExportYAML string `json:"exportYaml"`
	// Services lists each service with its discovered state.
	Services []ExportedService `json:"services"`
	// ProjectName is the project name from discovery.
	ProjectName string `json:"projectName"`
	// ProjectID is the project ID.
	ProjectID string `json:"projectId"`
	// Warnings collects non-fatal issues during export.
	Warnings []string `json:"warnings,omitempty"`
}

// ExportedService describes a service in the export result.
type ExportedService struct {
	Hostname         string `json:"hostname"`
	ServiceID        string `json:"serviceId"`
	Type             string `json:"type"`
	Status           string `json:"status"`
	Mode             string `json:"mode,omitempty"`
	IsInfrastructure bool   `json:"isInfrastructure"`
	SubdomainEnabled bool   `json:"subdomainEnabled,omitempty"`
}

// ExportProject retrieves the project export YAML and enriches it with
// discovered service metadata. The export YAML comes from the platform
// export API; discover fills in fields the export omits (mode, scaling
// ranges, ports, containers).
func ExportProject(
	ctx context.Context,
	client platform.Client,
	projectID string,
) (*ExportResult, error) {
	// Step 1: Get export YAML from platform API.
	exportYAML, err := client.GetProjectExport(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("export project: %w", err)
	}

	// Step 2: Discover services for metadata the export doesn't include.
	discoverResult, err := Discover(ctx, client, projectID, "", false, false, false)
	if err != nil {
		return nil, fmt.Errorf("discover for export: %w", err)
	}

	result := &ExportResult{
		ExportYAML:  exportYAML,
		ProjectName: discoverResult.Project.Name,
		ProjectID:   projectID,
	}

	// Step 3: Build service list from discovered data.
	result.Services = make([]ExportedService, 0, len(discoverResult.Services))
	for _, svc := range discoverResult.Services {
		result.Services = append(result.Services, ExportedService{
			Hostname:         svc.Hostname,
			ServiceID:        svc.ServiceID,
			Type:             svc.Type,
			Status:           svc.Status,
			Mode:             svc.Mode,
			IsInfrastructure: svc.IsInfrastructure,
			SubdomainEnabled: svc.SubdomainEnabled,
		})
	}

	result.Warnings = discoverResult.Warnings
	return result, nil
}

// ExportService retrieves the export YAML for a single service.
func ExportService(
	ctx context.Context,
	client platform.Client,
	projectID string,
	hostname string,
) (string, error) {
	services, err := client.ListServices(ctx, projectID)
	if err != nil {
		return "", fmt.Errorf("list services: %w", err)
	}
	svc, err := FindService(services, hostname)
	if err != nil {
		return "", err
	}
	yaml, err := client.GetServiceStackExport(ctx, svc.ID)
	if err != nil {
		return "", fmt.Errorf("export service %s: %w", hostname, err)
	}
	return yaml, nil
}

// RuntimeServices returns only the non-infrastructure services from an export result.
func (r *ExportResult) RuntimeServices() []ExportedService {
	var runtimes []ExportedService
	for _, svc := range r.Services {
		if !svc.IsInfrastructure {
			runtimes = append(runtimes, svc)
		}
	}
	return runtimes
}

// ManagedServices returns only the infrastructure services from an export result.
func (r *ExportResult) ManagedServices() []ExportedService {
	var managed []ExportedService
	for _, svc := range r.Services {
		if svc.IsInfrastructure {
			managed = append(managed, svc)
		}
	}
	return managed
}

// ServiceHostnames returns a comma-separated list of all service hostnames.
func (r *ExportResult) ServiceHostnames() string {
	names := make([]string, len(r.Services))
	for i, svc := range r.Services {
		names[i] = svc.Hostname
	}
	return strings.Join(names, ", ")
}
