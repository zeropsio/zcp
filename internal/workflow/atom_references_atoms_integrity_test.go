package workflow

import (
	"sort"
	"testing"
)

// TestAtomReferencesAtomsIntegrity proves every `references-atoms`
// frontmatter entry in the atom corpus points at an atom that exists.
// Prevents silent breakage when an atom gets renamed or deleted.
//
// Failure message names the atom, the missing target, and suggests
// the corrective action (rename the reference or restore the atom).
func TestAtomReferencesAtomsIntegrity(t *testing.T) {
	t.Parallel()

	corpus, err := LoadAtomCorpus()
	if err != nil {
		t.Fatalf("LoadAtomCorpus: %v", err)
	}

	exists := make(map[string]bool, len(corpus))
	for _, atom := range corpus {
		exists[atom.ID] = true
	}

	type miss struct {
		atomID string
		ref    string
	}
	var misses []miss
	for _, atom := range corpus {
		for _, ref := range atom.ReferencesAtoms {
			if !exists[ref] {
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
		t.Errorf("atom %q references-atoms entry %q does not resolve to an existing atom — fix the reference or restore the atom",
			m.atomID, m.ref)
	}
}
