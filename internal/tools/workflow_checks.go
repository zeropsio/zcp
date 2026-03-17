package tools

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

const (
	stepProvision = "provision"
	stepGenerate  = "generate"
	stepDeploy    = "deploy"
	stepVerify    = "verify"
	statusFail    = "fail"
	statusPass    = "pass"
)

func buildStepChecker(step string, client platform.Client, fetcher platform.LogFetcher, projectID string, httpClient ops.HTTPDoer, engine *workflow.Engine, stateDir string) workflow.StepChecker {
	switch step {
	case stepProvision:
		return checkProvision(client, projectID, engine)
	case stepGenerate:
		return checkGenerate(stateDir)
	case stepDeploy:
		return checkDeploy(client, projectID)
	case stepVerify:
		return checkVerify(client, fetcher, projectID, httpClient)
	}
	return nil
}

func checkProvision(client platform.Client, projectID string, engine *workflow.Engine) workflow.StepChecker {
	return func(ctx context.Context, plan *workflow.ServicePlan, _ *workflow.BootstrapState) (*workflow.StepCheckResult, error) {
		if plan == nil {
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

			// Check stage runtime status.
			// Existing runtimes (incremental bootstrap) may have stage already RUNNING/ACTIVE.
			// New runtimes expect stage to be NEW or READY_TO_DEPLOY (freshly imported).
			if stage := target.Runtime.StageHostname(); stage != "" {
				if target.Runtime.IsExisting {
					checks = append(checks, checkServiceRunning(svcMap, stage)...)
				} else {
					checks = append(checks, checkServiceStatusAny(svcMap, stage, "NEW", "READY_TO_DEPLOY")...)
				}
			}

			// Check dependencies.
			for _, dep := range target.Dependencies {
				checks = append(checks, checkServiceRunning(svcMap, dep.Hostname)...)

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
							_ = engine.StoreDiscoveredEnvVars(dep.Hostname, varNames)
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

func checkDeploy(client platform.Client, projectID string) workflow.StepChecker {
	return func(ctx context.Context, plan *workflow.ServicePlan, _ *workflow.BootstrapState) (*workflow.StepCheckResult, error) {
		if plan == nil {
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
			// Dev service must be RUNNING after deploy.
			checks = append(checks, checkServiceRunning(svcMap, target.Runtime.DevHostname)...)

			// Stage service must be RUNNING after deploy.
			if stage := target.Runtime.StageHostname(); stage != "" {
				checks = append(checks, checkServiceRunning(svcMap, stage)...)
			}

			// Check SubdomainAccess for services with ports.
			for _, hostname := range targetHostnames(target) {
				svc, exists := svcMap[hostname]
				if exists && len(svc.Ports) > 0 && !svc.SubdomainAccess {
					checks = append(checks, workflow.StepCheck{
						Name:   hostname + "_subdomain",
						Status: statusFail,
						Detail: "subdomain access not enabled — call zerops_subdomain action=enable",
					})
					allPassed = false
				} else if exists && len(svc.Ports) > 0 && svc.SubdomainAccess {
					checks = append(checks, workflow.StepCheck{
						Name:   hostname + "_subdomain",
						Status: statusPass,
					})
				}
			}
		}

		for i := range checks {
			if checks[i].Status == statusFail {
				allPassed = false
				break
			}
		}

		summary := "all services deployed"
		if !allPassed {
			summary = "deployment incomplete"
		}
		return &workflow.StepCheckResult{
			Passed:  allPassed,
			Checks:  checks,
			Summary: summary,
		}, nil
	}
}

func checkVerify(client platform.Client, fetcher platform.LogFetcher, projectID string, httpClient ops.HTTPDoer) workflow.StepChecker {
	return func(ctx context.Context, plan *workflow.ServicePlan, _ *workflow.BootstrapState) (*workflow.StepCheckResult, error) {
		if plan == nil {
			return nil, nil
		}

		result, err := ops.VerifyAll(ctx, client, fetcher, httpClient, projectID)
		if err != nil {
			return nil, fmt.Errorf("verify all: %w", err)
		}

		// Build set of plan target hostnames.
		planHostnames := make(map[string]bool)
		for _, target := range plan.Targets {
			planHostnames[target.Runtime.DevHostname] = true
			if stage := target.Runtime.StageHostname(); stage != "" {
				planHostnames[stage] = true
			}
			for _, dep := range target.Dependencies {
				planHostnames[dep.Hostname] = true
			}
		}

		var checks []workflow.StepCheck
		allPassed := true

		for _, svc := range result.Services {
			if !planHostnames[svc.Hostname] {
				continue // ignore pre-existing services not in plan
			}
			status := statusPass
			if svc.Status == ops.StatusUnhealthy {
				status = statusFail
				allPassed = false
			}
			checks = append(checks, workflow.StepCheck{
				Name:   svc.Hostname + "_health",
				Status: status,
				Detail: svc.Status,
			})
		}

		summary := "all plan targets healthy"
		if !allPassed {
			summary = "unhealthy services detected"
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
	return checkServiceStatusAny(svcMap, hostname, "RUNNING", "ACTIVE")
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

// targetHostnames returns all runtime hostnames (dev + stage) for a target.
func targetHostnames(target workflow.BootstrapTarget) []string {
	hostnames := []string{target.Runtime.DevHostname}
	if stage := target.Runtime.StageHostname(); stage != "" {
		hostnames = append(hostnames, stage)
	}
	return hostnames
}

// isManagedNonStorage returns true for managed services that are NOT storage types.
func isManagedNonStorage(serviceType string) bool {
	lower := strings.ToLower(serviceType)
	if strings.HasPrefix(lower, "shared-storage") || strings.HasPrefix(lower, "object-storage") {
		return false
	}
	managedPrefixes := []string{
		"postgresql", "mariadb", "valkey",
		"keydb", "elasticsearch", "meilisearch", "rabbitmq", "kafka",
		"nats", "clickhouse", "qdrant", "typesense",
	}
	for _, prefix := range managedPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}
