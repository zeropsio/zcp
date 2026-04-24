package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// deployPreFlight validates zerops.yaml configuration BEFORE deploy execution.
// This is the harness: it catches config errors that would cause silent deploy failures.
// Returns nil when stateDir is empty (no state directory = skip validation).
func deployPreFlight(ctx context.Context, client platform.Client, projectID, stateDir, targetHostname, setup string) (resolvedSetup string, result *workflow.StepCheckResult, err error) {
	if stateDir == "" {
		return setup, nil, nil
	}

	// Read ServiceMeta for target to derive role and mode.
	meta, err := workflow.ReadServiceMeta(stateDir, targetHostname)
	if err != nil {
		return setup, nil, fmt.Errorf("preflight read meta: %w", err)
	}
	// No meta = not adopted, but requireAdoption handles that gate.
	// If meta is nil, skip pre-flight (permissive).
	if meta == nil {
		return setup, nil, nil
	}

	projectRoot := projectRootFromState(stateDir)
	var checks []workflow.StepCheck

	// Find and parse zerops.yaml.
	doc, _, parseErr := findAndParseZeropsYml(projectRoot, targetHostname)
	if parseErr != nil {
		checks = append(checks, workflow.StepCheck{
			Name: "zerops_yml_exists", Status: statusFail,
			Detail: fmt.Sprintf("zerops.yaml not found or invalid: %v", parseErr),
		})
		return setup, &workflow.StepCheckResult{
			Passed: false, Checks: checks, Summary: "zerops.yaml not found or invalid",
		}, nil
	}
	checks = append(checks, workflow.StepCheck{
		Name: "zerops_yml_exists", Status: statusPass,
	})

	// Resolve setup entry: explicit setup param → role name → hostname.
	// v8.85 — when the input `setup` is empty and pre-flight resolves one
	// via role or hostname fallback, the resolved name is propagated back
	// to the caller so `zcli push --setup <name>` is invoked explicitly.
	// Without this, pre-flight silently matched the right setup but zcli
	// received an empty flag and failed with "Cannot find corresponding
	// setup in zerops.yaml" — the exact failure in session-log-16 (L145).
	role := meta.PrimaryRole()
	entry := resolveSetupEntry(doc, setup, role, targetHostname)
	if entry == nil {
		tried := targetHostname
		if setup != "" {
			tried = setup
		}
		checks = append(checks, workflow.StepCheck{
			Name: targetHostname + "_setup", Status: statusFail,
			Detail: fmt.Sprintf("no setup entry %q found in zerops.yaml — available setups: [%s]. Pass one explicitly via the `setup` parameter; in recipes setup names differ from hostnames (e.g. hostname=%s → setup=dev), the deploy tool cannot guess when multiple setups are declared.", tried, strings.Join(doc.SetupNames(), ", "), targetHostname),
		})
		return setup, &workflow.StepCheckResult{
			Passed: false, Checks: checks,
			Summary: fmt.Sprintf("no matching setup entry for %s", targetHostname),
		}, nil
	}
	// Entry resolved. The actual setup name to pass to zcli is entry.Setup —
	// even when the input was empty and role/hostname fallback found it.
	resolvedSetup = entry.Setup
	checks = append(checks, workflow.StepCheck{
		Name: targetHostname + "_setup", Status: statusPass,
	})

	// Dev/prod env divergence check.
	checks = append(checks, checkDevProdEnvDivergence(doc)...)

	// deployFiles path validation is owned by ops.ValidateZeropsYml (invoked
	// at the push site in deploy_ssh.go / deploy_local.go) and enforces DM-2
	// with full DeployClass context. DM-4 (docs/spec-workflows.md §8) forbids
	// a duplicate check in this layer. The pre-flight's sole yaml concern
	// here is setup resolution + schema + env-ref validation.

	// Validate env var references.
	if len(entry.EnvVariables) > 0 && client != nil {
		checks = append(checks, preflightEnvRefs(ctx, client, projectID, targetHostname, entry)...)
	}

	allPassed := checksAllPassed(checks)
	summary := "pre-flight checks passed"
	if !allPassed {
		summary = "pre-flight checks failed — fix issues before deploying"
	}
	return resolvedSetup, &workflow.StepCheckResult{
		Passed: allPassed, Checks: checks, Summary: summary,
	}, nil
}

// resolveSetupEntry finds the zerops.yaml setup entry using priority:
// explicit setup param → role-based name → hostname fallback.
func resolveSetupEntry(doc *ops.ZeropsYmlDoc, setup string, role workflow.Mode, hostname string) *ops.ZeropsYmlEntry {
	if setup != "" {
		return doc.FindEntry(setup)
	}
	// Role-based: "dev" or "stage" → try as setup name.
	if entry := doc.FindEntry(string(role)); entry != nil {
		return entry
	}
	// Stage and simple roles map to "prod" setup.
	if role == workflow.DeployRoleStage || role == workflow.DeployRoleSimple {
		if entry := doc.FindEntry(workflow.RecipeSetupProd); entry != nil {
			return entry
		}
	}
	// Fallback: hostname as setup name (legacy).
	return doc.FindEntry(hostname)
}

// preflightEnvRefs validates env var references against live API data for a single target.
func preflightEnvRefs(ctx context.Context, client platform.Client, projectID, hostname string, entry *ops.ZeropsYmlEntry) []workflow.StepCheck {
	services, err := client.ListServices(ctx, projectID)
	if err != nil {
		return []workflow.StepCheck{{
			Name: hostname + "_env_refs", Status: statusFail,
			Detail: fmt.Sprintf("failed to list services for env var validation: %v", err),
		}}
	}

	liveHostnames := make([]string, 0, len(services))
	discoveredEnvVars := make(map[string][]string)
	for _, svc := range services {
		liveHostnames = append(liveHostnames, svc.Name)
		envVars, envErr := client.GetServiceEnv(ctx, svc.ID)
		if envErr != nil {
			continue
		}
		names := make([]string, len(envVars))
		for i, v := range envVars {
			names[i] = v.Key
		}
		discoveredEnvVars[svc.Name] = names
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
