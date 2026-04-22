package workflow

import "strings"

// Atom is one leaf content unit in the atomic recipe tree introduced by
// zcprecipator2. Every atom is ≤300 lines (principle P6), declares exactly
// one audience (principle P2), and is loaded by the new stitcher in
// recipe_guidance.go at step-entry / substep-complete / dispatch-brief time.
//
// Paths are relative to internal/content/workflows/recipe/ (the embed.FS
// root set up in C-5). The manifest is the single source of truth for
// which atoms compose which brief or phase surface — C-13's build-time
// lints cross-check that every declared path exists on disk and no
// orphan files live under recipe/.
type Atom struct {
	// ID is the stable dotted identifier (e.g. "research.entry",
	// "briefs.scaffold.mandatory-core"). Used for lookup without
	// stringifying paths — briefs reference atoms by ID at stitch time.
	ID string
	// Path is the filesystem location relative to the recipe/ root.
	Path string
	// Audience names exactly one consumer of the atom's content (principle
	// P2 enforcement). "any" means the atom is pointer-included by
	// multiple briefs; stitch-time concatenation resolves the audience.
	Audience string
	// MaxLines is the upper bound declared in atomic-layout.md §1. C-13
	// lint fails the build if the atom file exceeds this line count.
	MaxLines int
	// TierCond declares whether the atom is tier-gated. Atoms with
	// TierAny participate in both tiers; TierShowcase / TierMinimal
	// atoms are included only in their tier's brief composition.
	TierCond string
}

// Audience enum values. Each value corresponds to exactly one sub-agent
// role (or "main" for the orchestrator, or "any" for pointer-include
// atoms under principles/).
const (
	AudienceMain               = "main"
	AudienceScaffoldSub        = "scaffold-sub"
	AudienceFeatureSub         = "feature-sub"
	AudienceWriterSub          = "writer-sub"
	AudienceCodeReviewSub      = "code-review-sub"
	AudienceEditorialReviewSub = "editorial-review-sub"
	AudienceAny                = "any"
)

// Tier-conditional values. Every atom declares one of these.
const (
	TierAny      = "any"
	TierShowcase = "showcase"
	TierMinimal  = "minimal"
)

// allAtoms is the full atomic manifest for the zcprecipator2 architecture.
// Assembled from the per-category slices below (phases, briefs, principles)
// so the category files stay within the 350-LoC CLAUDE.md cap.
//
// Ordering matters: phases first (stitched at step/substep boundaries),
// then briefs (composed into sub-agent dispatches), then principles
// (pointer-included into briefs at stitch time). Within each category,
// atoms are declared in tree-walk order so the manifest reads linearly
// against atomic-layout.md §1.
var allAtoms = func() []Atom {
	out := make([]Atom, 0, 128)
	out = append(out, phaseAtoms()...)
	out = append(out, briefAtoms()...)
	out = append(out, principleAtoms()...)
	return out
}()

// AllAtoms returns a defensive copy of the atom manifest. Callers must not
// mutate entries (paths are used for filesystem loading).
func AllAtoms() []Atom {
	out := make([]Atom, len(allAtoms))
	copy(out, allAtoms)
	return out
}

// AtomsForPhase returns every atom under phases/<phase>/ in tree-walk
// order. Phase is one of "research" / "provision" / "generate" / "deploy"
// / "finalize" / "close". Returns nil for unknown phases.
func AtomsForPhase(phase string) []Atom {
	prefix := "phases/" + phase + "/"
	var out []Atom
	for _, a := range allAtoms {
		if strings.HasPrefix(a.Path, prefix) {
			out = append(out, a)
		}
	}
	return out
}

// AtomsForBrief returns every atom that composes the dispatch brief for
// the named sub-agent role, filtered by tier. Role is one of "scaffold"
// / "feature" / "writer" / "code-review" / "editorial-review". The
// returned list includes the role's own briefs/<role>/ atoms plus any
// principles atoms the brief pointer-includes (pointer-includes are
// resolved by the stitcher, not by this function — this returns only
// direct atoms of the briefs/<role>/ subtree).
func AtomsForBrief(role, tier string) []Atom {
	prefix := "briefs/" + role + "/"
	var out []Atom
	for _, a := range allAtoms {
		if !strings.HasPrefix(a.Path, prefix) {
			continue
		}
		if a.TierCond != TierAny && a.TierCond != tier {
			continue
		}
		out = append(out, a)
	}
	return out
}

// AtomPath returns the filesystem path for the given atom ID, or ("", false)
// if the ID is not registered. Used by the stitcher to resolve atom IDs
// into embed.FS load keys.
func AtomPath(id string) (string, bool) {
	for _, a := range allAtoms {
		if a.ID == id {
			return a.Path, true
		}
	}
	return "", false
}

// FindAtom returns the full Atom record for the given ID, or (zero, false)
// if not registered.
func FindAtom(id string) (Atom, bool) {
	for _, a := range allAtoms {
		if a.ID == id {
			return a, true
		}
	}
	return Atom{}, false
}

// AtomsByAudience returns every atom whose Audience matches the given
// value. Useful for the C-13 build lint that greps briefs-audience atoms
// for dispatcher vocabulary separately from main-audience atoms.
func AtomsByAudience(audience string) []Atom {
	var out []Atom
	for _, a := range allAtoms {
		if a.Audience == audience {
			out = append(out, a)
		}
	}
	return out
}

// PrinciplesPath is the filesystem prefix for principles/ atoms. Exported
// so the stitcher can resolve pointer-include markers like
// {{ include "principles/where-commands-run.md" }} to concrete atom paths.
const PrinciplesPath = "principles/"

// atomCountBaseline is the expected number of atoms when the manifest
// matches atomic-layout.md §1 exactly. 122 = the sum of:
//
//	66 phase atoms (research 3 + provision 13 + generate 24 + deploy 14 +
//	   finalize 6 + close 6) — C-7.5 added close.editorial-review
//	40 brief atoms (scaffold 8 + feature 6 + writer 11 + code-review 5 +
//	   editorial-review 10) — v39 Commit 5a added writer.classification-
//	   pointer (replaces inlined classification-taxonomy + routing-matrix)
//	16 principle atoms (6 top-level + 6 platform-principles + 4 adjunct)
//
// Note: the "96 atoms" number that appears in atomic-layout.md §1 + the
// rollout-sequence summary refers to an earlier research-phase snapshot
// before the tree was expanded to per-substep entry/completion pairs. The
// canonical count is the tree (122 post-v39); the summary text is
// advisory. C-3's seed test asserts the declared manifest matches this
// baseline exactly — adding or removing an atom requires a deliberate
// update to both the manifest and the constant.
const atomCountBaseline = 122
