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

	// Run-33 architectural fix #2 — `derived_rules.md` is the SOLE
	// scoring substrate; the legacy rubric (`embedded_rubric.md`,
	// 5 criteria × 3 anchors each, line-count/pattern shaped) was
	// retired in run-33 and the atom deleted in the run-34 follow-up
	// (Fix A in plans/run-34-validation.md). Pattern-shaped anchors
	// missed principle-shaped failures (audience-model, tier-prefix
	// intros, slug-stem leakage); rule-walk against the stitched
	// output is closer to how a porter would read and apply rules.
	// Source + derivation in plans/run-32-rules-from-jetstream.md.
	if rules, err := readAtom("briefs/refinement/derived_rules.md"); err == nil {
		b.WriteString(rules)
		b.WriteString("\n\n")
		parts = append(parts, "briefs/refinement/derived_rules.md")
	}

	// Run-23 F-25 — fetchable reference atoms catalog. The 7
	// distillation atoms moved off the inline brief; the agent fetches
	// the one matching the suspect class via `zerops_knowledge uri=...`
	// when it investigates a specific fragment.
	b.WriteString("## Reference atoms — fetch on demand\n\n")
	b.WriteString("When investigating a suspect, fetch the matching reference atom via:\n\n")
	b.WriteString("    zerops_knowledge uri=zerops://themes/refinement-references/<name>\n\n")
	for _, ref := range refinementReferenceCatalog {
		fmt.Fprintf(&b, "- `zerops_knowledge uri=\"%s\"` — %s\n", ref.uri, ref.desc)
	}
	b.WriteByte('\n')
	parts = append(parts, "reference_atom_catalog")

	// Per-recipe context: pointer block to stitched output on disk.
	// Tier directories use Tier.Folder (e.g. "0 — AI Agent",
	// "4 — Small Production") — engine-stable per tiers.go.
	hasStitchedBody := runDir != "" || (parent != nil && parent.Slug != "" && parent.SourceRoot != "")
	if hasStitchedBody {
		b.WriteString("## Stitched output to refine\n\n")
		b.WriteString("Read each path; refine fragments where you can cite the violated rule (from `derived_rules.md`), the exact fragment, and the preserving edit.\n\n")
		if runDir != "" {
			// Run-40 S-3 — normalize trailing slash before string-
			// concat. Run-39's runDir was passed in as
			// `/var/www/zcprecipator/nestjs-showcase/` (trailing
			// slash); the fmt.Fprintf calls below then produced
			// `nestjs-showcase//README.md` etc. (every path
			// double-slashed). The agent dispatched Read calls with
			// the double-slash paths and the read succeeded on POSIX
			// (double-slash collapses), but porter-facing diagnostics
			// and citation-map links carried the broken shape.
			runDir = strings.TrimRight(runDir, "/")
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
	//
	// Run-28 fix #2 — RefinementBriefCap enforced via streaming
	// most-recent-first into a separate buffer; surplus facts elided to
	// disk with a marker line in the brief body so the agent sees the
	// trim. Eviction preserves the rubric blocks (assembled into `b`
	// above) by construction — only the facts buffer is sized against
	// the remaining budget.
	filteredFacts := facts[:0:0]
	for _, f := range facts {
		if FactBelongsToCodebases(f, plan.Codebases) {
			filteredFacts = append(filteredFacts, f)
		}
	}
	if len(filteredFacts) > 0 {
		factsBuf, evicted := composeRefinementFactsBlock(filteredFacts, RefinementBriefCap-b.Len())
		b.WriteString(factsBuf)
		parts = append(parts, "filtered-facts")
		if evicted > 0 {
			parts = append(parts, "facts_evicted_for_cap")
		}
	}

	// Run-40 ENG-4 — canonical-latest collapsed view. Same shape the
	// env-content composer renders; refinement's rule-walk reads it
	// when an ACT-edit touches a name-of-record (queue group, etc.).
	if appendCanonicalLatestSection(&b, filteredFacts) {
		parts = append(parts, "canonical-latest-facts")
	}

	// Run-40 A1 — named constants. Same shape the env-content composer
	// renders. Refinement reads it when validating tier yaml comments
	// against the canonical store of cross-codebase values.
	if appendNamedConstantsSection(&b, plan) {
		parts = append(parts, "named-constants")
	}

	return Brief{
		Kind:  BriefRefinement,
		Body:  b.String(),
		Bytes: b.Len(),
		Parts: parts,
	}, nil
}

// composeRefinementFactsBlock streams `facts` most-recent-first into a
// single "## Recorded facts" block whose total size stays under the
// remaining `budget` bytes. Returns the rendered block, the count of
// facts kept, and the count evicted. When eviction fires, an inline
// notice + tail elision marker land in the rendered block so the
// refinement sub-agent reading the brief sees that older facts went
// to disk; budget exhaustion is the only way to elide.
//
// Iteration most-recent-first per Run-28 fix #2 — keeping the LAST N
// facts is the right choice when authors record corrective findings
// late in the run (e.g. "Pattern A wires NATS via four vars, Pattern B
// crashes" added at feature pass after scaffold's earlier
// connectivity attempts).
func composeRefinementFactsBlock(facts []FactRecord, budget int) (block string, evicted int) {
	if len(facts) == 0 {
		return "", 0
	}
	if budget <= 0 {
		// No headroom even for the heading — drop the entire block.
		return "", len(facts)
	}

	// Render each fact into its own line so eviction is per-fact.
	rendered := make([]string, len(facts))
	for i, f := range facts {
		var line strings.Builder
		writeFactSummary(&line, f)
		rendered[i] = line.String()
	}

	const heading = "## Recorded facts (per-codebase scoped)\n\n"
	const trailer = "\n"
	// `notice` is reserved when eviction fires; rendered into the
	// pre-fact area so the agent encounters it before the kept facts.
	noticeFmt := "> Refinement brief approached the cap; %d facts were elided to disk (oldest-first). The newest %d are listed below.\n\n"
	tailFmt := "\n> _%d facts elided to fit RefinementBriefCap (80 KB); the most recent %d are included above. Full fact stream is on disk under the run output directory._\n"

	// First, optimistic pass — does the whole thing fit?
	totalAll := len(heading) + len(trailer)
	for _, line := range rendered {
		totalAll += len(line)
	}
	if totalAll <= budget {
		var b strings.Builder
		b.Grow(totalAll)
		b.WriteString(heading)
		for _, line := range rendered {
			b.WriteString(line)
		}
		b.WriteString(trailer)
		return b.String(), 0
	}

	// Need eviction. Walk most-recent-first and stop adding when the
	// next fact would exceed the budget, leaving headroom for the
	// notice + tail markers (sized against worst-case integer counts).
	// `%d` substituted with a 7-digit fact-count ceiling — facts.jsonl
	// shipping >9.9M records is far outside any observed run scale.
	worstNotice := len(fmt.Sprintf(noticeFmt, 9999999, 9999999))
	worstTail := len(fmt.Sprintf(tailFmt, 9999999, 9999999))
	overhead := len(heading) + worstNotice + worstTail
	available := budget - overhead
	if available <= 0 {
		// Even rubric overhead doesn't fit; return empty so the brief
		// composer can still report the eviction marker via parts.
		return "", len(rendered)
	}

	keptLines := make([]string, 0, len(rendered))
	used := 0
	for i := len(rendered) - 1; i >= 0; i-- {
		line := rendered[i]
		if used+len(line) > available {
			break
		}
		// Insert at front so output stays chronological even though we
		// iterate from the tail.
		keptLines = append([]string{line}, keptLines...)
		used += len(line)
	}
	evicted = len(rendered) - len(keptLines)

	var b strings.Builder
	b.WriteString(heading)
	if evicted > 0 {
		fmt.Fprintf(&b, noticeFmt, evicted, len(keptLines))
	}
	for _, line := range keptLines {
		b.WriteString(line)
	}
	if evicted > 0 {
		fmt.Fprintf(&b, tailFmt, evicted, len(keptLines))
	} else {
		b.WriteString(trailer)
	}
	return b.String(), evicted
}
