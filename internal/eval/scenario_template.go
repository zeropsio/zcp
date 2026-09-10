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

// Render substitutes {{runId}}/{{projectId}} into sc.Prompt, sc.UserPersona,
// and every string / []string field of sc.Verification (and its nested
// structs), in place, using the explicit values the caller supplies. A
// scenario with no placeholders renders to itself, byte for byte. A
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
	if sc.Verification != nil {
		if err := renderVerificationConfig(sc.Verification, values); err != nil {
			return fmt.Errorf("verification: %w", err)
		}
	}
	return nil
}

// renderVerificationString is substituteScenarioTemplate with the field
// name folded into the error for renderVerificationConfig's call sites.
func renderVerificationString(field, s string, values TemplateValues) (string, error) {
	out, err := substituteScenarioTemplate(s, values)
	if err != nil {
		return "", fmt.Errorf("%s: %w", field, err)
	}
	return out, nil
}

func renderVerificationStringSlice(field string, ss []string, values TemplateValues) error {
	for i, s := range ss {
		out, err := renderVerificationString(field, s, values)
		if err != nil {
			return err
		}
		ss[i] = out
	}
	return nil
}

// renderVerificationConfig substitutes {{runId}}/{{projectId}} into every
// string / []string leaf of cfg (docs/spec-eval-farm.md §2.2 FM-12). Field
// list is enumerated explicitly, not walked via reflect, so a field added to
// VerificationConfig later without a matching line here is caught by
// TestScenarioRender_CoversEveryVerificationString's reflect-based drift
// check rather than silently rendering unsubstituted.
func renderVerificationConfig(cfg *VerificationConfig, values TemplateValues) error {
	var err error
	if cfg.Mode, err = renderVerificationString("mode", cfg.Mode, values); err != nil {
		return err
	}
	if cfg.Spec, err = renderVerificationString("spec", cfg.Spec, values); err != nil {
		return err
	}
	if err := renderVerificationStringSlice("allowFailed", cfg.AllowFailed, values); err != nil {
		return err
	}
	if err := renderVerificationStringSlice("unchanged", cfg.Unchanged, values); err != nil {
		return err
	}
	if err := renderVerificationStringSlice("never", cfg.Never, values); err != nil {
		return err
	}
	if err := renderVerificationStringSlice("askWhen", cfg.AskWhen, values); err != nil {
		return err
	}
	if err := renderVerificationStringSlice("retrospectiveMustNotMention", cfg.RetrospectiveMustNotMention, values); err != nil {
		return err
	}
	for i := range cfg.ExpectedServices {
		if cfg.ExpectedServices[i].Hostname, err = renderVerificationString("expectedServices.hostname", cfg.ExpectedServices[i].Hostname, values); err != nil {
			return err
		}
		if cfg.ExpectedServices[i].Type, err = renderVerificationString("expectedServices.type", cfg.ExpectedServices[i].Type, values); err != nil {
			return err
		}
		if err := renderVerificationStringSlice("expectedServices.status", cfg.ExpectedServices[i].Status, values); err != nil {
			return err
		}
		if probe := cfg.ExpectedServices[i].SubdomainProbe; probe != nil {
			if probe.Path, err = renderVerificationString("expectedServices.subdomainProbe.path", probe.Path, values); err != nil {
				return err
			}
			if probe.ExpectStatus, err = renderVerificationString("expectedServices.subdomainProbe.expectStatus", probe.ExpectStatus, values); err != nil {
				return err
			}
		}
	}
	if cfg.Liveness != nil {
		if cfg.Liveness.Service, err = renderVerificationString("liveness.service", cfg.Liveness.Service, values); err != nil {
			return err
		}
		if cfg.Liveness.Marker, err = renderVerificationString("liveness.marker", cfg.Liveness.Marker, values); err != nil {
			return err
		}
	}
	if cfg.NodePostgresRecord != nil {
		n := cfg.NodePostgresRecord
		if n.Stage, err = renderVerificationString("nodePostgresRecord.stage", n.Stage, values); err != nil {
			return err
		}
		if n.Database, err = renderVerificationString("nodePostgresRecord.database", n.Database, values); err != nil {
			return err
		}
		if n.Unrelated, err = renderVerificationString("nodePostgresRecord.unrelated", n.Unrelated, values); err != nil {
			return err
		}
	}
	if cfg.LaunchShape != nil {
		if cfg.LaunchShape.ProdProject, err = renderVerificationString("launchShape.prodProject", cfg.LaunchShape.ProdProject, values); err != nil {
			return err
		}
	}
	for i := range cfg.ArtifactPromotion {
		if cfg.ArtifactPromotion[i].From, err = renderVerificationString("artifactPromotion.from", cfg.ArtifactPromotion[i].From, values); err != nil {
			return err
		}
		if cfg.ArtifactPromotion[i].To, err = renderVerificationString("artifactPromotion.to", cfg.ArtifactPromotion[i].To, values); err != nil {
			return err
		}
	}
	return nil
}
