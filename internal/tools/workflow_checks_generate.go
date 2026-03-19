package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/workflow"
)

// checkGenerate validates the generate step by checking zerops.yml quality.
// It verifies: existence, hostname match, env ref validity, port presence, and deployFiles.
func checkGenerate(stateDir string) workflow.StepChecker {
	return func(_ context.Context, plan *workflow.ServicePlan, state *workflow.BootstrapState) (*workflow.StepCheckResult, error) {
		if plan == nil {
			return nil, nil
		}

		// Derive project root from stateDir ({projectRoot}/.zcp/state/).
		projectRoot := filepath.Dir(filepath.Dir(stateDir))

		var checks []workflow.StepCheck

		// Check each target's zerops.yml. Agent writes to SSHFS mount at
		// /var/www/{hostname}/, so look there first. Fall back to project root
		// for local/test environments where files are written directly.
		for _, target := range plan.Targets {
			hostname := target.Runtime.DevHostname
			mountPath := filepath.Join(projectRoot, hostname)

			ymlDir := projectRoot
			if info, statErr := os.Stat(mountPath); statErr == nil && info.IsDir() {
				ymlDir = mountPath
			}

			doc, parseErr := ops.ParseZeropsYml(ymlDir)
			if parseErr != nil {
				checks = append(checks, workflow.StepCheck{
					Name: "zerops_yml_exists", Status: statusFail,
					Detail: fmt.Sprintf("zerops.yml not found at %s or %s: %v", mountPath, projectRoot, parseErr),
				})
				continue
			}
			checks = append(checks, workflow.StepCheck{
				Name: "zerops_yml_exists", Status: statusPass,
			})
			checks = append(checks, checkGenerateEntry(doc, hostname, target, state)...)
		}

		allPassed := true
		for i := range checks {
			if checks[i].Status == statusFail {
				allPassed = false
				break
			}
		}
		summary := "generate checks passed"
		if !allPassed {
			summary = "generate checks failed"
		}
		return &workflow.StepCheckResult{
			Passed: allPassed, Checks: checks, Summary: summary,
		}, nil
	}
}

// checkGenerateEntry validates a single hostname's zerops.yml entry.
func checkGenerateEntry(doc *ops.ZeropsYmlDoc, hostname string, target workflow.BootstrapTarget, state *workflow.BootstrapState) []workflow.StepCheck {
	entry := doc.FindEntry(hostname)
	if entry == nil {
		return []workflow.StepCheck{{
			Name: hostname + "_setup", Status: statusFail,
			Detail: fmt.Sprintf("no setup entry for %q in zerops.yml", hostname),
		}}
	}

	var checks []workflow.StepCheck
	checks = append(checks, workflow.StepCheck{
		Name: hostname + "_setup", Status: statusPass,
	})

	// Env ref validation.
	if len(entry.EnvVariables) > 0 && state != nil {
		liveHostnames := collectPlanHostnames(state)
		envErrs := ops.ValidateEnvReferences(entry.EnvVariables, state.DiscoveredEnvVars, liveHostnames)
		if len(envErrs) > 0 {
			details := make([]string, len(envErrs))
			for i, e := range envErrs {
				details[i] = fmt.Sprintf("%s: %s", e.Reference, e.Reason)
			}
			checks = append(checks, workflow.StepCheck{
				Name: hostname + "_env_refs", Status: statusFail,
				Detail: strings.Join(details, "; "),
			})
		} else {
			checks = append(checks, workflow.StepCheck{
				Name: hostname + "_env_refs", Status: statusPass,
			})
		}
	}

	// Port presence.
	if entry.HasPorts() {
		checks = append(checks, workflow.StepCheck{
			Name: hostname + "_ports", Status: statusPass,
		})
	} else {
		checks = append(checks, workflow.StepCheck{
			Name: hostname + "_ports", Status: statusFail,
			Detail: "no run.ports defined — service will not accept traffic",
		})
	}

	// DeployFiles sanity.
	if entry.HasDeployFiles() {
		checks = append(checks, workflow.StepCheck{
			Name: hostname + "_deploy_files", Status: statusPass,
		})
	} else {
		checks = append(checks, workflow.StepCheck{
			Name: hostname + "_deploy_files", Status: statusFail,
			Detail: "build.deployFiles is empty — nothing will be deployed",
		})
	}

	// HealthCheck required for simple mode (unless implicit web server).
	if target.Runtime.EffectiveMode() == workflow.PlanModeSimple && !entry.HasImplicitWebServer() {
		if entry.Run.HealthCheck != nil {
			checks = append(checks, workflow.StepCheck{
				Name: hostname + "_health_check", Status: statusPass,
			})
		} else {
			checks = append(checks, workflow.StepCheck{
				Name:   hostname + "_health_check",
				Status: statusFail,
				Detail: "simple mode requires run.healthCheck — add httpGet or exec health check so Zerops can verify the service is ready",
			})
		}
	}

	return checks
}

// collectPlanHostnames extracts all hostnames from the bootstrap plan.
func collectPlanHostnames(state *workflow.BootstrapState) []string {
	if state == nil || state.Plan == nil {
		return nil
	}
	var hostnames []string
	for _, target := range state.Plan.Targets {
		hostnames = append(hostnames, target.Runtime.DevHostname)
		if stage := target.Runtime.StageHostname(); stage != "" {
			hostnames = append(hostnames, stage)
		}
		for _, dep := range target.Dependencies {
			hostnames = append(hostnames, dep.Hostname)
		}
	}
	return hostnames
}
