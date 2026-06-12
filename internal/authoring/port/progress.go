package port

import "time"

// PortTerminator names which of the three independent graceful-stop terminators
// fired. The port deploy-debug loop self-terminates (never spins) when ANY of
// these trips; the agent then surfaces a partial FitCeiling rather than looping.
// Spec: plan §4 "Autonomous loop mechanics".
type PortTerminator string

const (
	// PortTermNone — no terminator fired; the loop continues.
	PortTermNone PortTerminator = ""
	// PortTermIterationCap — the per-band iteration cap was reached (EASY 4 /
	// MEDIUM 8 / HARD 12), measured from the last re-budget origin. The wrapped
	// work session is closed with CloseReasonIterationCap.
	PortTermIterationCap PortTerminator = "iteration-cap"
	// PortTermWallBudget — the monotonic wall deadline (EASY 45m / HARD 90m from
	// session start) elapsed, measured against the latest attempt's RecordedAt.
	PortTermWallBudget PortTerminator = "wall-budget"
	// PortTermEscalationBail — the escalation ladder reached T2 (no prebuilt
	// fallback, post-escalation phase stall, or an unfixable escalate flag).
	// Surfaced as a graceful stop with the ceiling the agent should capture.
	PortTermEscalationBail PortTerminator = "escalation-bail"
)

// PortProgressResult is the terminator-evaluation outcome for one loop turn. It
// is the decision API Phase 3 feeds tier-rise + rubric grades into: the
// progressRose input to PhaseStallStreak is the seam (default false here;
// Phase 3 passes the measured tier-rise). Stop+Terminator drive the handler's
// continue/stop branch.
type PortProgressResult struct {
	// Stop is true when any terminator fired this turn.
	Stop bool `json:"stop"`
	// Terminator names which terminator fired (empty when Stop is false).
	Terminator PortTerminator `json:"terminator,omitempty"`
	// ClassStall is the current class-stall streak (coarse, signal-invariant).
	ClassStall int `json:"classStall"`
	// PhaseStall is the current phase-stall streak (fix-class phase
	// non-advancement; Phase 3 will fold tier-non-rise in via progressRose).
	PhaseStall int `json:"phaseStall"`
	// Reason is a human-readable explanation surfaced to the agent on Stop.
	Reason string `json:"reason,omitempty"`
}

// IterationCapFor is the per-band iteration budget. Unknown / bail bands take
// the conservative EASY floor (bail never deploys, so the floor is moot).
func IterationCapFor(band FeasibilityBand) int {
	switch band {
	case BandHard:
		return 12
	case BandMedium:
		return 8
	case BandEasy, BandBail:
		return 4
	default:
		return 4
	}
}

// WallBudgetFor is the per-band monotonic wall deadline. The spec pins EASY 45m
// and HARD 90m; MEDIUM is interpolated to 60m (between the two anchors). Bail /
// unknown take the EASY floor.
func WallBudgetFor(band FeasibilityBand) time.Duration {
	switch band {
	case BandHard:
		return 90 * time.Minute
	case BandMedium:
		return 60 * time.Minute
	case BandEasy, BandBail:
		return 45 * time.Minute
	default:
		return 45 * time.Minute
	}
}

// ClassStallStreak counts consecutive TRAILING failing attempts that share the
// SAME FailureClass category. It is coarse by design — it keys on the category
// ONLY, so it survives Signals variation within one root cause (the §4 failure
// mode where a per-fix signal change spuriously resets the streak). A trailing
// success (or no attempts) yields 0.
func ClassStallStreak(attempts []PortAttempt) int {
	n := len(attempts)
	if n == 0 {
		return 0
	}
	last := attempts[n-1]
	if last.Succeeded || last.Class == "" {
		return 0
	}
	streak := 0
	for i := n - 1; i >= 0; i-- {
		a := attempts[i]
		if a.Succeeded || a.Class != last.Class {
			break
		}
		streak++
	}
	return streak
}

// fixClassPhase maps a derived fix-class to a coarse lifecycle phase rank, used
// as the failedPhase proxy for phase-stall detection. The progression is
// build (1) → start (2) → serve (3); fixes with no inherent phase (retry,
// git-push-setup, inspect, escalate) are phase 0 (neutral — they neither
// advance nor, on their own, regress; advancement is judged against the prior
// NON-neutral phase).
func fixClassPhase(kind FixClassKind) int {
	switch kind {
	case FixAddPrepareCommands:
		return 1 // build
	case FixWireDepRefs, FixSetEnv, FixMoveToInitCommands:
		return 2 // start
	case FixGlueYAML:
		return 3 // serve / config
	case FixRetry, FixGitPushSetup, FixEscalate, FixInspect:
		// No inherent lifecycle phase — neutral (treated as non-advancing).
		return 0
	default:
		return 0
	}
}

// PhaseStallStreak counts consecutive TRAILING failing attempts whose fix-class
// PHASE does not ADVANCE. The phase ordering (build → start → serve) is the
// failedPhase proxy; same-or-backward movement is a stall. progressRose is the
// Phase 3 seam: when true (a measured rubric tier rose this turn), the trailing
// turn is treated as advancing regardless of fix-class phase, breaking the
// streak. Default false (derive purely from fix-class non-advancement).
//
// Neutral-phase fixes (phase 0: retry / inspect / git-push / escalate) carry no
// advancement signal, so they are treated as non-advancing (part of a stall).
func PhaseStallStreak(attempts []PortAttempt, progressRose bool) int {
	n := len(attempts)
	if n == 0 {
		return 0
	}
	last := attempts[n-1]
	if last.Succeeded {
		return 0
	}
	// A measured tier-rise this turn means the trailing turn advanced; the
	// stall streak is just this one (non-stalled) turn.
	if progressRose {
		return 1
	}
	streak := 1
	for i := n - 1; i > 0; i-- {
		cur := attempts[i]
		prev := attempts[i-1]
		if cur.Succeeded || prev.Succeeded {
			break
		}
		// Advancement = the current phase is strictly higher than the prior
		// phase (and both carry a phase signal). Any same-or-backward movement,
		// or a neutral-phase turn, continues the stall.
		curPhase := fixClassPhase(cur.FixKind)
		prevPhase := fixClassPhase(prev.FixKind)
		if curPhase > prevPhase && prevPhase > 0 {
			break
		}
		streak++
	}
	return streak
}

// EvaluatePortProgress runs the three independent terminators over the session's
// attempt history and returns the stop/continue decision. Pure: time is injected
// via now (never time.Now), so it is deterministic and table-testable. ANY
// terminator firing → Stop. Order of precedence when several would fire:
// iteration-cap, then wall-budget, then escalation-bail (the cap is the cheapest
// signal and the canonical terminal close).
//
// progressRose is the Phase 3 seam threaded through to PhaseStallStreak: when a
// measured rubric honored-tier rose this turn, the trailing turn is treated as
// advancing, breaking the phase-stall streak (the §5 "phaseStall takes a
// progressRose seam that Phase 3 feeds with rubric tier-rise"). Phase 1/2 callers
// pass false; the Phase 3 handler passes the computed tier-rise.
//
// The escalation-bail terminator delegates to DecidePortEscalation so the two
// stay in lockstep: a T2 verdict here is the same T2 the handler surfaces.
func EvaluatePortProgress(ps *PortSession, now time.Time, progressRose bool) PortProgressResult {
	res := PortProgressResult{
		ClassStall: ClassStallStreak(ps.Attempts),
		PhaseStall: PhaseStallStreak(ps.Attempts, progressRose),
	}

	// 1) Iteration cap (measured from the last re-budget origin).
	capLimit := IterationCapFor(ps.Plan.Band)
	if ps.Iteration-ps.RebudgetOrigin >= capLimit {
		res.Stop = true
		res.Terminator = PortTermIterationCap
		res.Reason = "iteration cap reached for band " + string(ps.Plan.Band) + " — stop and capture the partial FitCeiling"
		return res
	}

	// 2) Wall budget (monotonic deadline from session start). The deadline is
	// breached when EITHER the latest recorded attempt OR the injected clock
	// (now — covers a loop hanging between attempts) is at/past it.
	if start, ok := portSessionStart(ps); ok {
		deadline := start.Add(WallBudgetFor(ps.Plan.Band))
		breached := !now.Before(deadline)
		if latest, ok := latestAttemptTime(ps); ok && !latest.Before(deadline) {
			breached = true
		}
		if breached {
			res.Stop = true
			res.Terminator = PortTermWallBudget
			res.Reason = "wall budget elapsed for band " + string(ps.Plan.Band) + " — stop and capture the partial FitCeiling"
			return res
		}
	}

	// 3) Escalation bail (T2 from the ladder).
	if dec := DecidePortEscalation(ps.Plan, ps.Attempts); dec.Tier == PortEscalateT2Bail {
		res.Stop = true
		res.Terminator = PortTermEscalationBail
		res.Reason = dec.Reason
		return res
	}

	return res
}

// portSessionStart parses the session's CreatedAt (RFC3339) — the wall-budget
// anchor. Returns ok=false when unset or unparseable — callers skip the
// wall-budget terminator then. Deliberately NOT StartTime: that field is the
// opaque process-identity token, not parseable time.
func portSessionStart(ps *PortSession) (time.Time, bool) {
	if ps.CreatedAt == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, ps.CreatedAt)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// latestAttemptTime returns the most recent attempt's RecordedAt parsed as
// RFC3339. Returns ok=false when there are no attempts or the timestamp is
// unparseable.
func latestAttemptTime(ps *PortSession) (time.Time, bool) {
	n := len(ps.Attempts)
	if n == 0 {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, ps.Attempts[n-1].RecordedAt)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}
