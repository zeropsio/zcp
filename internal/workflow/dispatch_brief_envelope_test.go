package workflow

import (
	"strings"
	"testing"
)

// Cx-BRIEF-OVERFLOW regression tests (HANDOFF-to-I6, defect-class-
// registry §16.1 `v35-dispatch-brief-overflow`). v35 evidence: the
// composed writer-substep brief was 71,720 chars at the
// `complete substep=feature-sweep-stage` response; that exceeds the MCP
// tool-response token cap, the harness spilled the payload to a scratch
// file, and the main agent excavated only the first ~3KB before
// dispatching writer-1 with a broken wire contract.
//
// These tests pin the envelope contract: when the composed brief would
// overflow, the substep guide emits a compact envelope naming atom IDs
// the main agent retrieves via `action=dispatch-brief-atom` instead of
// inlining the full brief.

// TestBuildWriterDispatchBrief_ExceedsInlineThreshold is the size
// assertion that makes the envelope gate meaningful: the current writer
// brief composition is larger than the inline threshold, so any
// substep-guide response embedding it inline is at risk of spillover.
// Pins the v35 regression class — if the writer brief shrinks below the
// threshold in a future rewrite, this test fails and the envelope gate
// can be revisited.
func TestBuildWriterDispatchBrief_ExceedsInlineThreshold(t *testing.T) {
	t.Parallel()
	plan := testShowcasePlan()
	brief, err := BuildWriterDispatchBrief(plan, "/tmp/zcp-facts-xyz.jsonl")
	if err != nil {
		t.Fatalf("BuildWriterDispatchBrief: %v", err)
	}
	if len(brief) <= dispatchBriefInlineThresholdBytes {
		t.Errorf("writer brief size %d is at-or-below the inline threshold %d — the envelope gate would not fire; either the brief has shrunk (revisit the threshold) or the atoms have churned (recompute)", len(brief), dispatchBriefInlineThresholdBytes)
	}
}

// TestFormatDispatchBriefAttachment_ReadmesSubstep_EmitsEnvelope verifies
// that for the readmes substep in showcase, the attachment returned by
// `formatDispatchBriefAttachment` is the stitch-instruction envelope —
// not an inlined 60KB blob. Envelope must be under 5KB.
func TestFormatDispatchBriefAttachment_ReadmesSubstep_EmitsEnvelope(t *testing.T) {
	t.Parallel()
	plan := testShowcasePlan()
	brief, err := BuildWriterDispatchBrief(plan, "/tmp/zcp-facts-xyz.jsonl")
	if err != nil {
		t.Fatalf("BuildWriterDispatchBrief: %v", err)
	}
	out := formatDispatchBriefAttachment(RecipeStepDeploy, SubStepReadmes, plan, "xyz", brief)
	if !strings.Contains(out, "dispatch-brief-atom") {
		t.Errorf("envelope must name the dispatch-brief-atom action; got: %s", out)
	}
	if strings.Contains(out, "transmit verbatim") && !strings.Contains(out, "retrieve + stitch before transmitting") {
		t.Errorf("envelope must not use the inline-brief heading 'Dispatch brief (transmit verbatim)'; got: %s", out)
	}
	if len(out) > 5000 {
		t.Errorf("envelope size %d exceeds 5000B cap — stitch instructions should be concise", len(out))
	}
	// Every atom ID composed into the writer brief must appear in the
	// envelope so the main agent can reproduce the full brief.
	for _, id := range writerBriefBodyAtomIDs() {
		if !strings.Contains(out, id) {
			t.Errorf("envelope missing body atom %q", id)
		}
	}
	for _, id := range writerPrinciples() {
		if !strings.Contains(out, id) {
			t.Errorf("envelope missing principle atom %q", id)
		}
	}
}

// TestFormatDispatchBriefAttachment_SmallBrief_EmitsInline verifies
// that when a brief fits inline, the attachment keeps the historical
// "Dispatch brief (transmit verbatim)" heading + the brief content —
// the envelope is a targeted counter to overflow, not a universal
// restructure.
func TestFormatDispatchBriefAttachment_SmallBrief_EmitsInline(t *testing.T) {
	t.Parallel()
	plan := testShowcasePlan()
	small := "## small brief\n\nverbatim body"
	out := formatDispatchBriefAttachment(RecipeStepDeploy, SubStepReadmes, plan, "xyz", small)
	if !strings.HasPrefix(out, "## Dispatch brief (transmit verbatim)") {
		firstN := min(len(out), 80)
		t.Errorf("small-brief attachment must use inline heading; got first 80B: %q", out[:firstN])
	}
	if !strings.Contains(out, "verbatim body") {
		t.Errorf("small-brief attachment must contain the verbatim brief; got: %s", out)
	}
}

// TestFormatDispatchBriefAttachment_UnmanagedLargeSubstep_FallsBackInline
// — envelopes are scoped narrowly to known-overflow substeps (writer
// readmes in showcase). Other substeps with large briefs still fall
// back to inline embedding because the envelope shape hasn't been
// declared for them; a future commit can extend envelopeForLargeBrief
// if needed.
func TestFormatDispatchBriefAttachment_UnmanagedLargeSubstep_FallsBackInline(t *testing.T) {
	t.Parallel()
	plan := testShowcasePlan()
	largeOpaque := strings.Repeat("x", dispatchBriefInlineThresholdBytes+1)
	out := formatDispatchBriefAttachment(RecipeStepDeploy, SubStepSubagent, plan, "xyz", largeOpaque)
	if !strings.HasPrefix(out, "## Dispatch brief (transmit verbatim)") {
		firstN := min(len(out), 80)
		t.Errorf("unmanaged large-brief substep must fall back to inline heading; got first 80B: %q", out[:firstN])
	}
}

// TestEnvelopeAtoms_StitchToFullBrief verifies the envelope's load-bearing
// invariant: concatenating the bodies of every listed atom per the
// documented stitch procedure reproduces the exact composed brief that
// BuildWriterDispatchBrief emits — byte-identical. If this drifts, the
// main agent's stitched result would diverge from what the sub-agent
// contract expects, re-opening the v35 wire-contract-loss window.
func TestEnvelopeAtoms_StitchToFullBrief(t *testing.T) {
	t.Parallel()
	plan := testShowcasePlan()
	factsPath := "/tmp/zcp-facts-xyz.jsonl"

	want, err := BuildWriterDispatchBrief(plan, factsPath)
	if err != nil {
		t.Fatalf("BuildWriterDispatchBrief: %v", err)
	}

	// Reproduce the envelope's stitch procedure locally.
	bodyParts := make([]string, 0, len(writerBriefBodyAtomIDs()))
	for _, id := range writerBriefBodyAtomIDs() {
		body, err := LoadAtomBody(id)
		if err != nil {
			t.Fatalf("LoadAtomBody %q: %v", id, err)
		}
		bodyParts = append(bodyParts, body)
	}
	bodyConcat := strings.Join(bodyParts, "\n\n---\n\n")

	princParts := make([]string, 0, len(writerPrinciples()))
	for _, id := range writerPrinciples() {
		body, err := LoadAtomBody(id)
		if err != nil {
			t.Fatalf("LoadAtomBody %q: %v", id, err)
		}
		princParts = append(princParts, body)
	}
	princConcat := strings.Join(princParts, "\n\n---\n\n")

	got := bodyConcat + "\n\n---\n\n" + princConcat + "\n\n---\n\n## Input files\n\n- Facts log: `" + factsPath + "`\n"

	if got != want {
		t.Errorf("stitched-from-envelope brief diverges from BuildWriterDispatchBrief output\n got len=%d\nwant len=%d", len(got), len(want))
	}
}
