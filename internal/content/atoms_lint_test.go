package content

import (
	"sort"
	"testing"
)

// TestAtomAuthoringLint proves the atom corpus is free of authoring-
// contract violations: no spec-ID citations, no handler-behavior verbs,
// no invisible-state field names, no plan-doc cross-references. Part
// of the atom authoring contract — atoms describe observable response/
// envelope state, not developer taxonomy or handler internals.
//
// Failure messages are grouped by atom file and name the category,
// pattern, 1-indexed line number, and matching snippet. An
// intentional exception can be added to atomLintAllowlist with a
// documented rationale.
func TestAtomAuthoringLint(t *testing.T) {
	t.Parallel()

	violations, err := LintAtomCorpus()
	if err != nil {
		t.Fatalf("LintAtomCorpus: %v", err)
	}
	if len(violations) == 0 {
		return
	}

	sort.Slice(violations, func(i, j int) bool {
		if violations[i].AtomFile != violations[j].AtomFile {
			return violations[i].AtomFile < violations[j].AtomFile
		}
		return violations[i].Line < violations[j].Line
	})
	for _, v := range violations {
		t.Errorf(
			"authoring-contract violation — %s:%d [%s / %s]\n\t%s",
			v.AtomFile, v.Line, v.Category, v.Pattern, v.Snippet,
		)
	}
}

// TestAtomAuthoringLint_FiresOnKnownViolations is the lint engine's
// self-test: the regression-floor `TestAtomAuthoringLint` only proves
// the production corpus is clean, which silently passes if any of the
// six rules in atomLintRules has a typo / wrong char-class / wrong
// anchor (broken regex matches nothing → zero violations → green).
//
// This test constructs one synthetic atom per rule that should trip
// exactly that rule. If any rule stops firing, this test catches it
// before the broken pattern ships.
//
// Pattern reference (atoms_lint.go::atomLintRules):
//   - spec-id:                 DM-/DS-/GLC-/KD-/TA-/E#/O#/F#/INV- citations
//   - handler-behavior-handler: handler verbs (writes/stamps/activates/etc.)
//   - handler-behavior-tool-auto: tool + auto-* / automatically
//   - handler-behavior-zcp:     bare "ZCP writes/stamps/..."
//   - invisible-state:          FirstDeployedAt / BootstrapSession / StrategyConfirmed
//   - plan-doc:                 plans/<slug>.md cross-refs
func TestAtomAuthoringLint_FiresOnKnownViolations(t *testing.T) {
	t.Parallel()

	const fmHeader = "---\nphase: idle\n---\n"

	tests := []struct {
		name        string
		body        string
		wantPattern string
		wantCat     string
	}{
		{
			name:        "spec-id-DM",
			body:        "The deploy mode invariant DM-2 means source IS target.\n",
			wantPattern: "spec-id",
			wantCat:     "spec-id",
		},
		{
			name:        "spec-id-INV",
			body:        "See INV-42 for the load-bearing assertion.\n",
			wantPattern: "spec-id",
			wantCat:     "spec-id",
		},
		{
			name:        "handler-behavior-handler",
			body:        "The deploy handler automatically enables the subdomain.\n",
			wantPattern: "handler-behavior-handler",
			wantCat:     "handler-behavior",
		},
		{
			name:        "handler-behavior-tool-auto",
			body:        "The tool will auto-enable the route on first call.\n",
			wantPattern: "handler-behavior-tool-auto",
			wantCat:     "handler-behavior",
		},
		{
			name:        "handler-behavior-zcp",
			body:        "ZCP writes the meta file on success.\n",
			wantPattern: "handler-behavior-zcp",
			wantCat:     "handler-behavior",
		},
		{
			name:        "invisible-state",
			body:        "FirstDeployedAt is stamped after the first deploy.\n",
			wantPattern: "invisible-state",
			wantCat:     "invisible-state",
		},
		{
			name:        "plan-doc",
			body:        "See plans/instruction-delivery-rewrite.md for context.\n",
			wantPattern: "plan-doc",
			wantCat:     "plan-doc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			atom := AtomFile{
				Name:    tt.name + "-fixture.md",
				Content: fmHeader + tt.body,
			}
			violations := lintAtomCorpus([]AtomFile{atom})
			if len(violations) == 0 {
				t.Fatalf("expected rule %q to fire on fixture %q, got zero violations",
					tt.wantPattern, tt.body)
			}
			var sawTarget bool
			for _, v := range violations {
				if v.Pattern == tt.wantPattern && v.Category == tt.wantCat {
					sawTarget = true
					break
				}
			}
			if !sawTarget {
				t.Errorf("rule %q did not fire on fixture %q; got violations: %+v",
					tt.wantPattern, tt.body, violations)
			}
		})
	}
}

// TestAtomAuthoringLint_CleanFixtureYieldsZero proves a fixture that
// intentionally avoids every forbidden pattern produces zero
// violations. Counterpoint to FiresOnKnownViolations: it asserts the
// engine does NOT spuriously match clean prose.
func TestAtomAuthoringLint_CleanFixtureYieldsZero(t *testing.T) {
	t.Parallel()

	atom := AtomFile{
		Name: "clean-fixture.md",
		Content: "---\nphase: idle\n---\n" +
			"The agent observes the project state and chooses the next action.\n" +
			"Service status transitions from PENDING to RUNNING after the first deploy.\n" +
			"Use zerops_workflow action=start to begin a develop session.\n",
	}
	violations := lintAtomCorpus([]AtomFile{atom})
	if len(violations) != 0 {
		t.Errorf("clean fixture produced %d violations: %+v", len(violations), violations)
	}
}

// TestAtomAuthoringLint_MultipleRulesOnOneAtom proves one atom can
// trip multiple rules independently — the rule engine doesn't short-
// circuit after the first match. Two rules on one line, plus a third
// on a separate line.
func TestAtomAuthoringLint_MultipleRulesOnOneAtom(t *testing.T) {
	t.Parallel()

	atom := AtomFile{
		Name: "multi-rule-fixture.md",
		Content: "---\nphase: idle\n---\n" +
			// One line, two rules: spec-id (DM-2) + handler-behavior-handler.
			"The handler automatically applies DM-2.\n" +
			// Separate line: plan-doc cross-ref.
			"See plans/test.md.\n",
	}
	violations := lintAtomCorpus([]AtomFile{atom})

	wantPatterns := map[string]bool{
		"spec-id":                  false,
		"handler-behavior-handler": false,
		"plan-doc":                 false,
	}
	for _, v := range violations {
		if _, ok := wantPatterns[v.Pattern]; ok {
			wantPatterns[v.Pattern] = true
		}
	}
	for pat, fired := range wantPatterns {
		if !fired {
			t.Errorf("expected rule %q to fire on multi-rule fixture; violations: %+v",
				pat, violations)
		}
	}
}
