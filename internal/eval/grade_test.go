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

	g := Grade(sc, log, calls)
	if !g.Passed {
		t.Errorf("expected PASS, got failures: %+v", g.Failures)
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

	g := Grade(sc, "", calls)
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

	g := Grade(sc, "", calls)
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

	g := Grade(sc, log, nil)
	if g.Passed {
		t.Fatal("expected FAIL")
	}
	if !containsFailure(g.Failures, "<projectId>") {
		t.Errorf("failure should mention forbidden pattern: %+v", g.Failures)
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

	g := Grade(sc, "", calls)
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
