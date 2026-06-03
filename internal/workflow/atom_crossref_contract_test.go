package workflow

import (
	"slices"
	"strings"
	"testing"
)

// TestAtomCrossRefContract pins the two-tier cross-reference contract (P0c
// cross-ref-integrity). Two kinds of inter-atom edge, by intent + by target tier:
//
//   - references-atoms = CONTENT dependency. The source body relies on the
//     target's body being present in the SAME rendered payload (shared
//     definition, consolidated topic). A content dependency MUST target an
//     INLINE atom (Reference==false). Depending on a pointer-rendered
//     (reference:true) body is incoherent: the body isn't there — Synthesize
//     emits only a one-line stub, and the develop-atom fetch returns ONE raw
//     body, not a transitive bundle. This is the bug class P0c round-2 closed:
//     deferring an atom that inline spine atoms had a content-dependency on.
//
//   - pointer-atoms = on-demand DEPTH pointer. The source body does not need
//     the target's content to be actionable; the agent fetches it via
//     develop-atom only for extra detail. A pointer MUST target a deferred
//     (reference:true) atom. For an INLINE source, the pointer must be
//     RESOLVABLE: the target co-renders under the source's axes (its stub
//     appears in the same payload), or the source body carries the explicit
//     develop-atom fetch command.
//
// This is the corpus analogue of the architecture layer rule
// (topology/architecture_test.go): a structural dependency direction pinned
// by a test so it cannot silently regress. Existence of every target is
// pinned separately by TestAtomReferencesAtomsIntegrity.
func TestAtomCrossRefContract(t *testing.T) {
	t.Parallel()

	corpus, err := LoadAtomCorpus()
	if err != nil {
		t.Fatalf("LoadAtomCorpus: %v", err)
	}

	byID := make(map[string]KnowledgeAtom, len(corpus))
	for _, a := range corpus {
		byID[a.ID] = a
	}

	for _, atom := range corpus {
		// Rule A — references-atoms (content dependency) must target an
		// INLINE atom. (Existence is pinned by the integrity test; here we
		// only judge tier for targets that resolve.)
		for _, ref := range atom.ReferencesAtoms {
			tgt, ok := byID[ref]
			if !ok {
				continue // missing target: TestAtomReferencesAtomsIntegrity owns this
			}
			if tgt.Reference {
				t.Errorf("atom %q has a references-atoms (content-dependency) edge to %q, but %q is reference:true (pointer-rendered — its body is NOT in the payload). A content dependency cannot point at a deferred body. Either inline the load-bearing fact into %q and drop the edge, or — if %q only needs %q for on-demand depth — move it to pointer-atoms.",
					atom.ID, ref, ref, atom.ID, atom.ID, ref)
			}
		}

		// Rule B — pointer-atoms (depth pointer) must target a DEFERRED atom.
		for _, ptr := range atom.PointerAtoms {
			tgt, ok := byID[ptr]
			if !ok {
				t.Errorf("atom %q pointer-atoms entry %q does not resolve to an existing atom — fix the reference or restore the atom",
					atom.ID, ptr)
				continue
			}
			if !tgt.Reference {
				t.Errorf("atom %q has a pointer-atoms (on-demand depth) edge to %q, but %q is NOT reference:true (it renders inline). A pointer points at deferred depth; an inline target should be a references-atoms content dependency (or no edge).",
					atom.ID, ptr, ptr)
				continue
			}
			// Rule C — for an INLINE source, the pointer must be resolvable:
			// the target co-renders (stub in the same payload), or the source
			// body carries the explicit fetch command.
			if atom.Reference {
				continue // reference→reference pointer: both deferred, no co-render contract
			}
			if pointerResolvable(atom, tgt) {
				continue
			}
			t.Errorf("atom %q pointer-atoms edge to %q is not resolvable: %q does not co-render under %q's axes (different phase or non-overlapping envelopeDeployStates) AND %q's body lacks the explicit fetch command `atomId=%q`. Add the fetch command to the body, or align the axes so the stub co-renders.",
				atom.ID, ptr, ptr, atom.ID, atom.ID, ptr)
		}
	}
}

// pointerResolvable reports whether an inline source's pointer to a deferred
// target is reachable by the agent: either the target's stub co-renders under
// the source's axes (so the agent sees it in the same payload), or the source
// body carries the explicit `atomId="<target>"` fetch command.
func pointerResolvable(src, tgt KnowledgeAtom) bool {
	if containsFetchCommand(src.Body, tgt.ID) {
		return true
	}
	return atomsCoRender(src.Axes, tgt.Axes)
}

// containsFetchCommand reports whether body carries an explicit develop-atom
// fetch for the given atom id (`atomId="<id>"`).
func containsFetchCommand(body, id string) bool {
	return strings.Contains(body, `atomId="`+id+`"`)
}

// atomsCoRender is a sound proxy for "these two atoms render in the same
// payload": they share at least one phase AND their envelope-scoped
// deploy-state gates are compatible (either side ungated, or they intersect).
// A finer check would compare every axis, but phase + envelopeDeployStates are
// the coarse gates that decide whether a never-deployed-only reference stub
// appears alongside an iterate-also spine atom — the exact mismatch this
// guards.
func atomsCoRender(src, tgt AxisVector) bool {
	if !sharesPhase(src.Phases, tgt.Phases) {
		return false
	}
	return deployStatesCompatible(src.EnvelopeDeployStates, tgt.EnvelopeDeployStates)
}

func sharesPhase(a, b []Phase) bool {
	for _, x := range a {
		if slices.Contains(b, x) {
			return true
		}
	}
	return false
}

func deployStatesCompatible(a, b []DeployState) bool {
	if len(a) == 0 || len(b) == 0 {
		return true
	}
	for _, x := range a {
		if slices.Contains(b, x) {
			return true
		}
	}
	return false
}
