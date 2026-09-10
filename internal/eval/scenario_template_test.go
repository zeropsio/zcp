package eval

import (
	"os"
	"testing"
)

// TestScenarioTemplate_RunIdProjectId_Substituted pins {{runId}}/{{projectId}}
// substitution in the prompt body and userPersona via Scenario.Render: a
// recognized token with no value given is a Render error, and a string with
// no tokens renders byte-identical.
func TestScenarioTemplate_RunIdProjectId_Substituted(t *testing.T) {
	t.Parallel()

	t.Run("prompt substitutes both tokens", func(t *testing.T) {
		t.Parallel()
		got, err := substituteScenarioTemplate(
			"Deploy run {{runId}} into project {{projectId}} now.",
			TemplateValues{RunID: "run-42", ProjectID: "proj-99"},
		)
		if err != nil {
			t.Fatalf("substituteScenarioTemplate: %v", err)
		}
		want := "Deploy run run-42 into project proj-99 now."
		if got != want {
			t.Errorf("got %q want %q", got, want)
		}
	})

	t.Run("persona substitutes both tokens", func(t *testing.T) {
		t.Parallel()
		got, err := substituteScenarioTemplate(
			"You are the developer on run {{runId}} ({{projectId}}).",
			TemplateValues{RunID: "run-1", ProjectID: "proj-1"},
		)
		if err != nil {
			t.Fatalf("substituteScenarioTemplate: %v", err)
		}
		want := "You are the developer on run run-1 (proj-1)."
		if got != want {
			t.Errorf("got %q want %q", got, want)
		}
	})

	t.Run("no tokens is byte-identical", func(t *testing.T) {
		t.Parallel()
		const body = "Deploy the app and verify it comes up healthy."
		got, err := substituteScenarioTemplate(body, TemplateValues{RunID: "run-1", ProjectID: "proj-1"})
		if err != nil {
			t.Fatalf("substituteScenarioTemplate: %v", err)
		}
		if got != body {
			t.Errorf("got %q want byte-identical %q", got, body)
		}
	})

	t.Run("empty RunID with runId token used is a load error", func(t *testing.T) {
		t.Parallel()
		_, err := substituteScenarioTemplate("run={{runId}}", TemplateValues{ProjectID: "proj-1"})
		if err == nil {
			t.Fatal("want error for {{runId}} with empty RunID, got nil")
		}
	})

	t.Run("empty ProjectID with projectId token used is a load error", func(t *testing.T) {
		t.Parallel()
		_, err := substituteScenarioTemplate("project={{projectId}}", TemplateValues{RunID: "run-1"})
		if err == nil {
			t.Fatal("want error for {{projectId}} with empty ProjectID, got nil")
		}
	})

	t.Run("zero-value TemplateValues is fine for a scenario with no placeholders", func(t *testing.T) {
		t.Parallel()
		const body = "No placeholders here at all."
		got, err := substituteScenarioTemplate(body, TemplateValues{})
		if err != nil {
			t.Fatalf("substituteScenarioTemplate: %v", err)
		}
		if got != body {
			t.Errorf("got %q want byte-identical %q", got, body)
		}
	})
}

// TestParseScenario_UnknownTemplateToken_RejectedAtParse pins that
// ParseScenario itself only rejects an UNKNOWN {{...}} token — it never
// substitutes and never requires values, so a scenario carrying
// {{runId}}/{{projectId}} parses successfully with the placeholders intact.
func TestParseScenario_UnknownTemplateToken_RejectedAtParse(t *testing.T) {
	t.Parallel()

	t.Run("known placeholders parse successfully, unsubstituted", func(t *testing.T) {
		t.Parallel()
		path := writeScenarioFixture(t, "---\n"+
			"id: templated-scenario\n"+
			"seed: empty\n"+
			"---\n"+
			"Deploy into project {{projectId}} for run {{runId}}.\n")

		sc, err := ParseScenario(path)
		if err != nil {
			t.Fatalf("ParseScenario: %v", err)
		}
		want := "Deploy into project {{projectId}} for run {{runId}}."
		if sc.Prompt != want {
			t.Errorf("Prompt: got %q want %q (ParseScenario must not substitute)", sc.Prompt, want)
		}
	})

	t.Run("unknown token is a load error", func(t *testing.T) {
		t.Parallel()
		path := writeScenarioFixture(t, "---\n"+
			"id: bad-token-scenario\n"+
			"seed: empty\n"+
			"---\n"+
			"Use {{serviceId}} for this task.\n")

		if _, err := ParseScenario(path); err == nil {
			t.Fatal("want error for unknown token {{serviceId}}, got nil")
		}
	})
}

// TestParseScenarioForRun_RunnerPath_Substitutes pins the runner call site
// (internal/eval/behavioral_run.go: parseScenarioForRun parses, then calls
// Scenario.Render with the run's suiteID/projectID already in hand): the
// rendered scenario carries the substituted prompt, and Render on a
// scenario without placeholders is a byte-identical no-op.
func TestParseScenarioForRun_RunnerPath_Substitutes(t *testing.T) {
	t.Parallel()

	t.Run("Render substitutes both values", func(t *testing.T) {
		t.Parallel()
		path := writeScenarioFixture(t, "---\n"+
			"id: templated-scenario\n"+
			"seed: empty\n"+
			"---\n"+
			"Deploy into project {{projectId}} for run {{runId}}.\n")

		sc, err := ParseScenario(path)
		if err != nil {
			t.Fatalf("ParseScenario: %v", err)
		}
		if err := sc.Render(TemplateValues{RunID: "run-suite-1", ProjectID: "proj-abc"}); err != nil {
			t.Fatalf("Render: %v", err)
		}
		want := "Deploy into project proj-abc for run run-suite-1."
		if sc.Prompt != want {
			t.Errorf("Prompt: got %q want %q", sc.Prompt, want)
		}
	})

	t.Run("Render without placeholders is a byte-identical no-op", func(t *testing.T) {
		t.Parallel()
		path := writeScenarioFixture(t, "---\n"+
			"id: plain-scenario\n"+
			"seed: empty\n"+
			"---\n"+
			"Deploy the app and verify it comes up healthy.\n")

		sc, err := ParseScenario(path)
		if err != nil {
			t.Fatalf("ParseScenario: %v", err)
		}
		before := sc.Prompt
		if err := sc.Render(TemplateValues{}); err != nil {
			t.Fatalf("Render: %v", err)
		}
		if sc.Prompt != before {
			t.Errorf("Render changed a placeholder-free prompt: got %q want %q", sc.Prompt, before)
		}
	})

	t.Run("Render errors on an unmet placeholder", func(t *testing.T) {
		t.Parallel()
		path := writeScenarioFixture(t, "---\n"+
			"id: templated-scenario-2\n"+
			"seed: empty\n"+
			"---\n"+
			"Deploy into project {{projectId}}.\n")

		sc, err := ParseScenario(path)
		if err != nil {
			t.Fatalf("ParseScenario: %v", err)
		}
		if err := sc.Render(TemplateValues{RunID: "run-1"}); err == nil {
			t.Fatal("want Render error for {{projectId}} with empty ProjectID, got nil")
		}
	})
}

func writeScenarioFixture(t *testing.T, content string) string {
	t.Helper()
	path := t.TempDir() + "/scenario.md"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write scenario fixture: %v", err)
	}
	return path
}
