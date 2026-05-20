package tools

import (
	"strings"
	"testing"
)

const sourceWithDevOnly = `zerops:
  - setup: dev
    build:
      base: nodejs@22
      buildCommands:
        - npm install
    run:
      base: nodejs@22
      start: node dist/server.js
`

const sourceWithDevAndStage = `zerops:
  - setup: dev
    build:
      base: nodejs@22
      buildCommands:
        - npm install
    run:
      base: nodejs@22
      start: node dist/server.js
  - setup: stage
    build:
      base: nodejs@22
      buildCommands:
        - npm ci
    run:
      base: nodejs@22
      start: node dist/server.js
`

const sourceWithSimpleOnly = `zerops:
  - setup: simple
    build:
      base: bun@1
      buildCommands:
        - bun install
    run:
      base: bun@1
      start: bun run index.ts
`

const sourceWithOnlyProd = `zerops:
  - setup: prod
    build:
      base: nodejs@22
`

// TestDeriveProdSetupBlock_PreferStage pins P3 priority reorder:
// stage wins over dev as the template source. Stage is the validated
// last-known-good copy that runs on Zerops (build-once, immutable);
// dev encodes iteration patterns that don't survive the prod build.
func TestDeriveProdSetupBlock_PreferStage(t *testing.T) {
	t.Parallel()
	got, err := deriveProdSetupBlock(sourceWithDevAndStage)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if !strings.Contains(got, "setup: prod") {
		t.Errorf("output missing setup: prod\n%s", got)
	}
	// dev's buildCommand was `npm install`; stage was `npm ci`.
	// Preferring stage means the proposed block carries `npm ci`.
	if !strings.Contains(got, "npm ci") {
		t.Errorf("expected stage's npm ci (template prefer stage post-P3), got:\n%s", got)
	}
	if strings.Contains(got, "npm install") {
		t.Errorf("expected stage template, not dev:\n%s", got)
	}
}

// TestDeriveProdSetupBlock_FallbackToFirstNonProd uses simple-mode
// source — dev/stage absent — falls back to first non-prod block.
func TestDeriveProdSetupBlock_FallbackToFirstNonProd(t *testing.T) {
	t.Parallel()
	got, err := deriveProdSetupBlock(sourceWithSimpleOnly)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if !strings.Contains(got, "setup: prod") {
		t.Errorf("output missing setup: prod\n%s", got)
	}
	if !strings.Contains(got, "bun@1") {
		t.Errorf("expected bun@1 base copied verbatim:\n%s", got)
	}
}

// TestDeriveProdSetupBlock_DevOnlyCopiesVerbatim verifies the
// derivation copies build + run blocks unchanged.
func TestDeriveProdSetupBlock_DevOnlyCopiesVerbatim(t *testing.T) {
	t.Parallel()
	got, err := deriveProdSetupBlock(sourceWithDevOnly)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	for _, want := range []string{
		"setup: prod",
		"base: nodejs@22",
		"npm install",
		"node dist/server.js",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in output:\n%s", want, got)
		}
	}
}

// TestDeriveProdSetupBlock_OnlyProdReturnsError pins the edge case
// where source has only setup:prod (defensive — caller checks
// hasSetupProd first so this shouldn't fire in practice).
func TestDeriveProdSetupBlock_OnlyProdReturnsError(t *testing.T) {
	t.Parallel()
	_, err := deriveProdSetupBlock(sourceWithOnlyProd)
	if err == nil {
		t.Fatal("expected error when only setup:prod is available as template")
	}
}

// TestDeriveProdSetupBlock_MalformedYAMLReturnsError pins yaml parse
// failure handling.
func TestDeriveProdSetupBlock_MalformedYAMLReturnsError(t *testing.T) {
	t.Parallel()
	_, err := deriveProdSetupBlock("zerops:\n  - setup: [unclosed")
	if err == nil {
		t.Fatal("expected error for malformed yaml")
	}
}

// TestProdSetupGuidanceWithBlock_EmbedsBlockAndChecklist verifies the
// response guidance carries the derived block + readiness checklist.
func TestProdSetupGuidanceWithBlock_EmbedsBlockAndChecklist(t *testing.T) {
	t.Parallel()
	proposed, err := deriveProdSetupBlock(sourceWithDevOnly)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	guidance := prodSetupGuidanceWithBlock("prod", []string{"dev"}, proposed)
	for _, want := range []string{
		"setup: prod",
		"Found setups: dev",
		"prodSetupNameOverride",
		"npm install",
		"healthCheck",
		"NODE_ENV=production",
		"```yaml",
	} {
		if !strings.Contains(guidance, want) {
			t.Errorf("guidance missing %q:\n%s", want, guidance)
		}
	}
}

// TestProdSetupGuidanceWithBlock_HonoursOverrideName verifies the
// guidance addresses a non-canonical override setup name (`appprod`)
// when the agent passes ProdSetupNameOverride.
func TestProdSetupGuidanceWithBlock_HonoursOverrideName(t *testing.T) {
	t.Parallel()
	proposed, err := deriveProdSetupBlock(sourceWithDevOnly)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	guidance := prodSetupGuidanceWithBlock("appprod", []string{"dev", "stage"}, proposed)
	if !strings.Contains(guidance, "setup: appprod") {
		t.Errorf("guidance missing override name `setup: appprod`:\n%s", guidance)
	}
	if !strings.Contains(guidance, "Found setups: dev, stage") {
		t.Errorf("guidance missing available-setups list:\n%s", guidance)
	}
}

// TestEffectiveProdSetupName_OverrideTakesPrecedence pins the override
// resolution: explicit non-empty override → that value; empty / whitespace
// → "prod" canonical default.
func TestEffectiveProdSetupName_OverrideTakesPrecedence(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		override string
		want     string
	}{
		{"empty_falls_back_to_prod", "", "prod"},
		{"whitespace_falls_back_to_prod", "   ", "prod"},
		{"explicit_override_wins", "appprod", "appprod"},
		{"app_override", "app", "app"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := effectiveProdSetupName(WorkflowInput{ProdSetupNameOverride: tt.override})
			if got != tt.want {
				t.Errorf("effectiveProdSetupName(%q): got %q, want %q", tt.override, got, tt.want)
			}
		})
	}
}

// TestHasSetupNamed_MatchesArbitraryName pins the generalized hasSetup
// helper. Used by the source-control gate to honor
// WorkflowInput.ProdSetupNameOverride.
func TestHasSetupNamed_MatchesArbitraryName(t *testing.T) {
	t.Parallel()
	body := `zerops:
  - setup: dev
  - setup: appprod
`
	tests := []struct {
		name  string
		setup string
		want  bool
	}{
		{"matches_dev", "dev", true},
		{"matches_appprod", "appprod", true},
		{"missing_prod_canonical", "prod", false},
		{"empty_returns_false", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := hasSetupNamed(body, tt.setup); got != tt.want {
				t.Errorf("hasSetupNamed(%q): got %v, want %v", tt.setup, got, tt.want)
			}
		})
	}
}
