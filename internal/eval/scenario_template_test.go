package eval

import "testing"

// TestScenarioTemplate_RunIdProjectId_Substituted pins {{runId}}/{{projectId}}
// substitution in the prompt body and userPersona at scenario load time: an
// unknown token is a load error, and a string with no tokens is returned
// byte-identical.
func TestScenarioTemplate_RunIdProjectId_Substituted(t *testing.T) {
	t.Parallel()

	t.Run("prompt substitutes both tokens", func(t *testing.T) {
		t.Parallel()
		got, err := substituteScenarioTemplate(
			"Deploy run {{runId}} into project {{projectId}} now.",
			scenarioTemplateValues{RunID: "run-42", ProjectID: "proj-99"},
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
			scenarioTemplateValues{RunID: "run-1", ProjectID: "proj-1"},
		)
		if err != nil {
			t.Fatalf("substituteScenarioTemplate: %v", err)
		}
		want := "You are the developer on run run-1 (proj-1)."
		if got != want {
			t.Errorf("got %q want %q", got, want)
		}
	})

	t.Run("unknown token is a load error", func(t *testing.T) {
		t.Parallel()
		_, err := substituteScenarioTemplate(
			"Use {{serviceId}} for this task.",
			scenarioTemplateValues{RunID: "run-1", ProjectID: "proj-1"},
		)
		if err == nil {
			t.Fatal("want error for unknown token {{serviceId}}, got nil")
		}
	})

	t.Run("no tokens is byte-identical", func(t *testing.T) {
		t.Parallel()
		const body = "Deploy the app and verify it comes up healthy."
		got, err := substituteScenarioTemplate(body, scenarioTemplateValues{RunID: "run-1", ProjectID: "proj-1"})
		if err != nil {
			t.Fatalf("substituteScenarioTemplate: %v", err)
		}
		if got != body {
			t.Errorf("got %q want byte-identical %q", got, body)
		}
	})

	t.Run("empty values still substitute (empty string)", func(t *testing.T) {
		t.Parallel()
		got, err := substituteScenarioTemplate("run={{runId}}", scenarioTemplateValues{})
		if err != nil {
			t.Fatalf("substituteScenarioTemplate: %v", err)
		}
		if got != "run=" {
			t.Errorf("got %q want %q", got, "run=")
		}
	})
}
