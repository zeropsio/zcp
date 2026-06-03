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

func buildStepChecker(step string, client platform.Client, fetcher platform.LogFetcher, projectID string, _ ops.HTTPDoer, engine *workflow.Engine, _ string) workflow.StepChecker {
	if step == stepProvision {
		return checkProvision(client, fetcher, projectID, engine)
	}
	// discover and close steps have nil checkers (attestation-only triggers
	// under Option A — bootstrap owns infra provisioning, not deploy).
	return nil
}

func checkProvision(client platform.Client, fetcher platform.LogFetcher, projectID string, engine *workflow.Engine) workflow.StepChecker {
	return func(ctx context.Context, plan *workflow.ServicePlan, bs *workflow.BootstrapState) (*workflow.StepCheckResult, error) {
		if plan == nil || len(plan.Targets) == 0 {
			return nil, nil
		}

		services, err := ops.ListProjectServices(ctx, client, projectID)
		if err != nil {
			return nil, fmt.Errorf("list services: %w", err)
		}
		svcMap := make(map[string]platform.ServiceStack, len(services))
		statusMap := make(map[string]string, len(services))
		for _, svc := range services {
			svcMap[svc.Name] = svc
			statusMap[svc.Name] = svc.Status
		}

		// Local-recipe path (Theme 1): the recipe match-derived plan
		// still names DevHostname (e.g. "appdev") because that's what
		// the recipe declares. The provision YAML is localized — appdev
		// is dropped on its way to zerops_import — so the dev runtime
		// never lands on Zerops, and a check that requires it would
		// always fail here. Surfaced by flow-eval-local
		// recipe-nodejs-hello-world suite 20260507-130710. Skip
		// dev-existence + dev-type checks in this case; stage takes
		// the type-check responsibility.
		skipDevCheck := false
		if engine != nil && engine.Environment() == workflow.EnvLocal {
			if bs != nil && bs.Route == workflow.BootstrapRouteRecipe {
				skipDevCheck = true
			}
		}

		var checks []workflow.StepCheck
		allPassed := true

		for _, target := range plan.Targets {
			// Check dev runtime exists and is RUNNING. Skipped in local-
			// mode + recipe route (see skipDevCheck rationale above) and
			// when DevHostname is empty (other local-mode shapes).
			if !skipDevCheck && target.Runtime.DevHostname != "" {
				checks = append(checks, checkServiceRunning(ctx, client, fetcher, projectID, svcMap, target.Runtime.DevHostname)...)
				checks = append(checks, checkServiceType(svcMap, target.Runtime.DevHostname, target.Runtime.Type)...)
			}

			// Check stage runtime exists in any alive status.
			// Stage may be newly imported (NEW/READY_TO_DEPLOY) or already running (RUNNING/ACTIVE).
			// Mixed cases (existing dev + new stage) are valid for adoption scenarios.
			if stage := target.Runtime.StageHostname(); stage != "" {
				checks = append(checks, checkServiceStatusAny(ctx, client, fetcher, projectID, svcMap, stage, serviceStatusNew, serviceStatusReadyToDeploy, serviceStatusRunning, serviceStatusActive)...)
				// When the dev runtime was skipped (local-recipe) or is
				// absent, stage MUST carry the type-check responsibility.
				if skipDevCheck || target.Runtime.DevHostname == "" {
					checks = append(checks, checkServiceType(svcMap, stage, target.Runtime.Type)...)
				}
			}

			// Check dependencies.
			for _, dep := range target.Dependencies {
				checks = append(checks, checkServiceRunning(ctx, client, fetcher, projectID, svcMap, dep.Hostname)...)

				// Cross-check dependency type matches plan.
				checks = append(checks, checkServiceType(svcMap, dep.Hostname, dep.Type)...)

				// Managed (non-storage) dependencies with resolution CREATE or EXISTS must have env vars.
				if (dep.Resolution == "CREATE" || dep.Resolution == "EXISTS") && isManagedNonStorage(dep.Type) {
					svc, exists := svcMap[dep.Hostname]
					if !exists {
						continue
					}
					envVars, envErr := ops.FetchServiceEnv(ctx, client, svc.ID)
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

		// Persist the per-hostname Status snapshot so synthesisEnvelope can
		// populate ServiceSnapshot.Status during bootstrap-active phase. Atoms
		// gated on serviceStatus (e.g. develop-ready-to-deploy.md gated on
		// READY_TO_DEPLOY) need this to fire — without it Status="" and the
		// gating never matches. Failure here is non-fatal: provision can still
		// proceed, the snapshot just doesn't carry Status. Fix per
		// plans/eval-review-20260518-subset/fix-plan.md Phase 2.1.
		if engine != nil {
			if storeErr := engine.StoreDiscoveredStatuses(statusMap); storeErr != nil {
				checks = append(checks, workflow.StepCheck{
					Name:   "_status_persist",
					Status: statusFail,
					Detail: fmt.Sprintf("persist discovered statuses: %v", storeErr),
				})
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
func checkServiceRunning(ctx context.Context, client platform.Client, fetcher platform.LogFetcher, projectID string, svcMap map[string]platform.ServiceStack, hostname string) []workflow.StepCheck {
	return checkServiceStatusAny(ctx, client, fetcher, projectID, svcMap, hostname, serviceStatusRunning, serviceStatusActive)
}

// checkServiceStatusAny checks a service exists with any of the expected
// statuses. On rejection by status, attaches a structured Recovery hint via
// ops.NonRunningRecovery so the agent has an explicit next-tool pointer for
// non-running terminal states (FAILED → events; READY_TO_DEPLOY with failed
// history → import override; READY_TO_DEPLOY clean → logs). Plan v4 §1.4.
func checkServiceStatusAny(ctx context.Context, client platform.Client, fetcher platform.LogFetcher, projectID string, svcMap map[string]platform.ServiceStack, hostname string, statuses ...string) []workflow.StepCheck {
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
	check := workflow.StepCheck{
		Name:   hostname + "_status",
		Status: statusFail,
		Detail: fmt.Sprintf("expected one of [%s], got %s", strings.Join(statuses, ", "), svc.Status),
	}
	check.Recovery = ops.NonRunningRecovery(ctx, client, fetcher, projectID, hostname, svc.Status)
	return []workflow.StepCheck{check}
}

// checkServiceType verifies a service's API type matches the plan type.
//
// Comparison is type-equivalence (topology.TypesAreEquivalent), not byte
// equality — Sunday-release 2026-05-18 moved Zerops upstream identifiers to
// composite form (`alpine/php-nginx@8.4`, `postgresql:single@18`) while the
// plan-side may still carry legacy bare form (`php-nginx@8.4`,
// `postgresql@18`). Both must accept.
func checkServiceType(svcMap map[string]platform.ServiceStack, hostname, expectedType string) []workflow.StepCheck {
	svc, exists := svcMap[hostname]
	if !exists {
		return nil // missing service is caught by checkServiceRunning
	}
	actual := svc.ServiceStackTypeInfo.ServiceStackTypeVersionName
	if actual == "" || topology.TypesAreEquivalent(actual, expectedType) {
		return nil // exact / OS-mode-equivalent match — pass silently (as before)
	}
	// Not byte/form-equivalent. The agent may have planned a version-family
	// SELECTOR (go@1, @latest) that the platform RESOLVED to a concrete patch
	// at import (go@1.22). That is NOT a mismatch — the platform owns version
	// resolution. Accept and REPORT the resolution so the agent learns the
	// concrete type that was actually created. A genuine mismatch (different
	// base, or a concrete-leaf plan that differs from live — e.g. nodejs@22 vs
	// nodejs@24) still fails.
	if isPlatformResolution(expectedType, actual) {
		return []workflow.StepCheck{{
			Name:   hostname + "_type",
			Status: statusPass,
			Detail: fmt.Sprintf("%s resolved to %s (platform-selected concrete version)", expectedType, actual),
		}}
	}
	return []workflow.StepCheck{{
		Name:   hostname + "_type",
		Status: statusFail,
		Detail: fmt.Sprintf("expected %s, got %s", expectedType, actual),
	}}
}

// isPlatformResolution reports whether `live` is a plausible platform resolution
// of the planned selector `planned`: same canonical base, and the planned
// version is either empty (bare base), a rolling tag (latest/canary/nightly/
// stable → any concrete), or a strict dot-component family prefix of the live
// version (go@1 → go@1.22, bun@1.3 → bun@1.3.9). It deliberately does NOT accept
// a same-base concrete-leaf mismatch (nodejs@22 vs nodejs@24): a concrete plan
// the platform did not transform must match exactly. Provision-scoped — never
// used for catalog existence (which stays strict via TypesAreEquivalent).
// versionTagDev is the rolling "dev" VERSION selector (e.g. `go@dev`) — a
// platform-resolved tag, semantically distinct from launch-production's
// `setupNameDev` (a zerops.yaml setup-block name). Named so goconst treats
// this occurrence as the version-tag concept, not a use of setupNameDev.
const versionTagDev = "dev"

func isPlatformResolution(planned, live string) bool {
	if topology.CanonicalBaseName(planned) != topology.CanonicalBaseName(live) {
		return false
	}
	pv := versionPart(planned)
	if pv == "" {
		return true
	}
	switch strings.ToLower(pv) {
	case "latest", "canary", "nightly", "stable", "edge", versionTagDev:
		return true
	}
	return isDotComponentPrefix(pv, versionPart(live))
}

// versionPart returns the version after '@' in a type's canonical bare form
// (OS prefix + mode suffix stripped), or "" when versionless.
func versionPart(t string) string {
	bare := topology.CanonicalBareForm(t)
	_, ver, ok := strings.Cut(bare, "@")
	if !ok {
		return ""
	}
	return ver
}

// isDotComponentPrefix reports whether a is a STRICT dot-component prefix of b
// ("1" ⊂ "1.22", "1.3" ⊂ "1.3.9"; "22" is NOT a prefix of "24").
func isDotComponentPrefix(a, b string) bool {
	ap, bp := strings.Split(a, "."), strings.Split(b, ".")
	if len(ap) >= len(bp) {
		return false
	}
	for i := range ap {
		if ap[i] != bp[i] {
			return false
		}
	}
	return true
}

// isManagedNonStorage returns true for managed services that are NOT storage types.
// Delegates entirely to topology predicates (which recognize every storage
// spelling — object-storage/shared-storage incl. no-hyphen + seaweedfs) so
// storage, which produces no env vars, is excluded regardless of spelling.
func isManagedNonStorage(serviceType string) bool {
	if topology.IsObjectStorageType(serviceType) || topology.IsSharedStorageType(serviceType) {
		return false
	}
	return topology.IsManagedService(serviceType)
}
