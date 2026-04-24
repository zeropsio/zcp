package recipe

import (
	"strings"
	"testing"
)

// TestBrief_Scaffold_IncludesContentRubric — the scaffold brief carries
// the placement rubric + fragment-id list so a sub-agent records the
// right fragment into the right surface without the engine guessing
// classification post-hoc. Run-8-readiness §2.F.
func TestBrief_Scaffold_IncludesContentRubric(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	brief, err := BuildScaffoldBrief(plan, plan.Codebases[0], nil)
	if err != nil {
		t.Fatalf("BuildScaffoldBrief: %v", err)
	}
	// Placement rubric anchors.
	for _, anchor := range []string{
		"Placement",
		"knowledge-base",
		"integration-guide",
		"claude-md",
		"yaml inline comment",
	} {
		if !strings.Contains(brief.Body, anchor) {
			t.Errorf("scaffold brief missing placement-rubric anchor %q", anchor)
		}
	}
	// All 5 fragment ids the scaffold sub-agent is expected to record.
	for _, frag := range []string{
		"codebase/<h>/intro",
		"codebase/<h>/integration-guide",
		"codebase/<h>/knowledge-base",
		"codebase/<h>/claude-md/service-facts",
		"codebase/<h>/claude-md/notes",
	} {
		if !strings.Contains(brief.Body, frag) {
			t.Errorf("scaffold brief missing fragment id %q", frag)
		}
	}
	// Citation-map reference (topic → guide binding) must be present.
	if !strings.Contains(brief.Body, "zerops_knowledge") {
		t.Error("scaffold brief missing zerops_knowledge citation reference")
	}
}

// TestBrief_Scaffold_IncludesInitCommandsModel — when any codebase in
// the plan declares HasInitCommands, the brief injects the execOnce
// key-shape concept atom so the sub-agent picks the right key shape.
func TestBrief_Scaffold_IncludesInitCommandsModel(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	// Mark the API codebase as authoring initCommands.
	plan.Codebases[0].HasInitCommands = true

	brief, err := BuildScaffoldBrief(plan, plan.Codebases[0], nil)
	if err != nil {
		t.Fatalf("BuildScaffoldBrief: %v", err)
	}
	for _, anchor := range []string{
		"execOnce",
		"${appVersionId}",
		"In-script guard",
		"Decomposition",
	} {
		if !strings.Contains(brief.Body, anchor) {
			t.Errorf("scaffold brief missing init-commands-model anchor %q", anchor)
		}
	}
}

// TestBrief_Scaffold_OmitsInitCommandsModelWhenUnused — when no
// codebase declares initCommands, the brief does not inject the atom
// (saves budget for codebases that don't have migrations).
func TestBrief_Scaffold_OmitsInitCommandsModelWhenUnused(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	for i := range plan.Codebases {
		plan.Codebases[i].HasInitCommands = false
	}
	brief, err := BuildScaffoldBrief(plan, plan.Codebases[0], nil)
	if err != nil {
		t.Fatalf("BuildScaffoldBrief: %v", err)
	}
	if strings.Contains(brief.Body, "${appVersionId}") {
		t.Error("init-commands-model injected when no codebase declares initCommands")
	}
}

// TestBrief_Feature_AppendSemantics — the feature brief carries the
// content-extension rubric so the sub-agent extends scaffold's
// fragments rather than replacing them.
func TestBrief_Feature_AppendSemantics(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	brief, err := BuildFeatureBrief(plan)
	if err != nil {
		t.Fatalf("BuildFeatureBrief: %v", err)
	}
	if !strings.Contains(brief.Body, "EXTEND") {
		t.Error("feature brief missing append-semantics anchor")
	}
	if !strings.Contains(brief.Body, "append") {
		t.Error("feature brief missing 'append' term — content-extension rubric not inlined")
	}
}

// TestBrief_Feature_SeedInjectsInitCommandsModel — when Plan.FeatureKinds
// declares a seed or scout-import step, the feature brief injects the
// execOnce concept atom.
func TestBrief_Feature_SeedInjectsInitCommandsModel(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	plan.FeatureKinds = []string{"crud", "seed", "search-items"}

	brief, err := BuildFeatureBrief(plan)
	if err != nil {
		t.Fatalf("BuildFeatureBrief: %v", err)
	}
	if !strings.Contains(brief.Body, "execOnce") {
		t.Error("feature brief missing init-commands-model when FeatureKinds declares seed")
	}

	// Without seed, no injection.
	plan2 := syntheticShowcasePlan()
	plan2.FeatureKinds = []string{"crud"}
	brief2, _ := BuildFeatureBrief(plan2)
	if strings.Contains(brief2.Body, "${appVersionId}") {
		t.Error("init-commands-model injected when no seed/scout-import declared")
	}
}

// TestPhaseEntry_Finalize_ListsRootEnvFragments — the finalize atom
// tells the main agent exactly which root + env fragment ids to
// author, so no guessing happens at finalize time.
func TestPhaseEntry_Finalize_ListsRootEnvFragments(t *testing.T) {
	t.Parallel()

	body := loadPhaseEntry("finalize")
	for _, id := range []string{
		"root/intro",
		"env/0/intro",
		"env/5/intro",
		"env/<N>/import-comments/project",
		"env/<N>/import-comments/<hostname>",
	} {
		if !strings.Contains(body, id) {
			t.Errorf("finalize atom missing fragment id %q", id)
		}
	}
}

// TestInitCommandsModel_TopicsListed — the ported concept atom covers
// every topic the plan requires: both key shapes + revision suffix +
// in-script-guard pitfall + decomposition rule. Pins the content
// contract so a future trim doesn't drop load-bearing teaching.
func TestInitCommandsModel_TopicsListed(t *testing.T) {
	t.Parallel()

	body, err := readAtom("principles/init-commands-model.md")
	if err != nil {
		t.Fatalf("read atom: %v", err)
	}
	for _, must := range []string{
		"${appVersionId}",       // per-deploy key
		"bootstrap-seed-r01",    // static-key example
		"r02",                   // revision suffix pattern
		"if (count > 0) return", // in-script-guard pitfall
		"Decomposition",         // decomposition rule
	} {
		if !strings.Contains(body, must) {
			t.Errorf("init-commands-model missing required topic anchor %q", must)
		}
	}
}

// TestCitationMap_CoversInitCommands — F adds init-commands to the
// engine-side citation map so KB fragments that cite execOnce /
// init-commands pick up the guide id at finalize.
func TestCitationMap_CoversInitCommands(t *testing.T) {
	t.Parallel()

	if GuideForTopic("init-commands") == "" {
		t.Error("CitationMap missing init-commands topic")
	}
	if GuideForTopic("execOnce") == "" {
		t.Error("CitationMap missing execOnce topic")
	}
}
