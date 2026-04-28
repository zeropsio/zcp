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
// Eligibility is computed from one of two sources:
//
//   - **Meta present (bootstrap-managed services)** — meta.Mode is checked
//     against modeEligibleForSubdomain (dev/stage/simple/standard/
//     local-stage). Preserves the historical bootstrap path.
//   - **Meta absent (recipe-authoring services)** — Cluster A.2 / R-13-12 +
//     run-15 R-14-1 + run-16 R-15-1. Recipe-authoring deploys land via
//     zerops_import content=<yaml> and never write meta. Eligibility
//     derives from REST-authoritative service-stack state via two ORed
//     signals: detail.SubdomainAccess (end-user click-deploy path; set
//     by the platform when the imported deliverable yaml's
//     enableSubdomainAccess: true has actually provisioned a subdomain)
//     OR detail.Ports[].HTTPSupport (recipe-authoring path; workspace
//     yaml emits enableSubdomainAccess: true but the platform does not
//     flip detail.SubdomainAccess until first enable, so the deploy-
//     time port signal is the only intent visible during scaffold).
//     The R-14-1 propagation race the run-14 fallback ran into is
//     absorbed by ops.enableSubdomainAccessWithRetry's bounded backoff
//     on noSubdomainPorts. spec-workflows.md §4.8 + O3.
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
	if meta != nil {
		if !modeEligibleForSubdomain(meta.Mode) {
			return
		}
	} else if !platformEligibleForSubdomain(ctx, client, projectID, targetService) {
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

	// HTTP readiness wait. Skip on already_enabled FOR meta-present services
	// — they were bootstrapped earlier so the L7 route has been live for a
	// while; a probe just adds latency. For meta-nil (recipe-authoring) the
	// already_enabled status reflects the import-time intent flag (yaml had
	// enableSubdomainAccess: true); this may be the FIRST deploy of the
	// service and the L7 router may not have finished propagating ports
	// (R-14-1). Always probe so the next zerops_verify doesn't race.
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

// modeEligibleForSubdomain is the allow-list that decides whether a
// deploy should trigger subdomain auto-enable. Production and unknown
// modes default to no auto-enable — explicit opt-in via extending the
// switch when the mode lands. Dev/stage/simple/standard name live Zerops
// runtimes that serve HTTP; local-stage is the stage half of a local
// standard pair (the remote runtime that serves traffic). ModeLocalOnly
// has no remote runtime at all, so there is nothing to auto-enable.
func modeEligibleForSubdomain(mode topology.Mode) bool {
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

// platformEligibleForSubdomain is the meta-nil fallback predicate
// (Cluster A.2 + run-15 R-14-1, run-16 R-15-1). Recipe-authoring
// deploys never write ServiceMeta; eligibility derives from REST-
// authoritative service-stack state via two ORed signals.
//
// Signal 1 — detail.SubdomainAccess: end-user click-deploy path. The
// imported deliverable yaml carries enableSubdomainAccess: true AND
// the platform has provisioned a subdomain. Holds for end-user runs
// where the deliverable has already been imported + activated.
//
// Signal 2 — detail.Ports[].HTTPSupport: recipe-authoring path.
// Workspace yaml emits enableSubdomainAccess: true (yaml_emitter.go:
// 164/181) but the platform does NOT flip detail.SubdomainAccess
// from import alone; it stays false until first enable. So on every
// recipe-authoring first-deploy, signal 1 is false. Falling back to
// the deploy-time port signal (any port with httpSupport=true means
// the deployed zerops.yaml intends HTTP) auto-enables correctly for
// recipe-authoring while staying safe for workers (no httpSupport
// ports → returns false). The R-14-1 race the run-14 fix targeted
// is absorbed independently by ops.enableSubdomainAccessWithRetry's
// bounded backoff on noSubdomainPorts.
//
// Lookup failures soft-fail (caller treats as not-eligible and skips
// auto-enable; agent's manual zerops_subdomain stays valid).
func platformEligibleForSubdomain(
	ctx context.Context,
	client platform.Client,
	projectID, targetService string,
) bool {
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
