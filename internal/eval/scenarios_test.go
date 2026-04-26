package eval

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestScenarios_LiveFilesParse ensures every shipped scenario file under
// internal/eval/scenarios/*.md parses and passes validation. Guards against a
// scenario being committed with bad frontmatter or a missing fixture.
func TestScenarios_LiveFilesParse(t *testing.T) {
	t.Parallel()

	matches, err := filepath.Glob("scenarios/*.md")
	if err != nil {
		t.Fatalf("glob scenarios: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("no scenarios found under scenarios/*.md")
	}

	for _, path := range matches {
		t.Run(filepath.Base(path), func(t *testing.T) {
			t.Parallel()
			sc, err := ParseScenario(path)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if sc.ID == "" {
				t.Error("id empty")
			}
			if len(sc.Expect.MustCallTools) == 0 {
				t.Error("mustCallTools should not be empty — every scenario should assert some tool usage")
			}
			// Fixture files must actually exist.
			if sc.Fixture != "" {
				fixturePath := resolveFixturePath(sc)
				if _, err := filepath.Abs(fixturePath); err != nil {
					t.Errorf("resolve fixture: %v", err)
				}
				matches, err := filepath.Glob(fixturePath)
				if err != nil || len(matches) == 0 {
					t.Errorf("fixture %q does not exist", fixturePath)
				}
			}
		})
	}
}

// TestScenario_AdoptExistingLaravel_GraderEndToEnd executes one
// representative scenario file through the full parse → grade
// pipeline. This is the eval surface's smoke test: it proves
// `ParseScenario` produces a Scenario shape the grader can actually
// drive, and that a synthetic transcript satisfying every expectation
// in the file's frontmatter receives a PASS verdict.
//
// Without this end-to-end check, a refactor of the grader could
// silently drop assertions (e.g. forget to call checkRequiredPatterns)
// while every individual checkX unit test still passes — the eval
// black-hole risk Codex flagged in the test-suite audit.
//
// adopt-existing-laravel.md was chosen because it exercises the
// richest assertion set: mustCallTools + workflowCallsMin +
// mustEnterWorkflow + requiredPatterns + requireAssessment +
// finalUrlStatus. Add additional scenario coverage as new failure
// modes appear; the goal isn't every-scenario coverage, it's "the
// pipeline is wired."
func TestScenario_AdoptExistingLaravel_GraderEndToEnd(t *testing.T) {
	t.Parallel()

	sc, err := ParseScenario("scenarios/adopt-existing-laravel.md")
	if err != nil {
		t.Fatalf("parse scenario: %v", err)
	}

	// Synthetic transcript that satisfies every expectation in the file:
	//   - tool name set covers MustCallTools (zerops_workflow + zerops_discover)
	//   - workflow call count >= WorkflowCallsMin (7)
	//   - bootstrap workflow entered (action=start workflow=bootstrap)
	//   - required patterns appear in some call's Input/Result
	//     (workflow=bootstrap, route=adopt, scope=[, app)
	calls := []ToolCall{
		{Name: "zerops_discover", Input: `{"includeEnvs":true}`, Result: `{"services":[{"hostname":"app"}]}`},
		{Name: "zerops_workflow", Input: `{"action":"start","workflow":"bootstrap"}`, Result: `{"routeOptions":[{"route":"adopt"}]}`},
		{Name: "zerops_workflow", Input: `{"action":"iterate","workflow":"bootstrap","route":"adopt"}`, Result: `{}`},
		{Name: "zerops_workflow", Input: `{"action":"complete","workflow":"bootstrap"}`, Result: `{}`},
		{Name: "zerops_workflow", Input: `{"action":"start","workflow":"develop","scope":["app"]}`, Result: `{}`},
		{Name: "zerops_workflow", Input: `{"action":"iterate","workflow":"develop"}`, Result: `{}`},
		{Name: "zerops_workflow", Input: `{"action":"status"}`, Result: `{}`},
		{Name: "zerops_workflow", Input: `{"action":"close","workflow":"develop"}`, Result: `{}`},
	}
	log := "agent walked through adopt route"
	assessment := "## EVAL REPORT\n\n### Deployment outcome\nState: SUCCESS"
	probe := &FinalURLProbe{Hostname: "app", URL: "https://example.com", Got: 200}

	g := GradeWithProbe(sc, log, calls, assessment, probe)
	if !g.Passed {
		t.Fatalf("expected scenario to pass with synthetic-success transcript, got failures: %v", g.Failures)
	}
	if g.ScenarioID != sc.ID {
		t.Errorf("ScenarioID = %q, want %q", g.ScenarioID, sc.ID)
	}
}

// TestScenario_AdoptExistingLaravel_GraderRejectsMissingPattern
// verifies the grader actually FAILS when a required pattern is
// absent. Counterpart to the success path: same parsed scenario + same
// transcript shape, but the agent never sent route=adopt — the grader
// must surface that gap by name.
//
// This pins the "negative path" the success test alone can't prove:
// without it, a regression that makes Grade unconditionally pass would
// only be visible by the success test still passing, not by anything
// failing.
func TestScenario_AdoptExistingLaravel_GraderRejectsMissingPattern(t *testing.T) {
	t.Parallel()

	sc, err := ParseScenario("scenarios/adopt-existing-laravel.md")
	if err != nil {
		t.Fatalf("parse scenario: %v", err)
	}

	// Same transcript as the success test, but the agent neither saw
	// the adopt route in the discovery response NOR sent route=adopt in
	// any iterate call. checkRequiredPatterns scans both Input and
	// Result, so the negative test must remove the pattern from both
	// surfaces (per the docstring: "Scanning both Input and Result is
	// load-bearing for state-detection scenarios").
	calls := []ToolCall{
		{Name: "zerops_discover", Input: `{"includeEnvs":true}`, Result: `{"services":[{"hostname":"app"}]}`},
		// start.bootstrap response does NOT name the adopt option.
		{Name: "zerops_workflow", Input: `{"action":"start","workflow":"bootstrap"}`, Result: `{"routeOptions":[]}`},
		// iterate goes classic — agent ignored adopt entirely.
		{Name: "zerops_workflow", Input: `{"action":"iterate","workflow":"bootstrap","route":"classic"}`, Result: `{}`},
		{Name: "zerops_workflow", Input: `{"action":"complete","workflow":"bootstrap"}`, Result: `{}`},
		{Name: "zerops_workflow", Input: `{"action":"start","workflow":"develop","scope":["app"]}`, Result: `{}`},
		{Name: "zerops_workflow", Input: `{"action":"iterate","workflow":"develop"}`, Result: `{}`},
		{Name: "zerops_workflow", Input: `{"action":"status"}`, Result: `{}`},
		{Name: "zerops_workflow", Input: `{"action":"close","workflow":"develop"}`, Result: `{}`},
	}
	log := "agent took the wrong branch"
	assessment := "## EVAL REPORT\n\n### Deployment outcome\nState: SUCCESS"
	probe := &FinalURLProbe{Hostname: "app", URL: "https://example.com", Got: 200}

	g := GradeWithProbe(sc, log, calls, assessment, probe)
	if g.Passed {
		t.Fatal("expected FAIL when route=adopt is missing, got PASS")
	}
	// Surface should name the specific missing pattern, not just "failed."
	// %q-formatted failure messages escape inner quotes (`\"`), so we
	// match against the unambiguous fragments the formatter preserves.
	if !containsAnyFailure(g.Failures, "requiredPattern") || !containsAnyFailure(g.Failures, "adopt") {
		t.Errorf("failure should name the missing requiredPattern with the adopt keyword; got: %v", g.Failures)
	}
}

// containsAnyFailure returns true when at least one failure message
// contains the given substring. Local helper.
func containsAnyFailure(failures []string, want string) bool {
	for _, f := range failures {
		if strings.Contains(f, want) {
			return true
		}
	}
	return false
}
