package tools

import (
	"context"
	"errors"
	"fmt"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// apiCodeServiceStackIsNotHTTP is the platform's "this service stack is not
// HTTP-shaped" rejection on EnableSubdomainAccess. Returned for workers, for
// dev runtimes whose start command hasn't published a listening HTTP port
// yet (e.g. F8: `zsc noop` deferred dev-server start), and for any other
// stack the platform won't route via L7. In the auto-enable context this is
// not an error — just a "not for this service" signal that we silently
// swallow. In the EXPLICIT zerops_subdomain enable context this is a real
// diagnostic the user needs (their yaml is missing httpSupport: true), so
// ops.Subdomain.Enable still returns it as an error to that caller.
const apiCodeServiceStackIsNotHTTP = "serviceStackIsNotHttp"

// maybeAutoEnableSubdomain activates the L7 subdomain route for a freshly
// deployed runtime. Called from every deploy handler after the platform
// reports the deploy succeeded (result.Status == DEPLOYED).
//
// Design: let the platform answer "should this have a subdomain?" rather
// than predicting from local signals. The flow:
//
//  1. Mode allow-list: production / unknown modes opt out (modeAllowsSubdomain).
//  2. IsSystem() defensive guard: BUILD/CORE/INTERNAL/PREPARE_RUNTIME/
//     HTTP_L7_BALANCER stacks are never routed via L7. Five upstream filters
//     (discover, route, compute_envelope, adopt) make these unreachable on
//     normal codepaths, but the guard defends against future call sites.
//  3. ops.Subdomain.Enable does check-before-mutate internally (returns
//     SubdomainStatusAlreadyEnabled when subdomain is currently active) and
//     bounded retry on noSubdomainPorts (L7 propagation race).
//  4. Classify the response:
//     - success → set SubdomainAccessEnabled + URL, probe HTTP-ready.
//     - already_enabled → set SubdomainAccessEnabled + URL, skip probe
//     when ServiceMeta is supplied (bootstrapped earlier, route is live).
//     - serviceStackIsNotHttp → silent benign skip (worker, F8 zsc-noop,
//     any non-HTTP stack the platform refuses to route).
//     - other error → result.Warnings entry, deploy still succeeds.
//
// Why no DTO checks (the historical wrong path): the previous predicate read
// detail.SubdomainAccess and detail.Ports[].HTTPSupport as if they reflected
// import-yaml intent. Live verification (plan §2.2) proved both flip true
// only AFTER a successful EnableSubdomainAccess call. The platform DTO
// `ServicePort.HttpRouting` (mapped to ZCP's HTTPSupport in
// internal/platform/zerops_mappers.go) is the post-enable routing flag, NOT
// the deployed zerops.yaml's ports[].httpSupport intent — the SDK type at
// /Users/macbook/go/pkg/mod/github.com/zeropsio/zerops-go@v1.0.17/dto/output/
// servicePort.go has no httpSupport field at all. Reading these fields as
// pre-enable intent always returned false → predicate skipped enable → user
// had to call zerops_subdomain manually. Plan archive details the chain of
// fixes (R-13-12, R-14-1, F8) that all operated on this misunderstanding.
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

	subRes, err := ops.Subdomain(ctx, client, projectID, targetService, "enable")
	if err != nil {
		if isServiceStackIsNotHTTPErr(err) {
			// Platform: "service stack is not http or https". Worker, F8
			// zsc-noop deferred dev-server, or any other non-HTTP-shaped
			// stack. Benign signal in the auto-enable context — silently
			// swallow. Explicit zerops_subdomain enable callers still get
			// this error from ops.Subdomain (the downgrade is contextual).
			return
		}
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
	// Without ServiceMeta (recipe-authoring scaffolds, manual import,
	// adoption), an already_enabled status reflects the import-time state
	// without proving the L7 router has finished propagating ports
	// (R-14-1 race). Always probe in that case so the next zerops_verify
	// doesn't race.
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
// auto-enable.
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

// serviceEligibleForSubdomain decides whether to attempt auto-enable. Two
// cheap checks, no platform DTO inspection — the platform's response on
// the actual Enable call is the source of truth for "is this HTTP-shaped?".
//
//  1. Mode allow-list (when meta is supplied with a non-empty Mode).
//     Empty Mode or nil meta → permissive: pass through to the system
//     check. Recipe-authoring and manual-import paths are meta-less; their
//     intent is signaled by import yaml's enableSubdomainAccess: true and
//     verified by the Enable response (success or serviceStackIsNotHttp).
//
//  2. IsSystem() defensive guard via ops.LookupService (one ListServices
//     RT). Five upstream filters already keep system stacks off this code-
//     path (discover.go:101, route.go:210/238/276, compute_envelope.go:186,
//     adopt_local.go:75, workflow_adopt_local.go:91), but explicit-hostname
//     paths through FindService/GetService accept any input. The guard
//     defends against future call sites and maintains the invariant that
//     L7 routing is never auto-enabled on platform-internal stacks.
//
// Lookup failures soft-fail (returns false → caller skips auto-enable,
// agent's manual zerops_subdomain stays valid).
func serviceEligibleForSubdomain(
	ctx context.Context,
	client platform.Client,
	meta *workflow.ServiceMeta,
	projectID, targetService string,
) bool {
	if meta != nil && meta.Mode != "" && !modeAllowsSubdomain(meta.Mode) {
		return false
	}
	svc, err := ops.LookupService(ctx, client, projectID, targetService)
	if err != nil || svc == nil {
		return false
	}
	if svc.IsSystem() {
		return false
	}
	return true
}

// isServiceStackIsNotHTTPErr classifies a platform error as the
// "stack is not HTTP-shaped" rejection. Used by maybeAutoEnableSubdomain
// to swallow this specific signal silently — workers, F8 deferred dev-
// servers, and any non-HTTP stack land here without polluting result.Warnings.
//
// Lives in tools/ (caller-side), not in ops/, so ops.Subdomain.Enable stays
// honest: explicit zerops_subdomain enable callers still receive the error
// as a real diagnostic ("your yaml is missing httpSupport: true on the
// port"). The downgrade is contextual to auto-enable, not structural.
func isServiceStackIsNotHTTPErr(err error) bool {
	var pe *platform.PlatformError
	if !errors.As(err, &pe) {
		return false
	}
	return pe.APICode == apiCodeServiceStackIsNotHTTP
}
