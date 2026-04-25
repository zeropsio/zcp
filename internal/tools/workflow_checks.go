package tools

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// projectRootFromState derives the project root from a state directory path.
// Convention: stateDir = {projectRoot}/.zcp/state/
func projectRootFromState(stateDir string) string {
	return filepath.Dir(filepath.Dir(stateDir))
}

// checksAllPassed returns true if no check has statusFail.
func checksAllPassed(checks []workflow.StepCheck) bool {
	for i := range checks {
		if checks[i].Status == statusFail {
			return false
		}
	}
	return true
}

const (
	stepProvision     = "provision"
	statusFail        = "fail"
	statusPass        = "pass"
	statusHealthy     = "healthy"
	defaultSkipReason = "skipped by user"
)

func buildStepChecker(step string, client platform.Client, _ platform.LogFetcher, projectID string, _ ops.HTTPDoer, engine *workflow.Engine, _ string) workflow.StepChecker {
	if step == stepProvision {
		return checkProvision(client, projectID, engine)
	}
	// discover and close steps have nil checkers (attestation-only triggers
	// under Option A — bootstrap owns infra provisioning, not deploy).
	return nil
}

func checkProvision(client platform.Client, projectID string, engine *workflow.Engine) workflow.StepChecker {
	return func(ctx context.Context, plan *workflow.ServicePlan, _ *workflow.BootstrapState) (*workflow.StepCheckResult, error) {
		if plan == nil || len(plan.Targets) == 0 {
			return nil, nil
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

		for _, target := range plan.Targets {
			// Check dev runtime exists and is RUNNING.
			checks = append(checks, checkServiceRunning(svcMap, target.Runtime.DevHostname)...)

			// Cross-check runtime type matches plan.
			checks = append(checks, checkServiceType(svcMap, target.Runtime.DevHostname, target.Runtime.Type)...)

			// Check stage runtime exists in any alive status.
			// Stage may be newly imported (NEW/READY_TO_DEPLOY) or already running (RUNNING/ACTIVE).
			// Mixed cases (existing dev + new stage) are valid for adoption scenarios.
			if stage := target.Runtime.StageHostname(); stage != "" {
				checks = append(checks, checkServiceStatusAny(svcMap, stage, serviceStatusNew, serviceStatusReadyToDeploy, serviceStatusRunning, serviceStatusActive)...)
			}

			// Check dependencies.
			for _, dep := range target.Dependencies {
				checks = append(checks, checkServiceRunning(svcMap, dep.Hostname)...)

				// Cross-check dependency type matches plan.
				checks = append(checks, checkServiceType(svcMap, dep.Hostname, dep.Type)...)

				// Managed (non-storage) dependencies with resolution CREATE or EXISTS must have env vars.
				if (dep.Resolution == "CREATE" || dep.Resolution == "EXISTS") && isManagedNonStorage(dep.Type) {
					svc, exists := svcMap[dep.Hostname]
					if !exists {
						continue
					}
					envVars, envErr := client.GetServiceEnv(ctx, svc.ID)
					switch {
					case envErr != nil:
						checks = append(checks, workflow.StepCheck{
							Name:   dep.Hostname + "_env_vars",
							Status: statusFail,
							Detail: fmt.Sprintf("failed to get env vars: %v", envErr),
						})
						allPassed = false
					case len(envVars) == 0:
						checks = append(checks, workflow.StepCheck{
							Name:   dep.Hostname + "_env_vars",
							Status: statusFail,
							Detail: "no env vars found — service may not be ready",
						})
						allPassed = false
					default:
						checks = append(checks, workflow.StepCheck{
							Name:   dep.Hostname + "_env_vars",
							Status: statusPass,
							Detail: fmt.Sprintf("%d env vars", len(envVars)),
						})
						if engine != nil {
							varNames := make([]string, len(envVars))
							for vi, v := range envVars {
								varNames[vi] = v.Key
							}
							if storeErr := engine.StoreDiscoveredEnvVars(dep.Hostname, varNames); storeErr != nil {
								checks = append(checks, workflow.StepCheck{
									Name:   dep.Hostname + "_env_store",
									Status: statusFail,
									Detail: fmt.Sprintf("failed to store env vars: %v", storeErr),
								})
								allPassed = false
							}
						}
					}
				}
			}
		}

		for i := range checks {
			if checks[i].Status == statusFail {
				allPassed = false
				break
			}
		}

		// C-10: surface-derived coupling removed (P1 supersedes). The
		// per-check PreAttestCmd is the runnable form; authors do not
		// need a separate coupling-hint stanza because running the shim
		// re-checks the affected surfaces directly.
		summary := "all services provisioned"
		if !allPassed {
			summary = "provisioning incomplete"
		}
		return &workflow.StepCheckResult{
			Passed:  allPassed,
			Checks:  checks,
			Summary: summary,
		}, nil
	}
}

// checkServiceRunning checks a service exists and is running (RUNNING or ACTIVE).
func checkServiceRunning(svcMap map[string]platform.ServiceStack, hostname string) []workflow.StepCheck {
	return checkServiceStatusAny(svcMap, hostname, serviceStatusRunning, serviceStatusActive)
}

// checkServiceStatusAny checks a service exists with any of the expected statuses.
func checkServiceStatusAny(svcMap map[string]platform.ServiceStack, hostname string, statuses ...string) []workflow.StepCheck {
	svc, exists := svcMap[hostname]
	if !exists {
		return []workflow.StepCheck{{
			Name:   hostname + "_exists",
			Status: statusFail,
			Detail: "service not found",
		}}
	}
	if slices.Contains(statuses, svc.Status) {
		return []workflow.StepCheck{{
			Name:   hostname + "_status",
			Status: statusPass,
		}}
	}
	return []workflow.StepCheck{{
		Name:   hostname + "_status",
		Status: statusFail,
		Detail: fmt.Sprintf("expected one of [%s], got %s", strings.Join(statuses, ", "), svc.Status),
	}}
}

// checkServiceType verifies a service's API type matches the plan type.
func checkServiceType(svcMap map[string]platform.ServiceStack, hostname, expectedType string) []workflow.StepCheck {
	svc, exists := svcMap[hostname]
	if !exists {
		return nil // missing service is caught by checkServiceRunning
	}
	actual := svc.ServiceStackTypeInfo.ServiceStackTypeVersionName
	if actual == "" || actual == expectedType {
		return nil
	}
	return []workflow.StepCheck{{
		Name:   hostname + "_type",
		Status: statusFail,
		Detail: fmt.Sprintf("expected %s, got %s", expectedType, actual),
	}}
}

// isManagedNonStorage returns true for managed services that are NOT storage types.
// Delegates to topology.IsManagedService for the canonical prefix list,
// then excludes storage types which don't produce env vars.
func isManagedNonStorage(serviceType string) bool {
	lower := strings.ToLower(serviceType)
	if strings.HasPrefix(lower, "shared-storage") || strings.HasPrefix(lower, "object-storage") {
		return false
	}
	return topology.IsManagedService(serviceType)
}
