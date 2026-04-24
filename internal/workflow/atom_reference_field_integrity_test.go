package workflow

import (
	"sort"
	"testing"
)

// TestAtomReferenceFieldIntegrity proves every `references-fields`
// frontmatter entry in the atom corpus resolves to a real Go struct
// field. Part of the atom authoring contract: atoms describe observable
// response/envelope state, and the authoring contract pins each cited
// field to the actual struct via AST scan.
//
// Failure message names the atom, the unresolved reference, and suggests
// the corrective action (fix the atom or update the struct).
//
// Scope: `internal/{ops,tools,platform,workflow}/*.go`. Embedded fields
// are excluded by design (see loadAtomReferenceFieldIndex); atoms that
// need an embedded field must either target the outer named struct or
// prompt a refactor that makes the field explicit.
func TestAtomReferenceFieldIntegrity(t *testing.T) {
	t.Parallel()

	index, err := loadAtomReferenceFieldIndex(atomReferenceFieldRoots)
	if err != nil {
		t.Fatalf("loadAtomReferenceFieldIndex: %v", err)
	}

	corpus, err := LoadAtomCorpus()
	if err != nil {
		t.Fatalf("LoadAtomCorpus: %v", err)
	}

	type miss struct {
		atomID string
		ref    string
	}
	var misses []miss
	for _, atom := range corpus {
		for _, ref := range atom.ReferencesFields {
			if !index[ref] {
				misses = append(misses, miss{atom.ID, ref})
			}
		}
	}
	if len(misses) == 0 {
		return
	}
	sort.Slice(misses, func(i, j int) bool {
		if misses[i].atomID != misses[j].atomID {
			return misses[i].atomID < misses[j].atomID
		}
		return misses[i].ref < misses[j].ref
	})
	for _, m := range misses {
		t.Errorf("atom %q references-fields entry %q does not resolve to a named struct field in internal/{ops,tools,platform,workflow} — fix the atom or add the field",
			m.atomID, m.ref)
	}
}
