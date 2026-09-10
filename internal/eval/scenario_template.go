package eval

import (
	"fmt"
	"regexp"
)

// scenarioTemplateToken matches a {{token}} placeholder in a scenario's
// prompt body or userPersona.
var scenarioTemplateToken = regexp.MustCompile(`\{\{\s*([A-Za-z0-9_]+)\s*\}\}`)

// TemplateValues are the explicit substitution values a caller supplies to
// Scenario.Render for {{runId}}/{{projectId}} placeholders in a scenario's
// prompt/userPersona. A zero-value TemplateValues is valid for a scenario
// with no placeholders; a placeholder present in the scenario whose
// corresponding value is empty is a Render error.
type TemplateValues struct {
	RunID     string
	ProjectID string
}

// rejectUnknownTemplateTokens scans s for {{...}} placeholders and errors on
// the first one that is not {{runId}} or {{projectId}}. Called at parse
// time (ParseScenario) so a scenario naming a typo'd or unsupported token
// fails to load immediately, before any run ever reaches Render.
func rejectUnknownTemplateTokens(s string) error {
	for _, m := range scenarioTemplateToken.FindAllStringSubmatch(s, -1) {
		switch m[1] {
		case "runId", "projectId":
		default:
			return fmt.Errorf("unknown template token {{%s}} (want {{runId}} or {{projectId}})", m[1])
		}
	}
	return nil
}

// substituteScenarioTemplate replaces {{runId}} and {{projectId}} tokens in
// s with values. A string with no tokens is returned unchanged, byte for
// byte. A recognized token whose value is empty is a load error. s is
// assumed to already be free of unknown tokens (rejectUnknownTemplateTokens
// ran at parse time).
func substituteScenarioTemplate(s string, values TemplateValues) (string, error) {
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
			if values.RunID == "" {
				firstErr = fmt.Errorf("template token {{runId}} used but no run id was given")
				return tok
			}
			return values.RunID
		case "projectId":
			if values.ProjectID == "" {
				firstErr = fmt.Errorf("template token {{projectId}} used but no project id was given")
				return tok
			}
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

// Render substitutes {{runId}}/{{projectId}} into sc.Prompt and
// sc.UserPersona in place, using the explicit values the caller supplies.
// A scenario with no placeholders renders to itself, byte for byte. A
// placeholder the scenario uses whose corresponding value is empty is an
// error — call Render before any seed/mutation for the run, and abort the
// run on error.
func (sc *Scenario) Render(values TemplateValues) error {
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
