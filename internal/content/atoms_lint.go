package content

import (
	"fmt"
	"regexp"
	"strings"
)

// AtomLintViolation describes one authoring-contract violation in an atom
// body. The atom's filename is included to speed up editor navigation.
type AtomLintViolation struct {
	AtomFile string // e.g. "bootstrap-close.md"
	Category string // "spec-id" | "handler-behavior" | "invisible-state" | "plan-doc"
	Pattern  string // the regex name that matched
	Line     int    // 1-indexed line in the atom file (including frontmatter)
	Snippet  string // the matching line, trimmed
}

// atomLintAllowlist keys are "<atomFile>::<exact-line-trimmed>" pairs.
// Allowlist entries require a short rationale committed alongside the
// entry — keep the set empty by default; every entry is an audit target.
var atomLintAllowlist = map[string]string{
	// Empty on purpose. Add entries in the form:
	//   "bootstrap-close.md::some specific line prose" : "rationale why this is not a violation",
}

type atomLintRule struct {
	name     string
	category string
	pattern  *regexp.Regexp
}

var atomLintRules = []atomLintRule{
	{
		name:     "spec-id",
		category: "spec-id",
		pattern:  regexp.MustCompile(`\bDM-[0-9]|\bDS-0[1-4]|\bGLC-[1-6]|\bKD-[0-9]{2}|\bTA-[0-9]{2}|\bE[1-8]\b|\bO[1-4]\b|\bF#[1-9]|\bINV-[0-9]+`),
	},
	{
		name:     "handler-behavior-handler",
		category: "handler-behavior",
		pattern:  regexp.MustCompile(`(?i)\bhandler\b[^\n]{0,80}\b(automatically|auto-\w+|writes|stamps|activates|enables|disables)\b`),
	},
	{
		name:     "handler-behavior-tool-auto",
		category: "handler-behavior",
		pattern:  regexp.MustCompile(`(?i)\btool\b[^\n]{0,40}\b(auto-\w+|automatically)\b`),
	},
	{
		name:     "handler-behavior-zcp",
		category: "handler-behavior",
		pattern:  regexp.MustCompile(`\bZCP\s+(writes|stamps|activates|enables|disables)\b`),
	},
	{
		name:     "invisible-state",
		category: "invisible-state",
		pattern:  regexp.MustCompile(`\bFirstDeployedAt\b|\bBootstrapSession\b|\bStrategyConfirmed\b`),
	},
	{
		name:     "plan-doc",
		category: "plan-doc",
		pattern:  regexp.MustCompile(`\bplans/[a-z][a-z0-9-]+\.md\b`),
	},
}

// LintAtomCorpus scans every atom body (frontmatter excluded) for the
// authoring-contract violations defined in atomLintRules. The returned
// slice is empty when the corpus is clean. Allowlist entries suppress
// specific matches with a documented rationale.
//
// Called by TestAtomAuthoringLint (Phase 3). Kept as an exported function
// so a future `zcp lint atoms` CLI or CI gate could call it directly.
func LintAtomCorpus() ([]AtomLintViolation, error) {
	atoms, err := ReadAllAtoms()
	if err != nil {
		return nil, fmt.Errorf("read atoms: %w", err)
	}
	return lintAtomCorpus(atoms), nil
}

// lintAtomCorpus runs the rule engine over an arbitrary atom slice.
// Unexported on purpose — production code goes through LintAtomCorpus
// (which sources atoms from the embedded corpus). The helper exists so
// fires-on-fixture tests can pass synthetic atoms in directly without
// monkeying with ReadAllAtoms.
func lintAtomCorpus(atoms []AtomFile) []AtomLintViolation {
	var out []AtomLintViolation
	for _, atom := range atoms {
		body, frontmatterLines := splitAtomBody(atom.Content)
		lines := strings.Split(body, "\n")
		for i, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				continue
			}
			for _, rule := range atomLintRules {
				if !rule.pattern.MatchString(line) {
					continue
				}
				key := atom.Name + "::" + trimmed
				if _, allowed := atomLintAllowlist[key]; allowed {
					continue
				}
				out = append(out, AtomLintViolation{
					AtomFile: atom.Name,
					Category: rule.category,
					Pattern:  rule.name,
					Line:     frontmatterLines + i + 1,
					Snippet:  trimmed,
				})
			}
		}
	}
	return out
}

// splitAtomBody returns the atom body (after the closing frontmatter
// delimiter) plus the count of frontmatter lines — used to report
// 1-indexed line numbers that match a text editor opening the file.
func splitAtomBody(content string) (string, int) {
	if !strings.HasPrefix(content, "---\n") {
		return content, 0
	}
	rest := content[4:]
	frontmatter, body, ok := strings.Cut(rest, "\n---\n")
	if !ok {
		return content, 0
	}
	// +1 for opening `---`, +1 for closing `---`, + N frontmatter-key lines
	return body, 2 + strings.Count(frontmatter, "\n") + 1
}
