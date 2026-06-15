package recipe

import (
	"strings"
	"testing"
)

// v9.79.0 Fix 3 — apps-repo README order is title → intro → cover →
// `## Deploy to Zerops` H2 (sentence + button) → `## Integration Guide`.
// Match jetstream golden /Users/fxck/www/laravel-jetstream-app/README.md.
// Run-37 had deploy button inline before the cover and no H2 wrapper.

// codebaseREADMEV979Plan returns a synthetic plan suitable for exercising
// AssembleCodebaseREADME end-to-end: codebases carry a SourceRoot and an
// authored whole-yaml fragment so the M-2 hardening at assemble.go:152
// is satisfied.
func codebaseREADMEV979Plan(t *testing.T) *Plan {
	t.Helper()
	plan := syntheticShowcasePlan()
	plan.Slug = "nestjs-showcase"
	plan.Framework = "nestjs"
	for i, cb := range plan.Codebases {
		plan.Codebases[i].SourceRoot = "/var/www/" + cb.Hostname + "dev"
	}
	plan.Fragments = map[string]string{}
	for _, cb := range plan.Codebases {
		plan.Fragments[fragmentIDCodebaseZeropsYAML(cb.Hostname)] = "zerops:\n  - setup: prod\n    run:\n      base: nodejs@22\n      start: npm start\n"
	}
	return plan
}

// TestAssembleCodebaseREADME_V9_79_DeployH2Wrapper pins the apps-repo
// README has a `## Deploy to Zerops` H2 wrapper with a one-sentence
// body. Match jetstream golden.
func TestAssembleCodebaseREADME_V9_79_DeployH2Wrapper(t *testing.T) {
	t.Parallel()
	plan := codebaseREADMEV979Plan(t)
	body, _, err := AssembleCodebaseREADME(plan, "api")
	if err != nil {
		t.Fatalf("AssembleCodebaseREADME: %v", err)
	}
	if !strings.Contains(body, "\n## Deploy to Zerops\n") {
		t.Errorf("rendered body missing `## Deploy to Zerops` H2:\n%s", body)
	}
	if !strings.Contains(body, "Click the deploy button to deploy directly to Zerops.") {
		t.Errorf("rendered body missing the H2 body sentence; got:\n%s", body)
	}
}

// TestAssembleCodebaseREADME_V9_79_CoverBeforeDeploy pins the order:
// cover image must appear before the `## Deploy to Zerops` H2,
// matching jetstream golden order title → intro → cover → deploy.
func TestAssembleCodebaseREADME_V9_79_CoverBeforeDeploy(t *testing.T) {
	t.Parallel()
	plan := codebaseREADMEV979Plan(t)
	body, _, err := AssembleCodebaseREADME(plan, "api")
	if err != nil {
		t.Fatalf("AssembleCodebaseREADME: %v", err)
	}
	coverIdx := strings.Index(body, "![nestjs cover]")
	deployH2Idx := strings.Index(body, "## Deploy to Zerops")
	deployButtonIdx := strings.Index(body, "[![Deploy on Zerops]")
	igH2Idx := strings.Index(body, "## Integration Guide")
	if coverIdx < 0 || deployH2Idx < 0 || deployButtonIdx < 0 || igH2Idx < 0 {
		t.Fatalf("missing landmarks: cover=%d deployH2=%d deployBtn=%d ig=%d\n%s",
			coverIdx, deployH2Idx, deployButtonIdx, igH2Idx, body)
	}
	if coverIdx >= deployH2Idx || deployH2Idx >= deployButtonIdx || deployButtonIdx >= igH2Idx {
		t.Errorf("ordering violation: want cover<deployH2<deployBtn<ig, got cover=%d deployH2=%d deployBtn=%d ig=%d\n%s",
			coverIdx, deployH2Idx, deployButtonIdx, igH2Idx, body)
	}
}

// TestAssembleCodebaseREADME_V9_79_NoFullRecipePageLead — the legacy
// "⬇️ **Full recipe page and deploy with one-click**" lead is dropped
// in favor of the explicit H2 wrapper.
func TestAssembleCodebaseREADME_V9_79_NoFullRecipePageLead(t *testing.T) {
	t.Parallel()
	plan := codebaseREADMEV979Plan(t)
	body, _, err := AssembleCodebaseREADME(plan, "api")
	if err != nil {
		t.Fatalf("AssembleCodebaseREADME: %v", err)
	}
	if strings.Contains(body, "Full recipe page and deploy with one-click") {
		t.Errorf("rendered body still contains the legacy 'Full recipe page' lead:\n%s", body)
	}
}
