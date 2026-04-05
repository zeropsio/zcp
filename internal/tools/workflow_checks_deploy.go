package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// buildDeployStepChecker returns the appropriate checker for a develop workflow step.
// Follows the same pattern as buildStepChecker for bootstrap (workflow_checks.go:22).
func buildDeployStepChecker(step string, client platform.Client, projectID, stateDir string) workflow.DeployStepChecker {
	switch step {
	case workflow.DeployStepPrepare:
		return checkDeployPrepare(client, projectID, stateDir)
	case workflow.DeployStepExecute:
		return checkDeployResult(client, projectID)
	}
	// deploy verify step has nil checker (informational, not blocking).
	return nil
}

// checkDeployPrepare validates platform integration at the prepare step:
// zerops.yaml exists and parses, setup entries match deploy targets,
// env var references valid (re-discovered from API).
func checkDeployPrepare(client platform.Client, projectID, stateDir string) workflow.DeployStepChecker {
	return func(ctx context.Context, state *workflow.DeployState) (*workflow.StepCheckResult, error) {
		if state == nil || len(state.Targets) == 0 {
			return nil, nil
		}

		// Derive project root from stateDir ({projectRoot}/.zcp/state/).
		projectRoot := filepath.Dir(filepath.Dir(stateDir))

		var checks []workflow.StepCheck

		// Find and parse zerops.yaml.
		doc, parseErr := findAndParseZeropsYml(projectRoot, state.Targets)
		if parseErr != nil {
			checks = append(checks, workflow.StepCheck{
				Name: "zerops_yml_exists", Status: statusFail,
				Detail: fmt.Sprintf("zerops.yaml not found or invalid: %v", parseErr),
			})
			return &workflow.StepCheckResult{
				Passed: false, Checks: checks, Summary: "zerops.yaml not found",
			}, nil
		}
		checks = append(checks, workflow.StepCheck{
			Name: "zerops_yml_exists", Status: statusPass,
		})

		// Dev/prod env-mode drift check — catches copy-paste where dev
		// accidentally inherits prod's APP_ENV/DEBUG/LOG_LEVEL values.
		checks = append(checks, checkDevProdEnvDivergence(doc)...)

		// Check setup entries match deploy targets.
		// Try generic names (dev/prod) first, then hostname (legacy).
		for _, t := range state.Targets {
			entry := doc.FindEntry(t.Role) // "dev" or "stage" → try as setup name
			if entry == nil && t.Role == "stage" {
				entry = doc.FindEntry("prod") // stage role maps to prod setup
			}
			if entry == nil {
				entry = doc.FindEntry(t.Hostname) // legacy: hostname matching
			}
			if entry == nil {
				checks = append(checks, workflow.StepCheck{
					Name: t.Hostname + "_setup", Status: statusFail,
					Detail: fmt.Sprintf("no setup entry for %q (also tried %q) in zerops.yaml", t.Hostname, t.Role),
				})
			} else {
				checks = append(checks, workflow.StepCheck{
					Name: t.Hostname + "_setup", Status: statusPass,
				})

				// Validate deployFiles paths exist.
				checks = append(checks, validateDeployFiles(projectRoot, t.Hostname, entry)...)

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
					Detail: "still READY_TO_DEPLOY — container didn't start. Check: start command, ports, env vars in zerops.yaml run section. Deploy creates new container, local files lost.",
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

// frameworkModeKeys lists envVariables that encode a framework's run mode —
// identical values across dev and prod setups signal copy-paste drift.
// Includes the major web frameworks' mode/debug/log-level keys; additions
// are welcome as new framework support lands.
var frameworkModeKeys = []string{
	// PHP frameworks
	"APP_ENV", "APP_DEBUG", "APP_ENVIRONMENT",
	"LOG_LEVEL", "APP_LOG_LEVEL",
	// Ruby / Rack
	"RAILS_ENV", "RACK_ENV",
	// Node.js
	"NODE_ENV",
	// Python
	"FLASK_ENV", "FLASK_DEBUG",
	"DJANGO_DEBUG", "DJANGO_SETTINGS_MODULE",
	// .NET
	"ASPNETCORE_ENVIRONMENT", "DOTNET_ENVIRONMENT",
	// Go
	"GIN_MODE", "GO_ENV",
	// Java
	"SPRING_PROFILES_ACTIVE",
	// Generic
	"DEBUG", "ENVIRONMENT", "ENV",
}

// checkDevProdEnvDivergence flags dev/prod setups whose framework-mode env
// variables carry identical values. The two setups should differ on at least
// the mode flag (APP_ENV=local vs production, RAILS_ENV=development vs
// production, etc.) — identical values mean the dev container will behave
// like production: debug pages hidden, caching enabled, full-source reloads
// suppressed. Copy-paste from prod → dev is the usual root cause.
func checkDevProdEnvDivergence(doc *ops.ZeropsYmlDoc) []workflow.StepCheck {
	devEntry := doc.FindEntry("dev")
	prodEntry := doc.FindEntry("prod")
	if devEntry == nil || prodEntry == nil {
		// Only fires when both setups coexist in zerops.yaml.
		return nil
	}
	devEnv := devEntry.Run.EnvVariables
	prodEnv := prodEntry.Run.EnvVariables
	if len(devEnv) == 0 || len(prodEnv) == 0 {
		return nil
	}

	var identical []string
	for _, key := range frameworkModeKeys {
		dv, inDev := devEnv[key]
		pv, inProd := prodEnv[key]
		if inDev && inProd && dv == pv {
			identical = append(identical, fmt.Sprintf("%s=%s", key, dv))
		}
	}
	if len(identical) == 0 {
		return []workflow.StepCheck{{
			Name: "dev_prod_env_divergence", Status: statusPass,
		}}
	}
	return []workflow.StepCheck{{
		Name:   "dev_prod_env_divergence",
		Status: statusFail,
		Detail: fmt.Sprintf("dev and prod setups share identical framework-mode values: %s — dev should use the framework's development mode (e.g., APP_ENV=local/APP_DEBUG=true for Laravel, RAILS_ENV=development for Rails, NODE_ENV=development for Node) so developers see stack traces and disabled caches while iterating", strings.Join(identical, ", ")),
	}}
}

// findAndParseZeropsYml locates and parses zerops.yaml from project root or mount paths.
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

// validateDeployFiles checks that cherry-picked deployFiles paths exist on the filesystem.
// Skipped when deployFiles is [.] (deploys everything).
func validateDeployFiles(projectRoot, hostname string, entry *ops.ZeropsYmlEntry) []workflow.StepCheck {
	if !entry.HasDeployFiles() {
		return nil
	}
	deployFiles := entry.DeployFilesList()
	// Skip if deploying everything.
	if slices.Contains(deployFiles, ".") || slices.Contains(deployFiles, "./") {
		return nil
	}

	var missing []string
	for _, df := range deployFiles {
		p := filepath.Join(projectRoot, df)
		if _, err := os.Stat(p); err != nil {
			missing = append(missing, df)
		}
	}
	if len(missing) > 0 {
		return []workflow.StepCheck{{
			Name:   hostname + "_deploy_files",
			Status: statusFail,
			Detail: fmt.Sprintf("deployFiles paths not found: %s — these will be missing from the deploy artifact", strings.Join(missing, ", ")),
		}}
	}
	return []workflow.StepCheck{{
		Name:   hostname + "_deploy_files",
		Status: statusPass,
	}}
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
		if !slices.Contains(liveHostnames, svc.Name) {
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
