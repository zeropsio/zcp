package tools

import (
	"context"
	"fmt"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// maybeAutoEnableSubdomain activates the L7 subdomain route for a freshly
// deployed runtime when the service is eligible. Best-effort: every
// failure path appends to result.Warnings and lets the deploy succeed — if
// auto-enable is skipped or fails, the agent's manual zerops_subdomain
// action=enable call remains a valid recovery path.
//
// Eligibility is a SINGLE predicate call to serviceEligibleForSubdomain
// (no upstream branching on meta presence). The predicate consumes the
// optional ServiceMeta as one input among several, and ALWAYS consults
// the live HTTP-route signal (detail.SubdomainAccess OR any
// detail.Ports[].HTTPSupport). This closes the F8 asymmetry: a
// dev+dynamic service with `zsc noop` start (no HTTP listener, no port
// flagged HTTPSupport) used to slip past the pre-fix mode-only check,
// then the platform rejected enable with "Service stack is not http or
// https". The unified predicate matches the platform contract — no
// stack-shape mismatch surfaces.
//
// Gate is platform-side via the ops.Subdomain call's internal
// check-before-enable (plans/archive/subdomain-robustness.md §3.2), NOT
// meta-side (FirstDeployedAt). meta.FirstDeployedAt is pair-keyed
// (invariant E8) and unusable for stage cross-deploy detection — the dev
// half's stamp covers both hostnames, but the stage container's L7 route
// is independent.
//
// I/O boundary: ops.LookupService and client.GetService are REST round
// trips; SubdomainAccess is set at yaml-import time, so reading it pre-
// first-deploy returns the plan-declared value (not racing with deploy-
// time port propagation). workflow.FindServiceMeta reads the per-PID
// local state dir (in-process consistency).
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
	if !serviceEligibleForSubdomain(ctx, client, meta, projectID, targetService) {
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

	// HTTP readiness wait. Skip on already_enabled when ServiceMeta is
	// supplied — meta presence proves the service was bootstrapped earlier,
	// so the L7 route has been live for a while; a probe just adds latency.
	// Without ServiceMeta (recipe-authoring scaffolds), an already_enabled
	// status reflects the import-time intent flag (yaml had
	// enableSubdomainAccess: true) without proving the L7 router has finished
	// propagating ports (R-14-1). Always probe in that case so the next
	// zerops_verify doesn't race.
	skipProbe := subRes.Status == ops.SubdomainStatusAlreadyEnabled && meta != nil
	if !skipProbe {
		for _, url := range subRes.SubdomainUrls {
			if waitErr := ops.WaitHTTPReady(ctx, httpClient, url); waitErr != nil {
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("subdomain %s not HTTP-ready: %v (next zerops_verify may need to retry)", url, waitErr))
			}
		}
	}
}

// modeAllowsSubdomain is the topology-side guard: production and unknown
// modes default to no auto-enable — explicit opt-in via extending the
// switch when a new mode lands. Dev/stage/simple/standard name live
// Zerops runtimes that serve HTTP; local-stage is the stage half of a
// local standard pair (the remote runtime that serves traffic).
// ModeLocalOnly has no remote runtime at all, so there is nothing to
// auto-enable. The unified predicate AND-combines this with a live-port
// HTTP signal — the platform is the source of truth on whether the
// service stack is HTTP-shaped.
func modeAllowsSubdomain(mode topology.Mode) bool {
	switch mode {
	case topology.PlanModeDev,
		topology.PlanModeStandard,
		topology.ModeStage,
		topology.PlanModeSimple,
		topology.PlanModeLocalStage:
		return true
	case topology.ModeLocalOnly:
		return false
	}
	return false
}

// serviceEligibleForSubdomain is the post-deploy auto-enable predicate.
// One function, no branching on meta presence at the caller — the
// optional ServiceMeta is just one of several inputs. Eligibility holds
// when the platform-side service stack carries an HTTP route AND (when
// meta is supplied) the topology mode is in the allow-list.
//
// Live HTTP signal sources (ORed):
//
//   - detail.SubdomainAccess — set by the platform when the imported
//     deliverable yaml's enableSubdomainAccess: true has actually
//     provisioned a subdomain. End-user click-deploy path.
//   - detail.Ports[].HTTPSupport — set per port from zerops.yaml
//     run.ports[].httpSupport. Recipe-authoring path: workspace yaml
//     emits enableSubdomainAccess: true but the platform doesn't flip
//     detail.SubdomainAccess until first enable, so the deploy-time
//     port signal is the only intent visible during scaffold. Workers
//     with no httpSupport ports stay correctly false.
//
// F8 closure: a dev+dynamic service whose zerops.yaml `start` is `zsc
// noop` (deferred dev-server start) has no port flagged HTTPSupport
// at deploy time. The old mode-only predicate triggered enable, the
// platform rejected with "Service stack is not http or https", and
// the agent saw a confusing warning. Now we skip silently — the
// deferred hint pattern (run zerops_dev_server action=start, then a
// zerops_subdomain action=enable will succeed) is the agent's normal
// recovery path.
//
// Lookup failures soft-fail (returns false → caller skips auto-enable,
// agent's manual zerops_subdomain stays valid).
func serviceEligibleForSubdomain(
	ctx context.Context,
	client platform.Client,
	meta *workflow.ServiceMeta,
	projectID, targetService string,
) bool {
	if meta != nil && !modeAllowsSubdomain(meta.Mode) {
		return false
	}
	svc, err := ops.LookupService(ctx, client, projectID, targetService)
	if err != nil || svc == nil {
		return false
	}
	if svc.IsSystem() {
		return false
	}
	detail, err := client.GetService(ctx, svc.ID)
	if err != nil || detail == nil {
		return false
	}
	if detail.SubdomainAccess {
		return true
	}
	for _, port := range detail.Ports {
		if port.HTTPSupport {
			return true
		}
	}
	return false
}
