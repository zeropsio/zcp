// Tests for stitch load-order semantics: refinement (and finalize)
// subdirs are recognized; refinement-phase fragments override prior
// phases at the same fragmentId; finalize loads BEFORE refinement so
// refinement remains the editorial pass.
//
// Pinned by the canonical zcprecipator3 pipeline:
//
//	codebase-content → claudemd-author → env-content → finalize → stitch → refinement → re-stitch
//
// Without these tests the sim driver silently dropped fragments-new/
// refinement/ + fragments-new/finalize/ contents because
// loadFragmentsTree only recognized codebase hosts + the literal "env"
// subdir.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/authoring/recipe"
)

// recipeTier0 returns the canonical folder name for tier 0 (the
// audience-first label baked into recipe.Tiers()). Used by tests that
// need to read environments/<folder>/import.yaml on disk.
func recipeTier0() (string, bool) {
	t, ok := recipe.TierAt(0)
	if !ok {
		return "", false
	}
	return t.Folder, true
}

// TestStitch_LoadsRefinementSubdir asserts a fragment authored under
// fragments-new/refinement/ reaches the stitched output. Pre-fix the
// loader skipped the refinement subdir with "(skip) unknown subdir".
func TestStitch_LoadsRefinementSubdir(t *testing.T) {
	dir := t.TempDir()
	if err := writeMinimalSimulationOpts(t, dir, false); err != nil {
		t.Fatalf("writeMinimalSimulationOpts: %v", err)
	}
	// Write a refinement-phase fragment that REPLACES the api KB
	// (codebase-content authored "Authored knowledge-base body...").
	refDir := filepath.Join(dir, "fragments-new", "refinement")
	if err := os.MkdirAll(refDir, 0o755); err != nil {
		t.Fatalf("mkdir refinement: %v", err)
	}
	const refMarker = "REFINEMENT KB OVERRIDE: load-bearing refinement-only token"
	mustWrite(t, filepath.Join(refDir, "codebase__api__knowledge-base.md"),
		"### Operations\n\n"+refMarker+"\n")

	if err := runStitch([]string{"-dir", dir, "-rounds", "1"}); err != nil {
		t.Fatalf("runStitch: %v", err)
	}

	// AssembleCodebaseREADME embeds the KB fragment; verify the
	// refinement marker reached the staged README.
	body, err := os.ReadFile(filepath.Join(dir, "apidev", "README.md"))
	if err != nil {
		t.Fatalf("read apidev/README.md: %v", err)
	}
	if !strings.Contains(string(body), refMarker) {
		t.Errorf("refinement KB override did not reach stitched README; body lacks marker %q\n--- README ---\n%s",
			refMarker, body)
	}
}

// TestStitch_LoadsFinalizeSubdir asserts a fragment authored under
// fragments-new/finalize/ reaches the stitched output. Same shape as
// the refinement test but for the finalize phase (root/intro is
// finalize's canonical orphan in production).
func TestStitch_LoadsFinalizeSubdir(t *testing.T) {
	dir := t.TempDir()
	if err := writeMinimalSimulationOpts(t, dir, false); err != nil {
		t.Fatalf("writeMinimalSimulationOpts: %v", err)
	}
	finalizeDir := filepath.Join(dir, "fragments-new", "finalize")
	if err := os.MkdirAll(finalizeDir, 0o755); err != nil {
		t.Fatalf("mkdir finalize: %v", err)
	}
	const finalizeMarker = "FINALIZE-AUTHORED-ROOT-INTRO-MARKER"
	mustWrite(t, filepath.Join(finalizeDir, "root__intro.md"),
		finalizeMarker+"\n\nA full-featured fixture recipe.\n")

	if err := runStitch([]string{"-dir", dir, "-rounds", "1"}); err != nil {
		t.Fatalf("runStitch: %v", err)
	}

	body, err := os.ReadFile(filepath.Join(dir, "README.md"))
	if err != nil {
		t.Fatalf("read root README: %v", err)
	}
	if !strings.Contains(string(body), finalizeMarker) {
		t.Errorf("finalize root/intro did not reach stitched root README; body:\n%s", body)
	}
}

// TestStitch_RefinementOverridesCodebaseContent asserts the load-order
// contract: when codebase-content writes `codebase/api/knowledge-base`
// AND refinement re-writes the same fragment, refinement wins. Pre-
// fix os.ReadDir's alphabetic order (api → app → env → refinement →
// worker) had refinement loaded BEFORE worker — wrong direction.
func TestStitch_RefinementOverridesCodebaseContent(t *testing.T) {
	dir := t.TempDir()
	if err := writeMinimalSimulationOpts(t, dir, false); err != nil {
		t.Fatalf("writeMinimalSimulationOpts: %v", err)
	}
	// writeMinimalSimulationOpts already wrote codebase/api/knowledge-base
	// under fragments-new/api/ (the codebase-content phase output).
	const ccMarker = "Authored knowledge-base body"
	const refMarker = "REFINEMENT WINS: editorial pass override"
	cbBody, err := os.ReadFile(filepath.Join(dir, "fragments-new", "api", "codebase__api__knowledge-base.md"))
	if err != nil {
		t.Fatalf("read fixture cc fragment: %v", err)
	}
	if !strings.Contains(string(cbBody), ccMarker) {
		t.Fatalf("fixture invariant: cc fragment missing baseline marker %q", ccMarker)
	}
	// Refinement re-records the same id with distinct content.
	refDir := filepath.Join(dir, "fragments-new", "refinement")
	if err := os.MkdirAll(refDir, 0o755); err != nil {
		t.Fatalf("mkdir refinement: %v", err)
	}
	mustWrite(t, filepath.Join(refDir, "codebase__api__knowledge-base.md"),
		"### Operations\n\n"+refMarker+"\n")

	if err := runStitch([]string{"-dir", dir, "-rounds", "1"}); err != nil {
		t.Fatalf("runStitch: %v", err)
	}

	body, err := os.ReadFile(filepath.Join(dir, "apidev", "README.md"))
	if err != nil {
		t.Fatalf("read apidev README: %v", err)
	}
	got := string(body)
	if !strings.Contains(got, refMarker) {
		t.Errorf("refinement KB override did not win; README lacks refinement marker %q", refMarker)
	}
	if strings.Contains(got, ccMarker) {
		t.Errorf("codebase-content KB body still present after refinement override; load-order broken\n--- README ---\n%s",
			got)
	}
}

// TestStitch_RefinementOverridesEnvComment asserts the load-order
// contract for env-import-comments: refinement's re-record of an
// env/<N>/import-comments/<service> fragment overrides env-content's.
// Routing goes through ApplyEnvComment (overwrite-on-conflict), so the
// last load wins; the load-order ensures that's refinement.
func TestStitch_RefinementOverridesEnvComment(t *testing.T) {
	dir := t.TempDir()
	if err := writeMinimalSimulationOpts(t, dir, false); err != nil {
		t.Fatalf("writeMinimalSimulationOpts: %v", err)
	}
	envDir := filepath.Join(dir, "fragments-new", "env")
	if err := os.MkdirAll(envDir, 0o755); err != nil {
		t.Fatalf("mkdir env: %v", err)
	}
	// env-content authors the project comment for tier 0.
	const envMarker = "ENV-CONTENT PROJECT COMMENT TIER 0"
	mustWrite(t, filepath.Join(envDir, "env__0__import-comments__project.md"),
		"# "+envMarker+"\n")
	// Refinement re-authors the same id.
	refDir := filepath.Join(dir, "fragments-new", "refinement")
	if err := os.MkdirAll(refDir, 0o755); err != nil {
		t.Fatalf("mkdir refinement: %v", err)
	}
	const refMarker = "REFINEMENT WINS: env comment override"
	mustWrite(t, filepath.Join(refDir, "env__0__import-comments__project.md"),
		"# "+refMarker+"\n")

	if err := runStitch([]string{"-dir", dir, "-rounds", "1"}); err != nil {
		t.Fatalf("runStitch: %v", err)
	}

	// The yaml emitter stamps EnvComments[N].Project at the top of the
	// import.yaml. Read tier 0's import.yaml and confirm the refinement
	// marker is what's written. Tier folder names carry the audience-
	// first label per recipe.Tiers().
	tier0, ok := recipeTier0()
	if !ok {
		t.Fatalf("recipeTier0: tier 0 missing from recipe.Tiers()")
	}
	body, err := os.ReadFile(filepath.Join(dir, "environments", tier0, "import.yaml"))
	if err != nil {
		t.Fatalf("read tier 0 import.yaml: %v", err)
	}
	got := string(body)
	if !strings.Contains(got, refMarker) {
		t.Errorf("refinement env-comment did not win; tier 0 import.yaml lacks refinement marker:\n%s", got)
	}
	if strings.Contains(got, envMarker) {
		t.Errorf("env-content comment still present after refinement override:\n%s", got)
	}
}

// TestStitch_FinalizeDoesNotOverrideRefinement asserts the load-order
// contract: finalize loads BEFORE refinement. If both phases write the
// SAME fragmentId (a non-issue in practice — they own non-overlapping
// surfaces), refinement still wins because it loads last.
func TestStitch_FinalizeDoesNotOverrideRefinement(t *testing.T) {
	dir := t.TempDir()
	if err := writeMinimalSimulationOpts(t, dir, false); err != nil {
		t.Fatalf("writeMinimalSimulationOpts: %v", err)
	}
	finalizeDir := filepath.Join(dir, "fragments-new", "finalize")
	refDir := filepath.Join(dir, "fragments-new", "refinement")
	for _, d := range []string{finalizeDir, refDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	const finalizeMarker = "FINALIZE PHASE ROOT INTRO"
	const refMarker = "REFINEMENT PHASE ROOT INTRO (wins)"
	mustWrite(t, filepath.Join(finalizeDir, "root__intro.md"), finalizeMarker+"\n")
	mustWrite(t, filepath.Join(refDir, "root__intro.md"), refMarker+"\n")

	if err := runStitch([]string{"-dir", dir, "-rounds", "1"}); err != nil {
		t.Fatalf("runStitch: %v", err)
	}

	body, err := os.ReadFile(filepath.Join(dir, "README.md"))
	if err != nil {
		t.Fatalf("read root README: %v", err)
	}
	got := string(body)
	if !strings.Contains(got, refMarker) {
		t.Errorf("refinement did not win over finalize; README lacks refinement marker:\n%s", got)
	}
	if strings.Contains(got, finalizeMarker) {
		t.Errorf("finalize body still present after refinement override; load-order broken:\n%s", got)
	}
}
