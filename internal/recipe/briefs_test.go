package recipe

import (
	"strings"
	"testing"
)

func TestBriefCompose_ScaffoldUnderCap(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	for _, cb := range plan.Codebases {
		t.Run(cb.Hostname, func(t *testing.T) {
			t.Parallel()
			brief, err := BuildScaffoldBrief(plan, cb, nil)
			if err != nil {
				t.Fatalf("BuildScaffoldBrief: %v", err)
			}
			if brief.Bytes > ScaffoldBriefCap {
				t.Errorf("scaffold brief for %s: %d bytes exceeds %d cap",
					cb.Hostname, brief.Bytes, ScaffoldBriefCap)
			}
			if !strings.Contains(brief.Body, "# Scaffold brief — "+cb.Hostname) {
				t.Error("missing scaffold brief header")
			}
			if !strings.Contains(brief.Body, "Platform obligations") {
				t.Error("missing platform obligations section")
			}
		})
	}
}

// TestBuildFinalizeBrief_CorrectCodebasePaths — run-11 gap S-1.
// Brief body names each codebase's actual SourceRoot path verbatim;
// no obsolete pre-§L paths.
func TestBuildFinalizeBrief_CorrectCodebasePaths(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	for i := range plan.Codebases {
		plan.Codebases[i].SourceRoot = "/var/www/" + plan.Codebases[i].Hostname + "dev"
	}
	brief, err := BuildFinalizeBrief(plan)
	if err != nil {
		t.Fatalf("BuildFinalizeBrief: %v", err)
	}
	for _, cb := range plan.Codebases {
		if !strings.Contains(brief.Body, cb.SourceRoot) {
			t.Errorf("brief missing SourceRoot %q", cb.SourceRoot)
		}
	}
	if strings.Contains(brief.Body, "/var/www/synth-showcase/api/") {
		t.Error("brief carries obsolete pre-§L path /var/www/<slug>/<host>/")
	}
}

// TestBuildFinalizeBrief_CorrectFragmentMath — run-11 S-1. Fragment
// count derives from Plan structure, not hand-typed math.
func TestBuildFinalizeBrief_CorrectFragmentMath(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	for i := range plan.Codebases {
		plan.Codebases[i].SourceRoot = "/var/www/" + plan.Codebases[i].Hostname + "dev"
	}
	brief, err := BuildFinalizeBrief(plan)
	if err != nil {
		t.Fatalf("BuildFinalizeBrief: %v", err)
	}
	// 3 codebases + 4 services = 7 import-comment hosts
	// 6 tiers × (1 env intro + 1 project comment + 7 service comments) + 1 root intro = 6×9 + 1 = 55
	if !strings.Contains(brief.Body, "Total: 1 root intro") {
		t.Errorf("brief math section missing total expression: %q", brief.Body)
	}
	if !strings.Contains(brief.Body, "= 55 fragments") {
		t.Errorf("expected fragment count = 55 in brief, got: %q", brief.Body)
	}
}

// TestBuildFinalizeBrief_NoCiteGuideInstruction — S-1 must not produce
// the citation-noise instruction that produced run-10's literal
// "Cite \`x\`" output. Brief content lints against the phrase.
func TestBuildFinalizeBrief_NoCiteGuideInstruction(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	for i := range plan.Codebases {
		plan.Codebases[i].SourceRoot = "/var/www/" + plan.Codebases[i].Hostname + "dev"
	}
	brief, err := BuildFinalizeBrief(plan)
	if err != nil {
		t.Fatalf("BuildFinalizeBrief: %v", err)
	}
	for _, banned := range []string{
		"Cite `rolling-deploys`",
		"cite by name in the prose",
	} {
		if strings.Contains(brief.Body, banned) {
			t.Errorf("brief contains banned phrase %q", banned)
		}
	}
}

// TestBuildFinalizeBrief_ValidatorTripwires — brief surfaces
// finalize-specific validator tripwires (citation-noise, IG-shape,
// self-inflicted-litmus references).
func TestBuildFinalizeBrief_ValidatorTripwires(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	for i := range plan.Codebases {
		plan.Codebases[i].SourceRoot = "/var/www/" + plan.Codebases[i].Hostname + "dev"
	}
	brief, err := BuildFinalizeBrief(plan)
	if err != nil {
		t.Fatalf("BuildFinalizeBrief: %v", err)
	}
	for _, anchor := range []string{
		"Validator tripwires",
		"porter voice",
		"Citations are author-time signals",
		"Self-inflicted litmus",
	} {
		if !strings.Contains(brief.Body, anchor) {
			t.Errorf("brief missing tripwire anchor %q", anchor)
		}
	}
}

// TestBuildFinalizeBrief_UnderCap — S-1 §6 watch.
func TestBuildFinalizeBrief_UnderCap(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	for i := range plan.Codebases {
		plan.Codebases[i].SourceRoot = "/var/www/" + plan.Codebases[i].Hostname + "dev"
	}
	brief, err := BuildFinalizeBrief(plan)
	if err != nil {
		t.Fatalf("BuildFinalizeBrief: %v", err)
	}
	if brief.Bytes > FinalizeBriefCap {
		t.Errorf("finalize brief: %d bytes exceeds %d cap", brief.Bytes, FinalizeBriefCap)
	}
}

func TestBriefCompose_FeatureUnderCap(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	brief, err := BuildFeatureBrief(plan)
	if err != nil {
		t.Fatalf("BuildFeatureBrief: %v", err)
	}
	if brief.Bytes > FeatureBriefCap {
		t.Errorf("feature brief: %d bytes exceeds %d cap", brief.Bytes, FeatureBriefCap)
	}
	for _, cb := range plan.Codebases {
		if !strings.Contains(brief.Body, cb.Hostname) {
			t.Errorf("feature brief missing codebase %q in symbol table", cb.Hostname)
		}
	}
	for _, svc := range plan.Services {
		if !strings.Contains(brief.Body, svc.Hostname) {
			t.Errorf("feature brief missing service %q in symbol table", svc.Hostname)
		}
	}
}
