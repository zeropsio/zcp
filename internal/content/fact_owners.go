package content

import "regexp"

// SingleOwnerFact registers a curated platform fact that must have exactly ONE
// authoring owner across the repo (spec-knowledge-architecture.md §5, invariant
// I1). A drift lint (TestSingleOwnerFactsNoDrift) asserts the forbidden
// fingerprint of a DUPLICATE / DRIFTED authoring does not appear outside the
// owner (and the explicitly-sanctioned reference surfaces), turning fact drift
// into a build failure the way internal/topology/architecture_test.go does for
// layer violations.
//
// This is a TRIPWIRE registry — seeded with high-fan-out / already-drifted
// facts ONLY, NOT an attempt to register all ~127 curated facts (that would be
// a maintenance sink, and the spec explicitly warns against it). Grow it only
// when real drift is found and fixed, so every entry pays for itself by pinning
// a fact that actually drifted.
type SingleOwnerFact struct {
	// ID is a stable slug for the fact.
	ID string
	// Owner names the canonical authoring site(s) — a file path or
	// "code:<symbol>". Informational: surfaced in the failure message so the
	// fix points at the owner, not just the symptom.
	Owner string
	// Why is a one-line description of the fact + why it is single-owner.
	Why string
	// Forbidden are fingerprints of a DRIFTED / DUPLICATE authoring. A scanned
	// line matching any pattern (and not exempted by AllowedLines) counts as
	// one occurrence.
	Forbidden []*regexp.Regexp
	// ScopeFiles, when non-empty, restricts the scan to these repo-relative
	// files — the surfaces that must stay clean of the fingerprint. Empty =
	// scan every git-tracked text file (minus skipForDriftScan exclusions).
	ScopeFiles []string
	// MaxMatches is the number of fingerprint occurrences allowed across the
	// scan scope. 0 = the fingerprint is pure drift and must not appear at all.
	// 1 = the fact has a single canonical authoring that must appear exactly
	// once (intra-owner duplicate guard — e.g. a section duplicated verbatim
	// within one file). AllowedLines occurrences never count toward this total.
	MaxMatches int
	// AllowedLines exempts specific "<rel>::<trimmed-line>" occurrences —
	// genuine past-tense / meta mentions (drift-as-example) that legitimately
	// carry the fingerprint.
	AllowedLines []string
}

// SingleOwnerFacts is the tripwire registry. See SingleOwnerFact.
//
// Seeded (2026-06-03) with the one acute drift the knowledge-architecture
// discovery surfaced that is UNAMBIGUOUSLY single-owner: the env-var "Three
// Levels" reference section, which core.md duplicated verbatim (a single-file
// double-authoring; editing one copy silently diverged the other).
//
// NOT seeded: a `dev-dynamic-run-start` fact. The proposed seed assumed the
// "omit run.start" convention (from the recipe-engine gate + the synced guide
// copy) was canonical — but the AUTHORITATIVE platform docs
// (zerops-yaml-advanced.mdx: "Dev services use `start: zsc noop --silent`"),
// the live dev container (runs `zsc noop --silent`), and the classic
// develop/bootstrap flow all use `zsc noop`. The "omit run.start" line is an
// INTERNAL ZCP convention (recipe gate + synced guide + one atom) that
// conflicts with the platform — an unresolved ownership conflict, NOT a
// settled single-owner fact. Registering it would have pinned the wrong side.
// Left unresolved (which convention is canonical) — reconcile before any
// fact-owner entry.
var SingleOwnerFacts = []SingleOwnerFact{
	{
		ID:    "env-vars-three-levels-section",
		Owner: "internal/knowledge/themes/core.md",
		Why:   "The 'Environment Variables — Three Levels' reference section is the single owner of env-var-placement doctrine. core.md carried it VERBATIM twice (≈20 lines each); editing one copy silently diverged the other. Exactly one copy may exist in the owner.",
		Forbidden: []*regexp.Regexp{
			regexp.MustCompile(`^### Environment Variables — Three Levels$`),
		},
		ScopeFiles: []string{"internal/knowledge/themes/core.md"},
		MaxMatches: 1,
	},
}
