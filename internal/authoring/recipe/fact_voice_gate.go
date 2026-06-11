package recipe

import (
	"fmt"
	"strings"
)

// Run-34 Fix 3 — engine-side runtime gate for forbidden recipe-author
// voice tokens at record-fact time.
//
// Brief edits in scaffold/decision_recording_slim.md and
// feature/decision_recording.md taught the rule but agents ignored at
// runtime: facts.jsonl carried 12.8% strict-token contamination
// unchanged from run-33 (15/117 records carry "zerops_dev_server" in
// `why`; 2 carry "the agent"). Brief teaches REQUIRED → engine MUST
// enforce REQUIRED. record-fact REFUSES the record (blocking error,
// not a notice) when any forbidden token appears in `why`,
// `mechanism`, `fixApplied`, `surfaceHint`, or `evidence`.
//
// Diagnosed in plans/run-34-validation.md §"Untested-in-sim items".

// forbiddenFactVoiceTokens lists the recipe-author / authoring-process
// voice tokens that MUST NOT appear in a fact's load-bearing text
// fields. Recipe-author voice in fact text propagates to porter prose
// at synthesis; the porter sees commands and processes that don't
// exist on their side ("zerops_dev_server is owned by..." reads as
// "your machine has a zerops_dev_server" when the porter ports the
// codebase to their own runtime). Same single-source list shared
// between the validator and tests so the rule has one canonical
// definition.
//
// Tokens are matched case-insensitively as substrings — overlapping
// occurrences in different fields all surface, and superset spellings
// (e.g. "the agent owns") catch the leaf rule.
var forbiddenFactVoiceTokens = []string{
	"the agent",
	"during scaffold",
	"during feature",
	"we chose",
	"we use",
	"we set",
	"recipe author",
	"scaffold sub-agent",
	"feature sub-agent",
	"record-fact",
	"zerops_dev_server",
}

// factVoiceFields enumerates the fact-record text fields the gate
// scans. Each entry pairs a (struct field name, value) so error
// messages point at the offending field by name. `topic` is recipe-
// internal taxonomy and not subject to the porter-voice rule —
// only the prose text fields are.
type factVoiceField struct {
	name  string
	value string
}

func factVoiceFieldsFor(f FactRecord) []factVoiceField {
	return []factVoiceField{
		{name: "why", value: f.Why},
		{name: "mechanism", value: f.Mechanism},
		{name: "fixApplied", value: f.FixApplied},
		{name: "surfaceHint", value: f.SurfaceHint},
		{name: "evidence", value: f.Evidence},
	}
}

// validateFactVoiceTokens returns an error naming the first
// forbidden-token-and-field pair it finds, or nil when the fact
// passes. Walks fields + tokens in deterministic order so the same
// contaminated fact always surfaces the same first violation
// (callers can re-scan post-fix; callers in test mode can rely on
// stable error text).
//
// Rejection message names the offending token + field + a pointer at
// the brief atom that teaches the rule, so the agent reads the
// guidance verbatim at the rejection site rather than burning
// tool-call hops to find it.
func validateFactVoiceTokens(f FactRecord) error {
	for _, field := range factVoiceFieldsFor(f) {
		if field.value == "" {
			continue
		}
		lower := strings.ToLower(field.value)
		for _, token := range forbiddenFactVoiceTokens {
			if strings.Contains(lower, strings.ToLower(token)) {
				return fmt.Errorf(
					"record-fact refused: forbidden recipe-author voice token %q in fact field %q — "+
						"recipe-author voice in fact text propagates to porter prose at synthesis; the porter sees commands + processes that don't exist on their side; "+
						"rewrite the field describing the porter's runtime, not the recipe-construction process; "+
						"see briefs/scaffold/decision_recording_slim.md §\"Forbidden tokens in fact text fields\" (scaffold pass) "+
						"or briefs/feature/decision_recording.md §\"Forbidden tokens in fact text fields\" (feature pass) for the worked example + the full token list",
					token, field.name,
				)
			}
		}
	}
	return nil
}
