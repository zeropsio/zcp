package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"

	"github.com/zeropsio/zcp/internal/workflow"
)

// v8.96 §5.4 — every P0 check (and migrated P1 check) must populate the
// structured diagnostic fields when failing. ReadSurface tells the author
// which file/region the check actually inspected; Required + Actual name
// the gate's threshold and the observed value; HowToFix is a concrete
// 1-3-sentence imperative remedy; CoupledWith names files whose state is
// implicitly bound to the ReadSurface.
//
// Tests in this file fail loudly until every named P0 check populates
// every required field, and HowToFix passes the quality assertions
// (length, no hedging, mentions CoupledWith basenames when set).

// hedgingPhrases are substrings that signal the author hasn't committed
// to a concrete remedy. v8.96 §5.4 quality bar — HowToFix must not contain
// any of these (case-insensitive). "should probably" is the single
// strongest signal in the v23 author-archaeology corpus; "consider" and
// "you might" are the weaker but still load-bearing offenders.
var hedgingPhrases = []string{
	"consider",
	"you might",
	"you may want to",
	"review the",
	"could ",
	"should probably",
}

// assertDiagnostics validates the v8.96 quality bar on a single failing
// StepCheck. Failure messages name the check by Name so a multi-check
// test fixture surfaces the offender immediately.
func assertDiagnostics(t *testing.T, c workflow.StepCheck) {
	t.Helper()
	if c.Status != "fail" {
		t.Fatalf("assertDiagnostics called on non-fail check %q (status=%s)", c.Name, c.Status)
	}
	if c.ReadSurface == "" {
		t.Errorf("check %q: ReadSurface is empty — author cannot know which file the gate inspected", c.Name)
	}
	if c.Required == "" {
		t.Errorf("check %q: Required is empty — author cannot know what threshold to clear", c.Name)
	}
	if c.Actual == "" {
		t.Errorf("check %q: Actual is empty — author cannot know how far below the threshold the value sits", c.Name)
	}
	if c.HowToFix == "" {
		t.Errorf("check %q: HowToFix is empty — author has no concrete remedy", c.Name)
		return
	}
	trimmed := strings.TrimSpace(c.HowToFix)
	if n := len(trimmed); n < 50 {
		t.Errorf("check %q: HowToFix too short (%d chars, need >= 50): %q", c.Name, n, trimmed)
	}
	if n := len(trimmed); n > 600 {
		t.Errorf("check %q: HowToFix too long (%d chars, need <= 600 ~ 3 sentences): %q", c.Name, n, trimmed)
	}
	if r := []rune(trimmed); len(r) > 0 && !unicode.IsUpper(r[0]) {
		t.Errorf("check %q: HowToFix should start with an uppercase letter (imperative mood proxy): %q", c.Name, trimmed)
	}
	low := strings.ToLower(trimmed)
	for _, h := range hedgingPhrases {
		if strings.Contains(low, h) {
			t.Errorf("check %q: HowToFix contains hedging phrase %q — rewrite as a concrete remedy: %q", c.Name, h, trimmed)
		}
	}
	for _, coupled := range c.CoupledWith {
		base := filepath.Base(coupled)
		if !strings.Contains(low, strings.ToLower(base)) {
			t.Errorf("check %q: HowToFix does not name CoupledWith basename %q — coupling will be silently dropped: %q", c.Name, base, trimmed)
		}
	}
}

// findFailingCheck returns the first failing check whose Name contains the
// given substring. Returns false when no such check exists.
func findFailingCheck(checks []workflow.StepCheck, nameContains string) (workflow.StepCheck, bool) {
	for _, c := range checks {
		if c.Status == "fail" && strings.Contains(c.Name, nameContains) {
			return c, true
		}
	}
	return workflow.StepCheck{}, false
}

// TestDiagnostics_CommentRatio_README — the v31 deploy-readmes 3-round
// loop offender. checkReadmeFragments emits a bare "comment_ratio" check
// (no host prefix) reading the YAML block embedded in the integration-
// guide fragment. The on-disk apidev/zerops.yaml is the coupled file —
// when the author boosts comments on disk and forgets to re-sync the
// embedded YAML, the check still fails and the author can't tell why
// without the diagnostics.
func TestDiagnostics_CommentRatio_README(t *testing.T) {
	t.Parallel()
	// README content with an embedded YAML block whose comment ratio is
	// below 30% — the same shape v31 hit at deploy-complete.
	readme := "<!-- #ZEROPS_EXTRACT_START:integration-guide# -->\n\n" +
		"```yaml\n" +
		"zerops:\n" +
		"  - setup: dev\n" +
		"    build:\n" +
		"      base: nodejs@22\n" +
		"      buildCommands:\n" +
		"        - npm ci\n" +
		"        - npm run build\n" +
		"      deployFiles:\n" +
		"        - dist\n" +
		"        - package.json\n" +
		"    run:\n" +
		"      base: nodejs@22\n" +
		"      ports:\n" +
		"        - port: 3000\n" +
		"          httpSupport: true\n" +
		"      start: npm run start:prod\n" +
		"# one comment line, far below 30%\n" +
		"```\n" +
		"<!-- #ZEROPS_EXTRACT_END:integration-guide# -->\n"

	checks := checkReadmeFragments(readme, "apidev")
	got, ok := findFailingCheck(checks, "comment_ratio")
	if !ok {
		t.Fatal("expected comment_ratio fail in fixture; check fixture not driving the failure path")
	}
	assertDiagnostics(t, got)
}

// envFinalizeFailingChecks runs validateImportYAML against a finalize
// import.yaml fixture engineered to fail the named env-comment quality
// checks. Returns the failing checks plus the prefix used so callers can
// scope their assertions.
func envFinalizeFailingChecks(t *testing.T) []workflow.StepCheck {
	t.Helper()
	plan := testFinalizePlan()
	folder := workflow.EnvFolder(0)
	// Engineered to fail comment_ratio (no comments past header), comment_depth
	// (no reasoning markers), cross_env_refs (names env 4 explicitly), and
	// factual_claims (claims minContainers: 5 next to a literal 2).
	content := `# zeropsPreprocessor=on
project:
  name: bun-hello-world-agent
services:
  - hostname: app
    type: bun@1
    priority: 10
    # see env 4 for the production replica count
    # minContainers: 5
    minContainers: 2
  - hostname: db
    type: postgresql@16
    priority: 10
    mode: NON_HA
`
	checks := validateImportYAML(content, plan, 0, folder)
	if len(checks) == 0 {
		t.Fatal("validateImportYAML returned no checks")
	}
	return checks
}

// TestDiagnostics_EnvImport_CommentRatio — env import.yaml comment ratio
// floor (30%). Coupled to nothing else: the comment must be added inside
// the same file. Required: ≥30% comment-only lines. Actual: the observed
// ratio. HowToFix: directly tells the author to add # comment lines until
// the ratio clears 30%.
func TestDiagnostics_EnvImport_CommentRatio(t *testing.T) {
	t.Parallel()
	checks := envFinalizeFailingChecks(t)
	got, ok := findFailingCheck(checks, "_comment_ratio")
	if !ok {
		t.Fatal("expected env import comment_ratio fail in fixture")
	}
	assertDiagnostics(t, got)
}

// TestDiagnostics_EnvImport_CommentDepth — env import.yaml comment depth
// rubric (35% reasoning markers among substantive comments). Same
// fixture as comment_ratio but the depth check fires when there ARE
// comments but none carry reasoning markers.
func TestDiagnostics_EnvImport_CommentDepth(t *testing.T) {
	t.Parallel()
	// Engineered fixture: 4 substantive comments, none with reasoning markers.
	content := `# zeropsPreprocessor=on
project:
  # name string for the project
  name: bun-hello-world-agent
services:
  # the bun runtime application service that handles requests
  - hostname: app
    type: bun@1
    priority: 10
    minContainers: 2
  # the postgresql database service for storing data records
  - hostname: db
    type: postgresql@16
    # priority ten ensures the database starts before the app container
    priority: 10
    mode: NON_HA
`
	checks := checkCommentDepth(content, "env-0_import")
	got, ok := findFailingCheck(checks, "comment_depth")
	if !ok {
		t.Fatalf("expected comment_depth fail in fixture; got %d checks", len(checks))
	}
	assertDiagnostics(t, got)
}

// TestDiagnostics_EnvImport_CrossEnvRefs — env import.yaml comments must
// not name a sibling env by tier number. Each env ships standalone on
// zerops.io/recipes; "see env 5" reads as a dangling pointer.
func TestDiagnostics_EnvImport_CrossEnvRefs(t *testing.T) {
	t.Parallel()
	content := `# bumps to env 4 minContainers when traffic increases
project:
  name: x
`
	checks := checkCrossEnvReferences(content, "env-0_import")
	got, ok := findFailingCheck(checks, "cross_env_refs")
	if !ok {
		t.Fatal("expected cross_env_refs fail")
	}
	assertDiagnostics(t, got)
}

// TestDiagnostics_EnvImport_FactualClaims — declarative numeric claim in
// a comment ("minContainers: 5") that contradicts the adjacent YAML
// value ("minContainers: 2"). The v31 finalize loop offender.
func TestDiagnostics_EnvImport_FactualClaims(t *testing.T) {
	t.Parallel()
	content := `services:
  - hostname: app
    type: bun@1
    # minContainers: 5
    minContainers: 2
`
	checks := checkFactualClaims(content, "env-0_import")
	got, ok := findFailingCheck(checks, "factual_claims")
	if !ok {
		t.Fatalf("expected factual_claims fail; got %d checks", len(checks))
	}
	assertDiagnostics(t, got)
}

// TestNextRoundPrediction confirms the heuristic in
// AnnotateNextRoundPrediction: HowToFix-empty → multi-round-likely;
// CoupledWith non-empty → coupled-surfaces-require-sequencing; else →
// single-round-fix-expected. Pass-state results get no annotation.
func TestNextRoundPrediction(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		result workflow.StepCheckResult
		want   string
	}{
		{
			name: "all-fails-with-howtofix-no-coupling",
			result: workflow.StepCheckResult{
				Passed: false,
				Checks: []workflow.StepCheck{
					{Name: "a", Status: "fail", HowToFix: "Add a # comment line above the services block summarizing what each tier provisions."},
					{Name: "b", Status: "fail", HowToFix: "Set readinessCheck to GET /healthz returning 200 within 5 seconds for every runtime."},
				},
			},
			want: "single-round-fix-expected",
		},
		{
			name: "any-fail-has-coupling",
			result: workflow.StepCheckResult{
				Passed: false,
				Checks: []workflow.StepCheck{
					{Name: "a", Status: "fail", HowToFix: "Add a # comment line summarizing each tier."},
					{
						Name:        "b",
						Status:      "fail",
						HowToFix:    "Re-sync the YAML block in apidev/README.md after editing apidev/zerops.yaml.",
						CoupledWith: []string{"apidev/zerops.yaml"},
					},
				},
			},
			want: "coupled-surfaces-require-sequencing",
		},
		{
			name: "any-fail-missing-howtofix",
			result: workflow.StepCheckResult{
				Passed: false,
				Checks: []workflow.StepCheck{
					{Name: "a", Status: "fail", HowToFix: "Add # comment lines until ratio clears 30%."},
					{Name: "b", Status: "fail", HowToFix: ""},
				},
			},
			want: "multi-round-likely",
		},
		{
			name: "passed-no-annotation",
			result: workflow.StepCheckResult{
				Passed: true,
				Checks: []workflow.StepCheck{{Name: "ok", Status: "pass"}},
			},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := tt.result
			workflow.AnnotateNextRoundPrediction(&r)
			if r.NextRoundPrediction != tt.want {
				t.Errorf("NextRoundPrediction = %q, want %q", r.NextRoundPrediction, tt.want)
			}
		})
	}
}

// TestDiagnostics_FinalizeFlow_AnnotatesPrediction — when checkRecipeFinalize
// runs against a fixture that fails P0 env-comment checks, the returned
// StepCheckResult must carry NextRoundPrediction (no failing check has
// CoupledWith, no failing check has empty HowToFix once migration is
// complete → "single-round-fix-expected").
func TestDiagnostics_FinalizeFlow_AnnotatesPrediction(t *testing.T) {
	t.Parallel()
	plan := testFinalizePlan()
	dir := t.TempDir()
	files := workflow.BuildFinalizeOutput(plan)
	for relPath, content := range files {
		fullPath := filepath.Join(dir, relPath)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	checker := checkRecipeFinalize(dir)
	result, err := checker(context.Background(), plan, &workflow.RecipeState{OutputDir: dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Fatal("template baseline must fail comment_ratio (test premise) — fixture changed")
	}
	if result.NextRoundPrediction == "" {
		t.Errorf("StepCheckResult.NextRoundPrediction must be populated on failed result; got empty")
	}
}
