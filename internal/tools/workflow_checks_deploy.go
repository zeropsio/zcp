package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// buildDeployStepChecker returns the appropriate checker for a deploy workflow step.
// Follows the same pattern as buildStepChecker for bootstrap (workflow_checks.go:22).
func buildDeployStepChecker(step string, client platform.Client, projectID, stateDir string) workflow.DeployStepChecker {
	switch step {
	case workflow.DeployStepPrepare:
		return checkDeployPrepare(client, projectID, stateDir)
	case workflow.DeployStepDeploy:
		return checkDeployResult(client, projectID)
	}
	// verify step has nil checker (informational).
	return nil
}

// checkDeployPrepare validates platform integration at the prepare step:
// zerops.yml exists and parses, setup entries match deploy targets,
// env var references valid (re-discovered from API).
func checkDeployPrepare(client platform.Client, projectID, stateDir string) workflow.DeployStepChecker {
	return func(ctx context.Context, state *workflow.DeployState) (*workflow.StepCheckResult, error) {
		if state == nil || len(state.Targets) == 0 {
			return nil, nil
		}

		// Derive project root from stateDir ({projectRoot}/.zcp/state/).
		projectRoot := filepath.Dir(filepath.Dir(stateDir))

		var checks []workflow.StepCheck

		// Find and parse zerops.yml.
		doc, parseErr := findAndParseZeropsYml(projectRoot, state.Targets)
		if parseErr != nil {
			checks = append(checks, workflow.StepCheck{
				Name: "zerops_yml_exists", Status: statusFail,
				Detail: fmt.Sprintf("zerops.yml not found or invalid: %v", parseErr),
			})
			return &workflow.StepCheckResult{
				Passed: false, Checks: checks, Summary: "zerops.yml not found",
			}, nil
		}
		checks = append(checks, workflow.StepCheck{
			Name: "zerops_yml_exists", Status: statusPass,
		})

		// Check setup entries match deploy targets.
		for _, t := range state.Targets {
			entry := doc.FindEntry(t.Hostname)
			if entry == nil {
				checks = append(checks, workflow.StepCheck{
					Name: t.Hostname + "_setup", Status: statusFail,
					Detail: fmt.Sprintf("no setup entry for %q in zerops.yml", t.Hostname),
				})
			} else {
				checks = append(checks, workflow.StepCheck{
					Name: t.Hostname + "_setup", Status: statusPass,
				})

				// Validate env var references if entry has envVariables.
				if len(entry.EnvVariables) > 0 && client != nil {
					checks = append(checks, validateDeployEnvRefs(ctx, client, projectID, t.Hostname, entry, state.Targets)...)
				}
			}
		}

		allPassed := true
		for _, c := range checks {
			if c.Status == statusFail {
				allPassed = false
				break
			}
		}
		summary := "prepare checks passed"
		if !allPassed {
			summary = "prepare checks failed"
		}
		return &workflow.StepCheckResult{
			Passed: allPassed, Checks: checks, Summary: summary,
		}, nil
	}
}

// checkDeployResult validates deployment outcome: service status + diagnostics.
// Informational — provides diagnostic feedback, not hard blocking.
func checkDeployResult(client platform.Client, projectID string) workflow.DeployStepChecker {
	return func(ctx context.Context, state *workflow.DeployState) (*workflow.StepCheckResult, error) {
		if state == nil || len(state.Targets) == 0 {
			return nil, nil
		}
		if client == nil {
			return nil, nil // no API client = skip check
		}

		services, err := client.ListServices(ctx, projectID)
		if err != nil {
			return nil, fmt.Errorf("list services: %w", err)
		}
		svcMap := make(map[string]platform.ServiceStack, len(services))
		for _, svc := range services {
			svcMap[svc.Name] = svc
		}

		var checks []workflow.StepCheck
		allPassed := true

		for _, t := range state.Targets {
			svc, exists := svcMap[t.Hostname]
			if !exists {
				checks = append(checks, workflow.StepCheck{
					Name: t.Hostname + "_status", Status: statusFail,
					Detail: "service not found in project",
				})
				allPassed = false
				continue
			}

			switch svc.Status {
			case "RUNNING", "ACTIVE":
				checks = append(checks, workflow.StepCheck{
					Name: t.Hostname + "_status", Status: statusPass,
					Detail: svc.Status,
				})
			case "READY_TO_DEPLOY":
				checks = append(checks, workflow.StepCheck{
					Name: t.Hostname + "_status", Status: statusFail,
					Detail: "still READY_TO_DEPLOY — container didn't start. Check: start command, ports, env vars in zerops.yml run section. Deploy creates new container, local files lost.",
				})
				allPassed = false
			default:
				checks = append(checks, workflow.StepCheck{
					Name: t.Hostname + "_status", Status: statusFail,
					Detail: fmt.Sprintf("status %s — check zerops_logs severity=error, review build output", svc.Status),
				})
				allPassed = false
			}

			// Subdomain access for services with ports.
			if len(svc.Ports) > 0 && !svc.SubdomainAccess {
				checks = append(checks, workflow.StepCheck{
					Name: t.Hostname + "_subdomain", Status: statusFail,
					Detail: "subdomain access not enabled — call zerops_subdomain action=enable",
				})
				allPassed = false
			}
		}

		summary := "deploy checks passed"
		if !allPassed {
			summary = "deploy issues detected — review diagnostics"
		}
		return &workflow.StepCheckResult{
			Passed: allPassed, Checks: checks, Summary: summary,
		}, nil
	}
}

// findAndParseZeropsYml locates and parses zerops.yml from project root or mount paths.
func findAndParseZeropsYml(projectRoot string, targets []workflow.DeployTarget) (*ops.ZeropsYmlDoc, error) {
	// Try mount path for first target (container environment).
	if len(targets) > 0 {
		mountPath := filepath.Join(projectRoot, targets[0].Hostname)
		if info, err := os.Stat(mountPath); err == nil && info.IsDir() {
			if doc, err := ops.ParseZeropsYml(mountPath); err == nil {
				return doc, nil
			}
		}
	}
	// Fall back to project root (local environment).
	return ops.ParseZeropsYml(projectRoot)
}

// validateDeployEnvRefs re-discovers env vars via API and validates references.
func validateDeployEnvRefs(ctx context.Context, client platform.Client, projectID, hostname string, entry *ops.ZeropsYmlEntry, targets []workflow.DeployTarget) []workflow.StepCheck {
	// Collect all hostnames referenced in targets for cross-service validation.
	liveHostnames := make([]string, 0, len(targets))
	for _, t := range targets {
		liveHostnames = append(liveHostnames, t.Hostname)
	}

	// Re-discover env vars from all services in the project.
	services, err := client.ListServices(ctx, projectID)
	if err != nil {
		return []workflow.StepCheck{{
			Name: hostname + "_env_refs", Status: statusFail,
			Detail: fmt.Sprintf("failed to list services for env var validation: %v", err),
		}}
	}

	discoveredEnvVars := make(map[string][]string)
	for _, svc := range services {
		envVars, envErr := client.GetServiceEnv(ctx, svc.ID)
		if envErr != nil {
			continue // best-effort
		}
		names := make([]string, len(envVars))
		for i, v := range envVars {
			names[i] = v.Key
		}
		discoveredEnvVars[svc.Name] = names
		// Include service hostname in live hostnames for validation.
		if !containsString(liveHostnames, svc.Name) {
			liveHostnames = append(liveHostnames, svc.Name)
		}
	}

	envErrs := ops.ValidateEnvReferences(entry.EnvVariables, discoveredEnvVars, liveHostnames)
	if len(envErrs) > 0 {
		details := make([]string, len(envErrs))
		for i, e := range envErrs {
			details[i] = fmt.Sprintf("%s: %s", e.Reference, e.Reason)
		}
		return []workflow.StepCheck{{
			Name: hostname + "_env_refs", Status: statusFail,
			Detail: strings.Join(details, "; "),
		}}
	}
	return []workflow.StepCheck{{
		Name: hostname + "_env_refs", Status: statusPass,
	}}
}

func containsString(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
