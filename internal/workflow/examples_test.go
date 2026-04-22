package workflow

import (
	"strings"
	"testing"
)

// TestExamplesBank_FrontmatterValid walks every example file in the
// bank and asserts the loader accepts every frontmatter. This is the
// structural gate — if a new example commit breaks the schema (bad
// surface name, missing title, wrong verdict), the test fails
// immediately and the commit is blocked at lint time.
func TestExamplesBank_FrontmatterValid(t *testing.T) {
	t.Parallel()
	all, err := AllExamples()
	if err != nil {
		t.Fatalf("AllExamples: %v", err)
	}
	if len(all) < 15 {
		t.Errorf("example bank has %d entries, want ≥ 15 (v39 seeded with 20)", len(all))
	}
}

// TestExamplesBank_CoverageBySurface asserts every surface the writer
// authors has at least one PASS and one FAIL example so the sampler
// can mix verdicts. Intro and env-comment are not writer-authored; the
// main agent owns those, so they're exempt from the pass+fail coverage
// check.
func TestExamplesBank_CoverageBySurface(t *testing.T) {
	t.Parallel()
	all, err := AllExamples()
	if err != nil {
		t.Fatalf("AllExamples: %v", err)
	}

	type counts struct{ pass, fail int }
	byCount := map[ExampleSurface]*counts{}
	for _, s := range surfacesRelevantToWriter() {
		byCount[s] = &counts{}
	}
	for _, ex := range all {
		c, ok := byCount[ex.Surface]
		if !ok {
			continue
		}
		if ex.Verdict == ExampleVerdictPass {
			c.pass++
		} else {
			c.fail++
		}
	}
	for surface, c := range byCount {
		if c.pass < 1 {
			t.Errorf("surface %s: expected ≥ 1 PASS example, got %d", surface, c.pass)
		}
		if c.fail < 1 {
			t.Errorf("surface %s: expected ≥ 1 FAIL example, got %d", surface, c.fail)
		}
	}
}

// TestSampleFor_MixesVerdicts validates the sampler returns a mix of
// pass and fail when both are available, respecting the n cap.
func TestSampleFor_MixesVerdicts(t *testing.T) {
	t.Parallel()
	got, err := SampleFor(ExampleSurfaceGotcha, 4)
	if err != nil {
		t.Fatalf("SampleFor: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("expected 4 samples, got %d", len(got))
	}
	var hasPass, hasFail bool
	for _, ex := range got {
		if ex.Verdict == ExampleVerdictPass {
			hasPass = true
		}
		if ex.Verdict == ExampleVerdictFail {
			hasFail = true
		}
	}
	if !hasPass {
		t.Error("expected at least one PASS sample; the gotcha surface has passes in the bank")
	}
	if !hasFail {
		t.Error("expected at least one FAIL sample; the gotcha surface has fails in the bank")
	}
}

// TestSampleFor_UnknownSurfaceReturnsEmpty asserts an unknown surface
// name returns an empty slice (no samples, no error) rather than
// panicking or returning error — the caller decides whether empty is
// worth a fallback.
func TestSampleFor_UnknownSurfaceReturnsEmpty(t *testing.T) {
	t.Parallel()
	got, err := SampleFor(ExampleSurface("nonexistent-surface"), 3)
	if err != nil {
		t.Fatalf("SampleFor(unknown): err = %v, want nil", err)
	}
	if len(got) != 0 {
		t.Errorf("expected 0 samples for unknown surface, got %d", len(got))
	}
}

// TestSampleFor_ZeroCountReturnsEmpty asserts n=0 returns empty
// immediately without parsing the bank.
func TestSampleFor_ZeroCountReturnsEmpty(t *testing.T) {
	t.Parallel()
	got, err := SampleFor(ExampleSurfaceGotcha, 0)
	if err != nil {
		t.Fatalf("SampleFor(n=0): err = %v, want nil", err)
	}
	if len(got) != 0 {
		t.Errorf("expected 0 samples for n=0, got %d", len(got))
	}
}

// TestBuildWriterExampleInputBlock_IncludesAllSurfaces asserts the
// engine-stitched input block covers every surface the writer authors,
// with a non-empty section per surface in the bank.
func TestBuildWriterExampleInputBlock_IncludesAllSurfaces(t *testing.T) {
	t.Parallel()
	block, err := BuildWriterExampleInputBlock(2)
	if err != nil {
		t.Fatalf("BuildWriterExampleInputBlock: %v", err)
	}
	for _, surface := range surfacesRelevantToWriter() {
		if !strings.Contains(block, "## "+string(surface)+" — annotated examples") {
			t.Errorf("writer example input block missing surface section %q", surface)
		}
	}
}

// TestBuildWriterExampleInputBlock_ZeroPerSurfaceReturnsEmpty —
// engine may opt to pass 0 when the bank is temporarily disabled.
func TestBuildWriterExampleInputBlock_ZeroPerSurfaceReturnsEmpty(t *testing.T) {
	t.Parallel()
	block, err := BuildWriterExampleInputBlock(0)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if block != "" {
		t.Errorf("expected empty block for perSurface=0, got %q", block)
	}
}

// TestRenderExampleBlock_FormatStable asserts the render format
// contains the verdict, title, and reason for each example so the
// writer can pattern-match against both the shape and the
// classification reason.
func TestRenderExampleBlock_FormatStable(t *testing.T) {
	t.Parallel()
	examples := []ContentExample{
		{
			Filename: "test_fail.md",
			Surface:  ExampleSurfaceGotcha,
			Verdict:  ExampleVerdictFail,
			Reason:   "folk-doctrine",
			Title:    "Test fail case",
			Body:     "**Why this fails.** Example body.",
		},
		{
			Filename: "test_pass.md",
			Surface:  ExampleSurfaceGotcha,
			Verdict:  ExampleVerdictPass,
			Reason:   "platform-invariant-ok",
			Title:    "Test pass case",
			Body:     "**Why this passes.** Example body.",
		},
	}
	got := RenderExampleBlock(examples)
	for _, want := range []string{
		"[FAIL]", "Test fail case", "reason: folk-doctrine",
		"[PASS]", "Test pass case", "reason: platform-invariant-ok",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered block missing %q\n\nblock:\n%s", want, got)
		}
	}
}
