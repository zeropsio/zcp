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

// TestBrief_Scaffold_InjectsDevLoopAtom — run-9-readiness §2.G1.
// The dev-loop atom ships with every scaffold brief (unconditional
// injection) so implicit-webserver codebases with a compiled frontend
// still get the `zerops_dev_server` guidance.
func TestBrief_Scaffold_InjectsDevLoopAtom(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	brief, err := BuildScaffoldBrief(plan, plan.Codebases[0], nil)
	if err != nil {
		t.Fatalf("BuildScaffoldBrief: %v", err)
	}
	for _, anchor := range []string{
		"zsc noop",
		"zerops_dev_server",
		"deployFiles: .",
	} {
		if !strings.Contains(brief.Body, anchor) {
			t.Errorf("scaffold brief missing dev-loop anchor %q", anchor)
		}
	}
}

// TestBrief_Scaffold_DevLoopInjectedForImplicitWebserver — the atom is
// injected even when the codebase's backend is php-nginx/static so the
// sub-agent sees the carve-out for compiled frontends.
func TestBrief_Scaffold_DevLoopInjectedForImplicitWebserver(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	for i := range plan.Codebases {
		plan.Codebases[i].BaseRuntime = "php-nginx@8.4"
	}
	brief, err := BuildScaffoldBrief(plan, plan.Codebases[0], nil)
	if err != nil {
		t.Fatalf("BuildScaffoldBrief: %v", err)
	}
	if !strings.Contains(brief.Body, "zerops_dev_server") {
		t.Error("dev-loop atom should inject unconditionally — implicit-webserver codebases may compile a frontend")
	}
}

// TestBrief_Scaffold_IncludesMountVsContainerAtom — run-9-readiness §2.G2.
// Unconditional injection across scaffold + feature briefs.
func TestBrief_Scaffold_IncludesMountVsContainerAtom(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	brief, err := BuildScaffoldBrief(plan, plan.Codebases[0], nil)
	if err != nil {
		t.Fatalf("BuildScaffoldBrief: %v", err)
	}
	if !strings.Contains(brief.Body, `ssh <hostname>dev "cd /var/www`) {
		t.Error("scaffold brief missing mount-vs-container ssh pattern")
	}
}

// TestBrief_Feature_IncludesMountVsContainerAtom — same atom injected
// into the feature brief so feature sub-agents stop running local
// `npm install` against the mount.
func TestBrief_Feature_IncludesMountVsContainerAtom(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	brief, err := BuildFeatureBrief(plan)
	if err != nil {
		t.Fatalf("BuildFeatureBrief: %v", err)
	}
	if !strings.Contains(brief.Body, `ssh <hostname>dev "cd /var/www`) {
		t.Error("feature brief missing mount-vs-container ssh pattern")
	}
}

// TestBrief_Scaffold_UnderCap_WithDevLoop — adding the dev-loop atom
// still fits under the 5 KB scaffold brief cap.
func TestBrief_Scaffold_UnderCap_WithDevLoop(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	brief, err := BuildScaffoldBrief(plan, plan.Codebases[0], nil)
	if err != nil {
		t.Fatalf("BuildScaffoldBrief: %v", err)
	}
	if brief.Bytes > ScaffoldBriefCap {
		t.Errorf("scaffold brief %d bytes exceeds cap %d", brief.Bytes, ScaffoldBriefCap)
	}
}

// TestFeatureBrief_IncludesV3FactRecordingSection — run-9-readiness §2.B.
// The feature brief teaches sub-agents to use v3's `zerops_recipe
// action=record-fact` (not the legacy `zerops_record_fact`) so facts
// land in `facts.jsonl` where the classifier + validators see them.
func TestFeatureBrief_IncludesV3FactRecordingSection(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	brief, err := BuildFeatureBrief(plan)
	if err != nil {
		t.Fatalf("BuildFeatureBrief: %v", err)
	}
	if !strings.Contains(brief.Body, "zerops_recipe action=record-fact") {
		t.Error("feature brief missing v3 fact-recording tool reference")
	}
	// The legacy tool must only appear when explicitly contrasted; the
	// brief's rule must name it as NOT the right tool — but the string
	// `zerops_record_fact` alone is fine if it's labeled as the legacy
	// one. What we ban is a bare instruction to use it. Check the rule
	// line spells out the redirection.
	if !strings.Contains(brief.Body, "NOT") {
		t.Error("feature brief should explicitly redirect away from the legacy tool")
	}
	if !strings.Contains(brief.Body, "browser-verification") {
		t.Error("feature brief missing browser-verification surfaceHint teaching")
	}
}

// TestPhaseEntry_Feature_BrowserWalkUsesV3Tool — run-9-readiness §2.B.
// The feature phase-entry atom's browser-walk step prescribes the v3
// `zerops_recipe action=record-fact` tool with
// `surfaceHint: browser-verification`.
func TestPhaseEntry_Feature_BrowserWalkUsesV3Tool(t *testing.T) {
	t.Parallel()

	body := loadPhaseEntry(PhaseFeature)
	if body == "" {
		t.Fatal("feature phase-entry atom unavailable")
	}
	if !strings.Contains(body, "zerops_recipe action=record-fact") {
		t.Error("feature phase-entry browser-walk step still uses the legacy tool")
	}
	if !strings.Contains(body, "surfaceHint: browser-verification") {
		t.Error("feature phase-entry missing browser-verification surfaceHint")
	}
}

// TestClassify_BrowserVerificationIsOperational — run-9-readiness §2.B
// (optional classifier refinement). `browser-verification` surfaceHint
// routes to the operational class so the record is publishable.
func TestClassify_BrowserVerificationIsOperational(t *testing.T) {
	t.Parallel()

	rec := FactRecord{
		Topic: "api-list-tab-browser", Symptom: "rows visible", Mechanism: "zerops_browser",
		SurfaceHint: "browser-verification", Citation: "none",
	}
	got := Classify(rec)
	if got != ClassOperational {
		t.Errorf("browser-verification classified as %q, want %q", got, ClassOperational)
	}
	if !IsPublishable(got) {
		t.Error("browser-verification classification should be publishable")
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
		"bootstrap-seed",        // canonical static-key example
		"<slug>.<operation>.v1", // arbitrary-static versioned key (§Q4)
		".v2",                   // version-bump re-run lever
		"if (count > 0) return", // in-script-guard pitfall
		"Decomposition",         // decomposition rule
	} {
		if !strings.Contains(body, must) {
			t.Errorf("init-commands-model missing required topic anchor %q", must)
		}
	}
}

// TestFinalizeAntiPatternsAtom_AllowsReplaceMode — run-13 §W. The
// previous "do not touch codebase/<h>/* ids" bullet contradicted §R
// (which intentionally enabled mode=replace for those ids so finalize
// CAN touch them when needed). Atom now says: codebase fragments
// SHOULD be green by finalize, and if a residual violation fires use
// mode=replace to correct.
func TestFinalizeAntiPatternsAtom_AllowsReplaceMode(t *testing.T) {
	t.Parallel()

	body, err := readAtom("briefs/finalize/anti_patterns.md")
	if err != nil {
		t.Fatalf("read atom: %v", err)
	}
	for _, must := range []string{
		"record-fragment mode=replace",
		"§R API was added exactly for this case",
	} {
		if !strings.Contains(body, must) {
			t.Errorf("anti_patterns missing replace-mode anchor %q", must)
		}
	}
}

// TestFeatureContentExtensionAtom_TeachesIGScopeRule — run-13 §I-feature.
// Run-12 apidev IG carried 7 numbered items + 2 unnumbered prose
// subsections (`### Cache-demo wrapper`, `### Liveness probe`) inside
// the integration-guide extract markers — the feature subagent
// appended recipe-internal contracts (cache-demo wrapper TTL, status
// aggregator endpoint) that §I would have routed to KB or
// claude-md/notes. The scaffold's IG-scope teaching didn't reach the
// feature brief; this atom now carries it explicitly.
func TestFeatureContentExtensionAtom_TeachesIGScopeRule(t *testing.T) {
	t.Parallel()

	body, err := readAtom("briefs/feature/content_extension.md")
	if err != nil {
		t.Fatalf("read atom: %v", err)
	}
	for _, must := range []string{
		"IG scope (extending scaffold's items)",
		"recipe-internal CONTRACT",
		"Aim for 0-1 IG appends",
	} {
		if !strings.Contains(body, must) {
			t.Errorf("content_extension missing IG-scope anchor %q", must)
		}
	}
}

// TestPlatformPrinciplesAtom_TeachesAliasResolutionTiming — run-13 §U.
// Run-12 scaffold-app recorded cross-service-alias-resolution-timing:
// `${apistage_zeropsSubdomain}` is a literal token until apistage's
// first deploy mints the URL; SPA Vite builds running before apistage
// deploys read the literal and inline it verbatim into dist/. The
// alias-type contracts table now teaches the build-time-vs-runtime
// asymmetry so the next SPA recipe doesn't rediscover the race.
func TestPlatformPrinciplesAtom_TeachesAliasResolutionTiming(t *testing.T) {
	t.Parallel()

	body, err := readAtom("briefs/scaffold/platform_principles.md")
	if err != nil {
		t.Fatalf("read atom: %v", err)
	}
	for _, must := range []string{
		"Resolution timing",
		"literal token",
		"Build-time-baked references",
		"no ordering concern",
	} {
		if !strings.Contains(body, must) {
			t.Errorf("platform_principles missing alias-resolution-timing anchor %q", must)
		}
	}
}

// TestInitCommandsAtom_TeachesDecomposedStepKeyDistinction — run-13 §N.
// Run-12 scaffold-api hit the execOnce-key-collision-across-decomposed-
// steps trap: two initCommands sharing the same `${appVersionId}`
// silently collapse to one lock — first runner wins, second runner
// sees the success marker and skips even though command tail differs.
// The atom now teaches per-step distinct keys explicitly so the next
// recipe doesn't rediscover it.
func TestInitCommandsAtom_TeachesDecomposedStepKeyDistinction(t *testing.T) {
	t.Parallel()

	body, err := readAtom("principles/init-commands-model.md")
	if err != nil {
		t.Fatalf("read atom: %v", err)
	}
	for _, must := range []string{
		"Distinct keys per step",
		"${appVersionId}-migrate",
		"${appVersionId}-seed",
		"collapse to one lock",
	} {
		if !strings.Contains(body, must) {
			t.Errorf("init-commands-model missing decomposed-step teaching anchor %q", must)
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
