package recipe

import (
	"errors"
	"fmt"
	"strings"
)

// Run-16 §6.2 — content-phase brief composers. Pointer-based composition:
// briefs carry session state (atoms + filtered facts + codebase metadata)
// plus pointers to canonical files (spec, source, parent surfaces). Sub-
// agents Read the canonical content on demand at authoring decision
// time, collapsing brief size from prep's ~80 KB/dispatch estimate to
// ~25-29 KB while matching today's working pattern.

// BuildCodebaseContentBrief composes the brief for the `codebase-content`
// sub-agent dispatched per codebase at phase 5. Zerops-aware: authors
// IG (slotted) + KB + zerops.yaml comments + intro for `cb`.
//
// `parent` (when non-nil) feeds the parent-recipe pointer block — the
// sub-agent reads the parent's published surfaces and cross-references
// instead of re-authoring (R-15-6 cross-recipe duplication closure).
//
// `facts` is the run's full FactsLog snapshot — the composer filters by
// this codebase's scope. Production callers (Session.BuildBrief) read
// FactsLog and pass the snapshot here; nil is acceptable for unit tests
// that don't need fact threading.
func BuildCodebaseContentBrief(plan *Plan, cb Codebase, parent *ParentRecipe, facts []FactRecord) (Brief, error) {
	if plan == nil {
		return Brief{}, errors.New("nil plan")
	}
	if cb.Hostname == "" {
		return Brief{}, errors.New("codebase hostname empty")
	}

	parts := []string{}
	var b strings.Builder

	// Phase entry atom — voice + dispatch shape teaching.
	if atom, err := readAtom("phase_entry/codebase-content.md"); err == nil {
		b.WriteString(atom)
		b.WriteString("\n\n")
		parts = append(parts, "phase_entry/codebase-content.md")
	}

	// Synthesis workflow atom — how to read facts, group into IG items,
	// dedup against KB, author zerops.yaml comments per block.
	if atom, err := readAtom("briefs/codebase-content/synthesis_workflow.md"); err == nil {
		b.WriteString(atom)
		b.WriteString("\n\n")
		parts = append(parts, "briefs/codebase-content/synthesis_workflow.md")
	}

	// Platform principles — universal Zerops mechanics referenced by the
	// IG/KB authoring decisions.
	if atom, err := readAtom("briefs/scaffold/platform_principles.md"); err == nil {
		b.WriteString(atom)
		b.WriteString("\n\n")
		parts = append(parts, "briefs/scaffold/platform_principles.md")
	}

	// Codebase metadata block — what this dispatch is for.
	b.WriteString("## Codebase\n\n")
	fmt.Fprintf(&b, "- Hostname: %s\n", cb.Hostname)
	fmt.Fprintf(&b, "- Role: %s\n", cb.Role)
	fmt.Fprintf(&b, "- Base runtime: %s\n", cb.BaseRuntime)
	fmt.Fprintf(&b, "- Source root: %s\n", cb.SourceRoot)
	if cb.HasInitCommands {
		b.WriteString("- HasInitCommands: true (zerops.yaml authors initCommands)\n")
	}
	b.WriteString("\n")
	parts = append(parts, "codebase-metadata")

	// Filtered fact stream — only this codebase's recorded
	// porter_change + field_rationale + Kind="" platform-trap facts.
	// Engine-emitted shells render in their own section below; the
	// agent-recorded facts are the bridge between deploy and content
	// phases (plan §1, §2.3).
	cbFacts := FilterByCodebase(facts, cb.Hostname)
	// Drop engine-emitted shells from this section — they have their
	// own list and shouldn't double-render.
	cbFacts = filterOutEngineEmitted(cbFacts)
	if len(cbFacts) > 0 {
		b.WriteString("## Recorded facts (codebase scope)\n\n")
		for _, f := range cbFacts {
			writeFactSummary(&b, f)
		}
		b.WriteString("\n")
		parts = append(parts, "filtered-facts")
	}

	// Engine-emitted shells (Class B + C umbrella + per-managed-service
	// shells). The agent fills empty slots via fill-fact-slot after
	// consulting zerops_knowledge.
	emitted := EmittedFactsForCodebase(plan, cb)
	if len(emitted) > 0 {
		b.WriteString("## Engine-emitted fact shells (fill via fill-fact-slot)\n\n")
		for _, f := range emitted {
			writeFactShellSummary(&b, f)
		}
		b.WriteString("\n")
		parts = append(parts, "engine-emitted-shells")
	}

	// Pointer block — files the sub-agent reads on demand.
	b.WriteString("## On-disk content (Read on demand)\n\n")
	b.WriteString("Before authoring any fragment, Read these in order:\n\n")
	b.WriteString("1. `/Users/fxck/www/zcp/docs/spec-content-surfaces.md` — the seven Surface contracts + classification taxonomy.\n")
	if cb.SourceRoot != "" {
		fmt.Fprintf(&b, "2. `%s/zerops.yaml` — the deploy config you're commenting.\n", cb.SourceRoot)
		fmt.Fprintf(&b, "3. `Glob %s/src/**` then `Read` key files for code-grounded references.\n", cb.SourceRoot)
	}
	if parent != nil && parent.Slug != "" && parent.SourceRoot != "" {
		fmt.Fprintf(&b, "4. `%s/...` — parent recipe (`%s`) published surfaces. Cross-reference instead of re-author when the parent already covers a topic.\n", parent.SourceRoot, parent.Slug)
	}
	b.WriteString("\nFor every engine-pre-seeded fact with empty Why, call `zerops_knowledge runtime=<svc-type>` first, then fill Why + Heading via `fill-fact-slot` grounded in the atom — do NOT paraphrase from memory.\n\n")
	parts = append(parts, "pointer-block")

	// Sibling sub-agent note — codebase-content does NOT author CLAUDE.md.
	b.WriteString("## A sibling claudemd-author sub-agent authors CLAUDE.md in parallel\n\n")
	b.WriteString("You do NOT author CLAUDE.md content. If you encounter Zerops-platform content that belongs in a porter dev guide, check whether it actually belongs in IG/KB/zerops.yaml comments instead — those are your surfaces.\n\n")
	parts = append(parts, "sibling-note")

	out := Brief{
		Kind:  BriefCodebaseContent,
		Body:  b.String(),
		Bytes: b.Len(),
		Parts: parts,
	}
	return out, nil
}

// BuildEnvContentBrief composes the brief for the single `env-content`
// sub-agent at phase 6. Authors root/intro + per-tier env intros +
// import-comments across 6 tiers.
//
// `facts` carries the run's FactsLog snapshot — env-content uses it to
// surface contract facts (cross-codebase NATS subjects, etc.) the
// agent should reference at root scope. Engine-emitted tier_decision
// facts come from EmittedTierDecisionFacts(plan) directly.
func BuildEnvContentBrief(plan *Plan, parent *ParentRecipe, facts []FactRecord) (Brief, error) {
	if plan == nil {
		return Brief{}, errors.New("nil plan")
	}

	parts := []string{}
	var b strings.Builder

	if atom, err := readAtom("phase_entry/env-content.md"); err == nil {
		b.WriteString(atom)
		b.WriteString("\n\n")
		parts = append(parts, "phase_entry/env-content.md")
	}
	if atom, err := readAtom("briefs/env-content/per_tier_authoring.md"); err == nil {
		b.WriteString(atom)
		b.WriteString("\n\n")
		parts = append(parts, "briefs/env-content/per_tier_authoring.md")
	}

	// Per-tier capability matrix (already computed) + cross-tier deltas.
	b.WriteString("## Per-tier capability matrix\n\n")
	tiers := Tiers()
	for _, t := range tiers {
		fmt.Fprintf(&b, "- Tier %d (%s): mode=%s, runtime min containers=%d, cpu=%s, runtime min RAM=%g GB, managed min RAM=%g GB\n",
			t.Index, t.Label, t.ServiceMode, t.RuntimeMinContainers, ifEmpty(t.CPUMode, "SHARED"), t.RuntimeMinRAM, t.ManagedMinRAM)
	}
	b.WriteString("\n")
	parts = append(parts, "tier-capability-matrix")

	b.WriteString("## Cross-tier deltas (tiers.go::Diff)\n\n")
	for i := 1; i < len(tiers); i++ {
		d := Diff(tiers[i-1], tiers[i])
		if len(d.Changes) == 0 {
			continue
		}
		fmt.Fprintf(&b, "Tier %d → tier %d:\n", d.FromIndex, d.ToIndex)
		for _, c := range d.Changes {
			fmt.Fprintf(&b, "  - %s: %s → %s (%s)\n", c.Field, c.From, c.To, c.Kind)
		}
	}
	b.WriteString("\n")
	parts = append(parts, "cross-tier-deltas")

	// Engine-emitted tier_decision facts.
	emitted := EmittedTierDecisionFacts(plan)
	if len(emitted) > 0 {
		b.WriteString("## Engine-emitted tier_decision facts\n\n")
		for _, f := range emitted {
			fmt.Fprintf(&b, "- topic=%s | tier=%d | service=%s | %s=%s | %s\n",
				f.Topic, f.Tier, f.Service, f.FieldPath, f.ChosenValue, f.TierContext)
		}
		b.WriteString("\nExtend `TierContext` via `fill-fact-slot` when the auto-derived prose is too thin.\n\n")
		parts = append(parts, "tier-decision-facts")
	}

	// Cross-codebase contract facts — read at root scope so the agent
	// can name shared contracts (NATS subjects, queue groups, payload
	// schemas) in env-level intros without relying on per-codebase
	// surfaces.
	contracts := FilterByKind(facts, FactKindContract)
	if len(contracts) > 0 {
		b.WriteString("## Cross-codebase contracts\n\n")
		for _, f := range contracts {
			writeFactSummary(&b, f)
		}
		b.WriteString("\n")
		parts = append(parts, "contract-facts")
	}

	// Codebases + services snapshot.
	b.WriteString("## Plan snapshot\n\n")
	for _, cb := range plan.Codebases {
		fmt.Fprintf(&b, "- codebase: %s (role=%s, runtime=%s)\n", cb.Hostname, cb.Role, cb.BaseRuntime)
	}
	for _, svc := range plan.Services {
		fmt.Fprintf(&b, "- service: %s (kind=%s, type=%s)\n", svc.Hostname, svc.Kind, svc.Type)
	}
	b.WriteString("\n")
	parts = append(parts, "plan-snapshot")

	if parent != nil && parent.Slug != "" && parent.SourceRoot != "" {
		fmt.Fprintf(&b, "## Parent recipe `%s`\n\nRead `%s/...` and cross-reference parent's per-tier intros instead of re-authoring.\n\n", parent.Slug, parent.SourceRoot)
		parts = append(parts, "parent-pointer")
	}

	b.WriteString("## On-disk content (Read on demand)\n\n")
	b.WriteString("- `/Users/fxck/www/zcp/docs/spec-content-surfaces.md` — Surface 1/2/3 contracts.\n\n")
	parts = append(parts, "pointer-block")

	out := Brief{
		Kind:  BriefEnvContent,
		Body:  b.String(),
		Bytes: b.Len(),
		Parts: parts,
	}
	return out, nil
}

// BuildClaudeMDBrief composes the brief for the `claudemd-author` sub-
// agent dispatched in parallel with `codebase-content` at phase 5.
//
// Strictly Zerops-free by construction (§6.7a, §Risk 7). The brief
// contains:
//   - The phase entry atom with the /init contract
//   - The codebase metadata (hostname + source root)
//   - The hard-prohibition block (Zerops content must not appear)
//   - On-demand pointers to package.json / src/** ONLY (zerops.yaml is
//     deliberately excluded from the pointer list)
//
// No platform principles, no env-var aliasing teaching, no managed-
// service hints, no reference recipe pointers. Validators backstop at
// record time + finalize.
func BuildClaudeMDBrief(plan *Plan, cb Codebase) (Brief, error) {
	if plan == nil {
		return Brief{}, errors.New("nil plan")
	}
	if cb.Hostname == "" {
		return Brief{}, errors.New("codebase hostname empty")
	}

	parts := []string{}
	var b strings.Builder

	if atom, err := readAtom("phase_entry/claudemd-author.md"); err == nil {
		b.WriteString(atom)
		b.WriteString("\n\n")
		parts = append(parts, "phase_entry/claudemd-author.md")
	}

	// Hard-prohibition block — verbatim from the dedicated atom file so
	// `TestBuildClaudeMDBrief_ContainsHardProhibitionBlock` can assert
	// the exact string.
	if atom, err := readAtom("briefs/claudemd-author/zerops_free_prohibition.md"); err == nil {
		b.WriteString(atom)
		b.WriteString("\n\n")
		parts = append(parts, "briefs/claudemd-author/zerops_free_prohibition.md")
	}

	b.WriteString("## Codebase\n\n")
	fmt.Fprintf(&b, "- Hostname: %s\n", cb.Hostname)
	fmt.Fprintf(&b, "- Source root: %s\n\n", cb.SourceRoot)
	parts = append(parts, "codebase-metadata")

	// On-demand pointers — package.json / src/* / framework-canonical
	// roots ONLY. zerops.yaml is excluded by design (§6.7a + §Risk 7).
	b.WriteString("## On-disk content (Read on demand)\n\n")
	if cb.SourceRoot != "" {
		fmt.Fprintf(&b, "- `%s/package.json` — script labels for Build & run section.\n", cb.SourceRoot)
		fmt.Fprintf(&b, "- `%s/composer.json` (when PHP-flavored) — script labels.\n", cb.SourceRoot)
		fmt.Fprintf(&b, "- `Glob %s/src/**` then `Read` key files for Architecture bullets.\n", cb.SourceRoot)
		fmt.Fprintf(&b, "- `Glob %s/app/**` (Laravel) — controllers/routes for Architecture.\n", cb.SourceRoot)
	}
	b.WriteString("\nNOTE: Do NOT read `zerops.yaml`. Do NOT read sibling recipes' CLAUDE.md as voice anchors. The `/init` contract IS the voice.\n\n")
	parts = append(parts, "pointer-block")

	b.WriteString("## Output\n\n")
	fmt.Fprintf(&b, "Record via `record-fragment fragmentId=codebase/%s/claude-md mode=replace fragment=<your /init output>`. Single fragment, single slot.\n", cb.Hostname)
	parts = append(parts, "output-instruction")

	out := Brief{
		Kind:  BriefClaudeMDAuthor,
		Body:  b.String(),
		Bytes: b.Len(),
		Parts: parts,
	}
	return out, nil
}

// filterOutEngineEmitted drops EngineEmitted=true records from a
// FactRecord slice. Engine-emitted shells appear in their own
// "## Engine-emitted fact shells" section in the brief; agent-recorded
// facts go in "## Recorded facts (codebase scope)". Filtering prevents
// double-rendering after fill-fact-slot flips EngineEmitted to false on
// merge — the post-merge record stays in the agent-recorded section.
func filterOutEngineEmitted(records []FactRecord) []FactRecord {
	out := make([]FactRecord, 0, len(records))
	for _, r := range records {
		if r.EngineEmitted {
			continue
		}
		out = append(out, r)
	}
	return out
}

func writeFactSummary(b *strings.Builder, f FactRecord) {
	switch f.Kind {
	case FactKindPorterChange:
		// Why is bounded by recording (typically 5-10 facts/codebase, 250-500
		// chars each); pass through verbatim so the codebase-content sub-agent
		// reads the full mechanism. Run-17 §4 closure of R-17-C9.
		fmt.Fprintf(b, "- porter_change | topic=%s | class=%s | surface=%s | %s\n",
			f.Topic, f.CandidateClass, f.CandidateSurface, f.Why)
	case FactKindFieldRationale:
		// Same rationale as porter_change — bounded count, pass through verbatim.
		fmt.Fprintf(b, "- field_rationale | topic=%s | %s | %s\n",
			f.Topic, f.FieldPath, f.Why)
	case FactKindTierDecision:
		fmt.Fprintf(b, "- tier_decision | topic=%s | tier=%d | %s=%s | %s\n",
			f.Topic, f.Tier, f.FieldPath, f.ChosenValue, truncate(f.TierContext, 120))
	case FactKindContract:
		fmt.Fprintf(b, "- contract | topic=%s | publishers=%s | subscribers=%s | %s | %s\n",
			f.Topic, strings.Join(f.Publishers, ","), strings.Join(f.Subscribers, ","), f.Subject, truncate(f.Purpose, 80))
	default:
		fmt.Fprintf(b, "- platform-trap | topic=%s | %s | %s\n", f.Topic, f.SurfaceHint, truncate(f.Symptom, 100))
	}
}

func writeFactShellSummary(b *strings.Builder, f FactRecord) {
	if f.Why == "" {
		fmt.Fprintf(b, "- shell | topic=%s | citation=%s | candidate-surface=%s | FILL Why + Heading via fill-fact-slot\n",
			f.Topic, f.CitationGuide, f.CandidateSurface)
		return
	}
	fmt.Fprintf(b, "- shell | topic=%s | candidate-heading=%q | %s\n",
		f.Topic, f.CandidateHeading, truncate(f.Why, 100))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func ifEmpty(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
