package recipe

import (
	"strings"
	"testing"
)

// briefs_friendly_auth_f6_test.go — Run-43 F6 substrate pin.
//
// User direction: add a refinement-1 derived rule that flags yaml +
// tier-import comments missing the friendly-authority adaptation
// pattern on porter-tunable values. LLM-judgment based; not regex.
//
// Spec anchor: docs/spec-content-surfaces.md §"Friendly-authority
// voice" — pattern is declarative statement + invitation to adapt +
// named porter-side trigger (custom domain swap, SMTP credentials
// switch, replica count bump, env secret rotation).
//
// The rule asks the LLM to judge whether porter-tunable comments
// carry an adaptation hint with a named porter-side trigger. FAIL
// when zero of the porter-tunable comments across the codebase
// carry the adaptation hint — bare-mechanism-everywhere is the
// failure mode.

// TestDerivedRules_FFRIENDLYAUTH_RulePresent — atom contains
// F-FRIENDLY-AUTH + judgment-not-regex framing + porter-tunable
// examples + spec cross-link + atom cross-link.
func TestDerivedRules_FFRIENDLYAUTH_RulePresent(t *testing.T) {
	t.Parallel()
	body, err := readAtom("briefs/refinement/derived_rules.md")
	if err != nil {
		t.Fatalf("read derived_rules.md: %v", err)
	}
	for _, want := range []string{
		"F-FRIENDLY-AUTH",
		// Judgment-not-regex framing.
		"friendly-authority voice",
		"Judge",
		"NOT a literal phrase",
		// Pattern shape from spec — three components.
		"declarative statement",
		"invitation to adapt",
		"porter-side trigger",
		// Porter-tunable trigger examples.
		"custom domain swap",
		"SMTP credentials",
		"replica count",
		// Example shapes (non-exhaustive).
		"Feel free to",
		"Swap to",
		"Replace with",
		// Failure mode: bare-mechanism-everywhere.
		"bare-mechanism-everywhere",
		// Per-comment audit; whole-codebase verdict.
		"whole-codebase verdict",
		// Spec cross-reference.
		"Friendly-authority voice",
		// Atom cross-reference.
		"golden_voice_principles.md",
		"Friendly-authority adaptation pattern",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("derived_rules.md missing F-FRIENDLY-AUTH anchor %q", want)
		}
	}
}

// TestDerivedRules_FFRIENDLYAUTH_RuleSurfacesInBrief — refinement
// brief composer threads the rule into the brief body so the
// sub-agent sees it during rule-walk.
func TestDerivedRules_FFRIENDLYAUTH_RuleSurfacesInBrief(t *testing.T) {
	t.Parallel()
	plan := &Plan{Slug: "x", Codebases: []Codebase{{Hostname: "api"}}}
	brief, err := BuildRefinementBrief(plan, nil, "/run", nil)
	if err != nil {
		t.Fatalf("BuildRefinementBrief: %v", err)
	}
	for _, want := range []string{
		"F-FRIENDLY-AUTH",
		"friendly-authority voice",
	} {
		if !strings.Contains(brief.Body, want) {
			t.Errorf("refinement brief missing F-FRIENDLY-AUTH surface anchor %q", want)
		}
	}
}
