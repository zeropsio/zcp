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
