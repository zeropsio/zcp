package tools

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/zeropsio/zcp/internal/workflow"
)

// checkClaudeReadmeConsistency enforces that procedures in CLAUDE.md do
// not use code-level mechanisms the README's gotchas explicitly forbid
// for production. The rationale (v8.78 reform): CLAUDE.md is the
// ambient context an agent reads when operating the codebase; if it
// teaches a pattern the README warns against, the agent will propagate
// that pattern into prod-affecting changes. The dev-loop should be the
// prod-loop reduced to dev-scoped arguments — not a different path.
//
// Rule: for each "do not use X in production" claim in the README's
// knowledge-base fragment, scan the CLAUDE.md content for X. If X
// appears, the surrounding context must contain an explicit cross-
// reference acknowledging the restriction (a marker like "dev only",
// "see README gotcha", "warned against in prod"). If no marker is
// present, the gotcha and CLAUDE.md disagree and the check fails.
//
// hostname scopes the check name so multi-codebase recipes surface
// per-codebase failures.
func checkClaudeReadmeConsistency(readmeContent, claudeContent, hostname string) []workflow.StepCheck {
	if readmeContent == "" || claudeContent == "" {
		return nil
	}
	kb := extractFragmentContent(readmeContent, "knowledge-base")
	if kb == "" {
		return nil
	}
	forbidden := extractForbiddenIdentifiers(kb)
	if len(forbidden) == 0 {
		return nil
	}

	hasCrossRef := containsCrossReferenceMarker(claudeContent)

	var conflicts []string
	for _, ident := range forbidden {
		if !identifierUsedIn(claudeContent, ident) {
			continue
		}
		if hasCrossRef {
			// CLAUDE.md acknowledges the restriction at least once
			// somewhere — accept whole-doc cross-reference because
			// CLAUDE.md procedures often span sections and require
			// the same caveat repeatedly. Strict per-call-site cross-
			// reference is a future tightening if drift continues.
			continue
		}
		conflicts = append(conflicts, ident)
	}

	checkName := hostname + "_claude_readme_consistency"
	if len(conflicts) == 0 {
		return []workflow.StepCheck{{Name: checkName, Status: statusPass}}
	}
	sort.Strings(conflicts)
	return []workflow.StepCheck{{
		Name:   checkName,
		Status: statusFail,
		Detail: fmt.Sprintf(
			"%s CLAUDE.md uses identifier(s) the README's Gotchas explicitly forbid in production: %s. CLAUDE.md is the ambient context an agent reads when operating this codebase; if it teaches a pattern the README warns against, the agent will propagate it into prod-affecting changes. Either (a) replace the procedure with the production-equivalent path (e.g. real migrations instead of `synchronize`), or (b) add an explicit cross-reference at the call site or document level — a marker like 'dev only', 'see README gotcha', or 'warned against in production' — so a reader knows the restriction.",
			hostname, strings.Join(conflicts, ", "),
		),
	}}
}

// forbiddenPatternRe matches gotcha bullet stems/bodies that name an
// identifier as forbidden in production. Captures the identifier in
// group 1. Patterns:
//
//	"`X` must be off in production"
//	"`X` must not be used"
//	"never use `X`"
//	"do not use `X`"
//	"`X` is forbidden"
//	"`X`: never set"
//
// The list is kept narrow — false positives here block CLAUDE.md
// procedures that simply share the same identifier with no real
// conflict. Add patterns conservatively.
var forbiddenPatternRe = regexp.MustCompile(
	"(?i)`([^`\\s][^`]*)`\\s*(?:must\\s+be\\s+off|must\\s+not(?:\\s+be)?(?:\\s+used)?|is\\s+forbidden|: never)" +
		"|(?i)(?:never|do\\s+not)\\s+(?:use|run|call)\\s+`([^`\\s][^`]*)`",
)

// identifierBaseRe captures the leading identifier-shaped token in a
// backtick-quoted forbidden item. Strips assignment values, call-arg
// lists, dot-property chains. Examples:
//
//	`synchronize: true`  → synchronize
//	`eval()`             → eval
//	`process.env.X`      → process
//	`ds.synchronize`     → ds (then we also keep the trailing identifier)
var identifierBaseRe = regexp.MustCompile(`[A-Za-z_$][A-Za-z0-9_$]*`)

// extractForbiddenIdentifiers walks the knowledge-base fragment looking
// for "must be off" / "never use" / "do not use" patterns and returns
// the de-duplicated set of base identifiers from each capture. The
// raw capture (e.g. `synchronize: true`) is normalized to its first
// identifier token (`synchronize`) plus, when the capture chains via
// `.`, every identifier in the chain — so a CLAUDE.md procedure that
// calls `ds.synchronize()` matches against the README's `synchronize`
// forbidden identifier even though the literal text differs.
//
// Identifiers shorter than 4 characters are skipped — too generic for
// a meaningful conflict claim.
func extractForbiddenIdentifiers(kbContent string) []string {
	matches := forbiddenPatternRe.FindAllStringSubmatch(kbContent, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, m := range matches {
		// Either capture group 1 or 2 has the identifier depending on
		// which alternation branch matched.
		raw := strings.TrimSpace(m[1])
		if raw == "" && len(m) > 2 {
			raw = strings.TrimSpace(m[2])
		}
		if raw == "" {
			continue
		}
		// Extract every identifier-shaped token in the capture. For
		// `synchronize: true` we pick `synchronize` (and drop `true`
		// via the < 4 char filter? `true` is 4 chars — skip via
		// genericForbiddenTokens below).
		tokens := identifierBaseRe.FindAllString(raw, -1)
		for _, tok := range tokens {
			if len(tok) < 4 {
				continue
			}
			if genericForbiddenTokens[strings.ToLower(tok)] {
				continue
			}
			if seen[tok] {
				continue
			}
			seen[tok] = true
			out = append(out, tok)
		}
	}
	return out
}

// genericForbiddenTokens are language keywords / values that show up in
// backtick captures alongside the real identifier (`synchronize: true`
// → `synchronize` + `true`). Don't enforce on these — they're noise.
var genericForbiddenTokens = map[string]bool{
	"true": true, "false": true, "null": true, "none": true,
	"void": true, "this": true, "self": true, "type": true,
	"yes": true, "no": true,
}

// identifierUsedIn checks whether the identifier appears in the body
// outside of cross-reference contexts. Right now this is a simple
// substring match — the cross-reference exemption is whole-document,
// so per-call-site scoping is unnecessary. If the document mentions
// the identifier AND has a cross-reference marker somewhere, we
// accept the use as documented.
func identifierUsedIn(body, identifier string) bool {
	return strings.Contains(body, identifier)
}

// crossReferenceMarkers signal that the CLAUDE.md author has
// explicitly acknowledged the README restriction. Whole-document
// matching: a single marker anywhere in the file authorizes uses
// throughout. List intentionally inclusive of the natural ways the
// agent phrases this caveat.
var crossReferenceMarkers = []string{
	"dev only",
	"dev-only",
	"in dev",
	"development only",
	"see readme",
	"readme gotcha",
	"warned against",
	"warning in production",
	"forbidden in production",
	"do not use in production",
	"never in production",
	"shortcut for dev",
	"dev shortcut",
}

func containsCrossReferenceMarker(body string) bool {
	low := strings.ToLower(body)
	for _, m := range crossReferenceMarkers {
		if strings.Contains(low, m) {
			return true
		}
	}
	return false
}
