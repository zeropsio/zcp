package recipe

import (
	"context"
	"fmt"
	"strings"
)

// Run-8-readiness Workstream D — root + env surface validators.
// validators.go carries the harness + shared helpers; this file
// implements the root-README, env-README, and env-import-comment
// checks from docs/spec-content-surfaces.md §"Surface 1-3".

// validateRootREADME covers length, intro marker, deploy-button count,
// and factuality (framework names claimed in body must appear in the
// plan's declared framework).
func validateRootREADME(_ context.Context, path string, body []byte, inputs SurfaceInputs) ([]Violation, error) {
	var vs []Violation
	s := string(body)
	lines := strings.Count(s, "\n")
	if lines < 20 {
		vs = append(vs, violation("root-readme-too-short", path, fmt.Sprintf("%d lines < 20", lines)))
	}
	if lines > 50 {
		vs = append(vs, violation("root-readme-too-long", path, fmt.Sprintf("%d lines > 50", lines)))
	}
	if !strings.Contains(s, "<!-- #ZEROPS_EXTRACT_START:intro# -->") {
		vs = append(vs, violation("root-readme-missing-intro-marker", path, "intro marker missing"))
	}
	if buttonCount := strings.Count(s, "app.zerops.io/recipes/"); buttonCount < 6 {
		vs = append(vs, violation("root-readme-deploy-buttons-missing",
			path, fmt.Sprintf("%d deploy-button URLs < 6 expected (one per tier)", buttonCount)))
	}
	vs = append(vs, factualityCheck(path, s, inputs)...)
	return vs, nil
}

// factualityCheck looks for common framework names in the body and
// compares them to plan.Framework. Mismatch is a violation. Node-only
// at run-8; plan Q5 defers multi-manifest generalization.
func factualityCheck(path, body string, inputs SurfaceInputs) []Violation {
	if inputs.Plan == nil || inputs.Plan.Framework == "" {
		return nil
	}
	candidates := []string{
		"Laravel", "NestJS", "Svelte", "Django", "Rails",
		"Flask", "Next.js", "Remix", "Angular", "Vue",
	}
	declared := strings.ToLower(inputs.Plan.Framework)
	for _, c := range candidates {
		if !strings.Contains(body, c) {
			continue
		}
		if strings.ToLower(c) == declared {
			continue
		}
		return []Violation{violation("factuality-mismatch", path,
			fmt.Sprintf("README names %q but plan.Framework = %q", c, inputs.Plan.Framework))}
	}
	return nil
}

// validateEnvREADME — length 40-120 lines; intro marker; no meta-voice;
// tier-promotion vocabulary present.
func validateEnvREADME(_ context.Context, path string, body []byte, _ SurfaceInputs) ([]Violation, error) {
	var vs []Violation
	s := string(body)
	lines := strings.Count(s, "\n")
	if lines < 40 {
		vs = append(vs, violation("env-readme-too-short", path, fmt.Sprintf("%d lines < 40", lines)))
	}
	if lines > 120 {
		vs = append(vs, violation("env-readme-too-long", path, fmt.Sprintf("%d lines > 120", lines)))
	}
	if !strings.Contains(s, "<!-- #ZEROPS_EXTRACT_START:intro# -->") {
		vs = append(vs, violation("env-readme-missing-intro-marker", path, "intro marker missing"))
	}
	if containsAny(s, metaVoiceWords) {
		vs = append(vs, violation("meta-agent-voice", path,
			"env README is porter-facing; contains meta-agent-voice words (agent, zerops_knowledge, sub-agent, scaffolder)"))
	}
	if !containsAny(s, tierPromotionVerbs) {
		vs = append(vs, violation("tier-promotion-verb-missing", path,
			"env README must teach when to outgrow this tier (promote/outgrow/upgrade/from tier N)"))
	}
	return vs, nil
}

// validateEnvImportComments — every runtime-service block in every tier
// has a comment; comment carries a causal word; no templated opening
// across runtime-service blocks within one tier.
func validateEnvImportComments(_ context.Context, path string, _ []byte, inputs SurfaceInputs) ([]Violation, error) {
	if inputs.Plan == nil {
		return nil, nil
	}
	tierKey := tierKeyFromPath(path)
	ec, ok := inputs.Plan.EnvComments[tierKey]
	if !ok {
		return []Violation{violation("env-comments-missing", path,
			fmt.Sprintf("tier %s has no EnvComments recorded", tierKey))}, nil
	}
	var vs []Violation
	for hostname, comment := range ec.Service {
		if !containsAnyCausal(comment) {
			vs = append(vs, violation("missing-causal-word", path,
				fmt.Sprintf("env %s / %s comment lacks a causal word: %q", tierKey, hostname, comment)))
		}
	}
	vs = append(vs, templatedOpeningCheck(path, ec, inputs.Plan)...)
	return vs, nil
}

// templatedOpeningCheck compares first-sentences across runtime
// codebases only (managed services like db/cache can share an opening
// legitimately).
func templatedOpeningCheck(path string, ec EnvComments, plan *Plan) []Violation {
	opens := map[string]int{}
	for _, cb := range plan.Codebases {
		comment, ok := ec.Service[cb.Hostname]
		if !ok || comment == "" {
			continue
		}
		first := firstSentence(comment)
		if first == "" {
			continue
		}
		opens[first]++
	}
	for opening, count := range opens {
		if count >= 2 {
			return []Violation{violation("templated-opening", path,
				fmt.Sprintf("%d runtime-service blocks share opening sentence %q — each must explain its own block", count, opening))}
		}
	}
	return nil
}

// firstSentence returns text up to the first `. `, `!`, `?`, or line
// break. Used by the templated-opening check.
func firstSentence(s string) string {
	s = strings.TrimSpace(s)
	for i, r := range s {
		if r == '.' || r == '!' || r == '?' || r == '\n' {
			return strings.TrimSpace(s[:i])
		}
	}
	return s
}

// tierKeyFromPath picks the tier index from a `<folder>/import.yaml`
// path by matching the folder against known tiers.
func tierKeyFromPath(p string) string {
	for _, t := range Tiers() {
		if strings.Contains(p, t.Folder) {
			return fmt.Sprintf("%d", t.Index)
		}
	}
	return ""
}
