package tools

import (
	"context"
	"fmt"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// maybeAutoEnableSubdomain activates the L7 subdomain route for a freshly
// deployed runtime when the service is eligible (dev/stage/simple/standard/
// local-stage mode, subdomain currently disabled). Best-effort: every
// failure path appends to result.Warnings and lets the deploy succeed — if
// auto-enable is skipped or fails, the agent's manual zerops_subdomain
// action=enable call remains a valid recovery path.
//
// Gate is platform-side via the ops.Subdomain call's internal
// check-before-enable (plans/archive/subdomain-robustness.md §3.2), NOT
// meta-side (FirstDeployedAt). meta.FirstDeployedAt is pair-keyed
// (invariant E8) and unusable for stage cross-deploy detection — the dev
// half's stamp covers both hostnames, but the stage container's L7 route
// is independent.
//
// HTTP readiness wait fires only on fresh enable (status != "already_enabled").
// Re-deploys on already-enabled subdomains skip the probe — the platform
// state is authoritative and the URL is already reachable.
func maybeAutoEnableSubdomain(
	ctx context.Context,
	client platform.Client,
	httpClient ops.HTTPDoer,
	projectID, stateDir string,
	targetService string,
	result *ops.DeployResult,
) {
	meta, _ := workflow.FindServiceMeta(stateDir, targetService)
	if meta == nil {
		// Not ZCP-managed (managed services have no meta per spec E6;
		// agent-owned services without bootstrap also absent). Skip.
		return
	}
	if !modeEligibleForSubdomain(meta.Mode) {
		return
	}

	// ops.Subdomain does check-before-enable internally: it returns
	// status=already_enabled without touching the platform API when the
	// subdomain is already active. On fresh enable it calls the API and
	// populates URLs. Warnings (FailReason, poll timeouts, etc.) get
	// surfaced up the stack.
	subRes, err := ops.Subdomain(ctx, client, projectID, targetService, "enable")
	if err != nil {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("auto-enable subdomain failed: %v (run zerops_subdomain action=enable manually)", err))
		return
	}

	result.SubdomainAccessEnabled = true
	if len(subRes.SubdomainUrls) > 0 {
		result.SubdomainURL = subRes.SubdomainUrls[0]
	}
	for _, w := range subRes.Warnings {
		result.Warnings = append(result.Warnings, "subdomain: "+w)
	}

	// HTTP readiness wait only on fresh enable. On already_enabled the L7
	// route has been live since the earlier enable; a probe would just add
	// latency for no signal.
	if subRes.Status != ops.SubdomainStatusAlreadyEnabled {
		for _, url := range subRes.SubdomainUrls {
			if waitErr := ops.WaitHTTPReady(ctx, httpClient, url); waitErr != nil {
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("subdomain %s not HTTP-ready: %v (next zerops_verify may need to retry)", url, waitErr))
			}
		}
	}
}

// modeEligibleForSubdomain is the allow-list that decides whether a
// deploy should trigger subdomain auto-enable. Production and unknown
// modes default to no auto-enable — explicit opt-in via extending the
// switch when the mode lands. Dev/stage/simple/standard name live Zerops
// runtimes that serve HTTP; local-stage is the stage half of a local
// standard pair (the remote runtime that serves traffic). ModeLocalOnly
// has no remote runtime at all, so there is nothing to auto-enable.
func modeEligibleForSubdomain(mode workflow.Mode) bool {
	switch mode {
	case workflow.PlanModeDev,
		workflow.PlanModeStandard,
		workflow.ModeStage,
		workflow.PlanModeSimple,
		workflow.PlanModeLocalStage:
		return true
	case workflow.ModeLocalOnly:
		return false
	}
	return false
}
