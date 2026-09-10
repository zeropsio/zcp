package eval

import (
	"fmt"
	"os"
	"regexp"
)

// scenarioTemplateToken matches a {{token}} placeholder in a scenario's
// prompt body or userPersona.
var scenarioTemplateToken = regexp.MustCompile(`\{\{\s*([A-Za-z0-9_]+)\s*\}\}`)

// scenarioTemplateValues are the substitution values recognized inside a
// scenario's prompt/persona. Any other {{...}} token is a load error.
type scenarioTemplateValues struct {
	RunID     string
	ProjectID string
}

// substituteScenarioTemplate replaces {{runId}} and {{projectId}} tokens in
// s with values. A string with no tokens is returned unchanged, byte for
// byte. Any other {{...}} token is a load error.
func substituteScenarioTemplate(s string, values scenarioTemplateValues) (string, error) {
	if !scenarioTemplateToken.MatchString(s) {
		return s, nil
	}
	var firstErr error
	result := scenarioTemplateToken.ReplaceAllStringFunc(s, func(tok string) string {
		if firstErr != nil {
			return tok
		}
		name := scenarioTemplateToken.FindStringSubmatch(tok)[1]
		switch name {
		case "runId":
			return values.RunID
		case "projectId":
			return values.ProjectID
		default:
			firstErr = fmt.Errorf("unknown template token {{%s}} (want {{runId}} or {{projectId}})", name)
			return tok
		}
	})
	if firstErr != nil {
		return "", firstErr
	}
	return result, nil
}

// applyScenarioTemplate substitutes {{runId}}/{{projectId}} into sc.Prompt
// and sc.UserPersona in place. Values come from the environment the runner
// has already populated at this point in the process: ZCP_FARM_RUN carries
// the farm run id (docs/spec-eval-farm.md §2.2, FM-12); ZCP_EVAL_RUN_ID is
// the fallback for a non-farm run (the runner's own suite/run identifier);
// ZCP_EVAL_PROJECT_ID carries the resolved --project-id. Called once, at
// scenario load time, from ParseScenario — the single place scenarios are
// loaded.
func applyScenarioTemplate(sc *Scenario) error {
	values := scenarioTemplateValues{
		RunID:     scenarioTemplateRunID(),
		ProjectID: os.Getenv("ZCP_EVAL_PROJECT_ID"),
	}
	prompt, err := substituteScenarioTemplate(sc.Prompt, values)
	if err != nil {
		return fmt.Errorf("prompt: %w", err)
	}
	persona, err := substituteScenarioTemplate(sc.UserPersona, values)
	if err != nil {
		return fmt.Errorf("userPersona: %w", err)
	}
	sc.Prompt = prompt
	sc.UserPersona = persona
	return nil
}

func scenarioTemplateRunID() string {
	if v := os.Getenv("ZCP_FARM_RUN"); v != "" {
		return v
	}
	return os.Getenv("ZCP_EVAL_RUN_ID")
}
