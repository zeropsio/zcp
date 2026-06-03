package workflow

// Tests for the `reference: true` pointer-render path (P0c round 2
// increment 2). A reference atom is rendered by Synthesize as a one-line
// on-demand-fetch stub instead of its body; the full body stays in the
// corpus and resolves via zerops_workflow action="develop-atom".
//
// Two invariants are load-bearing and pinned here:
//   1. No dead pointer — every Reference atomId resolves via LookupAtomBody
//      (the masking-fallback failure mode the single-owner design forbids).
//   2. Substitution safety — the develop-atom fetch returns the RAW body
//      with NO live envelope, so a Reference atom MUST NOT carry an
//      envelope-substitution placeholder ({hostname}/{stage-hostname}/
//      {project-name}); those would leak verbatim to the agent. Agent-filled
//      survivors ({port} etc.) are fine — identical in inline and fetched
//      paths. This is the structural guard that keeps the "flip an atom to
//      reference" edit honest: flipping an atom with {hostname} fails here.

import (
	"strings"
	"testing"
)

// envelopeSubstitutionPlaceholders are the tokens Synthesize resolves from
// the live envelope. A reference atom carrying any of these would leak the
// raw token through the placeholder-free develop-atom fetch.
var envelopeSubstitutionPlaceholders = []string{"{hostname}", "{stage-hostname}", "{project-name}"}

func TestReferenceAtoms_PointersResolve(t *testing.T) {
	t.Parallel()
	corpus, err := LoadAtomCorpus()
	if err != nil {
		t.Fatalf("LoadAtomCorpus: %v", err)
	}

	var refCount int
	for _, atom := range corpus {
		if !atom.Reference {
			continue
		}
		refCount++

		// 1. No dead pointer: the stub points at this id; the fetch must
		//    resolve it from the same corpus.
		if body := LookupAtomBody(corpus, atom.ID); body == "" {
			t.Errorf("reference atom %q does not resolve via LookupAtomBody — the pointer-render stub would be a dead pointer", atom.ID)
		}

		// 2. The stub must name its own id + title so the agent can fetch it
		//    and recognize the topic.
		stub := referenceStub(atom)
		if !strings.Contains(stub, atom.ID) {
			t.Errorf("reference atom %q: stub does not contain its own id: %q", atom.ID, stub)
		}
		if atom.Title == "" {
			t.Errorf("reference atom %q has no title — the stub is the agent's only signal to decide whether to fetch", atom.ID)
		} else if !strings.Contains(stub, atom.Title) {
			t.Errorf("reference atom %q: stub does not contain its title %q: %q", atom.ID, atom.Title, stub)
		}

		// 3. Substitution safety: raw body must be free of envelope-
		//    substitution placeholders (the fetch returns it without a
		//    live envelope to substitute them).
		for _, ph := range envelopeSubstitutionPlaceholders {
			if strings.Contains(atom.Body, ph) {
				t.Errorf("reference atom %q body contains envelope-substitution placeholder %q — it cannot be pointer-rendered (the develop-atom fetch returns the raw body with no envelope; the token would leak). Either drop `reference: true` or remove the placeholder.", atom.ID, ph)
			}
		}
	}

	if refCount == 0 {
		t.Fatal("no reference atoms in corpus — the pointer-render path is untested; if all reference atoms were intentionally removed, delete this test")
	}
}
