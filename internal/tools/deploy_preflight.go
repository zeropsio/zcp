package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// PreFlightResult is the outcome of pre-deploy validation.
// Returned as JSON to the LLM when validation fails, blocking deploy.
type PreFlightResult struct {
	Passed  bool                 `json:"passed"`
	Checks  []workflow.StepCheck `json:"checks"`
	Summary string               `json:"summary"`
}

// deployPreFlight validates zerops.yaml configuration BEFORE deploy execution.
// This is the harness: it catches config errors that would cause silent deploy failures.
// Returns nil when stateDir is empty (no state directory = skip validation).
func deployPreFlight(ctx context.Context, client platform.Client, projectID, stateDir, targetHostname, setup string) (*PreFlightResult, error) {
	if stateDir == "" {
		return nil, nil //nolint:nilnil // nil,nil = skip validation when no state dir
	}

	// Read ServiceMeta for target to derive role and mode.
	meta, err := workflow.ReadServiceMeta(stateDir, targetHostname)
	if err != nil {
		return nil, fmt.Errorf("preflight read meta: %w", err)
	}
	// No meta = not adopted, but requireAdoption handles that gate.
	// If meta is nil, skip pre-flight (permissive).
	if meta == nil {
		return nil, nil //nolint:nilnil // nil,nil = not found, skip pre-flight
	}

	projectRoot := projectRootFromState(stateDir)
	var checks []workflow.StepCheck

	// Find and parse zerops.yaml.
	doc, ymlDir, parseErr := findAndParseZeropsYml(projectRoot, targetHostname)
	if parseErr != nil {
		checks = append(checks, workflow.StepCheck{
			Name: "zerops_yml_exists", Status: statusFail,
			Detail: fmt.Sprintf("zerops.yaml not found or invalid: %v", parseErr),
		})
		return &PreFlightResult{
			Passed: false, Checks: checks, Summary: "zerops.yaml not found or invalid",
		}, nil
	}
	checks = append(checks, workflow.StepCheck{
		Name: "zerops_yml_exists", Status: statusPass,
	})

	// Resolve setup entry: explicit setup param → role name → hostname.
	role := preflightRole(meta)
	entry := resolveSetupEntry(doc, setup, role, targetHostname)
	if entry == nil {
		tried := targetHostname
		if setup != "" {
			tried = setup
		}
		checks = append(checks, workflow.StepCheck{
			Name: targetHostname + "_setup", Status: statusFail,
			Detail: fmt.Sprintf("no setup entry %q found in zerops.yaml (also tried role %q, hostname %q)", tried, role, targetHostname),
		})
		return &PreFlightResult{
			Passed: false, Checks: checks,
			Summary: fmt.Sprintf("no matching setup entry for %s", targetHostname),
		}, nil
	}
	checks = append(checks, workflow.StepCheck{
		Name: targetHostname + "_setup", Status: statusPass,
	})

	// Dev/prod env divergence check.
	checks = append(checks, checkDevProdEnvDivergence(doc)...)

	// Validate deployFiles paths (skip for stage — cross-deployed from dev).
	if role != workflow.DeployRoleStage {
		checks = append(checks, validateDeployFiles(ymlDir, targetHostname, entry)...)
	}

	// Validate env var references.
	if len(entry.EnvVariables) > 0 && client != nil {
		checks = append(checks, preflightEnvRefs(ctx, client, projectID, targetHostname, entry)...)
	}

	allPassed := checksAllPassed(checks)
	summary := "pre-flight checks passed"
	if !allPassed {
		summary = "pre-flight checks failed — fix issues before deploying"
	}
	return &PreFlightResult{
		Passed: allPassed, Checks: checks, Summary: summary,
	}, nil
}

// preflightRole derives the deploy role from ServiceMeta.
func preflightRole(meta *workflow.ServiceMeta) string {
	mode := meta.Mode
	if mode == "" {
		mode = workflow.PlanModeStandard
	}
	switch mode {
	case workflow.PlanModeSimple:
		return workflow.DeployRoleSimple
	case workflow.PlanModeDev:
		return workflow.DeployRoleDev
	default:
		if meta.StageHostname != "" {
			return workflow.DeployRoleDev
		}
		return workflow.DeployRoleSimple
	}
}

// resolveSetupEntry finds the zerops.yaml setup entry using priority:
// explicit setup param → role-based name → hostname fallback.
func resolveSetupEntry(doc *ops.ZeropsYmlDoc, setup, role, hostname string) *ops.ZeropsYmlEntry {
	if setup != "" {
		return doc.FindEntry(setup)
	}
	// Role-based: "dev" or "stage" → try as setup name.
	if entry := doc.FindEntry(role); entry != nil {
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

// preflightEnvRefs validates env var references from the API.
// Simplified version of validateDeployEnvRefs that doesn't need DeployTarget slice.
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
