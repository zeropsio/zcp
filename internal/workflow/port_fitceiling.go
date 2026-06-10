package workflow

// AchievableHA is the honest HA-capability breakdown for the ported deployment.
// It keeps throughput-scaling and HA-replication DISTINCT: AppContainers is the
// scale the app runtime reached (a throughput axis), ManagedHADeps are the
// managed dependencies actually running in HA mode (the replication axis), and
// NotHA names the surfaces that could NOT reach HA (with the reason embedded in
// the FitCeiling honored-tier list). A deployment can have AppContainers>=2 for
// throughput while NOT being HA-replication-capable — those are different claims.
type AchievableHA struct {
	// AppContainers is the app-runtime container count reached during the HA
	// probe (the throughput axis).
	AppContainers int `json:"appContainers"`
	// ManagedHADeps names the managed dependencies running in HA mode (the
	// replication axis) — e.g. "clickhouse@25.3", "postgresql@18".
	ManagedHADeps []string `json:"managedHaDeps,omitempty"`
	// NotHA names surfaces that could not reach HA replication (managed deps
	// stuck in NON_HA, single-container runtimes).
	NotHA []string `json:"notHa,omitempty"`
}

// GlueRepo carries the buildFromGit glue-repo coordinates Stage B (Phase 4) needs
// to publish the recipe. The port loop authors a glue zerops.yaml + commits it to
// a repo ZCP controls; this records WHERE and WHETHER buildFromGit can resolve it.
type GlueRepo struct {
	// URL is the glue repository URL (the buildFromGit target).
	URL string `json:"url,omitempty"`
	// CommittedSHA is the commit the working deployment was built from.
	CommittedSHA string `json:"committedSha,omitempty"`
	// BuildFromGitReady is true when the glue repo is committed + reachable so a
	// re-import's buildFromGit resolves (OQ-1: false flags a deferred commit).
	BuildFromGitReady bool `json:"buildFromGitReady"`
}

// FitCeiling is the port flow's honest, MEASURED fit report — the Stage A output
// the checkpoint stops on and Stage B (Phase 4) consumes to publish honored
// tiers. It reports BOTH the measured ceiling and the recon-band estimate (they
// can diverge — the band is only an estimate, the rubric is the truth).
//
// Phase 4 reads: HonoredTiers (which recipe tiers to emit + the omit reasons),
// GlueRepo (the buildFromGit target + readiness), AchievableHA + Rubric (the
// honesty content for the takeover-guide/knowledge-base fragments).
type FitCeiling struct {
	// Target is the OSS identity (PortPlan.Target).
	Target string `json:"target"`
	// Acquisition is the acquisition strategy the working deployment used (the
	// possibly-escalated final strategy, not necessarily recon's first guess).
	Acquisition AcquisitionStrategy `json:"acquisition"`
	// Band is the recon ESTIMATE (reported alongside the measured ceiling so the
	// divergence is visible — plan §5 "the report shows both").
	Band FeasibilityBand `json:"band"`
	// Rubric is the measured C1–C6 grade set + per-check evidence.
	Rubric PortRubric `json:"rubric"`
	// MeasuredCeiling is the highest honored tier level the rubric rolled up to.
	MeasuredCeiling PortTierLevel `json:"measuredCeiling"`
	// Feasible is false when the gate (C1/C2/C3) failed — no tier is honored.
	Feasible bool `json:"feasible"`
	// WhatRuns lists the observable capabilities that DID work (per honored check).
	WhatRuns []string `json:"whatRuns,omitempty"`
	// WhatDoesnt lists the capabilities that did NOT work (per unmet check).
	WhatDoesnt []string `json:"whatDoesnt,omitempty"`
	// AchievableHA is the throughput-vs-HA-replication breakdown.
	AchievableHA AchievableHA `json:"achievableHa"`
	// HonoredTiers is the full ladder with each tier's honored/excluded verdict +
	// reason (every excluded tier carries a Reason — "don't ship a tier whose
	// guide you can't honor").
	HonoredTiers []HonoredTier `json:"honoredTiers"`
	// GlueRepo is the buildFromGit glue-repo coordinates + readiness (Phase 4).
	GlueRepo GlueRepo `json:"glueRepo"`
	// UnresolvedConstraints names the honesty residue — discoveries the loop could
	// not resolve (the HARD-band no-signal class, recon constraints carried
	// forward). Surfaced verbatim in the report and the knowledge-base fragment.
	UnresolvedConstraints []string `json:"unresolvedConstraints,omitempty"`
}

// BuildFitCeilingInput carries the MEASURED rubric + the HA/glue facts the
// handler gathered (agent-reported, mocked in tests) into the pure FitCeiling
// builder. The builder is a pure projection — it does NOT grade (the C* graders
// already did) and does NOT call ops.
type BuildFitCeilingInput struct {
	Plan   PortPlan
	Rubric PortRubric
	HA     AchievableHA
	Glue   GlueRepo
	// FinalAcquisition is the acquisition strategy the working deployment ended
	// on (may differ from Plan.Acquisition after a T1 escalation). When empty the
	// builder falls back to Plan.Acquisition.
	FinalAcquisition AcquisitionStrategy
	// ExtraConstraints are loop-discovered unresolved constraints to append to the
	// recon plan's own Constraints (deduped).
	ExtraConstraints []string
}

// whatRunsLabels maps a check to its WhatRuns / WhatDoesnt human label.
var whatRunsLabels = map[PortCheck]string{
	PortCheckBuilds:   "builds on Zerops",
	PortCheckBoots:    "boots stable (survives the stability hold)",
	PortCheckServes:   "serves HTTP",
	PortCheckCoreFlow: "core flow works (agent-authored probe)",
	PortCheckPersists: "data persists across a redeploy",
	PortCheckHA:       "HA-replication capable",
}

// orderedChecks is the C1→C6 ordering for the WhatRuns/WhatDoesnt projection.
var orderedChecks = []PortCheck{
	PortCheckBuilds, PortCheckBoots, PortCheckServes,
	PortCheckCoreFlow, PortCheckPersists, PortCheckHA,
}

// checkFullySatisfied reports whether a check's grade clears its "runs" bar. The
// gate/serve/core checks clear at grade≥1; persistence + HA clear only at grade 2
// (a partial — ephemeral-FS survival / throughput-only scaling — is NOT a claim
// the FitCeiling lists as "runs").
func checkFullySatisfied(c PortCheck, grade int) bool {
	//exhaustive:ignore — only persistence/HA need the grade==2 bar; every other check falls through to grade>=1.
	switch c {
	case PortCheckPersists, PortCheckHA:
		return grade == 2
	default:
		return grade >= 1
	}
}

// BuildFitCeiling is the PURE FitCeiling projection over a measured rubric + the
// gathered HA/glue facts. No I/O, no ops, no grading — table-testable. It rolls
// up the honored tiers, splits WhatRuns/WhatDoesnt off the rubric grades, and
// carries the recon band estimate alongside the measured ceiling.
func BuildFitCeiling(in BuildFitCeilingInput) FitCeiling {
	tiers := RollUpHonoredTiers(in.Rubric)
	ceiling, feasible := HighestHonoredTier(in.Rubric)

	acq := in.FinalAcquisition
	if acq == "" {
		acq = in.Plan.Acquisition
	}

	fc := FitCeiling{
		Target:          in.Plan.Target,
		Acquisition:     acq,
		Band:            in.Plan.Band,
		Rubric:          in.Rubric,
		MeasuredCeiling: ceiling,
		Feasible:        feasible,
		AchievableHA:    in.HA,
		HonoredTiers:    tiers,
		GlueRepo:        in.Glue,
	}

	for _, c := range orderedChecks {
		grade := in.Rubric.grade(c)
		label := whatRunsLabels[c]
		if checkFullySatisfied(c, grade) {
			fc.WhatRuns = append(fc.WhatRuns, label)
		} else {
			fc.WhatDoesnt = append(fc.WhatDoesnt, label)
		}
	}

	fc.UnresolvedConstraints = mergeConstraints(in.Plan.Constraints, in.ExtraConstraints)
	return fc
}

// mergeConstraints concatenates the recon plan constraints with the loop-
// discovered extras, deduping (order-preserving: plan constraints first).
func mergeConstraints(planConstraints, extra []string) []string {
	if len(planConstraints) == 0 && len(extra) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(planConstraints)+len(extra))
	out := make([]string, 0, len(planConstraints)+len(extra))
	for _, c := range planConstraints {
		if c == "" || seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	for _, c := range extra {
		if c == "" || seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
