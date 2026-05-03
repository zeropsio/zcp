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

// Atom inventory: the seven Tranche 0.5 reference distillation atoms
// must exist on disk. This test pins the contract between Tranche 0.5
// (atoms shipped) and Tranche 4 (refinement composer reads them).

func TestRefinementAtoms_AllSevenPresent(t *testing.T) {
	t.Parallel()
	want := []string{
		"reference_kb_shapes.md",
		"reference_ig_one_mechanism.md",
		"reference_voice_patterns.md",
		"reference_yaml_comments.md",
		"reference_citations.md",
		"reference_trade_offs.md",
		"refinement_thresholds.md",
	}
	for _, name := range want {
		path := filepath.Join("content", "briefs", "refinement", name)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("Tranche 0.5 atom missing: %s (%v)", path, err)
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
