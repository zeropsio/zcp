package port

// PortTierLevel is the port-flow's OWN ordered honor ladder — a port-local
// projection of the six Zerops deployment tiers (AI-agent → remote → local →
// stage → small-prod → ha-prod). It is DELIBERATELY NOT recipe.Tiers()/TierAt():
// depguard forbids workflow→recipe (architecture_test.go::workflow-not-ops), and
// the rubric roll-up is a pure projection over THESE levels, not the recipe
// engine's tier structs. recipe.Tiers() is needed only at Stage B emit (Phase 4,
// tools layer) where the honored levels map back onto the recipe folders.
//
// The integer values are the canonical tier indices the FitCeiling reports and
// Phase 4 maps onto recipe.TierAt(index). The C-check → tier mapping is a
// port-flow INVENTION (plan §11 verification note), not an intrinsic tiers.go
// property: nothing in "tier 4" means "data persists" — the port flow asserts
// that mapping here.
type PortTierLevel int

const (
	// PortTierNone is the sentinel for "no measured ceiling" — an INFEASIBLE port
	// that honored no tier. It is DELIBERATELY distinct from PortTierAIAgent (tier
	// 0): an infeasible FitCeiling must NOT serialize MeasuredCeiling as a real,
	// honored tier, or a consumer that ignores the Feasible flag silently misreads
	// "nothing runs" as "tier 0 honored". Negative so it never collides with a real
	// tier index and never appears in the honored ladder (allPortTierLevels).
	PortTierNone PortTierLevel = -1
	// PortTierAIAgent — tier 0: an AI-agent dev container (builds + boots + serves).
	PortTierAIAgent PortTierLevel = 0
	// PortTierRemote — tier 1: a remote CDE (same gate prerequisites as tier 0).
	PortTierRemote PortTierLevel = 1
	// PortTierLocal — tier 2: local dev wiring (adds a proven core flow).
	PortTierLocal PortTierLevel = 2
	// PortTierStage — tier 3: a staging deploy (same core-flow prerequisite).
	PortTierStage PortTierLevel = 3
	// PortTierSmallProd — tier 4: small production (adds persistence-across-redeploy).
	PortTierSmallProd PortTierLevel = 4
	// PortTierHAProd — tier 5: highly-available production (adds HA replication).
	PortTierHAProd PortTierLevel = 5
)

// portTierLabels mirrors recipe.Tiers()'s canonical labels WITHOUT importing the
// recipe package. They are honor-report copy only; Phase 4 re-derives the
// authoritative recipe folder/label from recipe.TierAt(int(level)).
var portTierLabels = map[PortTierLevel]string{
	PortTierAIAgent:   "AI Agent",
	PortTierRemote:    "Remote (CDE)",
	PortTierLocal:     "Local",
	PortTierStage:     "Stage",
	PortTierSmallProd: "Small Production",
	PortTierHAProd:    "Highly-available Production",
}

// allPortTierLevels is the ordered ladder used by the roll-up projection.
var allPortTierLevels = []PortTierLevel{
	PortTierAIAgent, PortTierRemote, PortTierLocal,
	PortTierStage, PortTierSmallProd, PortTierHAProd,
}

// PortCheck names one rubric check (C1–C6). They roll up to the honored tier.
type PortCheck string

const (
	PortCheckBuilds   PortCheck = "C1-builds"
	PortCheckBoots    PortCheck = "C2-boots-stable"
	PortCheckServes   PortCheck = "C3-serves-http"
	PortCheckCoreFlow PortCheck = "C4-core-flow"
	PortCheckPersists PortCheck = "C5-persists-across-redeploy"
	PortCheckHA       PortCheck = "C6-ha-capable"
)

// PortGrade is one rubric check's measured outcome. The scale is intentionally
// small (0/1/2) and check-specific in meaning:
//
//	0 — not satisfied (failed / absent).
//	1 — partially satisfied (e.g. C2: ACTIVE-then-exit / crash-loop — booted but
//	    not STABLE; C6: scaled for throughput only, NOT HA replication).
//	2 — fully satisfied (C2: stable past the hold; C5: persisted across a
//	    redeploy; C6: HA replication — managed-HA + ≥2 app containers).
//
// C1/C3/C4 are effectively binary (0 or 1) — the 0/1/2 scale unifies the type so
// the roll-up reads one field. Evidence is the human-readable proof string.
type PortGrade struct {
	Check    PortCheck `json:"check"`
	Grade    int       `json:"grade"`
	Evidence string    `json:"evidence,omitempty"`
}

// PortRubric is the full C1–C6 grade set. Grade lookups are by check so the
// roll-up never positionally indexes.
type PortRubric struct {
	Grades []PortGrade `json:"grades"`
}

// grade returns the recorded grade for a check (0 when absent).
func (r PortRubric) grade(c PortCheck) int {
	for _, g := range r.Grades {
		if g.Check == c {
			return g.Grade
		}
	}
	return 0
}

// C1Builds grades the build gate from PollBuild's terminal outcome + warnings.
//
//	terminal success, no warnings → 2
//	terminal success WITH warnings → 1 (built, but the build emitted warnings)
//	not terminal-success           → 0 (build failed / never reached terminal)
func C1Builds(buildSucceeded, hadWarnings bool) PortGrade {
	g := PortGrade{Check: PortCheckBuilds}
	switch {
	case buildSucceeded && !hadWarnings:
		g.Grade, g.Evidence = 2, "build reached terminal success with no warnings"
	case buildSucceeded && hadWarnings:
		g.Grade, g.Evidence = 1, "build succeeded but emitted warnings"
	default:
		g.Grade, g.Evidence = 0, "build did not reach terminal success"
	}
	return g
}

// C2BootsStable grades the boot gate AFTER a stability hold — the §7 false-
// positive guard. A process that goes ACTIVE then exits / crash-loops within the
// hold grades 1 (booted, NOT stable), never 2. Inputs are what the agent observes
// post-hold: did the process reach ACTIVE, and is it STILL active after the hold
// (no exit / restart churn).
//
//	reachedActive && stableAfterHold → 2 (boots STABLE)
//	reachedActive && !stableAfterHold → 1 (ACTIVE-then-exit / crash-loop)
//	!reachedActive                    → 0 (never booted)
func C2BootsStable(reachedActive, stableAfterHold bool) PortGrade {
	g := PortGrade{Check: PortCheckBoots}
	switch {
	case reachedActive && stableAfterHold:
		g.Grade, g.Evidence = 2, "process reached ACTIVE and stayed stable past the hold"
	case reachedActive && !stableAfterHold:
		g.Grade, g.Evidence = 1, "process reached ACTIVE then exited / crash-looped within the stability hold — booted but not STABLE"
	default:
		g.Grade, g.Evidence = 0, "process never reached ACTIVE"
	}
	return g
}

// C3ServesHTTP grades the HTTP-serve check from a Verify http_root pass.
func C3ServesHTTP(httpRootPassed bool) PortGrade {
	g := PortGrade{Check: PortCheckServes}
	if httpRootPassed {
		g.Grade, g.Evidence = 1, "Verify http_root passed — the app serves HTTP"
	} else {
		g.Grade, g.Evidence = 0, "Verify http_root did not pass"
	}
	return g
}

// C4CoreFlow grades the agent-authored core-flow probe. The loop cannot
// understand a foreign app's health endpoint, so the agent authors a probe and
// reports its pass/fail; the rubric grades the reported outcome (it does not
// re-judge prose).
func C4CoreFlow(probePassed bool) PortGrade {
	g := PortGrade{Check: PortCheckCoreFlow}
	if probePassed {
		g.Grade, g.Evidence = 1, "agent-authored core-flow probe passed"
	} else {
		g.Grade, g.Evidence = 0, "agent-authored core-flow probe did not pass (or was not run)"
	}
	return g
}

// C5Persists grades the persistence sentinel: write a sentinel → redeploy →
// re-read it. Grade 2 only when the sentinel survived a redeploy on a managed /
// object-storage / shared-storage surface (container FS is ephemeral by design,
// so a sentinel that only survives in-container does NOT earn 2).
//
//	survivedRedeploy && onDurableSurface → 2
//	survivedRedeploy && !onDurableSurface → 1 (survived but only on ephemeral FS)
//	!survivedRedeploy                     → 0
func C5Persists(survivedRedeploy, onDurableSurface bool) PortGrade {
	g := PortGrade{Check: PortCheckPersists}
	switch {
	case survivedRedeploy && onDurableSurface:
		g.Grade, g.Evidence = 2, "sentinel survived a redeploy on a durable managed/storage surface"
	case survivedRedeploy && !onDurableSurface:
		g.Grade, g.Evidence = 1, "sentinel survived but only on the ephemeral container FS — not a durable claim"
	default:
		g.Grade, g.Evidence = 0, "sentinel did not survive the redeploy"
	}
	return g
}

// C6HACapable grades HA capability — keeping throughput-scaling and HA-
// replication DISTINCT (MEMORY: "horizontal scaling ≠ HA replication"). Grade 2
// is reserved for genuine HA: ≥2 app containers AND managedDepsHA AND the HA
// verify passed. Scaling app containers for THROUGHPUT only (no managed-HA, or HA
// verify absent) is grade 1.
//
// managedDepsHA is the caller's derived condition "≥1 planned mode-bearing managed
// dep was MEASURED running HA" (GradeHarden passes len(AchievableHA.ManagedHADeps)
// > 0 — the SAME filtered list the emitter uses). It is intentionally NOT "EVERY
// mode-bearing dep is HA": the honest port bar is the deps that REQUIRE HA (e.g.
// PostHog needs ClickHouse HA but runs Postgres/Valkey NON_HA), which the agent
// expresses by listing exactly those in haDeps.
//
//	appContainers>=2 && managedDepsHA && haVerifyPassed → 2 (HA replication)
//	appContainers>=2 (throughput only)                  → 1
//	otherwise                                           → 0
func C6HACapable(appContainers int, managedDepsHA, haVerifyPassed bool) PortGrade {
	g := PortGrade{Check: PortCheckHA}
	switch {
	case appContainers >= 2 && managedDepsHA && haVerifyPassed:
		g.Grade, g.Evidence = 2, "≥2 app containers + ≥1 mode-bearing managed dep measured HA + HA verify passed — HA replication"
	case appContainers >= 2:
		g.Grade, g.Evidence = 1, "scaled to ≥2 app containers for throughput, but not proven HA-replication-capable (no managed dep measured HA / HA verify not passed)"
	default:
		g.Grade, g.Evidence = 0, "not scaled beyond a single container"
	}
	return g
}

// HonoredTier is one ladder rung the measured rubric does or does not honor. An
// excluded tier carries the Reason it cannot be honored ("don't ship a tier
// whose guide you can't honor").
type HonoredTier struct {
	Level   PortTierLevel `json:"level"`
	Label   string        `json:"label"`
	Honored bool          `json:"honored"`
	// Reason is set ONLY for an excluded (Honored=false) tier — the constraint
	// string the agent surfaces. Empty for an honored tier.
	Reason string `json:"reason,omitempty"`
}

// RollUpHonoredTiers projects the measured rubric up the port tier ladder,
// returning every ladder rung with its honored/excluded verdict + reason. The
// projection is a pure function of the rubric grades. CONTRACT: a tier is honored
// only when its prerequisites are met — a higher grade does not honor a tier
// whose own prerequisite is unmet (the C5=2,C6=1 case: tiers 0-4 honored, tier 5
// NOT honored with the throughput-vs-HA reason).
//
// Prerequisite chain (each builds on the previous):
//
//	tier 0/1 (AI-agent / remote) — C1≥1 && C2==2 && C3≥1 (builds, boots STABLE, serves)
//	tier 2/3 (local / stage)     — the above + C4≥1 (a proven core flow)
//	tier 4   (small production)   — the above + C5==2 (persists across redeploy)
//	tier 5   (HA production)      — the above + C6==2 (HA replication, not throughput)
//
// The gate requires C2==2 (boots STABLE), NOT C2≥1: a C2=1 grade is the §7
// false-positive — ACTIVE-then-exit / crash-loop. A crash-looping deploy must
// honor NO tier (you cannot ship a guide for a process that does not stay up),
// so C2=1 fails the gate exactly like C2=0 does.
// C1=C2=C3=0 ⇒ INFEASIBLE: no tier is honored (the deploy never built/booted/served).
func RollUpHonoredTiers(r PortRubric) []HonoredTier {
	c1 := r.grade(PortCheckBuilds)
	c2 := r.grade(PortCheckBoots)
	c3 := r.grade(PortCheckServes)
	c4 := r.grade(PortCheckCoreFlow)
	c5 := r.grade(PortCheckPersists)
	c6 := r.grade(PortCheckHA)

	gate := c1 >= 1 && c2 >= 2 && c3 >= 1
	coreFlow := gate && c4 >= 1
	persists := coreFlow && c5 == 2
	haReplication := persists && c6 == 2

	out := make([]HonoredTier, 0, len(allPortTierLevels))
	for _, lvl := range allPortTierLevels {
		ht := HonoredTier{Level: lvl, Label: portTierLabels[lvl]}
		//exhaustive:ignore — PortTierNone is the infeasible sentinel; it is never a member of allPortTierLevels (the honored ladder), so it cannot appear here.
		switch lvl {
		case PortTierAIAgent, PortTierRemote:
			ht.Honored = gate
			if !ht.Honored {
				ht.Reason = gateUnmetReason(c1, c2, c3)
			}
		case PortTierLocal, PortTierStage:
			ht.Honored = coreFlow
			if !ht.Honored {
				ht.Reason = coreFlowUnmetReason(gate, c4)
			}
		case PortTierSmallProd:
			ht.Honored = persists
			if !ht.Honored {
				ht.Reason = persistUnmetReason(coreFlow, c5)
			}
		case PortTierHAProd:
			ht.Honored = haReplication
			if !ht.Honored {
				ht.Reason = haUnmetReason(persists, c6)
			}
		}
		out = append(out, ht)
	}
	return out
}

// HighestHonoredTier returns the highest honored tier level + ok=false when none
// is honored (INFEASIBLE — the gate failed). When none is honored the level is
// the PortTierNone sentinel (-1), NOT tier 0 — an infeasible ceiling is not a
// real honored tier and must not be mistaken for one.
func HighestHonoredTier(r PortRubric) (PortTierLevel, bool) {
	tiers := RollUpHonoredTiers(r)
	level, ok := PortTierNone, false
	for _, t := range tiers {
		if t.Honored {
			level, ok = t.Level, true
		}
	}
	return level, ok
}

func gateUnmetReason(c1, c2, c3 int) string {
	switch {
	case c1 < 1:
		return "C1 builds not satisfied — the deploy never reached a terminal-success build"
	case c2 < 2:
		if c2 == 0 {
			return "C2 boots-stable not satisfied — the process never reached ACTIVE"
		}
		return "C2 boots-stable not satisfied — the process reached ACTIVE then crash-looped / exited within the stability hold (booted, but NOT stable past the hold)"
	case c3 < 1:
		return "C3 serves-HTTP not satisfied — Verify http_root did not pass"
	default:
		return "gate prerequisites (C1/C2/C3) not satisfied"
	}
}

func coreFlowUnmetReason(gate bool, c4 int) string {
	if !gate {
		return "gate prerequisites (build/boot/serve) not satisfied, so the core-flow tiers cannot be honored"
	}
	if c4 < 1 {
		return "C4 core-flow not satisfied — no agent-authored probe proved the app's core flow works"
	}
	return "core-flow prerequisite not satisfied"
}

func persistUnmetReason(coreFlow bool, c5 int) string {
	if !coreFlow {
		return "core-flow tiers not honored, so the small-production tier cannot be honored"
	}
	if c5 < 2 {
		return "C5 persists-across-redeploy not fully satisfied — data was not proven to survive a redeploy on a durable surface"
	}
	return "persistence prerequisite not satisfied"
}

func haUnmetReason(persists bool, c6 int) string {
	if !persists {
		return "the small-production (persistence) tier is not honored, so the HA-production tier cannot be honored"
	}
	switch c6 {
	case 1:
		return "C6 HA-capable only reached throughput-scaling, NOT HA replication — scaling to ≥2 containers for throughput is distinct from HA-replication (managed deps in HA mode + a passing HA verify); ship the small-production tier, omit HA"
	default:
		return "C6 HA-capable not satisfied — the deployment was not scaled / HA-verified"
	}
}
