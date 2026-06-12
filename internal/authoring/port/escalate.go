package port

import "github.com/zeropsio/zcp/internal/topology"

// PortEscalationTier names a rung of the strategy escalation ladder. Triggers
// are typed (never a raw retry count). Spec: plan §4 "Strategy escalation
// ladder".
//
//	T0 — stay: a new FailureClass category with budget left → apply the next
//	     fix-class; no strategy change.
//	T1 — escalate: source-build → prebuilt-binary, when a build-class stall is
//	     deep (classStall ≥ 3) or an in-band-unfixable build escalate fired
//	     (e.g. OOM) AND a prebuilt URL is available. Re-budgets the loop.
//	T2 — bail: no prebuilt fallback, OR a post-escalation phase stall, OR an
//	     escalate flag with nowhere left to go. Stop with a partial FitCeiling.
type PortEscalationTier string

const (
	PortEscalateT0Stay PortEscalationTier = "T0-stay"
	PortEscalateT1     PortEscalationTier = "T1-escalate"
	PortEscalateT2Bail PortEscalationTier = "T2-bail"
)

// PortEscalationDecision is the typed escalation verdict over the attempt
// history. Pure function of (PortPlan, []PortAttempt) — no I/O, table-testable.
type PortEscalationDecision struct {
	// Tier is the ladder rung the triggers selected.
	Tier PortEscalationTier `json:"tier"`
	// NewStrategy is the acquisition strategy to switch to (only on T1).
	NewStrategy AcquisitionStrategy `json:"newStrategy,omitempty"`
	// Reason is the human-readable explanation surfaced to the agent.
	Reason string `json:"reason"`
}

// classStallBuildThreshold is the §4 trigger: a build-class stall this deep is a
// strategy-switch signal (source-build → prebuilt-binary), not just-another-fix.
const classStallBuildThreshold = 3

// DecidePortEscalation evaluates the typed escalation ladder over the loop's
// attempt history. Pure + deterministic. The decision is driven by the trailing
// failure class, the stall streaks, and whether a prebuilt fallback exists — NOT
// by a raw retry count.
//
// Precedence:
//  1. credential / network classes NEVER escalate the strategy → T0 stay (the
//     fix is "fix credentials" / "retry", not "switch how we acquire").
//  2. A deep build stall (classStall ≥ 3 on FailureClassBuild) OR an in-band
//     build escalate flag (e.g. OOM) → T1 if a prebuilt URL is available AND we
//     are still on source-build; otherwise T2 bail (nowhere left to escalate).
//  3. A post-escalation phase stall (already off source-build, phase stuck) →
//     T2 bail.
//  4. Otherwise (new class / budget remaining) → T0 stay.
func DecidePortEscalation(plan PortPlan, attempts []PortAttempt) PortEscalationDecision {
	last, ok := lastFailingAttempt(attempts)
	if !ok {
		return PortEscalationDecision{Tier: PortEscalateT0Stay, Reason: "no failing attempt — continue the loop"}
	}

	// (1) credential / network never escalate the acquisition strategy.
	if last.Class == topology.FailureClassCredential || last.Class == topology.FailureClassNetwork {
		return PortEscalationDecision{
			Tier:   PortEscalateT0Stay,
			Reason: "class " + string(last.Class) + " never escalates the acquisition strategy — apply the credential/retry fix and continue",
		}
	}

	classStall := ClassStallStreak(attempts)
	phaseStall := PhaseStallStreak(attempts, false)
	prebuiltAvailable := plan.PrebuiltURL != "" && plan.Acquisition == AcquireSourceBuild

	// (2) deep build stall or in-band build escalate → try the strategy switch.
	buildEscalation := (last.Class == topology.FailureClassBuild && classStall >= classStallBuildThreshold) ||
		(last.Escalate && last.Class == topology.FailureClassBuild)
	if buildEscalation {
		if prebuiltAvailable {
			return PortEscalationDecision{
				Tier:        PortEscalateT1,
				NewStrategy: AcquirePrebuiltBinary,
				Reason:      "build-class stall on source-build with a prebuilt-binary URL available — escalate acquisition source-build → prebuilt-binary and re-budget the loop",
			}
		}
		return PortEscalationDecision{
			Tier:   PortEscalateT2Bail,
			Reason: "build-class stall with no prebuilt-binary fallback (or already past source-build) — bail with a partial FitCeiling",
		}
	}

	// (3) post-escalation phase stall (no longer on source-build, phase stuck).
	if plan.Acquisition != AcquireSourceBuild && phaseStall >= classStallBuildThreshold {
		return PortEscalationDecision{
			Tier:   PortEscalateT2Bail,
			Reason: "phase stall after a strategy escalation — no further strategy to try, bail with a partial FitCeiling",
		}
	}

	// (4) default: new class / budget remaining → stay.
	return PortEscalationDecision{
		Tier:   PortEscalateT0Stay,
		Reason: "failure class is tractable with budget remaining — apply the derived fix-class and continue",
	}
}

// lastFailingAttempt returns the most recent attempt that is a failure (not a
// success). Returns ok=false when there are no attempts or the last is a success.
func lastFailingAttempt(attempts []PortAttempt) (PortAttempt, bool) {
	n := len(attempts)
	if n == 0 {
		return PortAttempt{}, false
	}
	last := attempts[n-1]
	if last.Succeeded {
		return PortAttempt{}, false
	}
	return last, true
}
