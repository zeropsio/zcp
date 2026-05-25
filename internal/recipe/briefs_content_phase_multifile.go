package recipe

import (
	"errors"
	"fmt"
	"strings"
)

// Run-31 Fix #1 closure — multi-file composers for codebase-content,
// env-content, and refinement.
//
// The legacy single-file composers (BuildCodebaseContentBrief +
// BuildEnvContentBrief in briefs_content_phase.go; BuildRefinementBrief
// in briefs_refinement.go) accumulate everything into a single
// strings.Builder; the multi-file composers below split that into N
// part files at semantic boundaries (phase entry, platform, cross-
// service, yaml style, context). Each part is bounded by
// PerPartTokenCeiling; the brief as a whole is unbounded.
//
// The dispatch handler at handlers.go::handleBuildSubagentPrompt
// branches on briefKind: codebase-content / env-content / refinement
// route to the multi-file path; the others (scaffold, feature,
// finalize, claudemd-author) stay on the legacy single-file path.

// buildCodebaseContentBriefMultiFile composes the codebase-content
// brief as an index.md plus N part files persisted under
// `<outputRoot>/.briefs/codebase-content-<cb>/`. Returns a Brief
// whose IndexPath + PartPaths are populated; Body is empty for the
// multi-file shape. The header parameter is rendered above the index's
// "Read order" section (recipe-level context, codebase identity); the
// closingNotes are rendered below.
//
// outputRoot is the session output root; the persist machinery creates
// <outputRoot>/.briefs/<dirName>/ and writes each part atomically.
//
// Part split (codebase-content):
//  1. part-1-phase-entry      — phase_entry/codebase-content.md + worker KB pointer (when applicable)
//  2. part-2-synthesis        — synthesis_workflow + citation guides
//  3. part-3-platform         — platform_principles
//  4. part-4-cross-service    — nats-shapes + cross-service-urls-summary (conditional)
//  5. part-5-yaml-style       — zerops-knowledge-attestation + yaml-comment-style
//  6. part-6-context          — codebase metadata + filtered facts + cross-codebase facts + pointer block + embedded parent baseline + sibling note
//
// Splitting phase-entry from synthesis-workflow keeps the largest atom
// (synthesis_workflow.md ~35 KB) in its own part with headroom; pre-split
// the worker variant lands at ~21.4K tokens, only ~600 below the per-part
// ceiling. With the split each part has ≥4K-token headroom for future
// teaching additions.
//
// When part-6 facts overflow the cap, facts split into a dedicated
// `facts` part and the remainder follows on `context-tail` (post-facts)
// or `context-tail-cross` (post-cross-codebase-facts). Two distinct
// suffix slugs because the same composer call can fire both recovery
// paths and the part-N filename doesn't disambiguate semantics on its
// own — Fix #5 of the run-31 multi-file review.
// parent is currently nil for every test caller; the parameter mirrors
// the production WithFraming variant so test fixtures stay aligned
// with the production signature without a wrapping shim.
//
//nolint:unparam // see comment above.
func buildCodebaseContentBriefMultiFile(plan *Plan, cb Codebase, parent *ParentRecipe, facts []FactRecord, outputRoot string) (Brief, error) {
	return buildCodebaseContentBriefMultiFileWithFraming(plan, cb, parent, facts, outputRoot, "", "")
}

// buildCodebaseContentBriefMultiFileWithFraming is the production
// variant called by the dispatch handler — header + closingNotes are
// non-empty (recipe-level context + close-phase invocation rendered
// above and below the read-order section in index.md).
func buildCodebaseContentBriefMultiFileWithFraming(plan *Plan, cb Codebase, parent *ParentRecipe, facts []FactRecord, outputRoot, header, closingNotes string) (Brief, error) {
	if plan == nil {
		return Brief{}, errors.New("nil plan")
	}
	if cb.Hostname == "" {
		return Brief{}, errors.New("codebase hostname empty")
	}
	if outputRoot == "" {
		return Brief{}, errors.New("multi-file brief requires outputRoot")
	}

	w := newPartWriter()

	// Part 1 — phase entry + worker KB pointer (when applicable).
	if err := w.StartPart("phase-entry", "phase entry — what this dispatch authors and how to dispatch"); err != nil {
		return Brief{}, err
	}
	if err := w.WriteRequiredAtom("phase_entry/codebase-content.md"); err != nil {
		return Brief{}, err
	}
	if plan.Tier == tierShowcase && cb.IsWorker {
		if err := w.Write(workerKBSupplementsPointerBody()); err != nil {
			return Brief{}, err
		}
	}

	// Part 2 — synthesis workflow + citation guides. Split off
	// phase-entry because synthesis_workflow.md alone is ~35 KB; combined
	// with phase-entry it leaves only ~600 tokens of headroom on the
	// worker variant.
	if err := w.StartPart("synthesis", "synthesis workflow rules + citation guides"); err != nil {
		return Brief{}, err
	}
	if err := writeSynthesisWorkflowBlockToPart(w); err != nil {
		return Brief{}, err
	}

	// Part 3 — platform principles.
	if err := w.StartPart("platform", "platform principles (env-var aliasing, project-vs-service scope, same-key trap)"); err != nil {
		return Brief{}, err
	}
	if err := w.WriteOptionalAtom("briefs/scaffold/platform_principles.md"); err != nil {
		return Brief{}, err
	}

	// Part 4 — cross-service (NATS shapes + cross-service URL summary).
	// Both conditional; if both are gated off, the part is omitted
	// entirely.
	loadNats := shouldLoadNATSShapes(plan, cb)
	loadURLs := shouldLoadCrossServiceURLs(cb)
	if loadNats || loadURLs {
		if err := w.StartPart("cross-service", "cross-service teaching (NATS shapes, URL aliasing summary)"); err != nil {
			return Brief{}, err
		}
		if loadNats {
			if err := w.WriteOptionalAtom("principles/nats-shapes.md"); err != nil {
				return Brief{}, err
			}
		}
		if loadURLs {
			if err := w.WriteOptionalAtom("principles/cross-service-urls-summary.md"); err != nil {
				return Brief{}, err
			}
		}
	}

	// Part 5 — yaml comment style + Zerops-knowledge attestation.
	if err := w.StartPart("yaml-style", "Zerops-knowledge attestation + yaml-comment style rules"); err != nil {
		return Brief{}, err
	}
	if err := w.WriteOptionalAtom("principles/zerops-knowledge-attestation.md"); err != nil {
		return Brief{}, err
	}
	if err := w.WriteOptionalAtom("principles/yaml-comment-style.md"); err != nil {
		return Brief{}, err
	}

	// Part 5b/5c — naming + rules-from-goldens parts. Extracted to keep
	// buildCodebaseContentBriefMultiFileWithFraming under the maintidx
	// threshold after run-33 fix #1 added the rules part.
	if err := writeNamingAndRulesParts(w); err != nil {
		return Brief{}, err
	}

	// Part 6 — context (codebase metadata, facts, parent baseline,
	// pointer block, sibling note). When facts overflow, split.
	if err := w.StartPart("context", "codebase metadata, filtered facts, parent recipe baseline, sibling note"); err != nil {
		return Brief{}, err
	}
	if err := w.Write(renderCodebaseMetadata(cb)); err != nil {
		return Brief{}, err
	}

	// Filtered facts — try to fit on the context part; if cap fires,
	// move to a dedicated `facts` part and continue context on
	// `context-tail`.
	cbFacts := filterOutEngineEmitted(FilterByCodebase(facts, cb.Hostname))
	postFactsTailOpened := false
	if len(cbFacts) > 0 {
		factsBody := renderRecordedFactsBlock(cbFacts)
		if err := w.Write(factsBody); errors.Is(err, ErrPartCapExceeded) {
			// Split: facts get their own part.
			if startErr := w.StartPart("facts", "recorded facts in this codebase's scope"); startErr != nil {
				return Brief{}, startErr
			}
			if wErr := w.Write(factsBody); wErr != nil {
				return Brief{}, fmt.Errorf("facts overflow even in dedicated part: %w", wErr)
			}
			// Continue context in a fresh part.
			if startErr := w.StartPart("context-tail", "parent recipe baseline, pointer block, sibling note"); startErr != nil {
				return Brief{}, startErr
			}
			postFactsTailOpened = true
		} else if err != nil {
			return Brief{}, err
		}
	}

	// Cross-codebase facts. WriteOrSplit opens a fresh part when the
	// current one (which may already be a post-facts context-tail) is
	// full. The fresh-part slug is `context-tail-cross` to keep slug
	// semantics distinct from the post-facts `context-tail` — Fix #5.
	if cross := renderCrossCodebaseFacts(facts, cb); cross != "" {
		freshSlug := "context-tail"
		freshDesc := "cross-codebase facts, parent baseline, pointer block, sibling note"
		if postFactsTailOpened {
			freshSlug = "context-tail-cross"
			freshDesc = "cross-codebase facts (overflow continuation)"
		}
		if err := w.WriteOrSplit(freshSlug, freshDesc, cross); err != nil {
			return Brief{}, fmt.Errorf("cross-codebase facts overflow: %w", err)
		}
	}

	// Pointer block. Use WriteOrSplit so an already-saturated context
	// part doesn't silently drop the pointer body.
	if err := w.WriteOrSplit("context-pointer", "on-disk content pointer block", renderCodebaseContentPointerBlock(cb, parent)); err != nil {
		return Brief{}, fmt.Errorf("pointer block overflow: %w", err)
	}

	// Embedded parent baseline (run-22 followup F-6 — only fires when
	// the chain resolver returns no parent and the slug has a chain
	// parent).
	if body := renderEmbeddedParentBaseline(plan.Slug, parent, codebaseContentEmbeddedFraming, 1000); body != "" {
		if err := w.WriteOrSplit("context-parent", "embedded parent recipe baseline", body); err != nil {
			return Brief{}, fmt.Errorf("embedded parent baseline overflow: %w", err)
		}
	}

	// Sibling sub-agent note.
	if err := w.WriteOrSplit("context-sibling", "sibling claudemd-author sub-agent note", siblingClaudeMDNote()); err != nil {
		return Brief{}, fmt.Errorf("sibling note overflow: %w", err)
	}

	indexPath, partPaths, _, err := w.Persist(outputRoot, header, closingNotes, BriefCodebaseContent, cb.Hostname)
	if err != nil {
		return Brief{}, err
	}

	brief := Brief{
		Kind:      BriefCodebaseContent,
		IndexPath: indexPath,
		PartPaths: partPaths,
	}
	if vErr := brief.Validate(); vErr != nil {
		return Brief{}, vErr
	}
	return brief, nil
}

// buildEnvContentBriefMultiFile composes the env-content brief as
// index.md + N part files. Same shape contract as
// buildCodebaseContentBriefMultiFile.
//
// Part split (env-content):
//  1. part-1-phase-entry   — phase_entry + per_tier_authoring
//  2. part-2-cross-service — cross-service-urls + nats-shapes (conditional)
//  3. part-3-yaml-style    — zerops-knowledge-attestation + yaml-comment-style
//  4. part-4-context       — capability matrix + cross-tier deltas + emitted facts + contracts + plan snapshot + parent pointer
func buildEnvContentBriefMultiFile(plan *Plan, parent *ParentRecipe, facts []FactRecord, outputRoot string) (Brief, error) {
	return buildEnvContentBriefMultiFileWithFraming(plan, parent, facts, outputRoot, "", "")
}

func buildEnvContentBriefMultiFileWithFraming(plan *Plan, parent *ParentRecipe, facts []FactRecord, outputRoot, header, closingNotes string) (Brief, error) {
	if plan == nil {
		return Brief{}, errors.New("nil plan")
	}
	if outputRoot == "" {
		return Brief{}, errors.New("multi-file brief requires outputRoot")
	}

	w := newPartWriter()

	if err := w.StartPart("phase-entry", "phase entry + per-tier authoring rules"); err != nil {
		return Brief{}, err
	}
	if err := w.WriteOptionalAtom("phase_entry/env-content.md"); err != nil {
		return Brief{}, err
	}
	if err := w.WriteOptionalAtom("briefs/env-content/per_tier_authoring.md"); err != nil {
		return Brief{}, err
	}

	// Part 2 — cross-service. cross-service-urls.md is loaded
	// unconditionally per Run-22 F-4; nats-shapes.md gates on
	// planUsesNATS.
	if err := w.StartPart("cross-service", "cross-service URL teaching + NATS shapes"); err != nil {
		return Brief{}, err
	}
	if err := w.WriteOptionalAtom("principles/cross-service-urls.md"); err != nil {
		return Brief{}, err
	}
	if planUsesNATS(plan) {
		if err := w.WriteOptionalAtom("principles/nats-shapes.md"); err != nil {
			return Brief{}, err
		}
	}

	// Part 3 — yaml style.
	if err := w.StartPart("yaml-style", "Zerops-knowledge attestation + yaml-comment style rules"); err != nil {
		return Brief{}, err
	}
	if err := w.WriteOptionalAtom("principles/zerops-knowledge-attestation.md"); err != nil {
		return Brief{}, err
	}
	if err := w.WriteOptionalAtom("principles/yaml-comment-style.md"); err != nil {
		return Brief{}, err
	}

	// Part 3b/3c — naming + rules-from-goldens parts. Same reasoning as
	// codebase-content's Part 5b/5c — keep the env-content composer
	// under the maintidx threshold after run-33 fix #1.
	if err := writeNamingAndRulesParts(w); err != nil {
		return Brief{}, err
	}

	// Run-40 S-3 — env-content-only managed-service `<host>` mapping.
	// Atom is inlined onto the existing naming part — the trap the atom
	// pre-empts is env-content-specific (env/<N>/import-comments/<host>
	// fragment ids), so this teaching doesn't belong on the
	// codebase-content composer that shares writeNamingAndRulesParts.
	if err := w.WriteOptionalAtom("briefs/env-content/managed_service_host_mapping.md"); err != nil {
		return Brief{}, err
	}

	// Part 4 — context. WriteOrSplit handles overflow into a fresh
	// `context-tail` part. Practically the env-content context fits
	// comfortably under the cap in the run-31 fixture (~10 KB), but the
	// defensive split keeps the cap path covered for future fact-volume
	// growth.
	if err := w.StartPart("context", "tier capability matrix, cross-tier deltas, contracts, plan snapshot"); err != nil {
		return Brief{}, err
	}
	contextBody := renderEnvContentContextBlock(plan, parent, facts)
	if err := w.Write(contextBody); errors.Is(err, ErrPartCapExceeded) {
		if startErr := w.StartPart("context-tail", "engine-emitted facts + cross-codebase contracts + plan snapshot"); startErr != nil {
			return Brief{}, startErr
		}
		if wErr := w.Write(contextBody); wErr != nil {
			return Brief{}, fmt.Errorf("env-content context overflow: %w", wErr)
		}
	} else if err != nil {
		return Brief{}, err
	}

	indexPath, partPaths, _, err := w.Persist(outputRoot, header, closingNotes, BriefEnvContent, "")
	if err != nil {
		return Brief{}, err
	}
	brief := Brief{
		Kind:      BriefEnvContent,
		IndexPath: indexPath,
		PartPaths: partPaths,
	}
	if vErr := brief.Validate(); vErr != nil {
		return Brief{}, vErr
	}
	return brief, nil
}

// buildRefinementBriefMultiFile composes the refinement brief as
// index.md + N part files.
//
// Part split (refinement):
//  1. part-1-phase-entry        — phase_entry
//  2. part-2-synthesis          — synthesis_workflow
//  3. part-3-rules-from-goldens — derived_rules (golden-grounded rule substrate; replaced legacy embedded_rubric in run-33 fix #2)
//  4. part-4-references         — fetchable reference catalog + stitched-output pointer block
//  5. part-5-context            — embedded parent baseline + engine-flagged suspects
//  6. part-6-facts              — recorded facts (most-recent-first with eviction)
func buildRefinementBriefMultiFile(plan *Plan, parent *ParentRecipe, runDir string, facts []FactRecord, outputRoot string) (Brief, error) {
	return buildRefinementBriefMultiFileWithFraming(plan, parent, runDir, facts, outputRoot, "", "")
}

// BuildRefinementBriefMultiFile is the exported entry the
// `cmd/zcp-recipe-sim` tool calls so offline replay produces the
// byte-identical multi-file shape (`<outputRoot>/.briefs/refinement-
// phase/`: index.md + N part-*.md) that the production refinement
// dispatch produces. Closes the SIM DIVERGENCE pinned by run-31 review
// (production routes refinement through the multi-file composer; sim
// previously called the legacy single-file BuildRefinementBrief and
// emitted one inline prompt). The header/footer parameters mirror the
// production WithFraming variant — sim callers pass sim-flavored
// header (recipe context) and footer (closing notes minus the MCP
// `complete-phase` step that has no replay equivalent); production
// callers go through buildSubagentDispatchForPhase which composes
// header/footer from composePromptHeaderAndFooter.
func BuildRefinementBriefMultiFile(plan *Plan, parent *ParentRecipe, runDir string, facts []FactRecord, outputRoot, header, closingNotes string) (Brief, error) {
	return buildRefinementBriefMultiFileWithFraming(plan, parent, runDir, facts, outputRoot, header, closingNotes)
}

func buildRefinementBriefMultiFileWithFraming(plan *Plan, parent *ParentRecipe, runDir string, facts []FactRecord, outputRoot, header, closingNotes string) (Brief, error) {
	if plan == nil {
		return Brief{}, errors.New("nil plan")
	}
	if outputRoot == "" {
		return Brief{}, errors.New("multi-file brief requires outputRoot")
	}

	w := newPartWriter()

	// Part 1 — phase entry. Refinement phase_entry alone is ~5 KB.
	if err := w.StartPart("phase-entry", "phase entry — what refinement does + the threshold rule"); err != nil {
		return Brief{}, err
	}
	if err := w.WriteOptionalAtom("phase_entry/refinement.md"); err != nil {
		return Brief{}, err
	}

	// Part 2 — synthesis workflow (~17 KB).
	if err := w.StartPart("synthesis", "synthesis workflow — the eight refinement actions"); err != nil {
		return Brief{}, err
	}
	if err := w.WriteOptionalAtom("briefs/refinement/synthesis_workflow.md"); err != nil {
		return Brief{}, err
	}

	// Part 3 — derived rules atom. Run-33 architectural fix #2 retired
	// the legacy embedded_rubric.md (deleted in the run-34 follow-up);
	// rule-walk against the stitched output is the sole scoring
	// substrate. Golden-grounded principle-shaped rules extracted from
	// laravel-jetstream + laravel-showcase.
	if err := w.StartPart("rules-from-goldens", "golden-grounded rule substrate — principle-shaped scoring rules"); err != nil {
		return Brief{}, err
	}
	if err := w.WriteOptionalAtom("briefs/refinement/derived_rules.md"); err != nil {
		return Brief{}, err
	}

	// Part 4 — fetchable references + stitched-output pointer block.
	if err := w.StartPart("references", "fetchable reference catalog + stitched-output pointer block"); err != nil {
		return Brief{}, err
	}
	if err := w.Write(renderRefinementReferenceCatalog()); err != nil {
		return Brief{}, err
	}
	if body := renderRefinementStitchedPointerBlock(plan, parent, runDir); body != "" {
		if err := w.Write(body); err != nil {
			return Brief{}, err
		}
	}

	// Part 5 — parent baseline + engine-flagged suspects (~5-10 KB).
	if err := w.StartPart("context", "parent recipe baseline + engine-flagged suspects (investigate at minimum these)"); err != nil {
		return Brief{}, err
	}
	if body := renderEmbeddedParentBaselineRefinement(plan.Slug, parent, refinementEmbeddedFraming, 4000); body != "" {
		if err := w.Write(body); err != nil {
			return Brief{}, err
		}
	}
	suspects := CollectRefinementSuspects(plan, nil)
	if formatted := FormatRefinementSuspects(suspects); formatted != "" {
		if err := w.Write(formatted); err != nil {
			return Brief{}, err
		}
	}

	// Part 6 — recorded facts. Always its own part — refinement
	// historically blew the cap because facts grew unbounded across the
	// run; multi-file shape isolates them so the per-fact eviction
	// allocates against PartFileCap, not against budget-minus-rubrics.
	filteredFacts := facts[:0:0]
	for _, f := range facts {
		if FactBelongsToCodebases(f, plan.Codebases) {
			filteredFacts = append(filteredFacts, f)
		}
	}
	if len(filteredFacts) > 0 {
		if err := w.StartPart("facts", "recorded facts (most-recent-first, oldest evicted to disk if cap fires)"); err != nil {
			return Brief{}, err
		}
		factsBuf, _ := composeRefinementFactsBlock(filteredFacts, PartFileCap)
		if factsBuf != "" {
			if wErr := w.Write(factsBuf); wErr != nil {
				return Brief{}, fmt.Errorf("refinement facts overflow even in dedicated part: %w", wErr)
			}
		}
	}

	// Run-40 ENG-4 + A1 — multi-file composer parity. Mirrors the
	// single-file BuildRefinementBrief path so refinement's rule-walk
	// reads the canonical-latest fact view AND the canonical
	// NamedConstants store. Codex code review caught the missing
	// wiring in the multi-file path that production dispatch routes
	// through. Pinned by TestBuildRefinementBriefMultiFile_*Carries*.
	if err := emitCanonicalAndNamedConstantsPart(w, filteredFacts, plan); err != nil {
		return Brief{}, err
	}

	indexPath, partPaths, _, err := w.Persist(outputRoot, header, closingNotes, BriefRefinement, "")
	if err != nil {
		return Brief{}, err
	}
	brief := Brief{
		Kind:      BriefRefinement,
		IndexPath: indexPath,
		PartPaths: partPaths,
	}
	if vErr := brief.Validate(); vErr != nil {
		return Brief{}, vErr
	}
	return brief, nil
}

// writeSynthesisWorkflowBlockToPart loads the codebase-content
// synthesis workflow atom plus the citation-guides list. Mirrors
// appendSynthesisWorkflowBlock but writes through the partWriter.
func writeSynthesisWorkflowBlockToPart(w *partWriter) error {
	if err := w.WriteRequiredAtom("briefs/codebase-content/synthesis_workflow.md"); err != nil {
		return err
	}
	guides := citationGuides()
	if len(guides) == 0 {
		return nil
	}
	var b strings.Builder
	// Run-32 Step 3 — relabel from "Citation guides for this recipe" to
	// lookup-key list with "never surface in published content" framing.
	// Closes the Pattern #2 priming vector (English-cased slug
	// verbalization). Mirrored in briefs_content_phase.go.
	b.WriteString("### `zerops_knowledge` lookup keys (internal taxonomy — NEVER surface in published content)\n\n")
	b.WriteString("Use these slugs as the `query=` value when calling `zerops_knowledge`. They identify the embedded knowledge corpus topics; they are NOT terms the porter recognizes. In published prose, link text must name the SUBJECT, not the lookup key. Forbidden in any rendered content: backticked slug, slug-in-link-text, or English-cased translation (`[managed NATS service]`, `[Zerops env-var model]`).\n\n")
	for _, g := range guides {
		fmt.Fprintf(&b, "- `%s`\n", g)
	}
	b.WriteString("\n")
	return w.Write(b.String())
}

// renderCodebaseMetadata returns the codebase-metadata block (hostname,
// role, base runtime, source root). Pure function — same string for the
// single-file and multi-file composers.
func renderCodebaseMetadata(cb Codebase) string {
	var b strings.Builder
	b.WriteString("## Codebase\n\n")
	fmt.Fprintf(&b, "- Hostname: %s\n", cb.Hostname)
	fmt.Fprintf(&b, "- Role: %s\n", cb.Role)
	fmt.Fprintf(&b, "- Base runtime: %s\n", cb.BaseRuntime)
	fmt.Fprintf(&b, "- Source root: %s\n", cb.SourceRoot)
	if cb.HasInitCommands {
		b.WriteString("- HasInitCommands: true (zerops.yaml authors initCommands)\n")
	}
	b.WriteString("\n")
	return b.String()
}

// renderRecordedFactsBlock returns the per-codebase recorded-facts
// section. Pure function.
func renderRecordedFactsBlock(cbFacts []FactRecord) string {
	if len(cbFacts) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Recorded facts (codebase scope)\n\n")
	for _, f := range cbFacts {
		writeFactSummary(&b, f)
	}
	b.WriteString("\n")
	return b.String()
}

// renderCrossCodebaseFacts returns the cross-codebase facts section
// when applicable. Returns "" when no cross-codebase facts apply.
func renderCrossCodebaseFacts(facts []FactRecord, cb Codebase) string {
	crossFacts := filterOutEngineEmitted(CrossCodebaseManagedServiceFacts(facts, cb))
	if len(crossFacts) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Cross-codebase managed-service facts\n\n")
	b.WriteString("Facts authored by sister codebases that reference managed services this codebase also consumes. Engine-propagated so connection-shape decisions stay consistent across codebases — when a sister codebase recorded that a specific env-key shape crashed at boot, this codebase's IG/KB must not endorse that shape. Treat each entry as authoritative for THIS recipe; the atom corpus's generic guidance defers to the run's recorded scaffold findings.\n\n")
	for _, f := range crossFacts {
		writeFactSummary(&b, f)
	}
	b.WriteString("\n")
	return b.String()
}

// renderCodebaseContentPointerBlock returns the on-disk pointer block.
func renderCodebaseContentPointerBlock(cb Codebase, parent *ParentRecipe) string {
	var b strings.Builder
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
	return b.String()
}

// renderEmbeddedParentBaseline returns the embedded-parent baseline
// section body when applicable. Same gate as appendEmbeddedParentBaseline
// but returns the rendered body so the caller can route it to a
// partWriter (or any other Builder).
func renderEmbeddedParentBaseline(slug string, parent *ParentRecipe, framing string, excerptCap int) string {
	if parent != nil && parent.SourceRoot != "" {
		return ""
	}
	body, parentSlug, ok := embeddedParentBody(slug, parent)
	if !ok {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Parent recipe baseline (embedded)\n\n")
	fmt.Fprintf(&b, framing, parentSlug)
	b.WriteString("```md\n")
	excerpt := excerptREADME(body, excerptCap)
	b.WriteString(excerpt)
	if !strings.HasSuffix(excerpt, "\n") {
		b.WriteByte('\n')
	}
	b.WriteString("```\n\n")
	return b.String()
}

// renderEmbeddedParentBaselineRefinement is the refinement-shape
// variant — bold-paragraph header so it nests inside the run-17
// stitched-output list authored elsewhere.
func renderEmbeddedParentBaselineRefinement(slug string, parent *ParentRecipe, framing string, excerptCap int) string {
	if parent != nil && parent.SourceRoot != "" {
		return ""
	}
	body, parentSlug, ok := embeddedParentBody(slug, parent)
	if !ok {
		return ""
	}
	var b strings.Builder
	b.WriteString("**Parent recipe baseline (embedded)**\n\n")
	fmt.Fprintf(&b, framing, parentSlug)
	b.WriteString("```md\n")
	excerpt := excerptREADME(body, excerptCap)
	b.WriteString(excerpt)
	if !strings.HasSuffix(excerpt, "\n") {
		b.WriteByte('\n')
	}
	b.WriteString("```\n\n")
	return b.String()
}

// siblingClaudeMDNote returns the sibling sub-agent note.
func siblingClaudeMDNote() string {
	var b strings.Builder
	b.WriteString("## A sibling claudemd-author sub-agent authors CLAUDE.md in parallel\n\n")
	b.WriteString("You do NOT author CLAUDE.md content. If you encounter Zerops-platform content that belongs in a porter dev guide, check whether it actually belongs in IG/KB/zerops.yaml comments instead — those are your surfaces.\n\n")
	return b.String()
}

// workerKBSupplementsPointerBody returns the inline worker-KB pointer
// (run-30 Fix #1 PARTIAL — moved off the codebase-content brief inline
// for byte budget; multi-file architecture restores the full teaching
// at no cap cost, but keeping the pointer pattern preserves the
// agent-side teaching).
func workerKBSupplementsPointerBody() string {
	var b strings.Builder
	b.WriteString("### Worker KB — two mandatory gotchas (showcase + worker codebase)\n\n")
	b.WriteString("This codebase ships at `minContainers ≥ 2` from tier 4 onwards. The worker KB MUST author two symptom-first stems naming the multi-replica failure modes:\n\n")
	b.WriteString("1. **Queue-group / consumer-group semantics under multi-replica** — name the broker, the term \"queue group\" (or library equivalent), and \"per-replica\" or \"exactly-once\". Body shows the client option that sets the group (e.g. NATS `queue: 'workers'`).\n")
	b.WriteString("2. **Graceful SIGTERM drain** — name SIGTERM or \"drain\" or \"graceful shutdown\". Body shows the catch → `await sub.drain()` (NOT `unsubscribe()`) → exit sequence.\n\n")
	b.WriteString("Both items map to the `rolling-deploys` citation-map key (engine-internal routing). The porter-facing link text MUST NOT contain the raw slug-stem even with a `Zerops/managed` prefix or `reference/guide/service` suffix — replace with a porter-recognized concept (e.g. `[zero-downtime deploys with multi-container setups]`) or in-body completion. The matching SOURCE-CODE shape (`{queue: 'workers'}` + `await sub.drain()` on shutdown) was authored at the feature pass per `briefs/feature/worker_subscription_shape.md` and is enforced at codebase-content `complete-phase` by `gateWorkerSubscription`. Your job here is the KB prose only.\n\n")
	b.WriteString("Skip these only when (a) the worker shares a codebase with api/monolith or (b) the recipe explicitly downscales `minContainers` to 1 even at showcase tier (rare).\n\n")
	return b.String()
}

// emitCanonicalAndNamedConstantsPart opens a dedicated part on w and
// emits the run-40 ENG-4 canonical-latest section + A1 named-constants
// section when either has content to render. No-op when neither
// applies. Helper for the multi-file refinement composer (which
// otherwise can't share the env-content context renderer because the
// part-writer interface differs). Run-40 fix-up #1.
func emitCanonicalAndNamedConstantsPart(w *partWriter, facts []FactRecord, plan *Plan) error {
	aliased := AliasedCanonicalTopics(facts)
	hasConstants := plan != nil && len(plan.NamedConstants) > 0
	if len(aliased) == 0 && !hasConstants {
		return nil
	}
	if err := w.StartPart("canonical-and-constants", "canonical-latest facts + named constants (cross-codebase source-of-truth)"); err != nil {
		return err
	}
	var b strings.Builder
	appendCanonicalLatestSection(&b, facts)
	appendNamedConstantsSection(&b, plan)
	if b.Len() == 0 {
		return nil
	}
	return w.Write(b.String())
}

// renderEnvContentContextBlock returns the env-content context section
// (capability matrix + cross-tier deltas + emitted facts + contracts +
// plan snapshot + parent pointer). Pure function shared by single-file
// and multi-file composers.
func renderEnvContentContextBlock(plan *Plan, parent *ParentRecipe, facts []FactRecord) string {
	var b strings.Builder

	b.WriteString("## Per-tier capability matrix\n\n")
	tiers := Tiers()
	for _, t := range tiers {
		fmt.Fprintf(&b, "- Tier %d (%s): mode=%s, runtime min containers=%d, cpu=%s, runtime min RAM=%g GB, managed min RAM=%g GB\n",
			t.Index, t.Label, t.ServiceMode, t.RuntimeMinContainers, ifEmpty(t.CPUMode, "SHARED"), t.RuntimeMinRAM, t.ManagedMinRAM)
	}
	b.WriteString("\n## Cross-tier deltas (tiers.go::Diff)\n\n")
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

	emitted := EmittedTierDecisionFacts(plan)
	if len(emitted) > 0 {
		b.WriteString("## Engine-emitted tier_decision facts\n\n")
		for _, f := range emitted {
			fmt.Fprintf(&b, "- topic=%s | tier=%d | service=%s | %s=%s | %s\n",
				f.Topic, f.Tier, f.Service, f.FieldPath, f.ChosenValue, f.TierContext)
		}
		b.WriteString("\nExtend `TierContext` via `fill-fact-slot` when the auto-derived prose is too thin.\n\n")
	}

	contracts := FilterByKind(facts, FactKindContract)
	if len(contracts) > 0 {
		b.WriteString("## Cross-codebase contracts\n\n")
		for _, f := range contracts {
			writeFactSummary(&b, f)
		}
		b.WriteString("\n")
	}

	// Run-40 ENG-4 + A1 — multi-file composer parity. The single-file
	// BuildEnvContentBrief path (used only by tests) emits these via
	// appendCanonicalLatestSection + appendNamedConstantsSection;
	// production env-content dispatch routes through this multi-file
	// path so both sections MUST emit here too or the gates fire
	// against substrate the agent never saw. Codex code review caught
	// this as the primary "tests pass, production broken" failure
	// mode — pinned by TestRenderEnvContentContextBlock_*.
	appendCanonicalLatestSection(&b, facts)
	appendNamedConstantsSection(&b, plan)

	b.WriteString("## Plan snapshot\n\n")
	for _, cb := range plan.Codebases {
		fmt.Fprintf(&b, "- codebase: %s (role=%s, runtime=%s)\n", cb.Hostname, cb.Role, cb.BaseRuntime)
	}
	for _, svc := range plan.Services {
		fmt.Fprintf(&b, "- service: %s (kind=%s, type=%s)\n", svc.Hostname, svc.Kind, svc.Type)
	}
	b.WriteString("\n")

	if parent != nil && parent.Slug != "" && parent.SourceRoot != "" {
		fmt.Fprintf(&b, "## Parent recipe `%s`\n\nRead `%s/...` and cross-reference parent's per-tier intros instead of re-authoring.\n\n", parent.Slug, parent.SourceRoot)
	} else if body := renderEmbeddedParentBaseline(plan.Slug, parent, envContentEmbeddedFraming, 4000); body != "" {
		b.WriteString(body)
	}

	return b.String()
}

// renderRefinementReferenceCatalog returns the fetchable-reference
// catalog block.
func renderRefinementReferenceCatalog() string {
	var b strings.Builder
	b.WriteString("## Reference atoms — fetch on demand\n\n")
	b.WriteString("When investigating a suspect, fetch the matching reference atom via:\n\n")
	b.WriteString("    zerops_knowledge uri=zerops://themes/refinement-references/<name>\n\n")
	for _, ref := range refinementReferenceCatalog {
		fmt.Fprintf(&b, "- `%s` — %s\n", ref.uri, ref.desc)
	}
	b.WriteByte('\n')
	return b.String()
}

// renderRefinementStitchedPointerBlock returns the stitched-output
// pointer-block body.
func renderRefinementStitchedPointerBlock(plan *Plan, parent *ParentRecipe, runDir string) string {
	hasStitchedBody := runDir != "" || (parent != nil && parent.Slug != "" && parent.SourceRoot != "")
	if !hasStitchedBody {
		return ""
	}
	// Run-40 S-3 — normalize trailing slash before string-concat so
	// the path lines don't double-slash (`nestjs-showcase//README.md`).
	// Same fix as the single-file BuildRefinementBrief above; see that
	// site for the run-39 forensics.
	runDir = strings.TrimRight(runDir, "/")
	var b strings.Builder
	b.WriteString("## Stitched output to refine\n\n")
	b.WriteString("Read each path; refine fragments where you can cite the violated rule, the exact fragment, and the preserving edit.\n\n")
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
	return b.String()
}

// writeNamingAndRulesParts emits the naming + rules-from-goldens parts
// shared by the codebase-content and env-content multi-file composers.
// Extracted to keep both composer functions under the maintidx
// threshold after run-33 architectural fix #1 added the rules part.
//
// `naming` carries the codebase-name-vs-slot-hostname principle (own
// part to keep the prior yaml-style part under PerPartTokenCeiling);
// `rules-from-goldens` carries the golden-grounded rule substrate
// (V1-V6 voice + IG6 Zerops-forced + KB shapes + Y8/Y15 yaml rules)
// visible at authoring time, not just at refinement.
func writeNamingAndRulesParts(w *partWriter) error {
	if err := w.StartPart("naming", "codebase name vs slot hostname (recipe-MCP vs filesystem path duality)"); err != nil {
		return err
	}
	if err := w.WriteOptionalAtom("principles/codebase-name-vs-slot-hostname.md"); err != nil {
		return err
	}
	if err := w.StartPart("rules-from-goldens", "golden-grounded rule substrate — principle-shaped scoring rules"); err != nil {
		return err
	}
	return w.WriteOptionalAtom("briefs/refinement/derived_rules.md")
}
