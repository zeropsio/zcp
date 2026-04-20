package eval

import (
	"strings"
	"testing"
)

func TestGrade_AllExpectationsMet_Passes(t *testing.T) {
	t.Parallel()

	sc := &Scenario{
		ID: "test",
		Expect: Expectation{
			MustCallTools:     []string{"zerops_workflow", "zerops_import"},
			WorkflowCallsMin:  2,
			MustEnterWorkflow: []string{"bootstrap"},
			ForbiddenPatterns: []string{"<projectId>"},
		},
	}
	calls := []ToolCall{
		{Name: "zerops_workflow", Input: `{"action":"start","workflow":"bootstrap"}`},
		{Name: "zerops_workflow", Input: `{"action":"status"}`},
		{Name: "zerops_import", Input: `{}`},
	}
	log := "agent did the thing"

	g := Grade(sc, log, calls, "")
	if !g.Passed {
		t.Errorf("expected PASS, got failures: %+v", g.Failures)
	}
}

func TestGrade_RequireAssessment_MissingReport_Fails(t *testing.T) {
	t.Parallel()

	sc := &Scenario{
		Expect: Expectation{RequireAssessment: true},
	}

	g := Grade(sc, "", nil, "")
	if g.Passed {
		t.Fatal("expected FAIL when RequireAssessment=true and assessment empty")
	}
	if !containsFailure(g.Failures, "requireAssessment") {
		t.Errorf("failure should mention requireAssessment: %+v", g.Failures)
	}
}

func TestGrade_RequireAssessment_NonSuccessState_Fails(t *testing.T) {
	t.Parallel()

	sc := &Scenario{
		Expect: Expectation{RequireAssessment: true},
	}
	assessment := `## EVAL REPORT

### Deployment outcome
State: PARTIAL (imports worked, verify failed)

### Failure chains
No failure chains.`

	g := Grade(sc, "", nil, assessment)
	if g.Passed {
		t.Fatal("expected FAIL when assessment state is not SUCCESS")
	}
	if !containsFailure(g.Failures, "SUCCESS") {
		t.Errorf("failure should mention SUCCESS: %+v", g.Failures)
	}
}

func TestGrade_RequireAssessment_SuccessState_Passes(t *testing.T) {
	t.Parallel()

	sc := &Scenario{
		Expect: Expectation{RequireAssessment: true},
	}
	assessment := `## EVAL REPORT

### Deployment outcome
State: SUCCESS — deployed and verified on subdomain

### Failure chains
No failure chains.`

	g := Grade(sc, "", nil, assessment)
	if !g.Passed {
		t.Errorf("expected PASS when assessment reports SUCCESS, got: %+v", g.Failures)
	}
}

func TestGrade_RequireAssessment_Off_IgnoresAssessment(t *testing.T) {
	t.Parallel()

	sc := &Scenario{
		Expect: Expectation{RequireAssessment: false},
	}

	g := Grade(sc, "", nil, "")
	if !g.Passed {
		t.Errorf("expected PASS when RequireAssessment=false, got: %+v", g.Failures)
	}
}

func TestGrade_MissingRequiredTool_Fails(t *testing.T) {
	t.Parallel()

	sc := &Scenario{
		Expect: Expectation{
			MustCallTools: []string{"zerops_workflow", "zerops_verify"},
		},
	}
	calls := []ToolCall{{Name: "zerops_workflow"}}

	g := Grade(sc, "", calls, "")
	if g.Passed {
		t.Fatal("expected FAIL")
	}
	if !containsFailure(g.Failures, "zerops_verify") {
		t.Errorf("failure should mention missing tool: %+v", g.Failures)
	}
}

func TestGrade_BelowMinWorkflowCalls_Fails(t *testing.T) {
	t.Parallel()

	sc := &Scenario{
		Expect: Expectation{WorkflowCallsMin: 5},
	}
	calls := []ToolCall{
		{Name: "zerops_workflow"},
		{Name: "zerops_workflow"},
	}

	g := Grade(sc, "", calls, "")
	if g.Passed {
		t.Fatal("expected FAIL")
	}
	if !containsFailure(g.Failures, "workflowCallsMin") {
		t.Errorf("failure should mention workflowCallsMin: %+v", g.Failures)
	}
}

func TestGrade_ForbiddenPatternPresent_Fails(t *testing.T) {
	t.Parallel()

	sc := &Scenario{
		Expect: Expectation{
			ForbiddenPatterns: []string{"<projectId>"},
		},
	}
	log := `fetched https://app-<projectId>.prg1.zerops.app/`

	g := Grade(sc, log, nil, "")
	if g.Passed {
		t.Fatal("expected FAIL")
	}
	if !containsFailure(g.Failures, "<projectId>") {
		t.Errorf("failure should mention forbidden pattern: %+v", g.Failures)
	}
}

func TestGrade_RequiredPattern_Missing_Fails(t *testing.T) {
	t.Parallel()

	sc := &Scenario{
		Expect: Expectation{
			RequiredPatterns: []string{`"route":"classic"`},
		},
	}
	calls := []ToolCall{
		{Name: "zerops_workflow", Input: `{"action":"start","workflow":"bootstrap","intent":"..."}`},
	}

	g := Grade(sc, "", calls, "")
	if g.Passed {
		t.Fatal("expected FAIL when requiredPattern never appears")
	}
	if !containsFailure(g.Failures, "requiredPattern") {
		t.Errorf("failure should mention requiredPattern: %+v", g.Failures)
	}
}

func TestGrade_RequiredPattern_Present_Passes(t *testing.T) {
	t.Parallel()

	sc := &Scenario{
		Expect: Expectation{
			RequiredPatterns: []string{`"route":"classic"`},
		},
	}
	calls := []ToolCall{
		{Name: "zerops_workflow", Input: `{"action":"start","workflow":"bootstrap","intent":"..."}`}, // discovery pass
		{Name: "zerops_workflow", Input: `{"action":"start","workflow":"bootstrap","route":"classic"}`},
	}

	g := Grade(sc, "", calls, "")
	// Assessment is not required here — this test only cares about the pattern.
	for _, f := range g.Failures {
		if strings.Contains(f, "requiredPattern") {
			t.Errorf("requiredPattern should not fail when pattern present: %+v", g.Failures)
		}
	}
}

// TestGrade_RequiredPattern_SpacingNormalized covers the JSON-spacing
// tolerance: an agent serializing with "key": "value" (space after colon) must
// match a pattern written as "key":"value". Both forms share a single
// canonical representation after normalization.
func TestGrade_RequiredPattern_SpacingNormalized(t *testing.T) {
	t.Parallel()

	sc := &Scenario{
		Expect: Expectation{
			RequiredPatterns: []string{`"route":"recipe"`},
		},
	}
	calls := []ToolCall{
		// Input uses "key": "value" spacing — the grader must still match.
		{Name: "zerops_workflow", Input: `{"action": "start", "workflow": "bootstrap", "route": "recipe", "recipeSlug": "laravel-minimal"}`},
	}

	g := Grade(sc, "", calls, "")
	for _, f := range g.Failures {
		if strings.Contains(f, "requiredPattern") {
			t.Errorf("requiredPattern should normalize spacing: %+v", g.Failures)
		}
	}
}

func TestGrade_MissingWorkflowEntry_Fails(t *testing.T) {
	t.Parallel()

	sc := &Scenario{
		Expect: Expectation{
			MustEnterWorkflow: []string{"bootstrap", "develop"},
		},
	}
	calls := []ToolCall{
		{Name: "zerops_workflow", Input: `{"action":"start","workflow":"bootstrap"}`},
		// No develop workflow started.
	}

	g := Grade(sc, "", calls, "")
	if g.Passed {
		t.Fatal("expected FAIL")
	}
	if !containsFailure(g.Failures, "develop") {
		t.Errorf("failure should mention missing workflow entry: %+v", g.Failures)
	}
}

func containsFailure(failures []string, substring string) bool {
	for _, f := range failures {
		if strings.Contains(f, substring) {
			return true
		}
	}
	return false
}
