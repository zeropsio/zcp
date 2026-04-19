package workflow

// BuildPlan is the single entry point for producing the typed Plan from an
// envelope. Pure — no I/O, no state, deterministic given the input JSON.
//
// Branching follows §7.2 strictly; first match wins:
//
//  1. develop-closed-auto    → close + start-next
//  2. develop-active, deploy pending anywhere → deploy (Primary), for that svc
//  3. develop-active, verify pending anywhere → verify
//  4. develop-active, last attempt failed     → fix-and-retry
//  5. bootstrap-active                         → continue-bootstrap (route-specific)
//  6. recipe-active                            → continue-recipe
//  7. idle, no services                        → start-bootstrap
//  8. idle, bootstrapped services              → start-develop + alternatives
//  9. idle, only unmanaged services            → adopt-via-develop
//
// Any branch whose precondition is not met falls through — no fallbacks,
// no defaults. If the envelope hits no branch, an empty Plan is returned,
// signalling a bug in envelope construction.
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
	case PhaseCICDActive, PhaseExportActive:
		// CI/CD and export phases don't drive a plan today — the tool handlers
		// for those workflows emit their own guidance directly. Fall through
		// to the empty Plan so the caller knows there's nothing to suggest.
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

// planDevelopActive handles the develop-active phase. Branches 2→3→4 in
// priority order: deploy gaps beat verify gaps beat retries, because deploy
// is the prerequisite for both verify and retry.
func planDevelopActive(env StateEnvelope) Plan {
	if host := firstServiceNeedingDeploy(env); host != "" {
		return Plan{Primary: deployAction(host)}
	}
	if host := firstServiceNeedingVerify(env); host != "" {
		return Plan{Primary: verifyAction(host)}
	}
	if host := firstServiceWithFailedAttempt(env); host != "" {
		return Plan{Primary: fixAndRetryAction(host)}
	}
	// Every service deployed + verified but the session isn't auto-closed:
	// this can happen when EvaluateAutoClose sees a mixed attempt history.
	// The workable next step is explicit close.
	return Plan{
		Primary: NextAction{
			Label:     "Close develop session",
			Tool:      "zerops_workflow",
			Args:      map[string]string{"action": "close", "workflow": "develop"},
			Rationale: "All scope services are deployed and verified.",
		},
	}
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

// firstServiceNeedingDeploy returns the hostname of the first service in
// envelope.Services that has no successful deploy recorded. Iteration order
// mirrors envelope.Services (already sorted by hostname) so the plan is
// deterministic.
func firstServiceNeedingDeploy(env StateEnvelope) string {
	if env.WorkSession == nil {
		return ""
	}
	for _, host := range env.WorkSession.Services {
		attempts := env.WorkSession.Deploys[host]
		if len(attempts) == 0 || !attempts[len(attempts)-1].Success {
			return host
		}
	}
	return ""
}

// firstServiceNeedingVerify returns the hostname of the first service that
// has a successful deploy but no passing verify yet.
func firstServiceNeedingVerify(env StateEnvelope) string {
	if env.WorkSession == nil {
		return ""
	}
	for _, host := range env.WorkSession.Services {
		deploys := env.WorkSession.Deploys[host]
		if len(deploys) == 0 || !deploys[len(deploys)-1].Success {
			continue
		}
		verifies := env.WorkSession.Verifies[host]
		if len(verifies) == 0 || !verifies[len(verifies)-1].Success {
			return host
		}
	}
	return ""
}

// firstServiceWithFailedAttempt returns the hostname of the first service
// whose last attempt (deploy or verify) failed. Only used when there are no
// pending deploys/verifies — i.e. every service has been tried but something
// regressed. Separate from the deploy/verify branches because its remediation
// is "diagnose the failure," not "run the next step."
func firstServiceWithFailedAttempt(env StateEnvelope) string {
	if env.WorkSession == nil {
		return ""
	}
	for _, host := range env.WorkSession.Services {
		if lastAttemptFailed(env.WorkSession.Deploys[host]) {
			return host
		}
		if lastAttemptFailed(env.WorkSession.Verifies[host]) {
			return host
		}
	}
	return ""
}

func lastAttemptFailed(attempts []AttemptInfo) bool {
	if len(attempts) == 0 {
		return false
	}
	return !attempts[len(attempts)-1].Success
}

// countIdleServices partitions the envelope's services for idle-phase plan
// dispatch. `bootstrapped` is the count with complete ServiceMeta; `adoptable`
// is unmanaged runtimes without complete meta.
func countIdleServices(env StateEnvelope) (bootstrapped, adoptable int) {
	for _, svc := range env.Services {
		if svc.RuntimeClass == RuntimeManaged {
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

func deployAction(host string) NextAction {
	return NextAction{
		Label:     "Deploy " + host,
		Tool:      "zerops_deploy",
		Args:      map[string]string{"hostname": host},
		Rationale: "No successful deploy recorded for this service.",
	}
}

func verifyAction(host string) NextAction {
	return NextAction{
		Label:     "Verify " + host,
		Tool:      "zerops_verify",
		Args:      map[string]string{"hostname": host},
		Rationale: "Deploy succeeded but verify has not passed yet.",
	}
}

func fixAndRetryAction(host string) NextAction {
	return NextAction{
		Label:     "Diagnose and retry " + host,
		Tool:      "zerops_logs",
		Args:      map[string]string{"hostname": host},
		Rationale: "The last deploy/verify attempt failed — read logs before retrying.",
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
