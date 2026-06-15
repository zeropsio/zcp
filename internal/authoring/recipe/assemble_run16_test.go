package recipe

import (
	"slices"
	"strings"
	"testing"
)

// Run-16 §6.5 — slotted IG concatenation, single-slot CLAUDE.md stitch.
// (Run-21-prep removed the per-block zerops-yaml-comments tests when the
// fragment shape became whole-yaml; see stitch_yaml_test.go for the new
// shape's coverage.)

func TestMergeSlottedIGFragments_ConcatenatesNumeric(t *testing.T) {
	t.Parallel()
	input := map[string]string{
		"codebase/api/integration-guide/3":   "### 3. Drain on SIGTERM\nbody",
		"codebase/api/integration-guide/2":   "### 2. Trust the L7\nbody",
		"codebase/api/integration-guide/4":   "### 4. Read managed-service credentials\nbody",
		"codebase/api/integration-guide":     "(legacy fragment, should be overwritten)",
		"codebase/other/integration-guide/2": "### 2. Other codebase",
	}
	out := mergeSlottedIGFragments(input, "api")

	merged := out["codebase/api/integration-guide"]
	if !strings.Contains(merged, "### 2. Trust the L7") {
		t.Error("merged IG missing slot 2")
	}
	if !strings.Contains(merged, "### 3. Drain") {
		t.Error("merged IG missing slot 3")
	}
	if !strings.Contains(merged, "### 4. Read") {
		t.Error("merged IG missing slot 4")
	}
	// Numeric ordering: slot 2 must appear before slot 3.
	idx2 := strings.Index(merged, "### 2.")
	idx3 := strings.Index(merged, "### 3.")
	idx4 := strings.Index(merged, "### 4.")
	if idx2 >= idx3 || idx3 >= idx4 {
		t.Errorf("slots not in numeric order: 2@%d, 3@%d, 4@%d", idx2, idx3, idx4)
	}
	// Other codebase's slot must NOT be in api's merged output.
	if strings.Contains(merged, "Other codebase") {
		t.Error("merged IG leaked another codebase's slot")
	}
}

func TestMergeSlottedIGFragments_FallsBackToLegacy(t *testing.T) {
	t.Parallel()
	// No slotted entries → legacy stays.
	input := map[string]string{
		"codebase/api/integration-guide": "### 2. Legacy single-fragment",
	}
	out := mergeSlottedIGFragments(input, "api")
	if out["codebase/api/integration-guide"] != "### 2. Legacy single-fragment" {
		t.Errorf("legacy IG should be preserved when no slots present, got %q", out["codebase/api/integration-guide"])
	}
}

// Run-16 reviewer D-6 — the legacy `claude-md/{service-facts,notes}`
// sub-slot back-compat synthesizer was dropped because its synthesized
// body opened with the very `## Zerops service facts` heading that the
// run-16 slot-shape refusal + finalize validator both reject. Recipes
// still on the legacy form fail loudly at stitch with a "missing
// fragment codebase/<h>/claude-md" error — that's the migration signal.
func TestAssembleClaudeMD_LegacySubslotsOnly_FailsWithMissingFragment(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Slug:      "synth",
		Framework: "nest",
		Codebases: []Codebase{{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"}},
		Fragments: map[string]string{
			"codebase/api/claude-md/service-facts": "Port 3000",
			"codebase/api/claude-md/notes":         "Dev loop",
		},
	}
	_, missing, err := AssembleCodebaseClaudeMD(plan, "api")
	if err != nil {
		t.Fatalf("AssembleCodebaseClaudeMD should not error on missing single-slot (returns it as missing): %v", err)
	}
	if !slices.Contains(missing, "codebase/api/claude-md") {
		t.Errorf("expected `codebase/api/claude-md` in missing list when only legacy sub-slots present; got missing=%v", missing)
	}
}

func TestAssembleClaudeMD_SingleSlot_StitchesCleanly(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Slug:      "synth",
		Framework: "nest",
		Codebases: []Codebase{{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"}},
		Fragments: map[string]string{
			"codebase/api/claude-md": "# api\n\nNestJS REST API.\n\n## Build & run\n\n- npm test\n\n## Architecture\n\n- src/main.ts",
		},
	}
	body, missing, err := AssembleCodebaseClaudeMD(plan, "api")
	if err != nil {
		t.Fatalf("AssembleCodebaseClaudeMD: %v", err)
	}
	if len(missing) != 0 {
		t.Errorf("single-slot fragment present → no missing; got %v", missing)
	}
	if !strings.Contains(body, "# api") {
		t.Error("single-slot fragment body should be substituted into CLAUDE.md")
	}
}
