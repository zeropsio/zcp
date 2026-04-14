package workflow

import (
	"regexp"
	"strings"

	"github.com/zeropsio/zcp/internal/knowledge"
)

// ExtractGotchaStems pulls the bolded stem of each "- **X** — ..." bullet
// from inside a Gotchas section in a knowledge-base markdown fragment.
//
// Both `### Gotchas` and `## Gotchas` headings are recognized. The section
// walk tracks the starting heading level and terminates only at a heading
// at the same-or-higher level — nested subheadings (e.g. `#### Rationale`
// inside `### Gotchas`, or `### Subcategory` inside `## Gotchas`) stay
// within the section. Bullets may start with "- " or "* ". Stems that are
// not wrapped in `**bold**` are skipped — the bolded prefix is the part
// we compare for clone detection.
//
// The returned stems are the raw bolded text with backticks and trailing
// punctuation preserved; clone matching normalizes them downstream.
func ExtractGotchaStems(content string) []string {
	var stems []string
	lines := strings.Split(content, "\n")

	sectionLevel := 0 // 0 = not yet inside a Gotchas section
	for _, line := range lines {
		level := markdownHeadingLevel(line)
		if sectionLevel == 0 {
			// Looking for a Gotchas heading at any level.
			if level > 0 && strings.TrimSpace(strings.TrimLeft(line, "# ")) == "Gotchas" {
				sectionLevel = level
			}
			continue
		}
		// Inside the section — terminate on any heading at the same or
		// higher level (lower number). Deeper nested headings pass through.
		if level > 0 && level <= sectionLevel {
			break
		}
		// Parse bullet with bolded stem.
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "- ") && !strings.HasPrefix(trimmed, "* ") {
			continue
		}
		rest := strings.TrimSpace(trimmed[2:])
		if !strings.HasPrefix(rest, "**") {
			continue
		}
		// Find closing ** to extract the stem.
		inner := rest[2:]
		closeIdx := strings.Index(inner, "**")
		if closeIdx <= 0 {
			continue
		}
		stem := strings.TrimSpace(inner[:closeIdx])
		if stem == "" {
			continue
		}
		stems = append(stems, stem)
	}
	return stems
}

// stemStopwords are low-signal English tokens that don't carry the identity
// of a gotcha. Stripping them before comparison prevents false non-matches
// when the agent rewords a clone by swapping "needs" for "requires" or
// adding auxiliary verbs like "must never".
var stemStopwords = map[string]bool{
	"a": true, "an": true, "and": true, "are": true, "as": true, "at": true,
	"be": true, "by": true, "for": true, "from": true, "has": true, "have": true,
	"in": true, "is": true, "it": true, "its": true, "of": true, "on": true,
	"or": true, "that": true, "the": true, "this": true, "to": true, "was": true,
	"will": true, "with": true, "must": true, "should": true, "can": true,
	"default": true, "true": true, "false": true, "never": true, "always": true,
	"ever": true, "requires": true, "require": true, "needs": true, "need": true,
	"uses": true, "use": true, "via": true, "not": true, "do": true, "does": true,
}

var stemNonAlnum = regexp.MustCompile(`[^a-z0-9 ]+`)

// markdownHeadingLevel returns the heading level (1 for `#`, 2 for `##`,
// etc.) for a markdown heading line, or 0 when the line is not a heading.
// A heading is exactly N `#` followed by a space — lines like `#include`
// (code) or `####nospace` (malformed) are not headings.
func markdownHeadingLevel(line string) int {
	level := 0
	for level < len(line) && line[level] == '#' {
		level++
	}
	if level == 0 || level >= len(line) || line[level] != ' ' {
		return 0
	}
	return level
}

// normalizeStem lowercases, strips punctuation and backticks, removes
// stopwords, and returns the remaining content tokens in order.
// The order is preserved only for stable test output; stemsMatch treats
// the result as an unordered set.
//
// Known limitation: camelCase identifiers are collapsed into a single
// token (`objectStorageSize` → `objectstoragesize`). A predecessor using
// the camelCase form and a showcase spelling the same concept as
// "object storage size" would not clone-match because the latter yields
// three tokens that never overlap the single-token former. No recipe in
// the store has hit this shape in practice; fix is a Unicode-aware
// camelCase splitter if it shows up.
func normalizeStem(s string) []string {
	s = strings.ToLower(s)
	s = stemNonAlnum.ReplaceAllString(s, " ")
	fields := strings.Fields(s)
	out := make([]string, 0, len(fields))
	for _, tok := range fields {
		if len(tok) < 2 || stemStopwords[tok] {
			continue
		}
		out = append(out, tok)
	}
	return out
}

// NormalizeStem exposes normalizeStem to callers outside this package. The
// cross-README deduplication and gotcha-restates-guide checks in the tools
// package need to normalize arbitrary strings (integration-guide headings,
// cross-codebase stems) with the same tokeniser that drives the predecessor-
// floor check, so the match surfaces stay consistent.
func NormalizeStem(s string) []string {
	return normalizeStem(s)
}

// StemsMatch exposes stemsMatch to the tools package for the cross-README
// and gotcha-restates-guide checks. Same semantics as the internal matcher:
// token-set intersection ≥ floor(min * 0.67), hard minimum 2 tokens.
func StemsMatch(a, b []string) bool {
	return stemsMatch(a, b)
}

// stemsMatch returns true when two normalized stems represent the same
// gotcha topic. The rule: the shorter side's key-token set must share at
// least floor(min(|A|,|B|) * 0.67) tokens with the longer side, with a
// hard minimum of 2. This catches lightly-reworded clones while letting
// genuinely different gotchas coexist even when they touch related topics.
//
// Single-token stems (e.g. "zerops" alone) never match, because clone
// detection needs at least two key concepts to establish identity.
func stemsMatch(a, b []string) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	setA := make(map[string]bool, len(a))
	for _, t := range a {
		setA[t] = true
	}
	intersection := 0
	for _, t := range b {
		if setA[t] {
			intersection++
			delete(setA, t) // avoid counting duplicate tokens in b
		}
	}
	minLen := min(len(a), len(b))
	threshold := max(minLen*67/100, 2)
	return intersection >= threshold
}

// PredecessorGotchaStems returns the bolded stem list from the direct
// predecessor recipe's ## Gotchas section. Uses findDirectPredecessor so
// the floor check resolves the same content that recipeKnowledgeChain
// injects at generate time — discipline-by-code, not comment.
// Returns nil when no direct predecessor exists or its Gotchas section
// is absent.
func PredecessorGotchaStems(plan *RecipePlan, kp knowledge.Provider) []string {
	_, content, ok := findDirectPredecessor(plan, kp)
	if !ok {
		return nil
	}
	gotchas := extractH2Section(content, "Gotchas")
	if gotchas == "" {
		return nil
	}
	return ExtractGotchaStems("## Gotchas\n" + gotchas)
}

// CountNetNewGotchas returns the number of emitted gotcha stems that do
// NOT match any predecessor stem. This is the forcing function for the
// predecessor-floor check: recipes that merely clone the injected chain
// recipe's Gotchas section return 0; recipes that add genuinely new
// content return the count of additions.
//
// Both inputs are raw stem strings (as returned by ExtractGotchaStems);
// this function normalizes them internally.
func CountNetNewGotchas(emitted, predecessor []string) int {
	if len(emitted) == 0 {
		return 0
	}
	predNorms := make([][]string, 0, len(predecessor))
	for _, p := range predecessor {
		if n := normalizeStem(p); len(n) > 0 {
			predNorms = append(predNorms, n)
		}
	}
	netNew := 0
	for _, e := range emitted {
		norm := normalizeStem(e)
		if len(norm) == 0 {
			continue
		}
		cloned := false
		for _, p := range predNorms {
			if stemsMatch(norm, p) {
				cloned = true
				break
			}
		}
		if !cloned {
			netNew++
		}
	}
	return netNew
}
