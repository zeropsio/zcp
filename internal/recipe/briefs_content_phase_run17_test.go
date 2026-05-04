package recipe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Run-17 §6 — Tranche 1 brief embed tests. The codebase-content brief
// must carry the classification table + voice patterns + KB symptom-
// first fail-vs-pass + IG one-mechanism examples + citation-guide
// list + (conditionally) showcase tier worker supplements. These are
// the upstream closures for R-17-C1..C7.

func TestBuildCodebaseContentBrief_EmbedsClassificationTable(t *testing.T) {
	t.Parallel()
	plan := contentPhaseTestPlan()
	brief, err := BuildCodebaseContentBrief(plan, plan.Codebases[0], nil, nil)
	if err != nil {
		t.Fatalf("BuildCodebaseContentBrief: %v", err)
	}
	if !strings.Contains(brief.Body, "Classification × surface compatibility") {
		t.Error("brief missing classification × surface compatibility section header")
	}
	// Run-21 fix P0-2: all seven Classification enum values from
	// `internal/recipe/classify.go` must be named. The parenthesized
	// `(config|code|recipe-internal)` variants of `scaffold-decision`
	// don't exist as enum values; agents reading them invented kinds.
	for _, want := range []string{
		"platform-invariant",
		"intersection",
		"framework-quirk",
		"library-metadata",
		"scaffold-decision",
		"operational",
		"self-inflicted",
	} {
		if !strings.Contains(brief.Body, want) {
			t.Errorf("classification table missing row %q", want)
		}
	}
}

func TestBuildCodebaseContentBrief_EmbedsVoicePatterns(t *testing.T) {
	t.Parallel()
	plan := contentPhaseTestPlan()
	brief, err := BuildCodebaseContentBrief(plan, plan.Codebases[0], nil, nil)
	if err != nil {
		t.Fatalf("BuildCodebaseContentBrief: %v", err)
	}
	// Four reference voice quotes verbatim from spec §312-317.
	for _, quote := range []string{
		"Feel free to change this value to your own custom domain",
		"Configure this to use real SMTP sinks",
		"Replace with real SMTP credentials for production use",
		"Disabling the subdomain access is recommended",
	} {
		if !strings.Contains(brief.Body, quote) {
			t.Errorf("brief missing verbatim voice quote %q", quote)
		}
	}
}

func TestBuildCodebaseContentBrief_EmbedsKBStemFailVsPass(t *testing.T) {
	t.Parallel()
	plan := contentPhaseTestPlan()
	brief, err := BuildCodebaseContentBrief(plan, plan.Codebases[0], nil, nil)
	if err != nil {
		t.Fatalf("BuildCodebaseContentBrief: %v", err)
	}
	// Run-16 fail-shape stem.
	if !strings.Contains(brief.Body, "TypeORM `synchronize: false` everywhere") {
		t.Error("brief missing FAIL KB stem `TypeORM synchronize: false everywhere`")
	}
	// Showcase pass-shape stems (symptom-first + directive-mapped).
	if !strings.Contains(brief.Body, "**No `.env` file**") {
		t.Error("brief missing PASS symptom-first KB stem `No .env file`")
	}
	if !strings.Contains(brief.Body, "**Cache commands in `initCommands`, not `buildCommands`**") {
		t.Error("brief missing PASS directive-tightly-mapped KB stem")
	}
}

func TestBuildCodebaseContentBrief_EmbedsIGOneMechanism(t *testing.T) {
	t.Parallel()
	plan := contentPhaseTestPlan()
	brief, err := BuildCodebaseContentBrief(plan, plan.Codebases[0], nil, nil)
	if err != nil {
		t.Fatalf("BuildCodebaseContentBrief: %v", err)
	}
	// FAIL: fused three-mechanism H3 from run-16 apidev IG #2.
	if !strings.Contains(brief.Body, "Bind `0.0.0.0`, trust the proxy, drain on SIGTERM") {
		t.Error("brief missing FAIL IG fused-H3 example")
	}
	// PASS: showcase three sequential one-mechanism H3s.
	for _, h3 := range []string{
		"### 2. Trust the reverse proxy",
		"### 3. Configure Redis client",
		"### 4. Configure S3 object storage",
	} {
		if !strings.Contains(brief.Body, h3) {
			t.Errorf("brief missing PASS IG sequential H3 %q", h3)
		}
	}
}

func TestBuildCodebaseContentBrief_ThreadsCitationGuides(t *testing.T) {
	t.Parallel()
	plan := contentPhaseTestPlan()
	brief, err := BuildCodebaseContentBrief(plan, plan.Codebases[0], nil, nil)
	if err != nil {
		t.Fatalf("BuildCodebaseContentBrief: %v", err)
	}
	if !strings.Contains(brief.Body, "### Citation guides for this recipe") {
		t.Error("brief missing citation-guides section header")
	}
	// At least the well-known guides referenced throughout the run-17
	// brief should appear in the threaded list.
	for _, g := range []string{"init-commands", "rolling-deploys", "http-support"} {
		needle := "`" + g + "`"
		if !strings.Contains(brief.Body, needle) {
			t.Errorf("citation-guides section missing guide %q", g)
		}
	}
}

func TestBuildCodebaseContentBrief_ShowcaseWorkerInjectsSupplement(t *testing.T) {
	t.Parallel()
	plan := contentPhaseTestPlan()
	plan.Tier = tierShowcase
	plan.Codebases = append(plan.Codebases,
		Codebase{Hostname: "worker", Role: RoleWorker, BaseRuntime: "nodejs@22", IsWorker: true, SourceRoot: "/var/www/workerdev"},
	)
	brief, err := BuildCodebaseContentBrief(plan, plan.Codebases[1], nil, nil)
	if err != nil {
		t.Fatalf("BuildCodebaseContentBrief: %v", err)
	}
	if !strings.Contains(brief.Body, "Required worker KB gotchas") {
		t.Error("showcase tier worker brief missing required worker KB gotchas section")
	}
	if !strings.Contains(brief.Body, "Queue-group") {
		t.Error("showcase tier worker brief missing queue-group teaching")
	}
	if !strings.Contains(brief.Body, "SIGTERM") {
		t.Error("showcase tier worker brief missing SIGTERM drain teaching")
	}
}

func TestBuildCodebaseContentBrief_NonShowcaseSkipsSupplement(t *testing.T) {
	t.Parallel()
	plan := contentPhaseTestPlan()
	plan.Tier = "small" // not showcase
	plan.Codebases = []Codebase{
		{Hostname: "worker", Role: RoleWorker, BaseRuntime: "nodejs@22", IsWorker: true, SourceRoot: "/var/www/workerdev"},
	}
	brief, err := BuildCodebaseContentBrief(plan, plan.Codebases[0], nil, nil)
	if err != nil {
		t.Fatalf("BuildCodebaseContentBrief: %v", err)
	}
	if strings.Contains(brief.Body, "Required worker KB gotchas") {
		t.Error("non-showcase tier worker brief should not include showcase tier supplements")
	}
}

func TestBuildCodebaseContentBrief_ShowcaseNonWorkerSkipsSupplement(t *testing.T) {
	t.Parallel()
	plan := contentPhaseTestPlan() // already tier=showcase, api codebase
	brief, err := BuildCodebaseContentBrief(plan, plan.Codebases[0], nil, nil)
	if err != nil {
		t.Fatalf("BuildCodebaseContentBrief: %v", err)
	}
	if strings.Contains(brief.Body, "Required worker KB gotchas") {
		t.Error("showcase tier non-worker codebase should not get worker supplements")
	}
}

// TestBuildCodebaseContentBrief_PreWarnsTopRejectionPatterns — run-23
// F-18 pin. cc-api hit 17 errors / 20 record-fragment calls (~85%
// iteration rate) at run-23; the validator catches three rejection
// classes that account for most of the churn. Front-load a "common
// rejection patterns — pre-empt these" section in the codebase-content
// phase-entry atom so the agent authors with these in mind from the
// start. Codex flag: framing must NOT read as exhaustive — the brief
// MUST cite the surface contract for the full set + use language that
// makes the three an enumerated subset, not a closed list.
func TestBuildCodebaseContentBrief_PreWarnsTopRejectionPatterns(t *testing.T) {
	t.Parallel()
	plan := contentPhaseTestPlan()
	brief, err := BuildCodebaseContentBrief(plan, plan.Codebases[0], nil, nil)
	if err != nil {
		t.Fatalf("BuildCodebaseContentBrief: %v", err)
	}
	// Section header naming the pattern enumeration.
	if !strings.Contains(brief.Body, "Common record-fragment rejections") {
		t.Error("codebase-content brief missing common-rejections section header")
	}
	// Three pattern names — KB stem shape, slug citation prose, and
	// classification × surface routing.
	for _, want := range []string{
		"KB stem",
		"Slug citations",
		"Classification × surface",
	} {
		if !strings.Contains(brief.Body, want) {
			t.Errorf("codebase-content brief missing rejection-pattern name %q", want)
		}
	}
	// Worked-example anchors — at least one concrete WRONG/RIGHT pair
	// per pattern reduces ambiguity.
	for _, want := range []string{
		"Re-fire seeds",         // KB stem WRONG example token
		"Seed silently skipped", // KB stem RIGHT example token
		"env-var-model",         // citation example slug
		"intersection",          // classification × surface refusal example
	} {
		if !strings.Contains(brief.Body, want) {
			t.Errorf("codebase-content brief missing worked-example token %q", want)
		}
	}
	// Codex framing fix: the three MUST NOT read as exhaustive. Assert
	// "not exhaustive" framing — both "most-frequent" wording AND a
	// pointer to the surface contract for the full set.
	if !strings.Contains(brief.Body, "most-frequent") {
		t.Error("rejection-patterns section missing 'most-frequent' framing — risks reading as exhaustive list")
	}
	if !strings.Contains(brief.Body, "spec-content-surfaces") {
		t.Error("rejection-patterns section must point at the surface contract for the full validator set")
	}
}

// Atom inventory: the seven distillation atoms must exist on disk so
// the refinement sub-agent can fetch them via `zerops_knowledge
// uri=zerops://themes/refinement-references/<name>`. Run-23 F-25
// moved them from `internal/recipe/content/briefs/refinement/` to
// `internal/knowledge/themes/refinement-references/` so they live on
// the discovery channel rather than preloaded into the brief.

func TestRefinementAtoms_AllSevenPresent(t *testing.T) {
	t.Parallel()
	want := []string{
		"kb_shapes.md",
		"ig_one_mechanism.md",
		"voice_patterns.md",
		"yaml_comments.md",
		"citations.md",
		"trade_offs.md",
		"refinement_thresholds.md",
	}
	for _, name := range want {
		// Tests run in the package dir (internal/recipe). The knowledge
		// theme tree lives one level up.
		path := filepath.Join("..", "knowledge", "themes", "refinement-references", name)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("refinement-reference atom missing: %s (%v)", path, err)
		}
	}
}

// Decision-recording atoms must carry the worked examples appended in
// Tranche 0.5 — these are what teach the deploy-phase sub-agents to
// record porter_change facts in the shape that survives Tranche 1's
// engine-emit retraction.

func TestScaffoldDecisionRecording_HasWorkedExamples(t *testing.T) {
	t.Parallel()
	body, err := os.ReadFile(filepath.Join("content", "briefs", "scaffold", "decision_recording.md"))
	if err != nil {
		t.Fatalf("read scaffold decision_recording.md: %v", err)
	}
	count := strings.Count(string(body), "## Worked example")
	if count < 5 {
		t.Errorf("scaffold decision_recording.md should have at least 5 '## Worked example' sections; got %d", count)
	}
}

func TestFeatureDecisionRecording_HasWorkedExamples(t *testing.T) {
	t.Parallel()
	body, err := os.ReadFile(filepath.Join("content", "briefs", "feature", "decision_recording.md"))
	if err != nil {
		t.Fatalf("read feature decision_recording.md: %v", err)
	}
	count := strings.Count(string(body), "## Worked example")
	if count < 3 {
		t.Errorf("feature decision_recording.md should have at least 3 '## Worked example' sections; got %d", count)
	}
}

// Synthesis workflow atom shape: must reference key teaching anchors
// so a reader can verify the embed without reading every line.

func TestSynthesisWorkflowAtom_HasReferencedQuotes(t *testing.T) {
	t.Parallel()
	body, err := readAtom("briefs/codebase-content/synthesis_workflow.md")
	if err != nil {
		t.Fatalf("read synthesis_workflow.md: %v", err)
	}
	for _, anchor := range []string{
		"Classification × surface compatibility",
		"Friendly-authority voice",
		"Citation map",
		"KB stem shape",
		"IG one mechanism per H3",
	} {
		if !strings.Contains(body, anchor) {
			t.Errorf("synthesis_workflow.md missing teaching anchor %q", anchor)
		}
	}
}
