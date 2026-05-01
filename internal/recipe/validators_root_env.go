package recipe

import (
	"context"
	"fmt"
	"regexp"
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
//
// Run-21 §A3 — multi-codebase recipes carry one framework per codebase
// (e.g. apidev=NestJS, appdev=Svelte, workerdev=NestJS). Plan.Framework
// is single-valued (the dominant label), so the check now accepts any
// framework also named in plan.Research.Description (which scaffold
// authors as a free-text catalog) as a per-codebase fallback. A future
// schema iteration may add Codebase.Framework for an explicit set.
func factualityCheck(path, body string, inputs SurfaceInputs) []Violation {
	if inputs.Plan == nil || inputs.Plan.Framework == "" {
		return nil
	}
	candidates := []string{
		"Laravel", "NestJS", "Svelte", "Django", "Rails",
		"Flask", "Next.js", "Remix", "Angular", "Vue",
	}
	declared := strings.ToLower(inputs.Plan.Framework)
	descLower := strings.ToLower(inputs.Plan.Research.Description)
	for _, c := range candidates {
		if !strings.Contains(body, c) {
			continue
		}
		clower := strings.ToLower(c)
		if clower == declared {
			continue
		}
		// Run-21 §A3 — graceful fallback for multi-codebase recipes.
		// If the framework is also named in the plan's research
		// description, the README mention is part of the recipe's
		// declared scope, not a fabrication.
		if descLower != "" && strings.Contains(descLower, clower) {
			continue
		}
		return []Violation{violation("factuality-mismatch", path,
			fmt.Sprintf("README names %q but plan.Framework = %q", c, inputs.Plan.Framework))}
	}
	return nil
}

// validateEnvREADME — Surface 2 contract per
// docs/spec-content-surfaces.md§surface-2--environment-readme. The
// recipe-page UI renders the content between
// `<!-- #ZEROPS_EXTRACT_START:intro# -->` markers as the tier-card
// description; the spec caps that extract at 1-2 sentences ≤ 350 chars.
//
// Run-15 F.4 — replaced the prior `env-readme-too-short` (≥ 40 lines)
// validator, which drove run-14's 35-line ladder content inside the
// extract markers (Shape at glance / Who fits / How iteration works /
// What you give up / When to outgrow / What changes at next tier).
// Both reference recipes (laravel-jetstream + laravel-showcase) leave
// the body empty; only the extract carries content. The cap below
// targets the extract specifically — body content remains optional.
func validateEnvREADME(_ context.Context, path string, body []byte, _ SurfaceInputs) ([]Violation, error) {
	var vs []Violation
	s := string(body)
	lines := strings.Count(s, "\n")
	if lines > 120 {
		vs = append(vs, violation("env-readme-too-long", path, fmt.Sprintf("%d lines > 120", lines)))
	}
	if !strings.Contains(s, "<!-- #ZEROPS_EXTRACT_START:intro# -->") {
		vs = append(vs, violation("env-readme-missing-intro-marker", path, "intro marker missing"))
	}
	// F.4 — extract char cap. Read the spec cap from the SurfaceContract
	// so the validator stays in sync with the spec value (350 chars).
	contract, _ := ContractFor(SurfaceEnvREADME)
	if contract.IntroExtractCharCap > 0 {
		extract := extractBetweenMarkers(s, "intro")
		if n := len(extract); n > contract.IntroExtractCharCap {
			vs = append(vs, violation("tier-readme-extract-too-long", path,
				fmt.Sprintf(
					"tier README intro extract is %d chars > %d cap (1-2 sentences, see spec §Surface 2). The recipe-page UI renders the extract as the tier-card description; ladder content (Shape at glance / Who fits / How iteration works) belongs in tier import.yaml comments, not inside the extract markers.",
					n, contract.IntroExtractCharCap,
				)))
		}
	}
	// Run-22 §N14 / run-23 fix-9 — strip canonical tier-0 audience labels
	// before the meta-voice scan. tier.Label is engine-emitted into the
	// tier README (`# {TIER_LABEL}`, "{TIER_LABEL} tier — ...") and the
	// meta-voice patrol's " agent " / "agent-" needles would otherwise
	// fire on the tier name itself. Strip both the legacy "AI Agent" and
	// the run-23 "Include Coding Agents" forms so back-compat content +
	// freshly-emitted content both pass cleanly.
	scanBody := strings.ReplaceAll(s, "AI Agent", "")
	scanBody = strings.ReplaceAll(scanBody, "Include Coding Agents", "")
	if containsAny(scanBody, metaVoiceWords) {
		vs = append(vs, notice("meta-agent-voice", path,
			"env README is porter-facing; contains meta-agent-voice words (agent, zerops_knowledge, sub-agent, scaffolder)"))
	}
	// Run-19 prep: the legacy `tier-promotion-verb-missing` notice was
	// removed because it forced "promote/outgrow/upgrade" verbs into
	// every env README extract and directly contradicted
	// docs/spec-content-surfaces.md §108 ("Tier-promotion narratives —
	// don't"). Tier shifts surface implicitly through the contrast
	// between tier yamls; this validator was the run-18 §1.4 axis the
	// spec resolves on the spec's side.
	return vs, nil
}

// validateEnvImportComments — every runtime-service block in every tier
// has a comment; comment carries a causal word; no templated opening
// across runtime-service blocks within one tier; tier-prose claims
// match the adjacent emitted yaml fields (run-13 §V).
func validateEnvImportComments(_ context.Context, path string, body []byte, inputs SurfaceInputs) ([]Violation, error) {
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
			vs = append(vs, notice("missing-causal-word", path,
				fmt.Sprintf("env %s / %s comment lacks a causal word: %q", tierKey, hostname, comment)))
		}
		if envYAMLCiteMetaRE.MatchString(comment) {
			vs = append(vs, notice("env-yaml-cite-meta", path,
				fmt.Sprintf("env %s / %s comment carries citation meta-talk (`(cite \\`x\\`)` / `(via the X guide)`): %q — citations are author-time signals, not env-yaml comment content", tierKey, hostname, comment)))
		}
	}
	if envYAMLCiteMetaRE.MatchString(ec.Project) {
		vs = append(vs, notice("env-yaml-cite-meta", path,
			fmt.Sprintf("env %s project comment carries citation meta-talk: %q", tierKey, ec.Project)))
	}
	vs = append(vs, templatedOpeningCheck(path, ec, inputs.Plan)...)
	vs = append(vs, validateTierProseVsEmit(path, body, inputs)...)
	// Run-15 F.5 — yaml-AST + audience-voice checks that need the on-
	// disk yaml body (not the per-fragment EnvComments map). Catches
	// fabricated field names (snake_case `project_env_vars` when the
	// schema uses camelCase `project.envVariables`) and authoring-voice
	// leaks ("recipe author", "during scaffold") inside comment lines.
	vs = append(vs, validateEnvYAMLImportCommentsExtra(path, body)...)
	return vs, nil
}

// envYAMLCiteMetaRE flags citation-meta phrasing inside env import.yaml
// comments. Run-10 produced lines like `# (cite \`init-commands\`
// via the nodejs@22 hello-world guide)` — meta-talk inside a yaml
// comment whose audience is the click-deploying porter. Citations are
// author-time signals, not render output (run-11 gap O-2).
var envYAMLCiteMetaRE = regexp.MustCompile("(?i)(\\(cite\\s+`|\\bcited guide:\\s*`)")

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
			return []Violation{notice("templated-opening", path,
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
