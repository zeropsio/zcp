package port

import (
	"slices"
	"testing"
)

// TestBuildFitCeiling_Shape pins the FitCeiling shape on a full-HA port: measured
// ceiling, feasible, all tiers honored, WhatRuns covers every check, glue repo +
// HA carried through.
func TestBuildFitCeiling_Shape(t *testing.T) {
	t.Parallel()
	in := BuildFitCeilingInput{
		Plan: PortPlan{
			Target:      "strapi",
			Acquisition: AcquireSourceBuild,
			Band:        BandEasy,
		},
		Rubric: rubricFrom(2, 2, 1, 1, 2, 2),
		HA: AchievableHA{
			AppContainers: 2,
			ManagedHADeps: []string{"postgresql@18"},
		},
		Glue: GlueRepo{URL: "github.com/zerops-recipe-apps/strapi", CommittedSHA: "abc123", BuildFromGitReady: true},
	}
	fc := BuildFitCeiling(in)

	if fc.Target != "strapi" {
		t.Errorf("target = %q", fc.Target)
	}
	if !fc.Feasible {
		t.Errorf("full-HA rubric must be feasible")
	}
	if fc.MeasuredCeiling != PortTierHAProd {
		t.Errorf("measured ceiling = %d, want ha-prod", fc.MeasuredCeiling)
	}
	if fc.Band != BandEasy {
		t.Errorf("band estimate must be carried alongside the measured ceiling")
	}
	if len(fc.HonoredTiers) != 6 {
		t.Errorf("honored tiers = %d, want 6", len(fc.HonoredTiers))
	}
	if len(fc.WhatRuns) != 6 || len(fc.WhatDoesnt) != 0 {
		t.Errorf("full-HA: WhatRuns=%v WhatDoesnt=%v", fc.WhatRuns, fc.WhatDoesnt)
	}
	if !fc.GlueRepo.BuildFromGitReady || fc.GlueRepo.URL == "" {
		t.Errorf("glue repo not carried through: %+v", fc.GlueRepo)
	}
	if fc.AchievableHA.AppContainers != 2 {
		t.Errorf("achievableHA not carried through")
	}
}

// TestBuildFitCeiling_PartialReportsBothCeilingAndBand: a C5=2,C6=1 port reports
// the measured ceiling (small-prod) distinct from the recon band, and tier 5
// lands in WhatDoesnt with an honored-tier reason.
func TestBuildFitCeiling_PartialReportsBothCeilingAndBand(t *testing.T) {
	t.Parallel()
	in := BuildFitCeilingInput{
		Plan:   PortPlan{Target: "posthog", Acquisition: AcquireCraneImageLift, Band: BandHard},
		Rubric: rubricFrom(2, 2, 1, 1, 2, 1),
		HA:     AchievableHA{AppContainers: 2, NotHA: []string{"clickhouse@25.3 stuck NON_HA"}},
	}
	fc := BuildFitCeiling(in)

	if fc.MeasuredCeiling != PortTierSmallProd {
		t.Errorf("measured ceiling = %d, want small-prod", fc.MeasuredCeiling)
	}
	if fc.Band != BandHard {
		t.Errorf("recon band must remain HARD alongside the measured small-prod ceiling")
	}
	// HA-replication is a 2-only check, so C6=1 lands in WhatDoesnt.
	if !slices.Contains(fc.WhatDoesnt, whatRunsLabels[PortCheckHA]) {
		t.Errorf("C6=1 (throughput only) should be in WhatDoesnt, got %v", fc.WhatDoesnt)
	}
	if !slices.Contains(fc.WhatRuns, whatRunsLabels[PortCheckPersists]) {
		t.Errorf("C5=2 should be in WhatRuns, got %v", fc.WhatRuns)
	}
	// Tier 5 reason present.
	var haReason string
	for _, ht := range fc.HonoredTiers {
		if ht.Level == PortTierHAProd {
			haReason = ht.Reason
		}
	}
	if haReason == "" {
		t.Errorf("tier 5 must carry an exclusion reason")
	}
}

// TestBuildFitCeiling_Infeasible: gate failure → not feasible, ceiling at 0, all
// checks in WhatDoesnt.
func TestBuildFitCeiling_Infeasible(t *testing.T) {
	t.Parallel()
	fc := BuildFitCeiling(BuildFitCeilingInput{
		Plan:   PortPlan{Target: "x", Band: BandMedium},
		Rubric: rubricFrom(0, 0, 0, 0, 0, 0),
	})
	if fc.Feasible {
		t.Errorf("infeasible rubric must report Feasible=false")
	}
	// The measured ceiling of an infeasible port is the PortTierNone sentinel
	// (-1), NOT tier 0 — a consumer that ignores Feasible must not be able to
	// misread "nothing runs" as "tier 0 (AI Agent) honored".
	if fc.MeasuredCeiling != PortTierNone {
		t.Errorf("infeasible MeasuredCeiling must be the PortTierNone sentinel (%d), got %d", PortTierNone, fc.MeasuredCeiling)
	}
	if fc.MeasuredCeiling == PortTierAIAgent {
		t.Errorf("infeasible MeasuredCeiling must NOT equal a real honored tier (tier 0)")
	}
	if len(fc.WhatRuns) != 0 {
		t.Errorf("nothing runs on an infeasible port, got %v", fc.WhatRuns)
	}
	if len(fc.WhatDoesnt) != 6 {
		t.Errorf("all six checks should be in WhatDoesnt, got %v", fc.WhatDoesnt)
	}
}

// TestBuildFitCeiling_C2CrashLoopNotInWhatRuns pins the WhatRuns/WhatDoesnt half
// of the C2 stable-boot contract: a crash-looping run (C2=1) honors no tier AND
// must NOT advertise "boots stable" in WhatRuns — the partial grade lands in
// WhatDoesnt, exactly like the gate. (The gate half is pinned in
// TestRollUpHonoredTiers_C2CrashLoopFailsGate.)
func TestBuildFitCeiling_C2CrashLoopNotInWhatRuns(t *testing.T) {
	t.Parallel()
	fc := BuildFitCeiling(BuildFitCeilingInput{
		Plan:   PortPlan{Target: "x", Band: BandMedium},
		Rubric: rubricFrom(2, 1, 1, 1, 0, 0), // builds, C2=1 crash-loop, serves, core-flow
	})
	bootsLabel := whatRunsLabels[PortCheckBoots]
	if slices.Contains(fc.WhatRuns, bootsLabel) {
		t.Errorf("C2=1 (crash-loop) must NOT appear in WhatRuns, got %v", fc.WhatRuns)
	}
	if !slices.Contains(fc.WhatDoesnt, bootsLabel) {
		t.Errorf("C2=1 (crash-loop) must appear in WhatDoesnt, got %v", fc.WhatDoesnt)
	}
	// And it honors no tier — infeasible, sentinel ceiling.
	if fc.Feasible || fc.MeasuredCeiling != PortTierNone {
		t.Errorf("C2=1 must be infeasible with PortTierNone ceiling, got feasible=%v ceiling=%d", fc.Feasible, fc.MeasuredCeiling)
	}
}

// TestBuildFitCeiling_FinalAcquisitionOverridesPlan: a T1-escalated port reports
// the FINAL acquisition strategy, not recon's first guess.
func TestBuildFitCeiling_FinalAcquisitionOverridesPlan(t *testing.T) {
	t.Parallel()
	fc := BuildFitCeiling(BuildFitCeilingInput{
		Plan:             PortPlan{Target: "x", Acquisition: AcquireSourceBuild, Band: BandEasy},
		Rubric:           rubricFrom(2, 2, 1, 0, 0, 0),
		FinalAcquisition: AcquirePrebuiltBinary,
	})
	if fc.Acquisition != AcquirePrebuiltBinary {
		t.Errorf("acquisition = %q, want the escalated prebuilt-binary", fc.Acquisition)
	}
}

// TestBuildFitCeiling_MergesConstraints: plan constraints + loop-discovered extras
// dedupe into UnresolvedConstraints.
func TestBuildFitCeiling_MergesConstraints(t *testing.T) {
	t.Parallel()
	fc := BuildFitCeiling(BuildFitCeilingInput{
		Plan:             PortPlan{Target: "x", Band: BandHard, Constraints: []string{"dep foo unmapped"}},
		Rubric:           rubricFrom(2, 2, 1, 1, 2, 1),
		ExtraConstraints: []string{"Kafka SASL per-producer prefix (no failure signal)", "dep foo unmapped"},
	})
	if len(fc.UnresolvedConstraints) != 2 {
		t.Errorf("constraints should dedupe to 2, got %v", fc.UnresolvedConstraints)
	}
	if fc.UnresolvedConstraints[0] != "dep foo unmapped" {
		t.Errorf("plan constraints must come first, got %v", fc.UnresolvedConstraints)
	}
}
