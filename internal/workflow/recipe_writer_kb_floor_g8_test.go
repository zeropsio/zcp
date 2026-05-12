package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// recipe_writer_kb_floor_g8_test.go — Run-44 G8 substrate pin.
//
// Run-43 validation §"Substrate priority 4 (writer-brief KB floor
// reconciliation)": the writer-brief atom at
// internal/content/workflows/recipe/briefs/writer/content-surface-contracts.md
// says S2 KB shape is "3 to 6 gotcha bullets". The authoritative spec
// at docs/spec-content-surfaces.md §S5 declares "no floor; cap 8"
// (Run-43 F2). Two substrate docs disagree; spec is canonical per its
// frontmatter.
//
// Fix: update the writer-brief atom to match spec. Replace "3 to 6
// gotcha bullets" with "no floor; cap 8 bullets per codebase" language.
// Update both the per-surface section AND the summary table.

// TestWriterBrief_KBFloorMatchesSpec — content-surface-contracts.md
// MUST NOT carry the "3 to 6" / "3–6" floor for Surface 2 KB anymore.
// Both the per-surface section (line ~19 / ~45 in the original file)
// and the summary table (line ~90) must reflect "no floor; cap 8".
func TestWriterBrief_KBFloorMatchesSpec(t *testing.T) {
	t.Parallel()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	// Tests run from internal/workflow; atom is in internal/content/...
	atomPath := filepath.Join(wd, "..", "content", "workflows", "recipe", "briefs", "writer", "content-surface-contracts.md")
	body, err := os.ReadFile(atomPath)
	if err != nil {
		t.Fatalf("read atom: %v", err)
	}
	text := string(body)

	// G8 — the floor framing must be gone from both surface-2 anchor sites.
	for _, banned := range []string{
		"3 to 6 gotcha bullets",
		"3–6 gotcha bullets",
		"3-6 gotcha bullets",
	} {
		if strings.Contains(text, banned) {
			t.Errorf("content-surface-contracts.md still carries floor framing %q — G8 reconciliation incomplete", banned)
		}
	}

	// G8 — positive shape — atom must carry the spec's "no floor; cap 8" anchor.
	for _, want := range []string{
		"no floor",
		"cap 8",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("content-surface-contracts.md missing G8 spec-aligned anchor %q", want)
		}
	}
}
