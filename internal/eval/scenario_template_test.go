package eval

import (
	"os"
	"reflect"
	"strings"
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

// TestScenarioRender_CoversEveryVerificationString is a reflect-based drift
// guard (docs/spec-eval-farm.md §2.2 FM-12): it walks VerificationConfig,
// fills every string / []string leaf (skipping the Reach field — FM-32
// exists only to be rejected at parse, never reaches Render) with
// "{{runId}}", calls Scenario.Render, and asserts no "{{" token survives
// anywhere in the rendered VerificationConfig. A field added to
// VerificationConfig later without matching Render coverage
// (renderVerificationConfig, scenario_template.go) fails this test instead
// of silently shipping an unsubstituted placeholder.
func TestScenarioRender_CoversEveryVerificationString(t *testing.T) {
	t.Parallel()

	cfg := &VerificationConfig{}
	fillTemplateTokens(reflect.ValueOf(cfg))

	sc := &Scenario{Prompt: "noop", Verification: cfg}
	if err := sc.Render(TemplateValues{RunID: "run-drift", ProjectID: "proj-drift"}); err != nil {
		t.Fatalf("Render: %v", err)
	}

	if leaks := findUnrenderedTokens(reflect.ValueOf(sc.Verification)); len(leaks) > 0 {
		t.Errorf("unrendered {{...}} tokens survived Render — add coverage in renderVerificationConfig: %v", leaks)
	}
}

// fillTemplateTokens recursively walks v (a struct, pointer, or slice) and
// sets every string field/element to "{{runId}}" and every []string
// field/element's entries to the same, allocating nil pointers and
// zero-length slices of one element as needed to reach every leaf. The
// VerificationConfig.Reach field (*yaml.Node, FM-32: exists only to be
// rejected at parse) is skipped — Render never touches it.
func fillTemplateTokens(v reflect.Value) {
	switch v.Kind() { //nolint:exhaustive // walks a generic reflect.Value; every non-container/string kind falls through as a no-op leaf
	case reflect.Ptr:
		if v.IsNil() {
			if !v.CanSet() {
				return
			}
			v.Set(reflect.New(v.Type().Elem()))
		}
		fillTemplateTokens(v.Elem())
	case reflect.Struct:
		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			field := v.Field(i)
			name := t.Field(i).Name
			if !field.CanSet() || name == "Reach" {
				continue
			}
			switch field.Kind() { //nolint:exhaustive // only string/slice/ptr/struct leaves carry template tokens; every other kind is a no-op
			case reflect.String:
				field.SetString("{{runId}}")
			case reflect.Slice:
				if field.Type().Elem().Kind() == reflect.String {
					field.Set(reflect.ValueOf([]string{"{{runId}}"}))
					continue
				}
				if field.Len() == 0 {
					field.Set(reflect.MakeSlice(field.Type(), 1, 1))
				}
				for j := 0; j < field.Len(); j++ {
					fillTemplateTokens(field.Index(j))
				}
			case reflect.Ptr, reflect.Struct:
				fillTemplateTokens(field)
			}
		}
	}
}

// findUnrenderedTokens recursively walks v and returns every string leaf
// that still contains "{{", for the drift test's assertion.
func findUnrenderedTokens(v reflect.Value) []string {
	var leaks []string
	var walk func(reflect.Value)
	walk = func(v reflect.Value) {
		switch v.Kind() { //nolint:exhaustive // walks a generic reflect.Value; every non-container/string kind falls through as a no-op leaf
		case reflect.Ptr:
			if !v.IsNil() {
				walk(v.Elem())
			}
		case reflect.Struct:
			t := v.Type()
			for i := 0; i < v.NumField(); i++ {
				if t.Field(i).Name == "Reach" {
					continue
				}
				walk(v.Field(i))
			}
		case reflect.Slice:
			for j := 0; j < v.Len(); j++ {
				walk(v.Index(j))
			}
		case reflect.String:
			if strings.Contains(v.String(), "{{") {
				leaks = append(leaks, v.String())
			}
		}
	}
	walk(v)
	return leaks
}
