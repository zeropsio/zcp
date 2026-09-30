package ops

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

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
	return scalingFromStack(detail), nil
}

// scalingFromStack maps a service's resolved autoscaling onto the bundle's
// Scaling, nil when the service exposes none.
func scalingFromStack(detail *platform.ServiceStack) *bundle.Scaling {
	a := detail.CurrentAutoscaling
	if a == nil {
		a = detail.CustomAutoscaling
	}
	if a == nil {
		return nil
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
	}
}

// ServiceShape is what the group recipe reads of one live service, in one
// GetService: its scale, its profile, and the public repository its active
// version was built from.
type ServiceShape struct {
	// Scaling is nil when the service exposes no resolved autoscaling.
	Scaling *bundle.Scaling
	Profile string
	// PublicGitURL is the public repository an import's buildFromGit built
	// the active version from, "" for any other origin.
	PublicGitURL string
	// ExplicitSetup reports that the build named its zeropsSetup — which
	// the platform does not return — rather than defaulting to the hostname.
	ExplicitSetup bool
}

// FetchServiceShape reads a service's ServiceShape.
func FetchServiceShape(ctx context.Context, client platform.Client, serviceID string) (ServiceShape, error) {
	detail, err := client.GetService(ctx, serviceID)
	if err != nil {
		return ServiceShape{}, fmt.Errorf("fetch service shape: %w", err)
	}
	shape := ServiceShape{Scaling: scalingFromStack(detail), Profile: detail.Profile}
	if av := detail.ActiveAppVersion; av != nil && av.PublicGitSource != nil {
		shape.PublicGitURL = strings.TrimSpace(av.PublicGitSource.GitURL)
		shape.ExplicitSetup = av.PublicGitSourceExplicitSet != nil && *av.PublicGitSourceExplicitSet
	}
	return shape, nil
}

// ObjectStorageShape is how an object storage runs: its size and its access
// policy.
type ObjectStorageShape struct {
	// SizeGB is its quota; 0 when neither source names one.
	SizeGB int
	// Policy is one of the import's policy names. A custom policy's document
	// is never read: it can hold a secret condition, and nothing that reads
	// this shape may publish one.
	Policy string
	// PolicyUnread says why no policy was read, "" when one was.
	PolicyUnread string
}

// objectStoragePolicies are the policy names the import accepts.
var objectStoragePolicies = map[string]bool{
	"private": true, "public-read": true, "public-objects-read": true,
	"public-write": true, "public-read-write": true, "custom": true,
}

// FetchObjectStorageShape reads an object storage's size and access policy.
// The size is its quotaGBytes variable, else the size the platform's export
// of the service names; the policy is only in that export. The export is the
// platform's own and not scrubbed, so nothing but these fields is read from
// it — a custom policy by its name alone. An unreadable variable is an error
// — a tier that lands with the wrong size stays wrong — while a policy the
// export does not give is said in PolicyUnread, never guessed.
func FetchObjectStorageShape(ctx context.Context, client platform.Client, serviceID, hostname string) (ObjectStorageShape, error) {
	envs, err := client.GetServiceEnv(ctx, serviceID)
	if err != nil {
		return ObjectStorageShape{}, fmt.Errorf("read %s's variables: %w", hostname, err)
	}
	var shape ObjectStorageShape
	for _, env := range envs {
		if env.Key != "quotaGBytes" {
			continue
		}
		if size, convErr := strconv.Atoi(strings.TrimSpace(env.Content)); convErr == nil && size > 0 {
			shape.SizeGB = size
		}
	}

	exported, err := client.GetServiceStackExport(ctx, serviceID)
	if err != nil {
		shape.PolicyUnread = fmt.Sprintf("the platform's export of %s could not be read (%v)", hostname, err)
		return shape, nil
	}
	var doc struct {
		Services []struct {
			Hostname            string  `yaml:"hostname"`
			ObjectStorageSize   float64 `yaml:"objectStorageSize"`
			ObjectStoragePolicy string  `yaml:"objectStoragePolicy"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal([]byte(exported), &doc); err != nil {
		shape.PolicyUnread = fmt.Sprintf("the platform's export of %s does not parse (%v)", hostname, err)
		return shape, nil
	}
	for _, svc := range doc.Services {
		if svc.Hostname != hostname && len(doc.Services) > 1 {
			continue
		}
		if shape.SizeGB == 0 && svc.ObjectStorageSize > 0 {
			shape.SizeGB = int(svc.ObjectStorageSize)
		}
		policy := strings.TrimSpace(svc.ObjectStoragePolicy)
		switch {
		case policy == "":
			shape.PolicyUnread = fmt.Sprintf("the platform's export of %s names no access policy", hostname)
		case !objectStoragePolicies[policy]:
			shape.PolicyUnread = fmt.Sprintf("the platform's export of %s names a policy the import does not take (%q)", hostname, policy)
		default:
			shape.Policy = policy
		}
		return shape, nil
	}
	shape.PolicyUnread = fmt.Sprintf("the platform's export does not list %s", hostname)
	return shape, nil
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
