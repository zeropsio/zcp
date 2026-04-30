package eval

import (
	"strings"
	"testing"
	"time"
)

// sampleReport mirrors the assessmentInstructions schema from prompt.go —
// realistic enough to exercise every section parser. Variations include a
// blank line between fields, a continuation line on `What went wrong`,
// missing `How you recovered`, and a `Total wasted tool calls:` with a
// trailing period (agents leave punctuation).
const sampleReport = `Some assistant prose before the report.

## EVAL REPORT

### Deployment outcome
State: PARTIAL — first deploy landed but verify failed once before recovery.

### Workflow execution
- Steps completed: discover, provision, close
- Steps skipped: none
- Iterations: 2
- Gate failures: zerops_yaml setup mismatch on first deploy
- Strategy chosen: closeDeployMode=auto

### Failure chains
- **Step**: provision
  - **What you received**: discovery returned routeOptions with recipe rank 0.4 and classic
  - **What you did with it**: chose route=recipe with slug "nodejs-hello-world"
  - **What went wrong**: hostname collision on appdev; recipe rewrite probe rejected the plan
  - **How you recovered**: re-ran route=adopt to register the existing service
  - **Root cause**: UNCLEAR_GUIDANCE

- **Step**: first deploy
  - **What you received**: develop-first-deploy-intro atom said no strategy arg
  - **What you did with it**: ran zerops_deploy targetService=appdev
  - **What went wrong**: zerops.yaml setup name mismatched the runtime hostname
  - **How you recovered**: edited setup: line to match
  - **Root cause**: WRONG_KNOWLEDGE

### Information gaps
- What you were trying to do: enable subdomain access
  - What query/tool you tried: zerops_subdomain action=enable
  - What you had to guess or figure out on your own: that auto-enable already ran on first deploy
  - What the knowledge base SHOULD contain: a hint that subdomain is auto-enabled by zerops_deploy

### Wasted steps
- zerops_subdomain — already auto-enabled by zerops_deploy
- zerops_logs — called twice with same params after fix
Total wasted tool calls: 3.

### What worked well
- zerops_workflow action="status" envelope let me reconstruct state post-compaction
- develop-first-deploy-intro atom gave the no-strategy hint cleanly
`

func TestParseAssessment_HappyPath(t *testing.T) {
	t.Parallel()
	parsed := ParseAssessment(sampleReport)

	if !parsed.HasReport {
		t.Fatal("expected HasReport=true")
	}
	if parsed.Outcome != "PARTIAL" {
		t.Errorf("Outcome: want PARTIAL, got %q", parsed.Outcome)
	}
	if !strings.Contains(parsed.OutcomeDetail, "first deploy landed") {
		t.Errorf("OutcomeDetail missing detail: %q", parsed.OutcomeDetail)
	}

	if got := len(parsed.FailureChains); got != 2 {
		t.Fatalf("FailureChains count: want 2, got %d", got)
	}
	first := parsed.FailureChains[0]
	if first.Step != "provision" {
		t.Errorf("FC[0].Step: want provision, got %q", first.Step)
	}
	if first.RootCause != RootUnclearGuidance {
		t.Errorf("FC[0].RootCause: want UNCLEAR_GUIDANCE, got %q", first.RootCause)
	}
	if !strings.Contains(first.WentWrong, "hostname collision") {
		t.Errorf("FC[0].WentWrong missing 'hostname collision': %q", first.WentWrong)
	}

	second := parsed.FailureChains[1]
	if second.RootCause != RootWrongKnowledge {
		t.Errorf("FC[1].RootCause: want WRONG_KNOWLEDGE, got %q", second.RootCause)
	}
	if !strings.Contains(second.WentWrong, "setup name mismatched") {
		t.Errorf("FC[1].WentWrong missing 'setup name mismatched': %q", second.WentWrong)
	}

	if len(parsed.InformationGaps) != 1 {
		t.Fatalf("InformationGaps count: want 1, got %d", len(parsed.InformationGaps))
	}
	gap := parsed.InformationGaps[0]
	if !strings.Contains(gap.RawBody, "enable subdomain access") {
		t.Errorf("Gap.RawBody missing trigger: %q", gap.RawBody)
	}

	if got, want := parsed.WastedStepsTotal, 3; got != want {
		t.Errorf("WastedStepsTotal: want %d, got %d", want, got)
	}
	if got := len(parsed.WastedSteps); got != 2 {
		t.Errorf("WastedSteps count: want 2, got %d", got)
	}

	if got := len(parsed.WhatWorked); got != 2 {
		t.Errorf("WhatWorked count: want 2, got %d", got)
	}
}

func TestParseAssessment_NoFailureChains(t *testing.T) {
	t.Parallel()
	report := `## EVAL REPORT

### Deployment outcome
State: SUCCESS

### Failure chains
No failure chains.

### Wasted steps
Total wasted tool calls: 0.

### What worked well
- everything was smooth
`
	parsed := ParseAssessment(report)
	if !parsed.HasReport {
		t.Fatal("HasReport=false")
	}
	if parsed.Outcome != "SUCCESS" {
		t.Errorf("Outcome: want SUCCESS, got %q", parsed.Outcome)
	}
	if got := len(parsed.FailureChains); got != 0 {
		t.Errorf("FailureChains: want 0, got %d (entries: %+v)", got, parsed.FailureChains)
	}
	if parsed.WastedStepsTotal != 0 {
		t.Errorf("WastedStepsTotal: want 0, got %d", parsed.WastedStepsTotal)
	}
}

func TestParseAssessment_EmptyInput(t *testing.T) {
	t.Parallel()
	parsed := ParseAssessment("")
	if parsed.HasReport {
		t.Error("HasReport should be false for empty input")
	}
}

func TestRootCauseNorm_VariantSpellings(t *testing.T) {
	t.Parallel()
	cases := map[string]RootCause{
		"WRONG_KNOWLEDGE":   RootWrongKnowledge,
		"wrong knowledge":   RootWrongKnowledge,
		"Wrong-Knowledge":   RootWrongKnowledge,
		"missing knowledge": RootMissingKnowledge,
		"PLATFORM_ISSUE":    RootPlatformIssue,
		"unclear-guidance":  RootUnclearGuidance,
		"unknown":           RootUncategorized,
		"":                  RootUncategorized,
	}
	for in, want := range cases {
		if got := rootCauseNorm(in); got != want {
			t.Errorf("rootCauseNorm(%q): want %q, got %q", in, want, got)
		}
	}
}

func TestAggregateAndRender_GroupsByRootCause(t *testing.T) {
	t.Parallel()
	suite := &ScenarioSuiteResult{
		SuiteID:   "test-suite",
		StartedAt: time.Now(),
		Duration:  Duration(5 * time.Minute),
		Results: []ScenarioResult{
			{
				ScenarioID: "weather-dashboard-bun",
				Grade:      GradeResult{ScenarioID: "weather-dashboard-bun", Passed: false, Failures: []string{"finalUrlStatus: want 200, got 502"}},
				Assessment: sampleReport,
			},
			{
				ScenarioID: "greenfield-nodejs-todo",
				Grade:      GradeResult{ScenarioID: "greenfield-nodejs-todo", Passed: true},
				Assessment: `## EVAL REPORT

### Deployment outcome
State: SUCCESS

### Failure chains
- **Step**: first deploy
  - **What went wrong**: started writing app code in bootstrap session
  - **Root cause**: WRONG_KNOWLEDGE

### Wasted steps
Total wasted tool calls: 0.

### What worked well
- bootstrap-intro atom was clear about infra-only scope
`,
			},
		},
	}

	triage := AggregateScenarioSuite(suite)

	// Both scenarios should be in briefs.
	if got := len(triage.Scenarios); got != 2 {
		t.Fatalf("Scenarios: want 2, got %d", got)
	}

	// WRONG_KNOWLEDGE appears in BOTH scenarios — count = 2.
	if got, want := len(triage.GroupedByRootCause[RootWrongKnowledge]), 2; got != want {
		t.Errorf("WRONG_KNOWLEDGE count: want %d, got %d", want, got)
	}
	// UNCLEAR_GUIDANCE only in first scenario.
	if got, want := len(triage.GroupedByRootCause[RootUnclearGuidance]), 1; got != want {
		t.Errorf("UNCLEAR_GUIDANCE count: want %d, got %d", want, got)
	}

	// Render and check key sections appear.
	md := RenderTriageMarkdown(triage)
	for _, want := range []string{
		"# Triage — suite `test-suite`",
		"## Per-scenario verdict",
		"WRONG_KNOWLEDGE (2)",
		"UNCLEAR_GUIDANCE (1)",
		"weather-dashboard-bun",
		"greenfield-nodejs-todo",
		"finalUrlStatus: want 200, got 502", // grader failure detail surfaces
		"## Wasted tool calls — by tool",
		"## What worked well",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("rendered triage missing %q", want)
		}
	}
}

func TestExtractToolFromWastedStep(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"zerops_subdomain — already auto-enabled":         "zerops_subdomain",
		"Called zerops_workflow when zerops_status would": "zerops_workflow",
		"zerops_logs called twice for nothing":            "zerops_logs",
		"some non-tool description":                       "some",
	}
	for in, want := range cases {
		if got := extractToolFromWastedStep(in); got != want {
			t.Errorf("extractToolFromWastedStep(%q): want %q, got %q", in, want, got)
		}
	}
}
