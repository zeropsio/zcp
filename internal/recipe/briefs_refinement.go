package recipe

import (
	"errors"
	"fmt"
	"strings"
)

// Run-17 §9 — refinement sub-agent brief composer. The refinement
// sub-agent runs at phase 8 (post-finalize) as a single transactional
// pass; it reads stitched output + reference distillation atoms +
// recorded facts + the rubric and Replaces fragments where the
// 100%-sure threshold holds.

// refinementReferenceAtoms — the seven Tranche 0.5 distillation atoms.
// Order is read order; each atom must exist or the composer errors so
// the dispatch never lands on a partial corpus.
var refinementReferenceAtoms = []string{
	"briefs/refinement/reference_kb_shapes.md",
	"briefs/refinement/reference_ig_one_mechanism.md",
	"briefs/refinement/reference_voice_patterns.md",
	"briefs/refinement/reference_yaml_comments.md",
	"briefs/refinement/reference_citations.md",
	"briefs/refinement/reference_trade_offs.md",
	"briefs/refinement/refinement_thresholds.md",
}

// BuildRefinementBrief composes the brief for the single refinement
// sub-agent dispatched at phase 8 (post-finalize). The sub-agent
// reads stitched output + reference distillation atoms + facts + the
// rubric; it Replaces fragments where the 100%-sure threshold holds.
//
// The composer assembles the brief from embedded atoms ONLY; runDir
// is read for (a) stitched-output paths the sub-agent will Read at
// dispatch time (the brief lists them) and (b) facts.jsonl path. No
// /Users/fxck/www/... paths leak into the brief — pinned by
// TestNoFilesystemReferenceLeak_RefinementBrief.
//
// Signature mirrors BuildCodebaseContentBrief — parent threaded so
// the sub-agent can read parent's published surfaces and skip
// refinements that would re-author parent material (Q5 resolution
// from run-17-implementation.md §15).
func BuildRefinementBrief(plan *Plan, parent *ParentRecipe, runDir string, facts []FactRecord) (Brief, error) {
	if plan == nil {
		return Brief{}, errors.New("nil plan")
	}
	parts := []string{}
	var b strings.Builder

	// Phase entry — voice + dispatch shape.
	if atom, err := readAtom("phase_entry/refinement.md"); err == nil {
		b.WriteString(atom)
		b.WriteString("\n\n")
		parts = append(parts, "phase_entry/refinement.md")
	}

	// Synthesis workflow — explicit refinement actions.
	if atom, err := readAtom("briefs/refinement/synthesis_workflow.md"); err == nil {
		b.WriteString(atom)
		b.WriteString("\n\n")
		parts = append(parts, "briefs/refinement/synthesis_workflow.md")
	}

	// The 7 reference distillation atoms — required.
	for _, p := range refinementReferenceAtoms {
		atom, err := readAtom(p)
		if err != nil {
			return Brief{}, fmt.Errorf("refinement reference atom %s: %w", p, err)
		}
		b.WriteString(atom)
		b.WriteString("\n\n")
		parts = append(parts, p)
	}

	// Quality rubric — embedded inline rather than via Read so the
	// sub-agent never needs to leave the brief context.
	if rubric, err := readAtom("briefs/refinement/embedded_rubric.md"); err == nil {
		b.WriteString(rubric)
		b.WriteString("\n\n")
		parts = append(parts, "briefs/refinement/embedded_rubric.md")
	}

	// Per-recipe context: pointer block to stitched output on disk.
	// Tier directories use Tier.Folder (e.g. "0 — AI Agent",
	// "4 — Small Production") — engine-stable per tiers.go.
	b.WriteString("## Stitched output to refine\n\n")
	b.WriteString("Read each path in order; refine fragments where the 100%-sure threshold holds.\n\n")
	if runDir != "" {
		fmt.Fprintf(&b, "1. `%s/README.md` — root README\n", runDir)
		for _, t := range Tiers() {
			fmt.Fprintf(&b, "2. `%s/environments/%s/README.md` + `import.yaml`\n", runDir, t.Folder)
		}
		for _, cb := range plan.Codebases {
			fmt.Fprintf(&b, "3. `%s/%s/README.md` + `zerops.yaml` + `CLAUDE.md`\n", runDir, cb.Hostname)
		}
	}
	if parent != nil && parent.Slug != "" && parent.SourceRoot != "" {
		fmt.Fprintf(&b, "4. parent recipe `%s` published surfaces (under the parent's source root). Refinement HOLDS on any fragment whose body would re-author parent material.\n", parent.Slug)
	}
	b.WriteString("\n")
	parts = append(parts, "stitched-output-pointer-block")

	// Facts log — full snapshot, no truncation. The refinement sub-
	// agent uses recorded facts to validate trade-off two-sidedness
	// (rejected alternatives are usually in the recorded Why prose)
	// and citation routing.
	if len(facts) > 0 {
		b.WriteString("## Recorded facts (run-wide)\n\n")
		for _, f := range facts {
			writeFactSummary(&b, f)
		}
		b.WriteString("\n")
		parts = append(parts, "filtered-facts")
	}

	return Brief{
		Kind:  BriefRefinement,
		Body:  b.String(),
		Bytes: b.Len(),
		Parts: parts,
	}, nil
}
