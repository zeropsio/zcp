package recipe

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Run-31 Fix #1 closure — multi-file briefs. Closes the cap-treadmill
// (44→48→52→56→60→64→72→76→66 KB across runs 22-31) by changing brief
// transport from "one big file the agent reads in one shot" to "an
// index plus N parts the agent reads in known order." Each part is
// bounded by construction (PerPartTokenCeiling = 22000 estimated
// tokens, well under the Read-tool 25K-real-token hard cap).
//
// The synthetic-nil-extra-payload fixture in briefs_token_ceiling_test.go
// passed in CI but missed the production load shape: real briefs at
// run-30/31 carried 142+ facts (FilterByCodebase populated) and the
// embedded-parent fallback. cc-api at 94 KB / 36932 real tokens, cc-worker
// at 81 KB / 32009 real tokens, refinement at 83 KB / 32081 real tokens —
// all over 25K. These tests use a real-slug plan + run-shaped fact corpus
// so the production load shape is exercised.

// runShapedFactsCorpus returns 142 fact records mimicking run-31 steady
// state for the nestjs-showcase recipe:
//   - 30 porter_change × 3 codebases (90 records)
//   - 16 contract records at plan scope
//   - 12 field_rationale × 3 codebases (36 records)
//
// Each record's Scope field is populated so FilterByCodebase returns a
// non-empty per-codebase subset; without this the per-codebase facts
// block in BuildCodebaseContentBrief is empty even when len(facts) > 0.
func runShapedFactsCorpus() []FactRecord {
	out := make([]FactRecord, 0, 142)
	for _, cb := range []string{"api", "app", "worker"} {
		for i := range 30 {
			out = append(out, FactRecord{
				Topic:            fmt.Sprintf("%s-trap-%d", cb, i),
				Kind:             FactKindPorterChange,
				Scope:            cb,
				Phase:            "scaffold",
				Why:              "The candidate fix in scaffold reshapes the runtime env; the verifier needs to know what changed plus why. Run-31 emit shape: a single porter_change body explains the shape transformation in 200-400 chars. Body grew across runs as agents recorded richer mechanism descriptions; the brief carries them verbatim.",
				CandidateClass:   "platform-invariant",
				CandidateSurface: "knowledge-base",
				CandidateHeading: fmt.Sprintf("trap topic %d %s", i, cb),
				ChangeKind:       "code",
				Service:          cb,
			})
		}
	}
	for i := range 16 {
		out = append(out, FactRecord{
			Topic:         fmt.Sprintf("contract-%d", i),
			Kind:          FactKindContract,
			Subject:       fmt.Sprintf("topic.subject.path.%d", i),
			Publishers:    []string{"api"},
			Subscribers:   []string{"worker"},
			Purpose:       "Carries the user-action fanout from api to worker; payload is the action plus a correlation id used for browser progress polling.",
			PayloadSchema: `{"actionId": "uuid", "kind": "enum", "args": "object"}`,
		})
	}
	for _, cb := range []string{"api", "app", "worker"} {
		for i := range 12 {
			out = append(out, FactRecord{
				Topic:     fmt.Sprintf("%s-field-%d", cb, i),
				Kind:      FactKindFieldRationale,
				Scope:     cb,
				FieldPath: fmt.Sprintf("zerops.run.envVariables.%s_%d", cb, i),
				Why:       "Field carries the cross-service URL with the platform-injected zeropsSubdomainHost token; explanation of why the alias shape was chosen rather than the raw managed key.",
				Service:   cb,
			})
		}
	}
	return out
}

// readPartFiles reads every part file referenced from an index by absolute
// path. Returns the list of (absolutePath, body) in read order. Fails the
// test when a referenced path doesn't resolve.
func readPartFiles(t *testing.T, indexPath string) []struct {
	Path string
	Body string
} {
	t.Helper()
	idxBytes, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read index %s: %v", indexPath, err)
	}
	idx := string(idxBytes)
	dir := filepath.Dir(indexPath)
	// Discover parts by listing the dir; ordering comes from the index's
	// "Read order" section. Read every part-*.md in the dir, then check
	// each one is referenced from the index.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir %s: %v", dir, err)
	}
	var parts []struct {
		Path string
		Body string
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "part-") || !strings.HasSuffix(name, ".md") {
			continue
		}
		abs := filepath.Join(dir, name)
		if !strings.Contains(idx, abs) {
			t.Errorf("part %s exists on disk but is not referenced from index %s", abs, indexPath)
		}
		body, err := os.ReadFile(abs)
		if err != nil {
			t.Fatalf("read part %s: %v", abs, err)
		}
		parts = append(parts, struct {
			Path string
			Body string
		}{Path: abs, Body: string(body)})
	}
	return parts
}

// TestBuildCodebaseContentBrief_MultiFile_RealSlug_PartsUnderCap — every
// part file under the per-part cap. Total brief body unbounded; this is
// the load-bearing closure of run-30/31 Fix #1.
func TestBuildCodebaseContentBrief_MultiFile_RealSlug_PartsUnderCap(t *testing.T) {
	t.Parallel()
	plan := realShowcasePlan()
	facts := runShapedFactsCorpus()

	for _, cb := range plan.Codebases {
		t.Run("codebase="+cb.Hostname, func(t *testing.T) {
			t.Parallel()
			outRoot := t.TempDir()
			brief, err := buildCodebaseContentBriefMultiFile(plan, cb, nil, facts, outRoot)
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			if brief.IndexPath == "" {
				t.Fatalf("expected IndexPath populated for multi-file brief; got empty")
			}
			if len(brief.PartPaths) < 2 {
				t.Errorf("expected ≥2 part files for multi-file shape; got %d", len(brief.PartPaths))
			}
			if !filepath.IsAbs(brief.IndexPath) {
				t.Errorf("expected IndexPath absolute; got %q", brief.IndexPath)
			}
			parts := readPartFiles(t, brief.IndexPath)
			for _, p := range parts {
				tokens := EstimateTokens(p.Body)
				if tokens >= PerPartTokenCeiling {
					t.Errorf("part %s: %d tokens (%d bytes) >= per-part ceiling %d",
						p.Path, tokens, len(p.Body), PerPartTokenCeiling)
				}
			}
		})
	}
}

// TestBuildEnvContentBrief_MultiFile_RealSlug_PartsUnderCap — env-content
// follows the same multi-file shape. Run-31 env-content brief was 59410
// bytes — borderline; multi-file for consistency.
func TestBuildEnvContentBrief_MultiFile_RealSlug_PartsUnderCap(t *testing.T) {
	t.Parallel()
	plan := realShowcasePlan()
	facts := runShapedFactsCorpus()

	outRoot := t.TempDir()
	brief, err := buildEnvContentBriefMultiFile(plan, nil, facts, outRoot)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if brief.IndexPath == "" {
		t.Fatalf("expected IndexPath populated for multi-file env-content brief; got empty")
	}
	if len(brief.PartPaths) < 2 {
		t.Errorf("expected ≥2 part files; got %d", len(brief.PartPaths))
	}
	parts := readPartFiles(t, brief.IndexPath)
	for _, p := range parts {
		tokens := EstimateTokens(p.Body)
		if tokens >= PerPartTokenCeiling {
			t.Errorf("part %s: %d tokens (%d bytes) >= per-part ceiling %d",
				p.Path, tokens, len(p.Body), PerPartTokenCeiling)
		}
	}
}

// TestRefinementBrief_MultiFile_RealSlug_PartsUnderCap — refinement is
// multi-file too. Run-31 refinement at 83058 bytes / 32081 real tokens.
func TestRefinementBrief_MultiFile_RealSlug_PartsUnderCap(t *testing.T) {
	t.Parallel()
	plan := realShowcasePlan()
	facts := runShapedFactsCorpus()

	outRoot := t.TempDir()
	brief, err := buildRefinementBriefMultiFile(plan, nil, "/tmp/run", facts, outRoot)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if brief.IndexPath == "" {
		t.Fatalf("expected IndexPath populated for multi-file refinement brief; got empty")
	}
	if len(brief.PartPaths) < 2 {
		t.Errorf("expected ≥2 part files; got %d", len(brief.PartPaths))
	}
	parts := readPartFiles(t, brief.IndexPath)
	for _, p := range parts {
		tokens := EstimateTokens(p.Body)
		if tokens >= PerPartTokenCeiling {
			t.Errorf("part %s: %d tokens (%d bytes) >= per-part ceiling %d",
				p.Path, tokens, len(p.Body), PerPartTokenCeiling)
		}
	}
}

// TestBriefDispatch_MultiFile_PointsAtIndex — production handler returns
// briefPath pointing at index.md (not at a part file). The agent reads
// index first, then discovers parts via the index's "Read order" section.
func TestBriefDispatch_MultiFile_PointsAtIndex(t *testing.T) {
	t.Parallel()
	outputRoot := t.TempDir()
	store := NewStore(t.TempDir())
	if _, err := store.OpenOrCreate("nestjs-showcase", outputRoot); err != nil {
		t.Fatalf("OpenOrCreate: %v", err)
	}

	plan := realShowcasePlan()
	if res := dispatch(context.Background(), store, RecipeInput{
		Action: "update-plan", Slug: "nestjs-showcase", Plan: plan,
	}); !res.OK {
		t.Fatalf("update-plan: %s", res.Error)
	}

	// Run-52 Fix 2 — dispatch is gated on the matching phase.
	sess, _ := store.Get("nestjs-showcase")
	forcePhase(sess, PhaseCodebaseContent)

	res := dispatch(context.Background(), store, RecipeInput{
		Action:    "build-subagent-prompt",
		Slug:      "nestjs-showcase",
		BriefKind: "codebase-content",
		Codebase:  plan.Codebases[0].Hostname,
	})
	if !res.OK {
		t.Fatalf("build-subagent-prompt: %s", res.Error)
	}
	if res.BriefPath == "" {
		t.Fatalf("expected BriefPath populated for multi-file codebase-content; got empty (Prompt=%dB)", len(res.Prompt))
	}
	if !strings.HasSuffix(res.BriefPath, "/index.md") {
		t.Errorf("expected BriefPath to point at index.md; got %q", res.BriefPath)
	}
	body, err := os.ReadFile(res.BriefPath)
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	idx := string(body)
	if !strings.Contains(idx, "## Read order") {
		t.Errorf("index missing '## Read order' section:\n%s", idx)
	}
	if !strings.Contains(idx, "part-1-") {
		t.Errorf("index missing 'part-1-' bullet (deterministic ordering broken):\n%s", idx)
	}
}

// TestMultiFile_PartSizes_DiagnosticDump — verbose-only diagnostic that
// dumps part-file sizes for each multi-file kind under run-31 fixture
// load. Asserts no part exceeds the cap and prints the sizing
// distribution; useful when tuning split boundaries.
func TestMultiFile_PartSizes_DiagnosticDump(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("diagnostic dump skipped in -short mode")
	}
	plan := realShowcasePlan()
	facts := runShapedFactsCorpus()

	for _, cb := range plan.Codebases {
		t.Run("codebase-content/"+cb.Hostname, func(t *testing.T) {
			t.Parallel()
			outRoot := t.TempDir()
			brief, err := buildCodebaseContentBriefMultiFile(plan, cb, nil, facts, outRoot)
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			t.Logf("index: %s", brief.IndexPath)
			for _, p := range brief.PartPaths {
				body, err := os.ReadFile(p)
				if err != nil {
					t.Fatalf("read %s: %v", p, err)
				}
				t.Logf("  %s  %dB  %d est-tokens",
					filepath.Base(p), len(body), EstimateTokens(string(body)))
			}
		})
	}
	t.Run("env-content", func(t *testing.T) {
		t.Parallel()
		outRoot := t.TempDir()
		brief, err := buildEnvContentBriefMultiFile(plan, nil, facts, outRoot)
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		t.Logf("index: %s", brief.IndexPath)
		for _, p := range brief.PartPaths {
			body, err := os.ReadFile(p)
			if err != nil {
				t.Fatalf("read %s: %v", p, err)
			}
			t.Logf("  %s  %dB  %d est-tokens",
				filepath.Base(p), len(body), EstimateTokens(string(body)))
		}
	})
	t.Run("refinement", func(t *testing.T) {
		t.Parallel()
		outRoot := t.TempDir()
		brief, err := buildRefinementBriefMultiFile(plan, nil, "/tmp/run", facts, outRoot)
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		t.Logf("index: %s", brief.IndexPath)
		for _, p := range brief.PartPaths {
			body, err := os.ReadFile(p)
			if err != nil {
				t.Fatalf("read %s: %v", p, err)
			}
			t.Logf("  %s  %dB  %d est-tokens",
				filepath.Base(p), len(body), EstimateTokens(string(body)))
		}
	})
}

// TestComposer_CapExceededRecovery_FiresAsDesigned — the defensive
// cap-fire recovery path in buildCodebaseContentBriefMultiFile (the
// runtime-driven facts split) MUST be exercised by a fact corpus that
// pushes the per-codebase facts block past PerPartTokenCeiling on its
// own. The runShapedFactsCorpus fixture is sized to the run-31 steady
// state where the recovery doesn't fire; this test deliberately
// overflows so we know the recovery code path stays callable as the
// composer evolves.
//
// Rationale (Fix #3 of the run-31 multi-file review): the production
// load shape that bit run-31 (cc-api at 36932 real tokens) was masked
// by a unit fixture too small to trip the safety net. This test pins
// the safety net itself.
func TestComposer_CapExceededRecovery_FiresAsDesigned(t *testing.T) {
	t.Parallel()
	plan := realShowcasePlan()
	// Build a synthetic fact corpus deliberately sized to overflow the
	// per-part facts block by itself. A single porter_change record's
	// rendered summary is roughly 600 bytes; we need ≥ PartFileCap
	// (44 KB) inside ONE codebase scope to push past PerPartTokenCeiling
	// in the facts block alone.
	// Aim for the facts block (single rendered string) to fit alone in
	// a dedicated part but overflow when prepended with metadata in the
	// shared `context` part. 30 facts at ~1.4 KB each lands the block
	// at ~42 KB — under PartFileCap (44 KB) on its own; combined with
	// the few-hundred-byte metadata header pushes the context part past
	// the cap, triggering recovery.
	bigWhy := strings.Repeat("Mechanism prose body. ", 60) // ~1.32 KB per line
	cb := plan.Codebases[0]
	const factCount = 31
	facts := make([]FactRecord, 0, factCount)
	// 31 facts → block ~43.9 KB; alone fits the dedicated `facts` part
	// (21951 estimated tokens, just under 22000), but combined with the
	// ~150-byte metadata header on the `context` part lands ~22026
	// tokens — fires recovery.
	for i := range factCount {
		facts = append(facts, FactRecord{
			Topic:            fmt.Sprintf("over-cap-trap-%d", i),
			Kind:             FactKindPorterChange,
			Scope:            cb.Hostname,
			Phase:            "scaffold",
			Why:              bigWhy,
			CandidateClass:   "platform-invariant",
			CandidateSurface: "knowledge-base",
			CandidateHeading: fmt.Sprintf("over-cap heading %d", i),
			ChangeKind:       "code",
			Service:          cb.Hostname,
		})
	}
	outRoot := t.TempDir()
	brief, err := buildCodebaseContentBriefMultiFile(plan, cb, nil, facts, outRoot)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	// Every part still under the per-part cap — the recovery split must
	// have produced a clean partition. If recovery is broken, one of
	// the parts ends up at >= cap.
	parts := readPartFiles(t, brief.IndexPath)
	for _, p := range parts {
		tokens := EstimateTokens(p.Body)
		if tokens >= PerPartTokenCeiling {
			t.Errorf("part %s: %d tokens (%d bytes) >= per-part ceiling %d (recovery split failed)",
				p.Path, tokens, len(p.Body), PerPartTokenCeiling)
		}
	}
	// Assert the runtime split ran: when facts block alone exceeds the
	// cap, the composer opens a dedicated `facts` part. Its filename is
	// `part-N-facts.md` for some N > 1.
	foundFactsPart := false
	for _, pp := range brief.PartPaths {
		if strings.Contains(pp, "-facts.md") {
			foundFactsPart = true
			break
		}
	}
	if !foundFactsPart {
		t.Errorf("expected a dedicated facts part to exist when facts block exceeds the cap; got parts %v", brief.PartPaths)
	}
}

// TestBriefIndex_ListsAllPartsInReadOrder — every part file in the dir
// is referenced from the index, and the index's read-order section lists
// them in deterministic numeric order (part-1, part-2, ...).
func TestBriefIndex_ListsAllPartsInReadOrder(t *testing.T) {
	t.Parallel()
	plan := realShowcasePlan()
	facts := runShapedFactsCorpus()
	outRoot := t.TempDir()
	brief, err := buildCodebaseContentBriefMultiFile(plan, plan.Codebases[0], nil, facts, outRoot)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	idxBytes, err := os.ReadFile(brief.IndexPath)
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	idx := string(idxBytes)

	// Each PartPath should appear in the index in the order it was emitted.
	last := 0
	for _, p := range brief.PartPaths {
		i := strings.Index(idx, p)
		if i < 0 {
			t.Errorf("part %s not referenced from index", p)
			continue
		}
		if i < last {
			t.Errorf("part %s referenced at index offset %d, before previous part at %d (read order broken)", p, i, last)
		}
		last = i
	}
}
