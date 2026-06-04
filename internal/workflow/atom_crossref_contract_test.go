package workflow

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
)

// TestAtomCrossRefContract pins the two-tier cross-reference contract (P0c
// cross-ref-integrity). Two kinds of inter-atom edge, by intent + by target tier:
//
//   - references-atoms = CONTENT dependency. The source body relies on the
//     target's body being present in the SAME rendered payload (shared
//     definition, consolidated topic). A content dependency MUST target an
//     INLINE atom (Reference==false). Depending on a pointer-rendered
//     (reference:true) body is incoherent: the body isn't there — Synthesize
//     emits only a one-line stub, and the pull fetch returns ONE raw body, not
//     a transitive bundle. This is the bug class P0c round-2 closed:
//     deferring an atom that inline spine atoms had a content-dependency on.
//
//   - pointer-atoms = on-demand DEPTH pointer. The source body does not need
//     the target's content to be actionable; the agent fetches it via
//     `zerops_knowledge uri="zerops://atoms/<id>"` only for extra detail. A
//     pointer MUST target a deferred (reference:true) atom. For an INLINE
//     source, the pointer must be RESOLVABLE: the target co-renders under the
//     source's axes (its stub appears in the same payload), or the source body
//     carries the explicit canonical pull URI.
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
			t.Errorf("atom %q pointer-atoms edge to %q is not resolvable: %q is narrower than %q on some axis (its stub is absent in payloads where %q renders) AND %q's body lacks the explicit pull URI `zerops://atoms/%s`. Add the URI to %q's body, or broaden %q's axes so the stub co-renders.",
				atom.ID, ptr, ptr, atom.ID, atom.ID, atom.ID, ptr, atom.ID, ptr)
		}
	}
}

// TestAtomCoRendersWhenever_Teeth proves the strengthened resolvability check
// actually rejects a pointer whose target is narrower on an axis the source
// leaves open — the regression class the earlier phase-only check could not
// catch. Without these assertions the lint could silently weaken to a
// tautology and pass any pointer.
func TestAtomCoRendersWhenever_Teeth(t *testing.T) {
	t.Parallel()
	const phase = PhaseDevelopActive
	cases := []struct {
		name string
		src  AxisVector
		tgt  AxisVector
		want bool
	}{
		{
			name: "target equal on all axes co-renders",
			src:  AxisVector{Phases: []Phase{phase}},
			tgt:  AxisVector{Phases: []Phase{phase}},
			want: true,
		},
		{
			name: "target broader (wildcard runtime) co-renders",
			src:  AxisVector{Phases: []Phase{phase}, Runtimes: []topology.RuntimeClass{topology.RuntimeDynamic}},
			tgt:  AxisVector{Phases: []Phase{phase}},
			want: true,
		},
		{
			name: "target narrower on runtime does NOT co-render",
			src:  AxisVector{Phases: []Phase{phase}},
			tgt:  AxisVector{Phases: []Phase{phase}, Runtimes: []topology.RuntimeClass{topology.RuntimeDynamic}},
			want: false,
		},
		{
			name: "disjoint runtime sets do NOT co-render",
			src:  AxisVector{Phases: []Phase{phase}, Runtimes: []topology.RuntimeClass{topology.RuntimeDynamic}},
			tgt:  AxisVector{Phases: []Phase{phase}, Runtimes: []topology.RuntimeClass{topology.RuntimeStatic}},
			want: false,
		},
		{
			name: "disjoint phase does NOT co-render",
			src:  AxisVector{Phases: []Phase{phase}},
			tgt:  AxisVector{Phases: []Phase{PhaseIdle}},
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := atomCoRendersWhenever(tc.src, tc.tgt); got != tc.want {
				t.Errorf("atomCoRendersWhenever = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestPointerResolvable_URIEscapeHatch proves the explicit canonical URI in the
// source body resolves a pointer even when the target is narrower (would fail
// co-render) — and that removing the URI flips it back to unresolvable.
func TestPointerResolvable_URIEscapeHatch(t *testing.T) {
	t.Parallel()
	// Target is narrower than source (constrains a runtime the source leaves
	// open), so co-render alone would fail.
	tgt := KnowledgeAtom{
		ID:        "tgt",
		Reference: true,
		Axes:      AxisVector{Phases: []Phase{PhaseDevelopActive}, Runtimes: []topology.RuntimeClass{topology.RuntimeDynamic}},
	}
	withURI := KnowledgeAtom{
		ID:   "src",
		Axes: AxisVector{Phases: []Phase{PhaseDevelopActive}},
		Body: "for depth: `zerops_knowledge uri=\"zerops://atoms/tgt\"`",
	}
	if !pointerResolvable(withURI, tgt) {
		t.Error("explicit canonical URI in body must resolve the pointer despite a narrower target")
	}
	noURI := withURI
	noURI.Body = "no pointer here"
	if pointerResolvable(noURI, tgt) {
		t.Error("without the URI and with a narrower target, the pointer must NOT resolve")
	}
}

// pointerResolvable reports whether an inline source's pointer to a deferred
// target is reachable by the agent: either the target's stub co-renders in
// every payload the source renders into (so the agent sees the stub alongside
// the source), or the source body carries the explicit canonical pull URI for
// the target (`zerops://atoms/<target>`), which the agent can fetch even when
// the stub is absent.
func pointerResolvable(src, tgt KnowledgeAtom) bool {
	if containsFetchCommand(src.Body, tgt.ID) {
		return true
	}
	return atomCoRendersWhenever(src.Axes, tgt.Axes)
}

// containsFetchCommand reports whether body carries the explicit canonical
// pull URI for the target atom (`zerops://atoms/<id>`) — the escape hatch the
// author uses when the target does NOT co-render under the source's axes.
func containsFetchCommand(body, id string) bool {
	return strings.Contains(body, "zerops://atoms/"+id)
}

// atomCoRendersWhenever is a SOUND proxy for "the target's stub appears in
// every payload the source renders into": on EVERY axis the target is no
// narrower than the source. This replaces the earlier phase +
// envelopeDeployStates-only check, which could pass a pointer whose target
// fires in a strictly narrower slice (e.g. constrained on a runtime or mode
// the source leaves open) and so would be absent exactly when the source
// renders. Sufficient, not necessary — a pointer this check rejects can still
// declare the explicit canonical URI in its body (containsFetchCommand).
func atomCoRendersWhenever(src, tgt AxisVector) bool {
	return axisNoNarrower(src.Phases, tgt.Phases) &&
		axisNoNarrower(src.Modes, tgt.Modes) &&
		axisNoNarrower(src.Environments, tgt.Environments) &&
		axisNoNarrower(src.CloseDeployModes, tgt.CloseDeployModes) &&
		axisNoNarrower(src.GitPushStates, tgt.GitPushStates) &&
		axisNoNarrower(src.BuildIntegrations, tgt.BuildIntegrations) &&
		axisNoNarrower(src.Runtimes, tgt.Runtimes) &&
		axisNoNarrower(src.RuntimeBases, tgt.RuntimeBases) &&
		axisNoNarrower(src.Routes, tgt.Routes) &&
		axisNoNarrower(src.Steps, tgt.Steps) &&
		axisNoNarrower(src.IdleScenarios, tgt.IdleScenarios) &&
		axisNoNarrower(src.DeployStates, tgt.DeployStates) &&
		axisNoNarrower(src.EnvelopeDeployStates, tgt.EnvelopeDeployStates) &&
		axisNoNarrower(src.ServiceStatuses, tgt.ServiceStatuses) &&
		axisNoNarrower(src.ExportStatuses, tgt.ExportStatuses) &&
		axisNoNarrower(src.ManagedTypes, tgt.ManagedTypes)
}

// axisNoNarrower reports whether tgt's allowed-value set on one axis is no
// narrower than src's — i.e. tgt matches in every situation src does. tgt
// empty = wildcard (always ok); src empty but tgt constrained = src fires on
// values tgt rejects (not ok); otherwise src ⊆ tgt.
func axisNoNarrower[T comparable](src, tgt []T) bool {
	if len(tgt) == 0 {
		return true
	}
	if len(src) == 0 {
		return false
	}
	allowed := make(map[T]struct{}, len(tgt))
	for _, v := range tgt {
		allowed[v] = struct{}{}
	}
	for _, v := range src {
		if _, ok := allowed[v]; !ok {
			return false
		}
	}
	return true
}
