package workflow

import (
	"encoding/json"
	"fmt"
	"strings"
)

// This file implements the C-5 foundation stitcher functions — six helpers
// that compose step-entry / substep-complete / sub-agent dispatch briefs
// from the atomic content tree under internal/content/workflows/recipe/.
//
// Every function reads atom bodies via LoadAtomBody and concatenates them
// with "\n---\n" separators. SymbolContract interpolation is explicit
// (the JSON fragment is appended to the scaffold / feature dispatch brief
// after its consumption atom, so parallel sub-agents see byte-identical
// contracts).
//
// These functions are dead code relative to the current buildGuide path
// — C-5's follow-up ("the flip") swaps buildGuide's block-based emission
// over to these. They ship here so C-5 lands the full stitcher surface
// alongside the atom tree and tests.

// BuildStepEntry composes the step-entry guidance emitted at step
// transitions. Returns the concatenation of `phases/<phase>/entry.md`
// plus every substep's entry atom in substep order (the orchestrator
// sees the phase's overall shape + the first action per substep).
//
// Tier filtering: substeps whose entry atom is tier-conditional (e.g.
// dashboard-skeleton-showcase) are included only when tier matches.
// Returns an error if a required atom is missing.
func BuildStepEntry(phase, tier string) (string, error) {
	phaseAtom := AtomID(phase + ".entry")
	if _, ok := AtomPath(phaseAtom); !ok {
		return "", fmt.Errorf("no entry atom for phase %q", phase)
	}

	body, err := LoadAtomBody(phaseAtom)
	if err != nil {
		return "", err
	}

	var parts []string
	parts = append(parts, body)

	// Append every substep entry atom under this phase.
	for _, a := range AtomsForPhase(phase) {
		if !strings.HasSuffix(a.ID, ".entry") || a.ID == phaseAtom {
			continue
		}
		if a.TierCond != TierAny && a.TierCond != tier {
			continue
		}
		sub, err := LoadAtomBody(a.ID)
		if err != nil {
			return "", err
		}
		parts = append(parts, sub)
	}

	return strings.Join(parts, "\n\n---\n\n"), nil
}

// BuildSubStepCompletion returns the attestation guidance emitted when a
// substep completes. Loads `phases/<phase>/<substep>/completion.md` if
// it exists, otherwise falls back to the phase-level completion atom.
// The agent reads this to confirm it can attest and move to the next
// substep.
func BuildSubStepCompletion(phase, substep string) (string, error) {
	// Try substep-scoped completion first.
	if substep != "" {
		id := AtomID(phase + "." + substep + ".completion")
		if _, ok := AtomPath(id); ok {
			return LoadAtomBody(id)
		}
	}
	// Fall back to phase-level completion.
	phaseID := AtomID(phase + ".completion")
	if _, ok := AtomPath(phaseID); !ok {
		return "", fmt.Errorf("no completion atom for phase %q substep %q", phase, substep)
	}
	return LoadAtomBody(phaseID)
}

// BuildScaffoldDispatchBrief composes the scaffold sub-agent dispatch
// prompt for the given codebase role ("api", "app", "worker") under the
// given plan. Order per atomic-layout.md §6:
//
//  1. briefs/scaffold/mandatory-core
//  2. briefs/scaffold/symbol-contract-consumption + {{.SymbolContract | toJSON}}
//  3. briefs/scaffold/framework-task
//  4. briefs/scaffold/pre-ship-assertions
//  5. briefs/scaffold/completion-shape
//  6. role-specific addendum (api / frontend / worker)
//  7. pointer-include principles (where-commands-run, file-op-sequencing,
//     tool-use-policy, symbol-naming-contract, platform-principles/*
//     relevant to the role)
//
// Prior Discoveries block is appended by the caller (buildGuide) at
// dispatch time — not by this function.
func BuildScaffoldDispatchBrief(plan *RecipePlan, role string) (string, error) {
	addendum := scaffoldAddendumID(role)
	ids := []string{
		"briefs.scaffold.mandatory-core",
		"briefs.scaffold.symbol-contract-consumption",
	}
	head, err := concatAtoms(ids...)
	if err != nil {
		return "", err
	}

	contractJSON, err := marshalSymbolContract(plan)
	if err != nil {
		return "", err
	}

	tail, err := concatAtoms(
		"briefs.scaffold.framework-task",
		"briefs.scaffold.pre-ship-assertions",
		"briefs.scaffold.completion-shape",
		addendum,
	)
	if err != nil {
		return "", err
	}

	principles, err := concatAtoms(scaffoldPrinciples(role)...)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString(head)
	b.WriteString("\n\n```json\n")
	b.WriteString(contractJSON)
	b.WriteString("\n```\n\n---\n\n")
	b.WriteString(tail)
	if principles != "" {
		b.WriteString("\n\n---\n\n")
		b.WriteString(principles)
	}
	return b.String(), nil
}

// BuildFeatureDispatchBrief composes the feature sub-agent dispatch for
// showcase tier. Minimal tier writes features inline from main.
func BuildFeatureDispatchBrief(plan *RecipePlan) (string, error) {
	head, err := concatAtoms(
		"briefs.feature.mandatory-core",
		"briefs.feature.symbol-contract-consumption",
	)
	if err != nil {
		return "", err
	}

	contractJSON, err := marshalSymbolContract(plan)
	if err != nil {
		return "", err
	}

	tail, err := concatAtoms(
		"briefs.feature.task",
		"briefs.feature.diagnostic-cadence",
		"briefs.feature.ux-quality",
		"briefs.feature.completion-shape",
	)
	if err != nil {
		return "", err
	}

	principles, err := concatAtoms(featurePrinciples()...)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString(head)
	b.WriteString("\n\n```json\n")
	b.WriteString(contractJSON)
	b.WriteString("\n```\n\n---\n\n")
	b.WriteString(tail)
	if principles != "" {
		b.WriteString("\n\n---\n\n")
		b.WriteString(principles)
	}
	return b.String(), nil
}

// BuildWriterDispatchBrief composes the writer sub-agent dispatch. The
// writer is dispatched at deploy.readmes substep. Per Q4: showcase uses
// the dispatched writer; minimal uses Path A main-inline for v35.
func BuildWriterDispatchBrief(plan *RecipePlan, factsLogPath string) (string, error) {
	body, err := concatAtoms(
		"briefs.writer.mandatory-core",
		"briefs.writer.fresh-context-premise",
		"briefs.writer.canonical-output-tree",
		"briefs.writer.content-surface-contracts",
		"briefs.writer.classification-taxonomy",
		"briefs.writer.routing-matrix",
		"briefs.writer.citation-map",
		"briefs.writer.manifest-contract",
		"briefs.writer.self-review-per-surface",
		"briefs.writer.completion-shape",
	)
	if err != nil {
		return "", err
	}

	principles, err := concatAtoms(writerPrinciples()...)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString(body)
	if principles != "" {
		b.WriteString("\n\n---\n\n")
		b.WriteString(principles)
	}
	if factsLogPath != "" {
		fmt.Fprintf(&b, "\n\n---\n\n## Input files\n\n- Facts log: `%s`\n", factsLogPath)
	}
	return b.String(), nil
}

// BuildCodeReviewDispatchBrief composes the code-review sub-agent
// dispatch at close.code-review substep. manifestPath is the absolute
// path to ZCP_CONTENT_MANIFEST.json for the output recipe directory.
func BuildCodeReviewDispatchBrief(plan *RecipePlan, manifestPath string) (string, error) {
	body, err := concatAtoms(
		"briefs.code-review.mandatory-core",
		"briefs.code-review.task",
		"briefs.code-review.manifest-consumption",
		"briefs.code-review.reporting-taxonomy",
		"briefs.code-review.completion-shape",
	)
	if err != nil {
		return "", err
	}

	principles, err := concatAtoms(codeReviewPrinciples()...)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString(body)
	if principles != "" {
		b.WriteString("\n\n---\n\n")
		b.WriteString(principles)
	}
	if manifestPath != "" {
		fmt.Fprintf(&b, "\n\n---\n\n## Input files\n\n- Content manifest: `%s`\n", manifestPath)
	}
	return b.String(), nil
}

// BuildEditorialReviewDispatchBrief composes the editorial-review
// sub-agent dispatch at close.editorial-review substep (added in C-7.5).
// Defined here for symmetry; C-7.5 wires the substep registration.
//
// Per refinement §10 open-question #6: NO Prior Discoveries block is
// included — porter-premise requires fresh-reader stance. Caller must
// not prepend prior discoveries when invoking this brief.
func BuildEditorialReviewDispatchBrief(plan *RecipePlan, factsLogPath, manifestPath string) (string, error) {
	body, err := concatAtoms(
		"briefs.editorial-review.mandatory-core",
		"briefs.editorial-review.porter-premise",
		"briefs.editorial-review.surface-walk-task",
		"briefs.editorial-review.single-question-tests",
		"briefs.editorial-review.classification-reclassify",
		"briefs.editorial-review.citation-audit",
		"briefs.editorial-review.counter-example-reference",
		"briefs.editorial-review.cross-surface-ledger",
		"briefs.editorial-review.reporting-taxonomy",
		"briefs.editorial-review.completion-shape",
	)
	if err != nil {
		return "", err
	}

	principles, err := concatAtoms(editorialReviewPrinciples()...)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString(body)
	if principles != "" {
		b.WriteString("\n\n---\n\n")
		b.WriteString(principles)
	}
	if factsLogPath != "" || manifestPath != "" {
		b.WriteString("\n\n---\n\n## Pointer inputs (open on demand only)\n\n")
		if factsLogPath != "" {
			fmt.Fprintf(&b, "- Facts log: `%s`\n", factsLogPath)
		}
		if manifestPath != "" {
			fmt.Fprintf(&b, "- Content manifest: `%s`\n", manifestPath)
		}
	}
	return b.String(), nil
}

// marshalSymbolContract returns the canonical JSON form of the plan's
// SymbolContract. Must produce byte-identical output across calls with
// the same plan so parallel scaffold dispatches see identical contract
// fragments.
func marshalSymbolContract(plan *RecipePlan) (string, error) {
	var contract SymbolContract
	if plan != nil {
		contract = plan.SymbolContract
	}
	data, err := json.MarshalIndent(contract, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal SymbolContract: %w", err)
	}
	return string(data), nil
}

// scaffoldAddendumID returns the role-specific scaffold addendum atom ID.
// Empty string if role has no addendum (e.g. rare single-hostname shape).
func scaffoldAddendumID(role string) string {
	switch role {
	case FeatureSurfaceAPI:
		return "briefs.scaffold.api-codebase-addendum"
	case contractRoleApp, "frontend":
		return "briefs.scaffold.frontend-codebase-addendum"
	case FeatureSurfaceWorker:
		return "briefs.scaffold.worker-codebase-addendum"
	}
	return ""
}

// scaffoldPrinciples returns the role-aware principle atom IDs the
// scaffold dispatch pointer-includes.
func scaffoldPrinciples(role string) []string {
	base := []string{
		"principles.where-commands-run",
		"principles.file-op-sequencing",
		"principles.tool-use-policy",
		"principles.symbol-naming-contract",
		"principles.fact-recording-discipline",
	}
	switch role {
	case FeatureSurfaceAPI:
		base = append(base,
			"principles.platform-principles.01-graceful-shutdown",
			"principles.platform-principles.02-routable-bind",
			"principles.platform-principles.03-proxy-trust",
			"principles.platform-principles.05-structured-creds",
		)
	case contractRoleApp, "frontend":
		base = append(base,
			"principles.platform-principles.02-routable-bind",
			"principles.dev-server-contract",
		)
	case FeatureSurfaceWorker:
		base = append(base,
			"principles.platform-principles.01-graceful-shutdown",
			"principles.platform-principles.04-competing-consumer",
			"principles.platform-principles.05-structured-creds",
		)
	}
	return base
}

// featurePrinciples returns the principle atoms included in the feature
// dispatch — cross-cutting concerns the feature sub-agent must obey
// across every codebase it touches.
func featurePrinciples() []string {
	return []string{
		"principles.where-commands-run",
		"principles.file-op-sequencing",
		"principles.tool-use-policy",
		"principles.symbol-naming-contract",
		"principles.fact-recording-discipline",
		"principles.platform-principles.01-graceful-shutdown",
		"principles.platform-principles.04-competing-consumer",
	}
}

// writerPrinciples returns the principle atoms included in the writer
// dispatch. The writer authors content; platform-invariant principles
// are load-bearing for citation honesty.
func writerPrinciples() []string {
	return []string{
		"principles.file-op-sequencing",
		"principles.tool-use-policy",
		"principles.fact-recording-discipline",
		"principles.comment-style",
		"principles.visual-style",
	}
}

// codeReviewPrinciples returns the principle atoms included in the
// code-review dispatch. The reviewer reads framework code; scope is
// narrower than the writer.
func codeReviewPrinciples() []string {
	return []string{
		"principles.file-op-sequencing",
		"principles.tool-use-policy",
	}
}

// editorialReviewPrinciples returns the principle atoms included in
// the editorial-review dispatch. Reviewer has NO Bash/SSH access —
// tool policy is heavily scoped by mandatory-core; these principles
// cover file-op + visual-style baseline.
func editorialReviewPrinciples() []string {
	return []string{
		"principles.file-op-sequencing",
		"principles.tool-use-policy",
		"principles.visual-style",
	}
}

// AtomID returns the manifest ID for a dotted-path atom name. Trivial
// helper that centralizes the conversion if naming convention changes.
func AtomID(dotted string) string {
	return dotted
}
