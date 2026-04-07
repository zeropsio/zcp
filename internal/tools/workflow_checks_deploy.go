package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"maps"
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
		projectRoot := projectRootFromState(stateDir)

		var checks []workflow.StepCheck

		// Find and parse zerops.yaml.
		doc, ymlDir, parseErr := findAndParseZeropsYml(projectRoot, state.Targets)
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
				entry = doc.FindEntry(workflow.RecipeSetupProd) // stage role maps to prod setup
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

				// Validate deployFiles paths exist on source filesystem.
				// Skip for stage — cross-deployed from dev, build artifacts don't exist on source.
				if t.Role != workflow.DeployRoleStage {
					checks = append(checks, validateDeployFiles(ymlDir, t.Hostname, entry)...)
				}

				// Validate env var references if entry has envVariables.
				if len(entry.EnvVariables) > 0 && client != nil {
					checks = append(checks, validateDeployEnvRefs(ctx, client, projectID, t.Hostname, entry, state.Targets)...)
				}
			}
		}

		allPassed := checksAllPassed(checks)
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

// checkDevProdEnvDivergence flags dev and prod setups whose run.envVariables
// maps are bit-identical. Two setups named differently exist to be different —
// the agent writes them precisely to carry different values for the framework
// to observe (mode flags, debug toggles, log levels, feature toggles). When
// the maps are literally equal, it is a copy-paste: the dev container will
// behave exactly like prod, hiding stack traces and enabling caches while
// developers iterate.
//
// This is a structural invariant — no knowledge of which env var keys carry
// mode signals for which framework is required. If an agent intentionally
// wants two setups with identical env vars, they can distinguish them with a
// single semantically-meaningful key (e.g. the framework's own env flag).
func checkDevProdEnvDivergence(doc *ops.ZeropsYmlDoc) []workflow.StepCheck {
	devEntry := doc.FindEntry(workflow.RecipeSetupDev)
	prodEntry := doc.FindEntry(workflow.RecipeSetupProd)
	if devEntry == nil || prodEntry == nil {
		// Only fires when both setups coexist in zerops.yaml.
		return nil
	}
	devEnv := devEntry.Run.EnvVariables
	prodEnv := prodEntry.Run.EnvVariables
	// If either side has no run.envVariables block, there is nothing to
	// compare — the framework's own defaults, OS env vars, or envSecrets
	// carry the mode signal.
	if len(devEnv) == 0 || len(prodEnv) == 0 {
		return nil
	}

	if !maps.Equal(devEnv, prodEnv) {
		return []workflow.StepCheck{{
			Name: "dev_prod_env_divergence", Status: statusPass,
		}}
	}
	return []workflow.StepCheck{{
		Name:   "dev_prod_env_divergence",
		Status: statusFail,
		Detail: "dev and prod setups in zerops.yaml have bit-identical run.envVariables — the dev container will behave exactly like prod (caches enabled, stack traces hidden). Differentiate the two setups using whichever env var your framework reads for its run mode",
	}}
}


// findAndParseZeropsYml locates and parses zerops.yaml from project root or mount paths.
// Returns the parsed doc and the directory where zerops.yaml was found.
func findAndParseZeropsYml(projectRoot string, targets []workflow.DeployTarget) (*ops.ZeropsYmlDoc, string, error) {
	// Try mount path for first target (container environment).
	if len(targets) > 0 {
		mountPath := filepath.Join(projectRoot, targets[0].Hostname)
		if info, err := os.Stat(mountPath); err == nil && info.IsDir() {
			if doc, err := ops.ParseZeropsYml(mountPath); err == nil {
				return doc, mountPath, nil
			}
		}
	}
	// Fall back to project root (local environment).
	doc, err := ops.ParseZeropsYml(projectRoot)
	return doc, projectRoot, err
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
