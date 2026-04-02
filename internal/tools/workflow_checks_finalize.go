package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/zeropsio/zcp/internal/workflow"
)

// checkRecipeFinalize validates that all recipe repo files are generated and correct.
func checkRecipeFinalize(outputDir string) workflow.RecipeStepChecker {
	return func(_ context.Context, plan *workflow.RecipePlan, _ *workflow.RecipeState) (*workflow.StepCheckResult, error) {
		if plan == nil {
			return nil, nil
		}

		dir := outputDir
		if dir == "" {
			return nil, fmt.Errorf("output directory not set")
		}

		var checks []workflow.StepCheck

		// Check main README exists.
		checks = append(checks, checkFileExists(dir, "README.md")...)

		// Check per-environment files.
		for i := 0; i < workflow.EnvTierCount(); i++ {
			folder := workflow.EnvFolder(i)
			checks = append(checks, checkFileExists(dir, filepath.Join(folder, "import.yaml"))...)
			checks = append(checks, checkFileExists(dir, filepath.Join(folder, "README.md"))...)
		}

		// Validate import.yaml files.
		for i := 0; i < workflow.EnvTierCount(); i++ {
			folder := workflow.EnvFolder(i)
			importPath := filepath.Join(dir, folder, "import.yaml")
			data, err := os.ReadFile(importPath)
			if err != nil {
				continue // file existence already checked above
			}
			checks = append(checks, validateImportYAML(string(data), plan, i, folder)...)
		}

		allPassed := true
		for i := range checks {
			if checks[i].Status == statusFail {
				allPassed = false
				break
			}
		}
		summary := "finalize checks passed"
		if !allPassed {
			summary = "finalize checks failed"
		}
		return &workflow.StepCheckResult{
			Passed: allPassed, Checks: checks, Summary: summary,
		}, nil
	}
}

// checkFileExists returns a pass/fail check for file existence.
func checkFileExists(baseDir, relPath string) []workflow.StepCheck {
	fullPath := filepath.Join(baseDir, relPath)
	if _, err := os.Stat(fullPath); err != nil {
		return []workflow.StepCheck{{
			Name:   "file_" + strings.ReplaceAll(relPath, "/", "_"),
			Status: statusFail,
			Detail: fmt.Sprintf("file not found: %s", relPath),
		}}
	}
	return []workflow.StepCheck{{
		Name:   "file_" + strings.ReplaceAll(relPath, "/", "_"),
		Status: statusPass,
	}}
}

// importYAMLDoc is a minimal struct for validating import.yaml structure.
type importYAMLDoc struct {
	Project struct {
		Name string `yaml:"name"`
	} `yaml:"project"`
	Services []importService `yaml:"services"`
}

type importService struct {
	Hostname            string                   `yaml:"hostname"`
	Type                string                   `yaml:"type"`
	Priority            *int                     `yaml:"priority,omitempty"`
	Mode                string                   `yaml:"mode,omitempty"`
	EnableSubdomain     *bool                    `yaml:"enableSubdomainAccess,omitempty"`
	MinContainers       *int                     `yaml:"minContainers,omitempty"`
	VerticalAutoscaling *importVerticalAutoscale `yaml:"verticalAutoscaling,omitempty"`
}

type importVerticalAutoscale struct {
	MinFreeRAMGB *float64 `yaml:"minFreeRamGB,omitempty"` //nolint:tagliatelle // Zerops API field name
	CPUMode      string   `yaml:"cpuMode,omitempty"`
	CorePackage  string   `yaml:"corePackage,omitempty"`
}

// validateImportYAML runs structural checks on an import.yaml file.
func validateImportYAML(content string, plan *workflow.RecipePlan, envIndex int, folder string) []workflow.StepCheck {
	prefix := folder + "_import"
	var checks []workflow.StepCheck

	// Parse YAML.
	var doc importYAMLDoc
	if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
		checks = append(checks, workflow.StepCheck{
			Name: prefix + "_valid_yaml", Status: statusFail,
			Detail: fmt.Sprintf("invalid YAML: %v", err),
		})
		return checks
	}
	checks = append(checks, workflow.StepCheck{
		Name: prefix + "_valid_yaml", Status: statusPass,
	})

	// Project name convention: {slug}-{suffix}.
	if doc.Project.Name == "" {
		checks = append(checks, workflow.StepCheck{
			Name: prefix + "_project_name", Status: statusFail,
			Detail: "project name is empty",
		})
	} else {
		checks = append(checks, workflow.StepCheck{
			Name: prefix + "_project_name", Status: statusPass,
			Detail: doc.Project.Name,
		})
	}

	// Data service priority.
	svcMap := make(map[string]importService, len(doc.Services))
	for _, svc := range doc.Services {
		svcMap[svc.Hostname] = svc
	}

	for _, target := range plan.Targets {
		if !workflow.TargetInEnv(target, envIndex) {
			continue
		}
		if workflow.IsDataService(target.Role) {
			svc, exists := svcMap[target.Hostname]
			if exists && (svc.Priority == nil || *svc.Priority != 10) {
				checks = append(checks, workflow.StepCheck{
					Name: prefix + "_" + target.Hostname + "_priority", Status: statusFail,
					Detail: fmt.Sprintf("data service %q should have priority: 10", target.Hostname),
				})
			} else if exists {
				checks = append(checks, workflow.StepCheck{
					Name: prefix + "_" + target.Hostname + "_priority", Status: statusPass,
				})
			}
		}
	}

	// Env 5: HA checks.
	if envIndex == 5 {
		checks = append(checks, checkEnv5Requirements(doc, plan, prefix)...)
	}

	// Env 4: minContainers check.
	if envIndex == 4 {
		checks = append(checks, checkEnv4Requirements(doc, plan, prefix)...)
	}

	// No placeholders.
	if matches := placeholderRe.FindAllString(content, -1); len(matches) > 0 {
		checks = append(checks, workflow.StepCheck{
			Name: prefix + "_no_placeholders", Status: statusFail,
			Detail: fmt.Sprintf("found placeholders: %s", strings.Join(uniqueStrings(matches), ", ")),
		})
	} else {
		checks = append(checks, workflow.StepCheck{
			Name: prefix + "_no_placeholders", Status: statusPass,
		})
	}

	// Comment ratio.
	ratio := commentRatio(content)
	if ratio >= 0.3 {
		checks = append(checks, workflow.StepCheck{
			Name: prefix + "_comment_ratio", Status: statusPass,
			Detail: fmt.Sprintf("%.0f%%", ratio*100),
		})
	} else {
		checks = append(checks, workflow.StepCheck{
			Name: prefix + "_comment_ratio", Status: statusFail,
			Detail: fmt.Sprintf("comment ratio %.0f%% is below 30%% minimum", ratio*100),
		})
	}

	// Comment line width.
	checks = append(checks, checkCommentWidth(content, prefix)...)

	// envSecrets check.
	if plan.Research.NeedsAppSecret {
		if strings.Contains(content, "envSecrets") {
			checks = append(checks, workflow.StepCheck{
				Name: prefix + "_env_secrets", Status: statusPass,
			})
		} else {
			checks = append(checks, workflow.StepCheck{
				Name: prefix + "_env_secrets", Status: statusFail,
				Detail: "envSecrets required (needsAppSecret=true) but not found",
			})
		}

		// Preprocessor check when using generateRandomString.
		if strings.Contains(content, "<@generateRandomString") {
			if strings.Contains(content, "zeropsPreprocessor=on") {
				checks = append(checks, workflow.StepCheck{
					Name: prefix + "_preprocessor", Status: statusPass,
				})
			} else {
				checks = append(checks, workflow.StepCheck{
					Name: prefix + "_preprocessor", Status: statusFail,
					Detail: "# zeropsPreprocessor=on required when using <@generateRandomString>",
				})
			}
		}
	}

	return checks
}

// checkEnv5Requirements validates HA production requirements.
func checkEnv5Requirements(doc importYAMLDoc, plan *workflow.RecipePlan, prefix string) []workflow.StepCheck {
	var checks []workflow.StepCheck

	for _, svc := range doc.Services {
		role := findTargetRole(plan, svc.Hostname)

		// HA mode on data services.
		if workflow.IsDataService(role) && svc.Mode != "HA" {
			checks = append(checks, workflow.StepCheck{
				Name: prefix + "_" + svc.Hostname + "_ha_mode", Status: statusFail,
				Detail: fmt.Sprintf("env 5 data service %q should have mode: HA", svc.Hostname),
			})
		}

		// DEDICATED cpuMode.
		if svc.VerticalAutoscaling != nil {
			if svc.VerticalAutoscaling.CPUMode != "DEDICATED" && (role == workflow.RecipeRoleApp || role == workflow.RecipeRoleWorker) {
				checks = append(checks, workflow.StepCheck{
					Name: prefix + "_" + svc.Hostname + "_cpu_mode", Status: statusFail,
					Detail: fmt.Sprintf("env 5 service %q should have cpuMode: DEDICATED", svc.Hostname),
				})
			}
		}
	}

	return checks
}

// checkEnv4Requirements validates small production requirements.
func checkEnv4Requirements(doc importYAMLDoc, plan *workflow.RecipePlan, prefix string) []workflow.StepCheck {
	var checks []workflow.StepCheck

	for _, svc := range doc.Services {
		role := findTargetRole(plan, svc.Hostname)
		if role == workflow.RecipeRoleApp || role == workflow.RecipeRoleWorker {
			if svc.MinContainers == nil || *svc.MinContainers < 2 {
				checks = append(checks, workflow.StepCheck{
					Name: prefix + "_" + svc.Hostname + "_min_containers", Status: statusFail,
					Detail: fmt.Sprintf("env 4 app service %q should have minContainers: 2", svc.Hostname),
				})
			} else {
				checks = append(checks, workflow.StepCheck{
					Name: prefix + "_" + svc.Hostname + "_min_containers", Status: statusPass,
				})
			}
		}
	}

	return checks
}

// checkCommentWidth validates that comment lines are <= 80 chars.
func checkCommentWidth(content, prefix string) []workflow.StepCheck {
	lines := strings.Split(content, "\n")
	var longLines []string
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") && len(trimmed) > 80 {
			longLines = append(longLines, fmt.Sprintf("line %d (%d chars)", i+1, len(trimmed)))
		}
	}
	if len(longLines) > 0 {
		detail := strings.Join(longLines, ", ")
		if len(longLines) > 3 {
			detail = strings.Join(longLines[:3], ", ") + fmt.Sprintf(" and %d more", len(longLines)-3)
		}
		return []workflow.StepCheck{{
			Name: prefix + "_comment_width", Status: statusFail,
			Detail: "comment lines exceed 80 chars: " + detail,
		}}
	}
	return []workflow.StepCheck{{
		Name: prefix + "_comment_width", Status: statusPass,
	}}
}

// findTargetRole finds the role for a hostname in the recipe plan.
func findTargetRole(plan *workflow.RecipePlan, hostname string) string {
	for _, t := range plan.Targets {
		if t.Hostname == hostname {
			return t.Role
		}
	}
	return ""
}
