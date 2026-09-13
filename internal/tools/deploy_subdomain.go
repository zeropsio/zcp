package tools

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// apiCodeServiceStackIsNotHTTP is the platform's "this service stack is not
// HTTP-shaped" rejection on EnableSubdomainAccess. Returned for workers, for
// dev runtimes whose container has no listening HTTP port yet (F8 deferred
// dev-server start — dynamic dev setups idle on the `zsc noop --silent`
// keepalive, so no app process exists until `zerops_dev_server action=start`
// runs), and for any other stack the
// platform won't route via L7. In the auto-enable context this is not an
// error — just a "not for this service" signal that we silently swallow. In
// the EXPLICIT zerops_subdomain enable context this is a real diagnostic the
// user needs (their yaml is missing httpSupport: true), so
// ops.Subdomain.Enable still returns it as an error to that caller.
const apiCodeServiceStackIsNotHTTP = "serviceStackIsNotHttp"

// ensurePublicAccess is the O3/E9 hook (docs/spec-workflows.md §8 O3 PA-1..
// PA-3, E9): auto-enable the subdomain at most once per runtime, only when
// intent allows it, and record what actually happened in ServiceMeta so a
// later call never re-enables a route the user switched off. Decided purely
// from mode+IsSystem and calling ops.Subdomain unconditionally used to be
// the whole predicate; this version reads the persisted intent
// (ServiceMeta.PublicAccess) and the live observation
// (ops.ObservePublicAccess) and only proceeds when
// topology.ShouldAutoEnableSubdomain agrees.
//
// listenerOverride is variadic so the deploy hooks (deploy_local.go,
// deploy_ssh.go, deploy_batch.go, workflow_record_deploy.go) need no change
// beyond the identifier rename: when omitted, "is a listener up now" is
// derived from the runtime's static deferred-start classification — a
// dev-mode dynamic runtime idling on `zsc noop --silent` has none until
// `zerops_dev_server action=start` runs, so the deploy hook correctly does
// NOT auto-enable there (PA-1's first hook only fires when a listener
// already exists). The dev-server-start hook passes true explicitly — a
// passing health probe just proved a live listener regardless of that
// static classification, which is what fires PA-1's second hook.
func ensurePublicAccess(
	ctx context.Context,
	client platform.Client,
	httpClient ops.HTTPDoer,
	projectID, stateDir, hostname string,
	result *ops.DeployResult,
	listenerOverride ...bool,
) {
	meta, _ := workflow.FindServiceMeta(stateDir, hostname)
	svc, eligible := serviceEligibleForSubdomain(ctx, client, meta, projectID, hostname)
	if !eligible {
		return
	}

	deferredStart := isDeferredStartTarget(meta, svc, hostname)
	listener := !deferredStart
	if len(listenerOverride) > 0 {
		listener = listenerOverride[0]
	}

	obs, err := ops.ObservePublicAccess(ctx, client, projectID, svc)
	if err != nil {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("observe public access failed: %v (subdomain state unknown)", err))
		return
	}
	obs.Observed.Listener = listener

	rec := meta.PublicAccessFor(hostname)
	rec2, changed := topology.ReconcilePublicAccess(rec, obs.Observed)
	if changed && meta != nil {
		persistPublicAccess(stateDir, hostname, rec2)
	}

	enabledNow := false
	if topology.ShouldAutoEnableSubdomain(rec2, obs.Observed) {
		subRes, enableErr := ops.Subdomain(ctx, client, projectID, hostname, "enable")
		switch {
		case enableErr == nil:
			rec2.SubdomainEnabledByZcpAt = time.Now().UTC().Format(time.RFC3339)
			if meta != nil {
				persistPublicAccess(stateDir, hostname, rec2)
			}
			result.SubdomainAccessEnabled = true
			// Pick the HTTP-serving port's URL by scheme — multi-port
			// services (e.g. mailpit: SMTP 1025 + HTTP UI 8025) must not
			// report a non-HTTP port as the URL. svc here is the
			// pre-enable snapshot (SubdomainAccess still false), so
			// ResolveSubdomainURL is a no-op belt-and-suspenders read;
			// the real value comes from subRes (built fresh inside
			// ops.Subdomain via GetService).
			httpURL := ops.ResolveSubdomainURL(ctx, client, projectID, svc)
			if httpURL == "" && len(subRes.SubdomainUrls) > 0 {
				httpURL = subRes.SubdomainUrls[0]
			}
			result.SubdomainURL = httpURL
			for _, w := range subRes.Warnings {
				result.Warnings = append(result.Warnings, "subdomain: "+w)
			}
			enabledNow = true
		case isServiceStackIsNotHTTPErr(enableErr):
			// Platform: "service stack is not http or https" — benign in
			// this context (worker, deferred-start dev runtime not yet
			// started, any other non-HTTP-shaped stack). No stamp, no
			// warning — see isServiceStackIsNotHTTPErr doc.
		default:
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("auto-enable subdomain failed: %v (run zerops_subdomain action=enable manually)", enableErr))
		}
	} else if obs.Observed.Subdomain == topology.SubdomainOn {
		// Not eligible for a fresh auto-enable (already stamped, custom
		// domain, or simply already on) but the route IS live — surface
		// it so the deploy result stays accurate.
		result.SubdomainAccessEnabled = true
		result.SubdomainURL = obs.URL
	}

	// HTTP readiness wait. Skip when the route was already live before this
	// call (meta present — an unmeta'd already-on state can't prove the L7
	// route finished propagating, so still probe) or on deferred-start
	// (container alive, no app process by design — 502 is expected until
	// zerops_dev_server action=start runs).
	skipProbe := (!enabledNow && meta != nil) || deferredStart
	if !skipProbe && result.SubdomainURL != "" {
		if waitErr := ops.WaitHTTPReady(ctx, httpClient, result.SubdomainURL); waitErr != nil {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("subdomain %s not HTTP-ready: %v (next zerops_verify may need to retry)", result.SubdomainURL, waitErr))
		}
	}
}

// persistPublicAccess writes rec as hostname's public-access record via the
// locked FindServiceMeta/WriteServiceMeta read-modify-write pair
// (workflow.UpdateServiceMeta). Errors are swallowed — a failed persist
// means the next hook re-reconciles from the live observation again next
// time, same as every other best-effort ServiceMeta stamp in this file.
func persistPublicAccess(stateDir, hostname string, rec topology.PublicAccessRecord) {
	_ = workflow.UpdateServiceMeta(stateDir, hostname, func(m *workflow.ServiceMeta) error {
		m.SetPublicAccess(hostname, rec)
		return nil
	})
}

// isDeferredStartTarget reports whether hostname is a deferred-start runtime
// right now — a dev-mode dynamic container idling on `zsc noop --silent`
// with no app process until `zerops_dev_server action=start`. Nil meta or
// svc (recipe-authoring / manual-import / lookup failure) fails closed to
// false (not deferred) so the caller's default listener assumption stays
// permissive, matching the pre-E9 behavior for meta-less services.
func isDeferredStartTarget(meta *workflow.ServiceMeta, svc *platform.ServiceStack, hostname string) bool {
	if meta == nil || svc == nil {
		return false
	}
	class := topology.RuntimeClassFor(svc.ServiceStackTypeInfo.ServiceStackTypeVersionName)
	return topology.IsDeferredStart(meta.ModeFor(hostname), class)
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

// serviceEligibleForSubdomain decides whether to attempt auto-enable, and
// returns the looked-up service alongside the boolean so callers that
// proceed to enable can read its TypeVersion for runtime-class
// classification (used by the deferred-start probe-skip predicate).
//
// Two cheap checks, no platform DTO inspection — the platform's response
// on the actual Enable call is the source of truth for "is this
// HTTP-shaped?".
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
// Lookup failures soft-fail (returns nil, false → caller skips auto-enable,
// agent's manual zerops_subdomain stays valid).
func serviceEligibleForSubdomain(
	ctx context.Context,
	client platform.Client,
	meta *workflow.ServiceMeta,
	projectID, targetService string,
) (*platform.ServiceStack, bool) {
	if meta != nil && meta.Mode != "" && !modeAllowsSubdomain(meta.Mode) {
		return nil, false
	}
	svc, err := ops.LookupService(ctx, client, projectID, targetService)
	if err != nil || svc == nil {
		return nil, false
	}
	if svc.IsSystem() {
		return nil, false
	}
	return svc, true
}

// isServiceStackIsNotHTTPErr classifies a platform error as the
// "stack is not HTTP-shaped" rejection. Used by ensurePublicAccess
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
