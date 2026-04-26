package workflow

import

// BuildPlan is the single entry point for producing the typed Plan from an
// envelope. Pure — no I/O, no state, deterministic given the input JSON.
//
// Branching; first match wins:
//
//  1. develop-closed-auto    → close + start-next
//  2. develop-active, deploy pending anywhere → deploy (Primary), for that svc
//  3. develop-active, verify pending anywhere → verify
//  4. develop-active, all green                → close (explicit)
//  5. bootstrap-active                         → continue-bootstrap (route-specific)
//  6. recipe-active                            → continue-recipe
//  7. idle, no services                        → start-bootstrap
//  8. idle, bootstrapped services              → start-develop + alternatives
//  9. idle, only unmanaged services            → adopt-via-develop
//
// Note: "last attempt failed" doesn't get its own branch — branches 2 and 3
// already match those cases. needsDeploy/needsVerify both key off
// `!attempts[last].Success`, so a failed service gets a deploy or verify
// action pointed at it. The atom layer (develop-checklist + iteration-delta)
// surfaces the diagnosis guidance.
//
// Any branch whose precondition is not met falls through — no fallbacks,
// no defaults. If the envelope hits no branch, an empty Plan is returned,
// signalling a bug in envelope construction.
"github.com/zeropsio/zcp/internal/topology"

func BuildPlan(env StateEnvelope) Plan {
	switch env.Phase {
	case PhaseDevelopClosed:
		return planDevelopClosed()
	case PhaseDevelopActive:
		return planDevelopActive(env)
	case PhaseBootstrapActive:
		return planBootstrapActive()
	case PhaseRecipeActive:
		return planRecipeActive()
	case PhaseIdle:
		return planIdle(env)
	case PhaseStrategySetup, PhaseExportActive:
		// Strategy-setup and export phases don't drive a plan — the handlers
		// for those paths emit their own guidance directly. Fall through to
		// the empty Plan so the caller knows there's nothing to suggest.
	}
	return Plan{}
}

func planDevelopClosed() Plan {
	return Plan{
		Primary: NextAction{
			Label:     "Close current develop session",
			Tool:      "zerops_workflow",
			Args:      map[string]string{"action": "close", "workflow": "develop"},
			Rationale: "All services deployed and verified — close to reclaim the slot.",
		},
		Secondary: &NextAction{
			Label:     "Start next develop task",
			Tool:      "zerops_workflow",
			Args:      map[string]string{"action": "start", "workflow": "develop", "intent": "..."},
			Rationale: "After closing, begin the next task to keep momentum.",
		},
	}
}

// planDevelopActive handles the develop-active phase. Deploy gaps beat
// verify gaps across the whole scope: any service needing deploy wins
// Primary before a service with deploy ok + verify pending is considered.
// PerService lists every pending service; green services are omitted.
//
// When the most recent deploy/verify for the chosen host carries a
// FailureClass + Reason, the per-host action's Rationale references the
// failure shape (Phase 1 C1 of the pipeline-repair plan) so post-compaction
// recovery sees "build failed: timeout — fix and redeploy" instead of
// generic "deploy this service" wording.
func planDevelopActive(env StateEnvelope) Plan {
	perService := perServiceDevelopActions(env)
	if env.WorkSession != nil {
		for _, host := range env.WorkSession.Services {
			if needsDeploy(env.WorkSession, host) {
				last := lastAttempt(env.WorkSession.Deploys[host])
				return Plan{Primary: deployActionFor(host, last), PerService: perService}
			}
		}
		for _, host := range env.WorkSession.Services {
			if needsVerify(env.WorkSession, host) {
				last := lastAttempt(env.WorkSession.Verifies[host])
				return Plan{Primary: verifyActionFor(host, last), PerService: perService}
			}
		}
	}
	// Every service deployed + verified but the session isn't auto-closed
	// (can happen with a mixed attempt history). Fall back to explicit close.
	return Plan{
		Primary: NextAction{
			Label:     "Close develop session",
			Tool:      "zerops_workflow",
			Args:      map[string]string{"action": "close", "workflow": "develop"},
			Rationale: "All scope services are deployed and verified.",
		},
	}
}

// perServiceDevelopActions returns a hostname → next-action map for every
// scope service still pending work (deploy or verify). Green services are
// dropped so the render layer only prints remaining work. Returns nil for
// empty scopes or all-green so callers can treat absence as "nothing to
// list".
func perServiceDevelopActions(env StateEnvelope) map[string]NextAction {
	if env.WorkSession == nil || len(env.WorkSession.Services) == 0 {
		return nil
	}
	out := make(map[string]NextAction, len(env.WorkSession.Services))
	for _, host := range env.WorkSession.Services {
		switch {
		case needsDeploy(env.WorkSession, host):
			out[host] = deployActionFor(host, lastAttempt(env.WorkSession.Deploys[host]))
		case needsVerify(env.WorkSession, host):
			out[host] = verifyActionFor(host, lastAttempt(env.WorkSession.Verifies[host]))
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// lastAttempt returns the most recent attempt or a zero AttemptInfo when
// the slice is empty. Used by deployActionFor / verifyActionFor to phrase
// failure-aware Rationale text without nil checks at call sites.
func lastAttempt(attempts []AttemptInfo) AttemptInfo {
	if len(attempts) == 0 {
		return AttemptInfo{}
	}
	return attempts[len(attempts)-1]
}

// lastSucceeded returns true when attempts has at least one entry and the
// most recent one succeeded. The shared "has a green-tipped history"
// predicate — used by both the develop Primary dispatch and the PerService
// classifier. A failed last attempt (even after prior successes) counts as
// "not succeeded" because the agent's next action is retry.
func lastSucceeded(attempts []AttemptInfo) bool {
	return len(attempts) > 0 && attempts[len(attempts)-1].Success
}

// needsDeploy reports whether host needs a (re-)deploy: no attempts, or the
// last one failed.
func needsDeploy(ws *WorkSessionSummary, host string) bool {
	return !lastSucceeded(ws.Deploys[host])
}

// needsVerify reports whether host needs verification: deploy ok but
// verify missing or last one failed. Services still needing a deploy
// return false here so the deploy branch in planDevelopActive fires first.
func needsVerify(ws *WorkSessionSummary, host string) bool {
	return lastSucceeded(ws.Deploys[host]) && !lastSucceeded(ws.Verifies[host])
}

// planBootstrapActive points at action=iterate with bootstrap workflow — the
// engine applies the route-specific step logic from the session file.
func planBootstrapActive() Plan {
	return Plan{
		Primary: NextAction{
			Label:     "Continue bootstrap",
			Tool:      "zerops_workflow",
			Args:      map[string]string{"action": "iterate", "workflow": "bootstrap"},
			Rationale: "Bootstrap session is in progress — iterate to advance.",
		},
	}
}

func planRecipeActive() Plan {
	return Plan{
		Primary: NextAction{
			Label:     "Continue recipe",
			Tool:      "zerops_workflow",
			Args:      map[string]string{"action": "iterate", "workflow": "recipe"},
			Rationale: "Recipe session is in progress — iterate to advance.",
		},
	}
}

// planIdle handles the three idle sub-cases:
//   - no services at all → bootstrap
//   - bootstrapped services present → develop (primary) + optional adopt / add
//   - only unmanaged runtimes → adopt via develop
func planIdle(env StateEnvelope) Plan {
	bootstrapped, adoptable := countIdleServices(env)

	if bootstrapped == 0 && adoptable == 0 {
		return Plan{Primary: startBootstrapAction()}
	}

	if bootstrapped > 0 {
		plan := Plan{Primary: startDevelopAction()}
		if adoptable > 0 {
			plan.Alternatives = append(plan.Alternatives, adoptRuntimesAction())
		}
		plan.Alternatives = append(plan.Alternatives, addServicesAction())
		return plan
	}

	// Only unmanaged runtimes exist — adoption is the gate to develop.
	return Plan{Primary: adoptRuntimesAction()}
}

// countIdleServices partitions the envelope's services for idle-phase plan
// dispatch. `bootstrapped` is the count with complete ServiceMeta; `adoptable`
// is unmanaged runtimes without complete meta.
func countIdleServices(env StateEnvelope) (bootstrapped, adoptable int) {
	for _, svc := range env.Services {
		if svc.RuntimeClass == topology.RuntimeManaged {
			continue
		}
		if svc.Bootstrapped {
			bootstrapped++
			continue
		}
		adoptable++
	}
	return bootstrapped, adoptable
}

// Action constructors — plain constants dressed up as functions so the plan
// text is centralized. Every Args map uses explicit strings (not constants)
// to match the MCP wire format literally.

// deployActionFor returns a deploy NextAction whose Rationale reflects the
// most recent attempt. Empty/successful last → generic "no successful deploy
// recorded" wording. Failed last with FailureClass → wording naming the
// failure shape ("Last attempt: build failed — <reason>") so the LLM has
// the recovery context inline. The Reason text is included verbatim so a
// post-compaction `action="status"` reconstructs the actionable diagnosis.
func deployActionFor(host string, last AttemptInfo) NextAction {
	return NextAction{
		Label:     "Deploy " + host,
		Tool:      "zerops_deploy",
		Args:      map[string]string{"targetService": host},
		Rationale: deployRationale(last),
	}
}

// verifyActionFor mirrors deployActionFor for verify attempts. When the
// last verify carries Reason + FailureClass, the Rationale names the
// failing check class so the LLM picks targeted recovery (e.g. "verify
// failed — http_root: 502" → check the route, not the start command).
func verifyActionFor(host string, last AttemptInfo) NextAction {
	return NextAction{
		Label:     "Verify " + host,
		Tool:      "zerops_verify",
		Args:      map[string]string{"serviceHostname": host},
		Rationale: verifyRationale(last),
	}
}

// deployRationale phrases the deploy NextAction.Rationale based on the
// last attempt's outcome. The class-prefix wording keeps the LLM's plan-
// dispatch deterministic across attempt iterations: same Reason+class
// always produces the same Rationale string, satisfying the compaction-
// safety invariant.
func deployRationale(last AttemptInfo) string {
	if last.At.IsZero() || last.Success {
		return "No successful deploy recorded for this service."
	}
	prefix := failureClassPrefix(last.FailureClass)
	if last.Reason == "" {
		return prefix + "."
	}
	return prefix + ": " + last.Reason + ". Fix and redeploy."
}

func verifyRationale(last AttemptInfo) string {
	if last.At.IsZero() {
		return "Deploy succeeded but verify has not passed yet."
	}
	if last.Success {
		// Should not normally reach here — needsVerify only fires when last
		// failed — but if a caller invokes verifyActionFor with a green
		// last, fall back to the generic line.
		return "Deploy succeeded but verify has not passed yet."
	}
	prefix := failureClassPrefix(last.FailureClass)
	if last.Reason == "" {
		return prefix + "."
	}
	return prefix + ": " + last.Reason + ". Investigate and re-verify."
}

// failureClassPrefix maps a FailureClass to a short human-readable phrase
// for the deploy/verify Rationale. Unknown / unset returns the generic
// "Last attempt failed" — the Reason text still carries the actionable
// content so the agent isn't blocked on missing classification.
func failureClassPrefix(class FailureClass) string {
	switch class {
	case FailureClassBuild:
		return "Last attempt: build failed (timeout, compile, or dependency install)"
	case FailureClassStart:
		return "Last attempt: container didn't start"
	case FailureClassVerify:
		return "Last attempt: verify check failed"
	case FailureClassNetwork:
		return "Last attempt: network/transport error"
	case FailureClassConfig:
		return "Last attempt: config validation failed"
	case FailureClassOther:
		return "Last attempt failed"
	default:
		return "Last attempt failed"
	}
}

func startBootstrapAction() NextAction {
	return NextAction{
		Label:     "Create services",
		Tool:      "zerops_workflow",
		Args:      map[string]string{"action": "start", "workflow": "bootstrap"},
		Rationale: "Project has no services yet.",
	}
}

func startDevelopAction() NextAction {
	return NextAction{
		Label:     "Start a develop task",
		Tool:      "zerops_workflow",
		Args:      map[string]string{"action": "start", "workflow": "develop", "intent": "..."},
		Rationale: "Bootstrapped services are ready for code tasks.",
	}
}

func adoptRuntimesAction() NextAction {
	return NextAction{
		Label:     "Adopt unmanaged runtimes",
		Tool:      "zerops_workflow",
		Args:      map[string]string{"action": "start", "workflow": "develop", "intent": "adopt"},
		Rationale: "Existing runtime services have no bootstrap metadata yet.",
	}
}

func addServicesAction() NextAction {
	return NextAction{
		Label:     "Add more services",
		Tool:      "zerops_workflow",
		Args:      map[string]string{"action": "start", "workflow": "bootstrap"},
		Rationale: "Add additional managed or runtime services to the project.",
	}
}
