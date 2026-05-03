package recipe

import (
	"strings"
	"testing"
)

// TestBrief_Scaffold_IncludesContentRubric — the scaffold brief carries
// the placement rubric + fragment-id list so a sub-agent records the
// right fragment into the right surface without the engine guessing
// classification post-hoc. Run-8-readiness §2.F.
// TestBrief_Scaffold_TeachesDecisionRecording — run-16 §6.2 retired
// the legacy placement-rubric (which taught content authoring
// in-phase) and replaced it with `decision_recording.md`. The scaffold
// sub-agent now records facts only; documentation surfaces are
// authored by the codebase-content sub-agent at phase 5. Replaces the
// pre-run-16 TestBrief_Scaffold_IncludesContentRubric expectations.
func TestBrief_Scaffold_TeachesDecisionRecording(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	brief, err := BuildScaffoldBrief(plan, plan.Codebases[0], nil)
	if err != nil {
		t.Fatalf("BuildScaffoldBrief: %v", err)
	}
	for _, anchor := range []string{
		"porter_change",
		"field_rationale",
		"record-fact",
	} {
		if !strings.Contains(brief.Body, anchor) {
			t.Errorf("scaffold brief missing decision-recording anchor %q", anchor)
		}
	}
	// Citation-map reference (topic → guide binding) must still be present
	// — scaffold reads citation guides when filling per-managed-service shells.
	if !strings.Contains(brief.Body, "zerops_knowledge") {
		t.Error("scaffold brief missing zerops_knowledge citation reference")
	}
	// The legacy fragment-id taxonomy must NOT appear in the scaffold
	// brief — those fragments are authored by phase-5 codebase-content,
	// not by scaffold.
	for _, legacyFrag := range []string{
		"codebase/<h>/integration-guide",
		"codebase/<h>/knowledge-base",
		"codebase/<h>/claude-md/service-facts",
	} {
		if strings.Contains(brief.Body, legacyFrag) {
			t.Errorf("scaffold brief still teaches legacy fragment id %q — content authoring moved to phase 5 (run-16 §6.2)", legacyFrag)
		}
	}
}

// TestBrief_Scaffold_IncludesInitCommandsModel — Run-21 R2-1 (#5).
//
// Pre-fix the predicate was `anyCodebaseHasInitCommands(plan)` — leaked
// the atom into every codebase's brief whenever any peer declared
// initCommands. Fixed to `cb.HasInitCommands` (per-codebase). This test
// now asserts:
//  1. The brief for the codebase that DOES author initCommands carries
//     the atom.
//  2. The brief for a SIBLING codebase that does NOT author initCommands
//     omits the atom — even though a peer in the same plan does.
func TestBrief_Scaffold_IncludesInitCommandsModel(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	// API authors initCommands; the SPA + worker do not.
	plan.Codebases[0].HasInitCommands = true
	for i := 1; i < len(plan.Codebases); i++ {
		plan.Codebases[i].HasInitCommands = false
	}

	apiBrief, err := BuildScaffoldBrief(plan, plan.Codebases[0], nil)
	if err != nil {
		t.Fatalf("BuildScaffoldBrief api: %v", err)
	}
	for _, anchor := range []string{
		"execOnce",
		"${appVersionId}",
		"In-script guard",
		"Decomposition",
	} {
		if !strings.Contains(apiBrief.Body, anchor) {
			t.Errorf("api brief missing init-commands-model anchor %q", anchor)
		}
	}

	// Sister codebase (SPA / worker) — must NOT carry the atom body
	// even though API peer authors initCommands. Anchor `In-script
	// guard` is unique to the atom body; bare `execOnce` plus the
	// atom header `execOnce — key shape by lifetime` both appear as
	// cross-references in cross-service-urls.md and are not load
	// signals.
	for i := 1; i < len(plan.Codebases); i++ {
		sib := plan.Codebases[i]
		sibBrief, err := BuildScaffoldBrief(plan, sib, nil)
		if err != nil {
			t.Fatalf("BuildScaffoldBrief %s: %v", sib.Hostname, err)
		}
		if strings.Contains(sibBrief.Body, "In-script guard") ||
			strings.Contains(sibBrief.Body, "Three key shapes, three lifetimes") {
			t.Errorf("init-commands-model leaked into sibling %q brief (cb.HasInitCommands=false but peer api has it)", sib.Hostname)
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

// TestBrief_Feature_TeachesDecisionRecording — run-16 §6.2 retired the
// content-extension rubric; feature sub-agent records `porter_change`
// + `field_rationale` facts at densest context, and the codebase-
// content sub-agent at phase 5 synthesizes IG/KB. Replaces the pre-
// run-16 TestBrief_Feature_AppendSemantics that asserted EXTEND/append
// teaching.
func TestBrief_Feature_TeachesDecisionRecording(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	brief, err := BuildFeatureBrief(plan)
	if err != nil {
		t.Fatalf("BuildFeatureBrief: %v", err)
	}
	for _, anchor := range []string{
		"porter_change",
		"field_rationale",
	} {
		if !strings.Contains(brief.Body, anchor) {
			t.Errorf("feature brief missing decision-recording anchor %q", anchor)
		}
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

// TestBrief_Scaffold_FrontendSPA_UnderTargetSize — Run-21 R2-1.
//
// Frontend-SPA scaffold brief should drop from the pre-R2-1 ~49 KB
// down to ~32 KB after the slim-down (full decision_recording.md
// → slim variant; yaml-comment-style.md drop). 35 KB is the soft
// target; the hard upper bound is ScaffoldBriefCap (48 KB).
func TestBrief_Scaffold_FrontendSPA_UnderTargetSize(t *testing.T) {
	t.Parallel()

	const targetCap = 35 * 1024
	plan := syntheticShowcasePlan()
	// app is the RoleFrontend codebase (largest scaffold brief shape —
	// adds tier-fact table + build-tool-host-allowlist + spa-static).
	var spa Codebase
	for _, cb := range plan.Codebases {
		if cb.Role == RoleFrontend {
			spa = cb
			break
		}
	}
	brief, err := BuildScaffoldBrief(plan, spa, nil)
	if err != nil {
		t.Fatalf("BuildScaffoldBrief: %v", err)
	}
	if brief.Bytes > targetCap {
		t.Errorf("frontend SPA scaffold brief %d bytes exceeds R2-1 target %d (post-slim ceiling)", brief.Bytes, targetCap)
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
