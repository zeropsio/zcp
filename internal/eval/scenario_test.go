package eval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseScenario_Valid_Success(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		fixture  string
		wantID   string
		wantSeed SeedMode
		wantMin  int
	}{
		{"empty_seed", "empty_seed.md", "test-empty", ModeEmpty, 5},
		{"imported_seed", "imported_seed.md", "test-imported", ModeImported, 10},
		{"deployed_seed", "deployed_seed.md", "test-deployed", ModeDeployed, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join("testdata", "scenarios", tt.fixture)
			sc, err := ParseScenario(path)
			if err != nil {
				t.Fatalf("ParseScenario: %v", err)
			}

			if sc.ID != tt.wantID {
				t.Errorf("ID: got %q, want %q", sc.ID, tt.wantID)
			}
			if sc.Seed != tt.wantSeed {
				t.Errorf("Seed: got %q, want %q", sc.Seed, tt.wantSeed)
			}
			if sc.Expect.WorkflowCallsMin != tt.wantMin {
				t.Errorf("WorkflowCallsMin: got %d, want %d", sc.Expect.WorkflowCallsMin, tt.wantMin)
			}
			if sc.Prompt == "" {
				t.Error("Prompt: empty")
			}
		})
	}
}

func TestParseScenario_Invalid_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		fixture    string
		wantErrMsg string
	}{
		{"missing_id", "missing_id.md", "id required"},
		{"bad_seed", "bad_seed.md", "invalid seed mode"},
		{"no_frontmatter", "no_frontmatter.md", "missing frontmatter"},
		{"missing_fixture_for_imported", "imported_no_fixture.md", "fixture required"},
		{"empty_prompt", "empty_prompt.md", "prompt body required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join("testdata", "scenarios", tt.fixture)
			_, err := ParseScenario(path)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErrMsg) {
				t.Errorf("error: got %q, want substring %q", err.Error(), tt.wantErrMsg)
			}
		})
	}
}

func TestParseScenario_Prompt_PreservesBody(t *testing.T) {
	t.Parallel()

	path := filepath.Join("testdata", "scenarios", "empty_seed.md")
	sc, err := ParseScenario(path)
	if err != nil {
		t.Fatalf("ParseScenario: %v", err)
	}

	if !strings.Contains(sc.Prompt, "Create a simple web app") {
		t.Errorf("prompt body missing task text, got: %q", sc.Prompt)
	}
	if strings.Contains(sc.Prompt, "---") {
		t.Error("prompt should not contain frontmatter delimiters")
	}
}

func TestBuildScenarioPrompt_RequireAssessment_AppendsEvalReport(t *testing.T) {
	t.Parallel()

	sc := &Scenario{
		Prompt: "Do the thing.",
		Expect: Expectation{RequireAssessment: true},
	}

	got := buildScenarioPrompt(sc)
	if !strings.Contains(got, "## EVAL REPORT") {
		t.Errorf("prompt should contain '## EVAL REPORT' when RequireAssessment=true\n%s", got)
	}
	if !strings.Contains(got, "Deployment outcome") {
		t.Errorf("prompt should contain 'Deployment outcome' section, got:\n%s", got)
	}
}

func TestBuildScenarioPrompt_NoRequireAssessment_OmitsEvalReport(t *testing.T) {
	t.Parallel()

	sc := &Scenario{
		Prompt: "Do the thing.",
		Expect: Expectation{RequireAssessment: false},
	}

	got := buildScenarioPrompt(sc)
	if strings.Contains(got, "## EVAL REPORT") {
		t.Errorf("prompt should NOT contain '## EVAL REPORT' when RequireAssessment=false\n%s", got)
	}
}

func TestParseScenario_FollowUp_Parsed(t *testing.T) {
	t.Parallel()

	path := filepath.Join("testdata", "scenarios", "empty_seed.md")
	sc, err := ParseScenario(path)
	if err != nil {
		t.Fatalf("ParseScenario: %v", err)
	}

	if len(sc.FollowUp) != 2 {
		t.Fatalf("followUp count: got %d, want 2", len(sc.FollowUp))
	}
	if sc.FollowUp[0] != "Why did you choose that approach?" {
		t.Errorf("followUp[0]: got %q", sc.FollowUp[0])
	}
}

// TestParseScenario_PreseedScript covers the state-detection scenarios that
// need local state pre-populated after init wipes the workdir. The frontmatter
// field resolves relative to the scenario file so authors don't have to
// duplicate path prefixes.
func TestParseScenario_PreseedScript(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	scPath := filepath.Join(dir, "scenario.md")
	content := `---
id: preseed-test
description: preseed frontmatter check
seed: empty
preseedScript: scripts/seed-state.sh
expect:
  mustCallTools:
    - zerops_workflow
---

Run the thing.
`
	if err := os.WriteFile(scPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write scenario: %v", err)
	}
	sc, err := ParseScenario(scPath)
	if err != nil {
		t.Fatalf("ParseScenario: %v", err)
	}
	if sc.PreseedScript != "scripts/seed-state.sh" {
		t.Errorf("PreseedScript: got %q, want scripts/seed-state.sh", sc.PreseedScript)
	}
}
