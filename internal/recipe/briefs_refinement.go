package recipe

import (
	"errors"
	"fmt"
	"strings"
)

// Run-17 §9 — refinement sub-agent brief composer. The refinement
// sub-agent runs at phase 8 (post-finalize) as a single transactional
// pass; it reads stitched output + recorded facts + the rubric and
// Replaces fragments where the threshold holds.
//
// Run-23 F-24 + F-25 — the seven reference distillation atoms moved
// off the inline brief and onto the discovery channel
// (`zerops_knowledge uri=zerops://themes/refinement-references/<name>`)
// so the agent fetches the atom WHEN it's investigating a class, not
// preloaded into the brief. Per-codebase fact filtering further trims
// the brief from ~167 KB to ~30-50 KB.

// refinementReferenceCatalog — what the agent can fetch on demand.
// Each entry is a one-line description rendered into the brief so the
// agent picks the right atom for the class it's investigating.
var refinementReferenceCatalog = []struct {
	uri  string
	desc string
}{
	{"zerops://themes/refinement-references/kb_shapes", "KB stem symptom-first heuristic + worked-example anchors at 7.0/8.5/9.0."},
	{"zerops://themes/refinement-references/ig_one_mechanism", "IG H3 one-mechanism-per-heading rule + fusion-split decision examples."},
	{"zerops://themes/refinement-references/voice_patterns", "Friendly-authority phrasing patterns for zerops.yaml + tier import.yaml comments."},
	{"zerops://themes/refinement-references/yaml_comments", "Yaml-comment shape: mechanism-first, no field-restatement preamble."},
	{"zerops://themes/refinement-references/citations", "Cite-by-name pattern + application-specific corollary phrasing."},
	{"zerops://themes/refinement-references/trade_offs", "Two-sided trade-off bodies: name the chosen path + the rejected alternative."},
	{"zerops://themes/refinement-references/refinement_thresholds", "ACT vs HOLD decision rules, the 8 refinement actions, per-fragment edit cap."},
}

// BuildRefinementBrief composes the brief for the single refinement
// sub-agent dispatched at phase 8 (post-finalize). The sub-agent
// reads stitched output + facts + the rubric; it Replaces fragments
// where the threshold holds.
//
// The composer assembles the brief from embedded atoms ONLY; runDir
// is read for (a) stitched-output paths the sub-agent will Read at
// dispatch time (the brief lists them) and (b) facts.jsonl path. No
// /Users/fxck/www/... paths leak into the brief — pinned by
// TestNoFilesystemReferenceLeak_RefinementBrief.
//
// Run-23 F-24 — per-codebase fact filtering: facts whose `service`
// field doesn't match any codebase in the plan get dropped. Run-23
// F-25 — the 7 reference distillation atoms moved to the
// `zerops_knowledge uri=...` discovery channel; the brief lists them
// as fetchable references with descriptions.
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

	// Quality rubric — embedded inline rather than via Read so the
	// sub-agent never needs to leave the brief context.
	if rubric, err := readAtom("briefs/refinement/embedded_rubric.md"); err == nil {
		b.WriteString(rubric)
		b.WriteString("\n\n")
		parts = append(parts, "briefs/refinement/embedded_rubric.md")
	}

	// Run-23 F-25 — fetchable reference atoms catalog. The 7
	// distillation atoms moved off the inline brief; the agent fetches
	// the one matching the suspect class via `zerops_knowledge uri=...`
	// when it investigates a specific fragment.
	b.WriteString("## Reference atoms — fetch on demand\n\n")
	b.WriteString("When investigating a suspect, fetch the matching reference atom via:\n\n")
	b.WriteString("    zerops_knowledge uri=zerops://themes/refinement-references/<name>\n\n")
	for _, ref := range refinementReferenceCatalog {
		fmt.Fprintf(&b, "- `%s` — %s\n", ref.uri, ref.desc)
	}
	b.WriteByte('\n')
	parts = append(parts, "reference_atom_catalog")

	// Per-recipe context: pointer block to stitched output on disk.
	// Tier directories use Tier.Folder (e.g. "0 — AI Agent",
	// "4 — Small Production") — engine-stable per tiers.go.
	hasStitchedBody := runDir != "" || (parent != nil && parent.Slug != "" && parent.SourceRoot != "")
	if hasStitchedBody {
		b.WriteString("## Stitched output to refine\n\n")
		b.WriteString("Read each path; refine fragments where you can cite the violated rubric criterion, the exact fragment, and the preserving edit.\n\n")
		if runDir != "" {
			b.WriteString("**Root**\n\n")
			fmt.Fprintf(&b, "- `%s/README.md` — root README\n", runDir)
			b.WriteString("\n**Tier environments**\n\n")
			for _, t := range Tiers() {
				fmt.Fprintf(&b, "- `%s/environments/%s/README.md` + `import.yaml`\n", runDir, t.Folder)
			}
			b.WriteString("\n**Codebases**\n\n")
			for _, cb := range plan.Codebases {
				fmt.Fprintf(&b, "- `%s/%s/README.md` + `zerops.yaml` + `CLAUDE.md`\n", runDir, cb.Hostname)
			}
		}
		if parent != nil && parent.Slug != "" && parent.SourceRoot != "" {
			b.WriteString("\n**Parent recipe (read-only)**\n\n")
			fmt.Fprintf(&b, "- parent recipe `%s` published surfaces (under the parent's source root). Refinement HOLDS on any fragment whose body would re-author parent material.\n", parent.Slug)
		}
		b.WriteString("\n")
		parts = append(parts, "stitched-output-pointer-block")
	}

	// Run-22 followup F-6 — embedded-parent fallback at refinement,
	// mirror of the R3-RC-0 scaffold pattern. When the chain resolver
	// returns no parent (filesystem mount empty in dogfood) but the
	// slug has a recognized chain parent (`*-showcase` → `*-minimal`),
	// inject the embedded recipe `.md` as a read-only baseline so the
	// refinement sub-agent's anti-cross-recipe-duplication HOLD rule
	// has something concrete to compare against. Filesystem-mount
	// path (above) wins when present.
	//
	// Excerpt cap is 4000 bytes here (matches scaffold). Refinement
	// has no enforced cap; the embedded fallback only fires in the
	// dogfood path, and the per-recipe brief is composed once per
	// run.
	if appendEmbeddedParentBaselineRefinement(&b, plan.Slug, parent, refinementEmbeddedFraming, 4000) {
		parts = append(parts, "embedded_parent_baseline")
	}

	// Run-23 F-24 — engine-pre-flagged suspect list. Engine collects
	// suspects from notices + a cheap rubric pre-scan over per-codebase
	// KB fragment bodies; the agent investigates the named fragments
	// against the rubric and ACTs or HOLDs with reasons. The list is
	// "investigate at minimum these," NOT "ONLY these."
	suspects := CollectRefinementSuspects(plan, nil)
	if formatted := FormatRefinementSuspects(suspects); formatted != "" {
		b.WriteString(formatted)
		parts = append(parts, "engine_flagged_suspects")
	}

	// Facts log — filtered to per-codebase scope. Run-23 F-24 — facts
	// whose `service` field doesn't match any codebase under review get
	// dropped so the refinement composer ships ~half the prior fact
	// volume. Service-empty facts (run-wide tier_decisions, project-
	// scope porter_changes) stay in scope.
	filteredFacts := facts[:0:0]
	for _, f := range facts {
		if FactBelongsToCodebases(f, plan.Codebases) {
			filteredFacts = append(filteredFacts, f)
		}
	}
	if len(filteredFacts) > 0 {
		b.WriteString("## Recorded facts (per-codebase scoped)\n\n")
		for _, f := range filteredFacts {
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
