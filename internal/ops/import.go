package ops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/zeropsio/zcp/internal/platform"
)

// ServiceImportError represents an error for a specific service during import.
// Meta carries server-sent field-level detail for this service's rejection
// (same shape as PlatformError.APIMeta — see internal/platform/errors.go).
// When non-nil the LLM reads apiMeta[].metadata for the failing fields
// instead of guessing from the generic Message.
type ServiceImportError struct {
	Service string                 `json:"service"`
	Code    string                 `json:"code"`
	Message string                 `json:"message"`
	Meta    []platform.APIMetaItem `json:"meta,omitempty"`
}

// ImportResult is returned after a successful API import.
type ImportResult struct {
	ProjectID     string                `json:"projectId"`
	ProjectName   string                `json:"projectName"`
	Processes     []ImportProcessOutput `json:"processes"`
	ServiceErrors []ServiceImportError  `json:"serviceErrors,omitempty"`
	Warnings      []string              `json:"warnings,omitempty"`
	Summary       string                `json:"summary,omitempty"`
	NextActions   string                `json:"nextActions,omitempty"`
}

// ImportProcessOutput represents one process from the import result.
type ImportProcessOutput struct {
	ProcessID  string  `json:"processId"`
	ActionName string  `json:"actionName"`
	Status     string  `json:"status"`
	Service    string  `json:"service"`
	ServiceID  string  `json:"serviceId"`
	FailReason *string `json:"failReason,omitempty"`
}

// Import imports services from YAML into a project.
// Input: content XOR filePath (not both, not neither).
//
// Validation split: the Zerops API is the authoritative validator for every
// platform concept (service types, modes, field names, cross-field rules,
// hostname format). ZCP's pre-flight does only the two things the API does
// NOT tell the LLM clearly:
//  1. `envVariables` at service-level silently drops — surfaced as warning.
//  2. A 'project:' section — rejected with a specific code instead of the
//     generic projectImport error, because the resolution ("remove it")
//     is unambiguous.
//
// Everything else — field names, hostname format, mode enums, type
// existence — the API catches with structured meta that PlatformError.APIMeta
// now propagates to the LLM (see plans/api-validation-plumbing.md).
//
// override: when true, sets `override: true` on every service so the API
// replaces existing service stacks instead of rejecting with
// serviceStackNameUnavailable.
func Import(
	ctx context.Context,
	client platform.Client,
	projectID string,
	content string,
	filePath string,
	override bool,
) (*ImportResult, error) {
	yamlContent, err := resolveInput(content, filePath)
	if err != nil {
		return nil, err
	}

	// Parse YAML into generic map for the two ZCP-specific preflights.
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(yamlContent), &doc); err != nil {
		return nil, platform.NewPlatformError(
			platform.ErrInvalidImportYml,
			fmt.Sprintf("invalid YAML: %v", err),
			"Check YAML syntax",
		)
	}

	// Check for project: key — K12 in the validation-plumbing plan. The
	// platform's projectImportInvalidParameter for this case is generic;
	// the specific code IMPORT_HAS_PROJECT is clearer.
	if _, ok := doc["project"]; ok {
		return nil, platform.NewPlatformError(
			platform.ErrImportHasProject,
			"import YAML must not contain a 'project:' section",
			"Remove the 'project:' section. Import works within an existing project.",
		)
	}

	// When override is requested, set `override: true` on each service and
	// re-marshal so the API replaces existing service stacks instead of
	// rejecting with serviceStackNameUnavailable.
	if override {
		if raw, ok := doc["services"].([]any); ok {
			for _, svc := range raw {
				if svcMap, ok := svc.(map[string]any); ok {
					svcMap["override"] = true
				}
			}
		}
		remarshaled, err := yaml.Marshal(doc)
		if err != nil {
			return nil, platform.NewPlatformError(
				platform.ErrInvalidImportYml,
				fmt.Sprintf("re-marshal after override injection: %v", err),
				"Report this as a zcp bug.",
			)
		}
		yamlContent = string(remarshaled)
	}

	// Sole retained client-side warning — K1 in the plan: the API accepts
	// service-level `envVariables:` then silently discards it, producing
	// neither an error nor a meta entry. ZCP is the only place this can
	// surface.
	var warnings []string
	if raw, ok := doc["services"]; ok {
		if servicesList, ok := raw.([]any); ok {
			for _, svc := range servicesList {
				svcMap, ok := svc.(map[string]any)
				if !ok {
					continue
				}
				if _, has := svcMap["envVariables"]; !has {
					continue
				}
				hostname, _ := svcMap["hostname"].(string)
				warnings = append(warnings, fmt.Sprintf(
					"service %q: 'envVariables' at service level is silently dropped by the API. Use 'envSecrets' for import-time secrets, or zerops.yaml run.envVariables for runtime config.",
					hostname,
				))
			}
		}
	}

	// Wait for any DELETING services with conflicting hostnames to finish.
	// Race prevention, not validation — API would reject the import with a
	// timing-dependent error and the LLM would have to retry anyway.
	hostnames := extractHostnames(doc)
	if err := waitForDeletingServices(ctx, client, projectID, hostnames); err != nil {
		return nil, err
	}

	result, err := client.ImportServices(ctx, projectID, yamlContent)
	if err != nil {
		return nil, err
	}

	var processes []ImportProcessOutput
	var serviceErrors []ServiceImportError
	for _, ss := range result.ServiceStacks {
		if ss.Error != nil {
			serviceErrors = append(serviceErrors, ServiceImportError{
				Service: ss.Name,
				Code:    ss.Error.Code,
				Message: ss.Error.Message,
				Meta:    ss.Error.Meta,
			})
		}
		for _, p := range ss.Processes {
			processes = append(processes, ImportProcessOutput{
				ProcessID:  p.ID,
				ActionName: p.ActionName,
				Status:     p.Status,
				Service:    ss.Name,
				ServiceID:  ss.ID,
				FailReason: p.FailReason,
			})
		}
	}

	return &ImportResult{
		ProjectID:     result.ProjectID,
		ProjectName:   result.ProjectName,
		Processes:     processes,
		ServiceErrors: serviceErrors,
		Warnings:      warnings,
	}, nil
}

// extractHostnames parses service hostnames from the parsed import YAML document.
func extractHostnames(doc map[string]any) []string {
	raw, ok := doc["services"]
	if !ok {
		return nil
	}
	servicesList, ok := raw.([]any)
	if !ok {
		return nil
	}
	var hostnames []string
	for _, svc := range servicesList {
		svcMap, ok := svc.(map[string]any)
		if !ok {
			continue
		}
		if h, ok := svcMap["hostname"].(string); ok && h != "" {
			hostnames = append(hostnames, h)
		}
	}
	return hostnames
}

// waitForDeletingServices polls ListServices until no DELETING services
// conflict with the requested hostnames. Returns ErrAPITimeout on context
// cancellation, deadline exceeded, or after a 5-minute hardcoded timeout.
func waitForDeletingServices(
	ctx context.Context,
	client platform.Client,
	projectID string,
	hostnames []string,
) error {
	if len(hostnames) == 0 {
		return nil
	}

	wantSet := make(map[string]bool, len(hostnames))
	for _, h := range hostnames {
		wantSet[h] = true
	}

	const (
		pollInterval = 3 * time.Second
		timeout      = 5 * time.Minute
	)
	start := time.Now()

	for {
		services, err := client.ListServices(ctx, projectID)
		if err != nil {
			return fmt.Errorf("list services for DELETING check: %w", err)
		}

		var conflicts []string
		for _, svc := range services {
			if svc.Status == "DELETING" && wantSet[svc.Name] {
				conflicts = append(conflicts, svc.Name)
			}
		}
		if len(conflicts) == 0 {
			return nil
		}

		if time.Since(start) > timeout {
			return platform.NewPlatformError(
				platform.ErrAPITimeout,
				fmt.Sprintf("timed out waiting for DELETING services after %s: %v", timeout, conflicts),
				"Services are still being deleted. Wait and retry, or use a different hostname.",
			)
		}

		select {
		case <-ctx.Done():
			return platform.NewPlatformError(
				platform.ErrAPITimeout,
				fmt.Sprintf("timed out waiting for DELETING services to finish: %v", conflicts),
				"Services are still being deleted. Wait and retry, or use a different hostname.",
			)
		case <-time.After(pollInterval):
			// Continue polling.
		}
	}
}

// resolveInput resolves content XOR filePath into YAML content string.
func resolveInput(content, filePath string) (string, error) {
	if content != "" && filePath != "" {
		return "", platform.NewPlatformError(
			platform.ErrInvalidUsage,
			"provide either content or filePath, not both",
			"Use content for inline YAML or filePath for a file",
		)
	}
	if content == "" && filePath == "" {
		return "", platform.NewPlatformError(
			platform.ErrInvalidUsage,
			"provide either content or filePath",
			"Use content for inline YAML or filePath for a file",
		)
	}
	if filePath != "" {
		data, err := os.ReadFile(filePath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return "", platform.NewPlatformError(
					platform.ErrFileNotFound,
					fmt.Sprintf("file not found: %s", filePath),
					"Check the file path",
				)
			}
			return "", platform.NewPlatformError(
				platform.ErrFileNotFound,
				fmt.Sprintf("read file: %v", err),
				"Check file permissions",
			)
		}
		return string(data), nil
	}
	return content, nil
}
