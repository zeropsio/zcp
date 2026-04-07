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

// buildCommandPrefixes are command prefixes that belong in build, not run.start.
var buildCommandPrefixes = []string{
	"npm install", "pip install", "go build", "cargo build",
	"mvn package", "gradle build",
}

// checkGenerate validates the generate step by checking zerops.yaml quality.
// It verifies: existence, hostname match, env ref validity, port presence, and deployFiles.
func checkGenerate(stateDir string) workflow.StepChecker {
	return func(_ context.Context, plan *workflow.ServicePlan, state *workflow.BootstrapState) (*workflow.StepCheckResult, error) {
		if plan == nil {
			return nil, nil
		}

		// Derive project root from stateDir ({projectRoot}/.zcp/state/).
		projectRoot := projectRootFromState(stateDir)

		var checks []workflow.StepCheck

		// Check each target's zerops.yaml. Agent writes to SSHFS mount at
		// /var/www/{hostname}/, so look there first. Fall back to project root
		// for local/test environments where files are written directly.
		for _, target := range plan.Targets {
			// Skip validation for adopted (pre-existing) services — they keep their own code+config.
			if target.Runtime.IsExisting {
				continue
			}
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
					Detail: fmt.Sprintf("zerops.yaml not found at %s or %s: %v", mountPath, projectRoot, parseErr),
				})
				continue
			}
			checks = append(checks, workflow.StepCheck{
				Name: "zerops_yml_exists", Status: statusPass,
			})
			checks = append(checks, checkZeropsYmlSize(ymlDir)...)
			checks = append(checks, checkGenerateEntry(doc, hostname, target, state)...)
		}

		allPassed := checksAllPassed(checks)
		summary := "generate checks passed"
		if !allPassed {
			summary = "generate checks failed"
		}
		return &workflow.StepCheckResult{
			Passed: allPassed, Checks: checks, Summary: summary,
		}, nil
	}
}

// checkGenerateEntry validates a single hostname's zerops.yaml entry.
// Expects canonical recipe setup names: "dev" for dev/standard targets, "prod" for simple targets.
func checkGenerateEntry(doc *ops.ZeropsYmlDoc, hostname string, target workflow.BootstrapTarget, state *workflow.BootstrapState) []workflow.StepCheck {
	expected := workflow.RecipeSetupForMode(target.Runtime.EffectiveMode())
	entry := doc.FindEntry(expected)
	if entry == nil {
		return []workflow.StepCheck{{
			Name: hostname + "_setup", Status: statusFail,
			Detail: fmt.Sprintf("zerops.yaml must have setup: %q (canonical recipe name), found none — do NOT use hostname as setup name", expected),
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

	// Implicit web server: check both zerops.yaml bases AND service type from plan.
	implicitWS := entry.HasImplicitWebServer() || ops.IsImplicitWebServerType(target.Runtime.Type)

	// Port presence (skip for implicit web server — port 80 is fixed).
	if implicitWS || entry.HasPorts() {
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
	if target.Runtime.EffectiveMode() == workflow.PlanModeSimple && !implicitWS {
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

	// run.start required for dynamic runtimes (no implicit web server).
	if !implicitWS {
		if entry.Run.Start == "" {
			checks = append(checks, workflow.StepCheck{
				Name:   hostname + "_run_start",
				Status: statusFail,
				Detail: "run.start is empty — app will not start after deploy (required for this runtime)",
			})
		} else {
			checks = append(checks, workflow.StepCheck{
				Name: hostname + "_run_start", Status: statusPass,
			})
		}
	}

	// run.start must not be a build command.
	if entry.Run.Start != "" {
		startLower := strings.ToLower(entry.Run.Start)
		for _, prefix := range buildCommandPrefixes {
			if strings.HasPrefix(startLower, prefix) {
				checks = append(checks, workflow.StepCheck{
					Name:   hostname + "_run_start_build_cmd",
					Status: statusFail,
					Detail: fmt.Sprintf("run.start %q looks like a build command — move it to build.buildCommands", entry.Run.Start),
				})
				break
			}
		}
	}

	// run.prepareCommands must not reference /var/www (deploy files arrive AFTER prepare).
	if cmds := stringifyCommands(entry.Run.PrepareCommands); cmds != "" {
		if strings.Contains(cmds, "/var/www") {
			checks = append(checks, workflow.StepCheck{
				Name:   hostname + "_prepare_varwww",
				Status: statusFail,
				Detail: "run.prepareCommands references /var/www — deploy files arrive AFTER prepareCommands. Use addToRunPrepare + /home/zerops/ instead.",
			})
		}
		// Package install commands need sudo — containers run as zerops user.
		if ops.HasPkgInstallWithoutSudo(entry.Run.PrepareCommands) {
			checks = append(checks, workflow.StepCheck{
				Name:   hostname + "_prepare_missing_sudo",
				Status: statusFail,
				Detail: "run.prepareCommands has package install commands without sudo (apk add / apt-get install). Containers run as zerops user — prefix with sudo.",
			})
		}
	}

	// build.base must not be a webserver variant (php-nginx, php-apache).
	for _, base := range entry.Build.BaseStrings() {
		baseLower := strings.ToLower(base)
		if strings.Contains(baseLower, "php-nginx") || strings.Contains(baseLower, "php-apache") {
			checks = append(checks, workflow.StepCheck{
				Name:   hostname + "_build_base_webserver",
				Status: statusFail,
				Detail: fmt.Sprintf("build.base %q is a webserver variant (run base only). Use build.base: php@X, run.base: %s", base, base),
			})
			break
		}
	}

	// Dev and standard mode services need deployFiles: [.] for full source iteration.
	mode := target.Runtime.EffectiveMode()
	if mode == workflow.PlanModeStandard || mode == workflow.PlanModeDev {
		if deployFilesContainsDot(entry.Build.DeployFiles) {
			checks = append(checks, workflow.StepCheck{
				Name: hostname + "_dev_deploy_files", Status: statusPass,
			})
		} else {
			checks = append(checks, workflow.StepCheck{
				Name:   hostname + "_dev_deploy_files",
				Status: statusFail,
				Detail: "dev service should use deployFiles: [.] — ensures source files persist across deploys for iteration",
			})
		}
	}

	return checks
}

// deployFilesContainsDot checks if deployFiles includes "." or "./".
func deployFilesContainsDot(deployFiles any) bool {
	switch v := deployFiles.(type) {
	case string:
		return v == "." || v == "./"
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok && (s == "." || s == "./") {
				return true
			}
		}
	}
	return false
}

// stringifyCommands converts a YAML commands field (string or []string) to a single string for pattern matching.
func stringifyCommands(commands any) string {
	switch v := commands.(type) {
	case string:
		return v
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, " ")
	default:
		return ""
	}
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
