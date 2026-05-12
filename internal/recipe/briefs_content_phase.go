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

	parts = appendCodebaseContentAtoms(&b, parts, plan, cb)

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
	// Run-17 §6 — Class B / C / per-service engine-emit shells are
	// retracted; the deploy-phase sub-agents record the equivalent
	// porter_change facts per the worked examples in scaffold/feature
	// decision_recording atoms. EmittedFactsForCodebase still runs for
	// signature compatibility but always returns nil; defensive
	// filterOutEngineEmitted drops any historical shells if a run-16
	// session log replays through this composer.
	cbFacts := FilterByCodebase(facts, cb.Hostname)
	cbFacts = filterOutEngineEmitted(cbFacts)
	if len(cbFacts) > 0 {
		b.WriteString("## Recorded facts (codebase scope)\n\n")
		for _, f := range cbFacts {
			writeFactSummary(&b, f)
		}
		b.WriteString("\n")
		parts = append(parts, "filtered-facts")
	}

	if appendCrossCodebaseFactsBlock(&b, facts, cb) {
		parts = append(parts, "cross-codebase-facts")
	}

	// Pointer block — files the sub-agent reads on demand. The Surface
	// contracts + classification taxonomy live in the embedded
	// synthesis_workflow.md atom above, NOT in an external file pointer
	// (a hardcoded absolute path in the brief was dev-machine-specific
	// and broke every other run target — the atom is portable).
	b.WriteString("## On-disk content (Read on demand)\n\n")
	if cb.SourceRoot != "" {
		b.WriteString("Before authoring any fragment, Read in order:\n\n")
		fmt.Fprintf(&b, "1. `%s/zerops.yaml` — the deploy config you're commenting.\n", cb.SourceRoot)
		fmt.Fprintf(&b, "2. `Glob %s/src/**` then `Read` key files for code-grounded references.\n", cb.SourceRoot)
		nextIdx := 3
		if parent != nil && parent.Slug != "" && parent.SourceRoot != "" {
			fmt.Fprintf(&b, "%d. `%s/...` — parent recipe (`%s`) published surfaces. Cross-reference instead of re-author when the parent already covers a topic.\n", nextIdx, parent.SourceRoot, parent.Slug)
		}
	} else if parent != nil && parent.Slug != "" && parent.SourceRoot != "" {
		fmt.Fprintf(&b, "Before authoring any fragment, Read `%s/...` — parent recipe (`%s`) published surfaces. Cross-reference instead of re-author when the parent already covers a topic.\n", parent.SourceRoot, parent.Slug)
	}
	b.WriteString("\nFor any platform mechanism your synthesis touches, call `zerops_knowledge runtime=<svc-type>` first and ground the prose in the atom — do NOT paraphrase from memory.\n\n")
	parts = append(parts, "pointer-block")

	// Run-22 followup F-6 — embedded-parent fallback. Mirror the
	// R3-RC-0 scaffold pattern (briefs.go::BuildScaffoldBriefWithResolver
	// ~L338-361). When the chain resolver returns no parent (filesystem
	// mount empty in dogfood) but the slug has a recognized chain
	// parent (`*-showcase` → `*-minimal`), inject the embedded recipe
	// `.md` so the codebase-content sub-agent has IG/KB framing
	// inheritance — what topics the parent already covers, so it can
	// cross-reference instead of re-author. Filesystem-mount path wins
	// when present (parent.SourceRoot != ""); the embedded fallback
	// only fires when no filesystem parent is available.
	//
	// Excerpt cap is 1000 bytes here (vs scaffold's 4000) because
	// codebase-content has tight headroom under the 56 KB cap on the
	// showcase + worker variant — R2 left only ~1.1 KB of headroom and
	// the worker brief is the highest-load shape (worker_kb_supplements
	// already loads). 1000 bytes still carries the parent's frontmatter
	// + Gotchas section + the leading zerops.yaml shape (setup-naming,
	// comment style, opening of build section) — signal density is
	// highest in the first ~1 KB of every minimal recipe `.md`.
	// Earlier iterations of this fix tested 2500-byte (worker brief
	// 58.8 KB / cap 56 KB — over) and 1500-byte (57.8 KB — still over)
	// excerpts before settling on 1000. Resist the cap-bump habit —
	// the 56 KB cap was already bumped 44→48→52→56 over recent runs.
	if appendEmbeddedParentBaseline(&b, plan.Slug, parent, codebaseContentEmbeddedFraming, 1000) {
		parts = append(parts, "embedded_parent_baseline")
	}

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

	// Run-22 followup F-4 — workspace dual-runtime URL teaching reaches
	// env-content unconditionally. The per-tier import-comments fragments
	// authored at this phase routinely narrate URL constants
	// (`${zeropsSubdomainHost}`, `API_URL`, etc); without the atom
	// the env-content sub-agent lacks (a) the literal-stays-literal rule
	// for the deliverable tier yaml and (b) the `update-plan
	// projectEnvVars` channel-sync teaching that explains why those
	// constants exist. Loaded unconditionally because the brief has
	// >25 KB headroom under the 56 KB cap and gating on
	// `len(plan.ProjectEnvVars) > 0` would only save ~10.5 KB on plans
	// that ALSO don't have URL-pair codebases — a vanishing sub-class.
	if atom, err := readAtom("principles/cross-service-urls.md"); err == nil {
		b.WriteString(atom)
		b.WriteString("\n\n")
		parts = append(parts, "principles/cross-service-urls.md")
	}

	// Run-20 C1 — NATS messaging-shape teaching is shared with the
	// codebase-content brief composer. Run-19 dropped JetStream
	// fabrication into tier 0 + tier 5 import.yaml comments because
	// the env-content sub-agent didn't see this teaching; only
	// codebase-content did. Pulling from one source keeps the framing
	// aligned across phases.
	//
	// Run-21 R3-2 — gate on plan-level NATS presence. Env-content is
	// plan-wide so it can't use cb.ConsumesServices the way R2-2's
	// codebase-content composer does; the right precision lever is
	// `planUsesNATS(plan)`. Plans without a nats@* service can't
	// author NATS comments at any tier — the teaching is dead weight.
	if planUsesNATS(plan) {
		if atom, err := readAtom("principles/nats-shapes.md"); err == nil {
			b.WriteString(atom)
			b.WriteString("\n\n")
			parts = append(parts, "principles/nats-shapes.md")
		}
	}

	// Run-20 (post sub-agent C verification) — Zerops platform claim
	// attestation. Closes corePackage / mode / placeholder-URL
	// fabrications surfaced in the first sim verification. Tier yaml
	// comments are the densest fabrication site; this atom is most
	// load-bearing here.
	if atom, err := readAtom("principles/zerops-knowledge-attestation.md"); err == nil {
		b.WriteString(atom)
		b.WriteString("\n\n")
		parts = append(parts, "principles/zerops-knowledge-attestation.md")
	}

	// Run-20 — sharpened yaml-comment-style atom (multi-line wrap +
	// GOOD/BAD examples). The env-content sub-agent authors tier
	// import.yaml comments; same wrap rule applies.
	if atom, err := readAtom("principles/yaml-comment-style.md"); err == nil {
		b.WriteString(atom)
		b.WriteString("\n\n")
		parts = append(parts, "principles/yaml-comment-style.md")
	}

	// Run-32 F-49 + Run-40 S-3 — codebase-name-vs-slot-hostname plus
	// the env-content-only managed-service `<host>` mapping. Pre-
	// empts `record-fragment fragmentId=env/<N>/import-comments/...`
	// traps in both shapes: runtime codebases (slot vs bare name)
	// and managed services (no dev/stage suffix).
	parts = appendEnvFragmentNamingAtoms(&b, parts)

	// Run-33 architectural fix #1 — golden-grounded rule substrate.
	// Voice + IG6 Zerops-forced + tier-vocab forbidden rules visible at
	// authoring time, not just refinement. Multi-file path is production;
	// single-file kept in sync.
	if atom, err := readAtom("briefs/refinement/derived_rules.md"); err == nil {
		b.WriteString(atom)
		b.WriteString("\n\n")
		parts = append(parts, "briefs/refinement/derived_rules.md")
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

	// Run-40 ENG-4 — canonical-latest facts. When the same observable
	// has been recorded under multiple topic strings across phases
	// (e.g. the run-39 NATS queue-group concept under three aliases),
	// surface the recordedAt-latest record per canonical key.
	// Section is rendered only when actual aliasing is observed.
	if appendCanonicalLatestSection(&b, facts) {
		parts = append(parts, "canonical-latest-facts")
	}

	// Run-40 A1 — named constants. Surface plan.NamedConstants so the
	// agent cites the canonical value when authoring tier yaml
	// import-comments. Section is rendered only when the plan carries
	// at least one named constant.
	if appendNamedConstantsSection(&b, plan) {
		parts = append(parts, "named-constants")
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
	} else if appendEmbeddedParentBaseline(&b, plan.Slug, parent, envContentEmbeddedFraming, 4000) {
		// Run-22 followup F-6 — embedded-parent fallback at env-content,
		// mirror of the R3-RC-0 scaffold pattern. When the chain
		// resolver returns no parent (filesystem mount empty in
		// dogfood) but the slug has a recognized chain parent
		// (`*-showcase` → `*-minimal`), inject the embedded recipe
		// `.md` so the env-content sub-agent has tier-decision and
		// per-tier framing inheritance — align tier intros + import
		// comments with the parent's tier-flavor voice instead of
		// re-authoring.
		//
		// Excerpt cap is 4000 bytes here (matches scaffold). Env-
		// content sat at 34,558 bytes / 56 KB cap after R2 with
		// 22,786 bytes of headroom; +4 KB lands at ~38.5 KB, well
		// inside the cap.
		parts = append(parts, "embedded_parent_baseline")
	}

	// Surface 1/2/3 contracts live in the embedded per_tier_authoring.md
	// + intro.md atoms above. No external file pointer — those were
	// dev-machine-specific absolute paths.

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

// appendCrossCodebaseFactsBlock emits the run-26 F-31 cross-codebase
// managed-service facts block. Returns true when the block was emitted
// (caller appends `cross-codebase-facts` to brief.Parts), false when
// no propagation was needed. Closes the run-26 apidev-NATS factuality
// drift where the worker-scaffold finding about
// `${broker_connectionString}` never reached apidev's brief.
func appendCrossCodebaseFactsBlock(b *strings.Builder, facts []FactRecord, cb Codebase) bool {
	crossFacts := filterOutEngineEmitted(CrossCodebaseManagedServiceFacts(facts, cb))
	if len(crossFacts) == 0 {
		return false
	}
	b.WriteString("## Cross-codebase managed-service facts\n\n")
	b.WriteString("Facts authored by sister codebases that reference managed services this codebase also consumes. Engine-propagated so connection-shape decisions stay consistent across codebases — when a sister codebase recorded that a specific env-key shape crashed at boot, this codebase's IG/KB must not endorse that shape. Treat each entry as authoritative for THIS recipe; the atom corpus's generic guidance defers to the run's recorded scaffold findings.\n\n")
	for _, f := range crossFacts {
		writeFactSummary(b, f)
	}
	b.WriteString("\n")
	return true
}

// appendEnvFragmentNamingAtoms loads the two env-fragment-id naming
// atoms in order: the codebase-name-vs-slot-hostname principle and
// the env-content-only managed-service host mapping (run-40 S-3).
// Extracted to keep BuildEnvContentBrief under the maintidx ceiling.
func appendEnvFragmentNamingAtoms(b *strings.Builder, parts []string) []string {
	if atom, err := readAtom("principles/codebase-name-vs-slot-hostname.md"); err == nil {
		b.WriteString(atom)
		b.WriteString("\n\n")
		parts = append(parts, "principles/codebase-name-vs-slot-hostname.md")
	}
	if atom, err := readAtom("briefs/env-content/managed_service_host_mapping.md"); err == nil {
		b.WriteString(atom)
		b.WriteString("\n\n")
		parts = append(parts, "briefs/env-content/managed_service_host_mapping.md")
	}
	return parts
}

// appendNamedConstantsSection writes the canonical named-constants
// table to b when plan.NamedConstants is non-empty. The map captures
// cross-codebase string constants (queue groups, cache prefixes,
// signing-key aliases); env-content + refinement composers surface it
// so the agent cites the canonical value when authoring tier yaml
// comments rather than re-deriving from a free-text fact. Run-40 A1.
func appendNamedConstantsSection(b *strings.Builder, plan *Plan) bool {
	if plan == nil || len(plan.NamedConstants) == 0 {
		return false
	}
	b.WriteString("## Named constants (cross-codebase canonical values)\n\n")
	b.WriteString("These constants must read identically across source code, codebase yaml `run.envVariables`, and tier yaml `import.yaml` comments. The named-constants-consistency gate refuses env-content close when a tier comment cites a value the table says is wrong. Cite these exactly — do not paraphrase or substitute.\n\n")
	for _, key := range sortedConstantKeys(plan.NamedConstants) {
		fmt.Fprintf(b, "- `%s` = `%s`\n", key, plan.NamedConstants[key])
	}
	b.WriteString("\n")
	return true
}

// appendCanonicalLatestSection writes the canonical-latest section
// to b when at least one aliased canonical topic appears in facts.
// Returns whether the section was emitted. The env-content and
// refinement brief composers share this body — the rendered shape is
// identical so both sub-agents read the same single-source-of-truth
// for cross-phase name-of-record. Run-40 ENG-4.
func appendCanonicalLatestSection(b *strings.Builder, facts []FactRecord) bool {
	aliased := AliasedCanonicalTopics(facts)
	if len(aliased) == 0 {
		return false
	}
	b.WriteString("## Latest by canonical topic (cross-phase collapsed)\n\n")
	b.WriteString("Multiple recorded topics describe the same observable concept. The engine collapses them by canonical key and surfaces the recordedAt-latest record per key. Read these for cross-codebase name-of-record (queue groups, connection patterns) — the raw facts stream above also carries the superseded recordings for audit, but tier yaml comments and root-scope claims should cite the canonical-latest below.\n\n")
	for canonical, rec := range aliased {
		fmt.Fprintf(b, "- canonical=%s | from topic=%s | recordedAt=%s | %s\n",
			canonical, rec.Topic, rec.RecordedAt, canonicalFactSummary(rec))
	}
	b.WriteString("\n")
	return true
}

// canonicalFactSummary returns a one-line summary of the most
// concept-bearing fields for a canonical-latest record. Picks the
// kind-specific field that carries the live value (FieldValue for
// field_rationale, Diff for porter_change, Subject + QueueGroups
// for contract). Run-40 ENG-4.
func canonicalFactSummary(f FactRecord) string {
	switch f.Kind {
	case FactKindFieldRationale:
		if f.FieldPath != "" && f.FieldValue != "" {
			return fmt.Sprintf("%s=%s", f.FieldPath, f.FieldValue)
		}
		return truncate(f.Why, 120)
	case FactKindPorterChange:
		if f.Diff != "" {
			return truncate(f.Diff, 120)
		}
		return truncate(f.Why, 120)
	case FactKindContract:
		if len(f.QueueGroups) > 0 {
			return fmt.Sprintf("subject=%s | queueGroups=%s", f.Subject, strings.Join(f.QueueGroups, ","))
		}
		return fmt.Sprintf("subject=%s | %s", f.Subject, truncate(f.Purpose, 80))
	case FactKindTierDecision:
		return fmt.Sprintf("%s=%s", f.FieldPath, f.ChosenValue)
	default:
		return truncate(f.Why, 120)
	}
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

// appendCodebaseContentAtoms loads the static + conditional atoms for
// the codebase-content brief in canonical order. Run-30 Fix #1 PARTIAL
// extracted this from BuildCodebaseContentBrief to keep the parent
// function's cyclomatic complexity / maintainability index inside the
// linter threshold after the cross-service-urls + worker_kb_supplements
// trim landed. Returns the updated parts slice.
func appendCodebaseContentAtoms(b *strings.Builder, parts []string, plan *Plan, cb Codebase) []string {
	parts = appendAtomIfFound(b, parts, "phase_entry/codebase-content.md")
	parts = appendSynthesisWorkflowBlock(b, parts)
	// Run-43 Edit C — golden voice principles atom. The reference
	// recipes (laravel-jetstream + laravel-showcase) define the
	// empirical floor for voice + structure; the goldens themselves
	// are NOT embedded (large + frequently iterated). This atom
	// describes the golden voice on PRINCIPLE — operational over
	// defensive, friendly-authority adaptation pattern, self-contained
	// yaml comments, citation pattern — citing the goldens by name as
	// anchors without quoting them verbatim. Pinned by
	// TestGoldenVoicePrinciples_AtomPresent.
	parts = appendAtomIfFound(b, parts, "briefs/codebase-content/golden_voice_principles.md")
	if plan.Tier == tierShowcase && cb.IsWorker {
		writeWorkerKBSupplementsPointer(b)
		parts = append(parts, "worker_kb_supplements (deferred)")
	}
	parts = appendAtomIfFound(b, parts, "briefs/scaffold/platform_principles.md")
	if shouldLoadNATSShapes(plan, cb) {
		parts = appendAtomIfFound(b, parts, "principles/nats-shapes.md")
	}
	if shouldLoadCrossServiceURLs(cb) {
		parts = appendAtomIfFound(b, parts, "principles/cross-service-urls-summary.md")
	}
	parts = appendAtomIfFound(b, parts, "principles/zerops-knowledge-attestation.md")
	parts = appendAtomIfFound(b, parts, "principles/yaml-comment-style.md")
	// Run-32 F-49 — codebase-name-vs-slot-hostname. Pre-empts
	// `record-fragment fragmentId=codebase/<host>dev/...` traps where
	// the codebase-content agent reaches for the slot hostname
	// (`apidev`/`workerdev`) it sees on SSHFS mounts.
	parts = appendAtomIfFound(b, parts, "principles/codebase-name-vs-slot-hostname.md")
	// Run-33 architectural fix #1 — golden-grounded rule substrate.
	// Voice + IG6 Zerops-forced + KB shape + Y15 density rules visible
	// at authoring time, not just refinement. Multi-file path is the
	// production composer; single-file kept in sync to avoid drift.
	parts = appendAtomIfFound(b, parts, "briefs/refinement/derived_rules.md")
	return parts
}

// appendAtomIfFound writes the named atom (with trailing newline pair)
// to b and appends the atom path to parts when readAtom returns a body
// without error. Missing atoms are silently skipped — composer parts
// list reflects only successfully-loaded atoms.
func appendAtomIfFound(b *strings.Builder, parts []string, atomPath string) []string {
	atom, err := readAtom(atomPath)
	if err != nil {
		return parts
	}
	b.WriteString(atom)
	b.WriteString("\n\n")
	return append(parts, atomPath)
}

// appendSynthesisWorkflowBlock loads the codebase-content synthesis
// workflow atom plus the citation-guides list that anchors the agent's
// cite-by-name pattern. Run-17 §6.4 — citation guides land right after
// synthesis_workflow.md so the agent can resolve "the X guide" inline.
func appendSynthesisWorkflowBlock(b *strings.Builder, parts []string) []string {
	atom, err := readAtom("briefs/codebase-content/synthesis_workflow.md")
	if err != nil {
		return parts
	}
	b.WriteString(atom)
	b.WriteString("\n\n")
	parts = append(parts, "briefs/codebase-content/synthesis_workflow.md")
	guides := citationGuides()
	if len(guides) == 0 {
		return parts
	}
	// Run-32 Step 3 — relabel from "Citation guides for this recipe"
	// (which read like a menu of options) to a lookup-key list with
	// explicit "never surface in published content" framing. The
	// original bullet header primed slug verbalization (Pattern #2):
	// agents read the slug list as English-cased candidates and
	// shipped `[managed NATS service]`, `[Zerops env-var model]` etc.
	// in published link text. Slug ids are zerops_knowledge LOOKUP
	// KEYS — never published as link labels or surface text.
	b.WriteString("### `zerops_knowledge` lookup keys (internal taxonomy — NEVER surface in published content)\n\n")
	b.WriteString("Use these slugs as the `query=` value when calling `zerops_knowledge`. They identify the embedded knowledge corpus topics; they are NOT terms the porter recognizes. In published prose, link text must name the SUBJECT, not the lookup key. Forbidden in any rendered content: backticked slug, slug-in-link-text, or English-cased translation (`[managed NATS service]`, `[Zerops env-var model]`).\n\n")
	for _, g := range guides {
		fmt.Fprintf(b, "- `%s`\n", g)
	}
	b.WriteString("\n")
	return append(parts, "citation-guides")
}

// writeWorkerKBSupplementsPointer emits the worker codebase-content
// reminder for the two MANDATORY worker-KB gotchas (queue-group +
// SIGTERM drain). Run-30 Fix #1 PARTIAL — the 3.6 KB inline atom moved
// off the codebase-content brief to keep the worker variant under the
// Read-tool 22K-token target. The matching code-shape teaching at
// `briefs/feature/worker_subscription_shape.md` already covers the
// mechanism for the feature pass; this pointer tells the KB-author to
// phrase the same two traps for the worker KB.
func writeWorkerKBSupplementsPointer(b *strings.Builder) {
	b.WriteString("### Worker KB — two mandatory gotchas (showcase + worker codebase)\n\n")
	b.WriteString("This codebase ships at `minContainers ≥ 2` from tier 4 onwards. The worker KB MUST author two symptom-first stems naming the multi-replica failure modes:\n\n")
	b.WriteString("1. **Queue-group / consumer-group semantics under multi-replica** — name the broker, the term \"queue group\" (or library equivalent), and \"per-replica\" or \"exactly-once\". Body shows the client option that sets the group (e.g. NATS `queue: 'workers'`).\n")
	b.WriteString("2. **Graceful SIGTERM drain** — name SIGTERM or \"drain\" or \"graceful shutdown\". Body shows the catch → `await sub.drain()` (NOT `unsubscribe()`) → exit sequence.\n\n")
	b.WriteString("Both items map to the `rolling-deploys` citation-map key (engine-internal routing). The porter-facing link text MUST NOT contain the raw slug-stem even with a `Zerops/managed` prefix or `reference/guide/service` suffix — replace with a porter-recognized concept (e.g. `[zero-downtime deploys with multi-container setups]`) or in-body completion. The matching SOURCE-CODE shape (`{queue: 'workers'}` + `await sub.drain()` on shutdown) was authored at the feature pass per `briefs/feature/worker_subscription_shape.md` and is enforced at codebase-content `complete-phase` by `gateWorkerSubscription`. Your job here is the KB prose only.\n\n")
	b.WriteString("Skip these only when (a) the worker shares a codebase with api/monolith or (b) the recipe explicitly downscales `minContainers` to 1 even at showcase tier (rare).\n\n")
}

// Run-22 followup F-6 — embedded-parent fallback framing strings.
// One per downstream phase. Each carries a `%s` placeholder for the
// parent slug. Framing rationale per phase:
//
//   - codebase-content: parent's IG/KB framing (what topics parent
//     already covers — cross-reference instead of re-author).
//   - env-content: tier-decision facts + per-tier voice inheritance
//     (align with parent's tier-flavor voice).
//   - refinement: parent's published surfaces (refinement HOLDS on
//     fragments that re-author parent material).
//
// Refinement uses a slightly different markdown shape (bold-strong
// header, no `##` heading) because refinement's parent block is
// nested inside a list-style "stitched-output / parent recipe"
// section pre-existing from run-17 §9.
//
// Run-40 ENG-3 — Run-22 fixup F-6 added concrete "Cross-reference
// shape" worked-example strings to the codebase-content and
// env-content framings. Run-39 forensics traced the resulting
// `See parent recipe <slug> for <topic>` prose all the way to the
// porter-facing apidev/README.md:351 heading
// (`### See parent recipe for foundational NestJS-on-Zerops traps`)
// — the exact shape refinement/synthesis_workflow.md:113-119 forbids
// as a V1 (porter-clones-and-runs) violation. The worked example
// turned out to be substrate-positive guidance loud enough to outrank
// the refinement-time warning; the agent followed what it was shown,
// not the conditional negative rule downstream. The framings now
// describe WHY parent matters and what to do (cross-reference instead
// of re-author) without handing the agent a forbidden template. The
// `Parent slug: %[1]s` knowledge-routing prefix is retained because
// subagents read it to decide whether to fetch the embedded baseline.
const (
	codebaseContentEmbeddedFraming = "Parent slug: %[1]s — read this for IG/KB framing inheritance " +
		"(what topics the parent already covers; cross-reference instead of " +
		"re-author when the parent already explains a mechanism).\n\n"

	envContentEmbeddedFraming = "Parent slug: %[1]s — read this for tier-decision and per-tier " +
		"framing inheritance. Align tier intros + import comments with the " +
		"parent's tier-flavor voice; don't re-author what the parent already " +
		"established.\n\n"

	refinementEmbeddedFraming = "Parent slug: %s. Read the embedded baseline before " +
		"flagging cross-recipe duplication; refinement HOLDS on any fragment " +
		"whose body would re-author what the parent already covers.\n\n"
)

// appendEmbeddedParentBaseline emits a parent-recipe-baseline section
// to b when filesystem-mounted parent is absent AND the slug has a
// recognized chain parent (`*-showcase` → `*-minimal`) AND the
// embedded recipe `.md` exists in `internal/knowledge/recipes/`.
// Returns true when the section was appended (caller should append
// the corresponding "embedded_parent_baseline" Parts entry). Returns
// false otherwise (filesystem parent present, no chain parent, or
// embedded `.md` missing).
//
// `framing` is a `fmt.Sprintf`-style string with one `%s` slot for
// the parent slug. `excerptCap` is the per-phase byte cap on the
// embedded body (1000 for codebase-content, 4000 for env-content +
// refinement; see per-phase rationale in each composer). Section
// header is fixed: `## Parent recipe baseline (embedded)` for
// composer phases that emit Markdown headings; refinement uses a
// bold-paragraph variant via `appendEmbeddedParentBaselineRefinement`
// so it nests cleanly inside the run-17 stitched-output list.
func appendEmbeddedParentBaseline(b *strings.Builder, slug string, parent *ParentRecipe, framing string, excerptCap int) bool {
	if parent != nil && parent.SourceRoot != "" {
		return false
	}
	body, parentSlug, ok := embeddedParentBody(slug, parent)
	if !ok {
		return false
	}
	b.WriteString("## Parent recipe baseline (embedded)\n\n")
	fmt.Fprintf(b, framing, parentSlug)
	b.WriteString("```md\n")
	excerpt := excerptREADME(body, excerptCap)
	b.WriteString(excerpt)
	if !strings.HasSuffix(excerpt, "\n") {
		b.WriteByte('\n')
	}
	b.WriteString("```\n\n")
	return true
}

// appendEmbeddedParentBaselineRefinement is the refinement-shape
// variant of appendEmbeddedParentBaseline — bold-paragraph header
// (no `##`) so the block nests inside refinement's stitched-output
// pointer list authored by run-17 §9. Returns true when appended.
func appendEmbeddedParentBaselineRefinement(b *strings.Builder, slug string, parent *ParentRecipe, framing string, excerptCap int) bool {
	if parent != nil && parent.SourceRoot != "" {
		return false
	}
	body, parentSlug, ok := embeddedParentBody(slug, parent)
	if !ok {
		return false
	}
	b.WriteString("**Parent recipe baseline (embedded)**\n\n")
	fmt.Fprintf(b, framing, parentSlug)
	b.WriteString("```md\n")
	excerpt := excerptREADME(body, excerptCap)
	b.WriteString(excerpt)
	if !strings.HasSuffix(excerpt, "\n") {
		b.WriteByte('\n')
	}
	b.WriteString("```\n\n")
	return true
}

// embeddedParentBody returns the embedded parent recipe `.md` body
// for slug. Prefers parent.EmbeddedBody when the caller pre-loaded
// it (sess.LoadParent → cached on session); falls back to
// loadEmbeddedRecipeMD for callers that bypassed the session
// resolver (tests, single-file BuildScaffoldBrief path). Returns
// (body, parentSlug, true) on hit; (_, _, false) when slug has no
// chain parent or the embedded corpus has no published `.md`.
//
// Run-40 post-ship — consolidates the embedded-body lookup so brief
// composers stop re-walking the embed.FS once the session's already
// resolved the parent.
func embeddedParentBody(slug string, parent *ParentRecipe) (string, string, bool) {
	if parent != nil && parent.IsEmbedded() && parent.EmbeddedBody != "" {
		return parent.EmbeddedBody, parent.Slug, true
	}
	parentSlug := parentSlugFor(slug)
	if parentSlug == "" {
		return "", "", false
	}
	body, err := loadEmbeddedRecipeMD(parentSlug)
	if err != nil {
		return "", "", false
	}
	return body, parentSlug, true
}
