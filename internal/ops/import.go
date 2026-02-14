package ops

import (
	"context"
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/platform"
)

// ImportResult is returned after a successful API import.
type ImportResult struct {
	ProjectID   string                `json:"projectId"`
	ProjectName string                `json:"projectName"`
	Processes   []ImportProcessOutput `json:"processes"`
	Warnings    []string              `json:"warnings,omitempty"`
}

// ImportProcessOutput represents one process from the import result.
type ImportProcessOutput struct {
	ProcessID  string `json:"processId"`
	ActionName string `json:"actionName"`
	Status     string `json:"status"`
	Service    string `json:"service"`
	ServiceID  string `json:"serviceId"`
}

// Import imports services from YAML into a project.
// Input: content XOR filePath (not both, not neither).
// Validates YAML structure and service types, then calls client.ImportServices.
// liveTypes: optional live service stack types for version/mode validation (nil = skip).
func Import(
	ctx context.Context,
	client platform.Client,
	projectID string,
	content string,
	filePath string,
	liveTypes []platform.ServiceStackType,
) (*ImportResult, error) {
	yamlContent, err := resolveInput(content, filePath)
	if err != nil {
		return nil, err
	}

	// Parse YAML into generic map for validation.
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(yamlContent), &doc); err != nil {
		return nil, platform.NewPlatformError(
			platform.ErrInvalidImportYml,
			fmt.Sprintf("invalid YAML: %v", err),
			"Check YAML syntax",
		)
	}

	// Check for project: key.
	if _, ok := doc["project"]; ok {
		return nil, platform.NewPlatformError(
			platform.ErrImportHasProject,
			"import YAML must not contain a 'project:' section",
			"Remove the 'project:' section. Import works within an existing project.",
		)
	}

	// Pre-flight validation: check service types against live data.
	var warnings []string
	if raw, ok := doc["services"]; ok {
		if servicesList, ok := raw.([]any); ok {
			services := make([]map[string]any, 0, len(servicesList))
			for _, svc := range servicesList {
				if svcMap, ok := svc.(map[string]any); ok {
					services = append(services, svcMap)
				}
			}
			warnings = knowledge.ValidateServiceTypes(services, liveTypes)
		}
	}

	result, err := client.ImportServices(ctx, projectID, yamlContent)
	if err != nil {
		return nil, err
	}

	var processes []ImportProcessOutput
	for _, ss := range result.ServiceStacks {
		for _, p := range ss.Processes {
			processes = append(processes, ImportProcessOutput{
				ProcessID:  p.ID,
				ActionName: p.ActionName,
				Status:     p.Status,
				Service:    ss.Name,
				ServiceID:  ss.ID,
			})
		}
	}

	return &ImportResult{
		ProjectID:   result.ProjectID,
		ProjectName: result.ProjectName,
		Processes:   processes,
		Warnings:    warnings,
	}, nil
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
