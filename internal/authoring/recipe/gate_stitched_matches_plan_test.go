package recipe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Run-40 ENG-6 — gateStitchedMatchesPlan refuses refinement close when
// any disk-stitched surface diverges from a fresh re-render of the
// in-memory plan. Diagnosed in
// plans/run-40-evidence-grounded-plan.md §"ENG-6".

// TestGatesForPhase_Refinement_IncludesStitchedMatchesPlan pins the
// gate registration: refinement complete-phase MUST include the
// stitched-matches-plan check.
func TestGatesForPhase_Refinement_IncludesStitchedMatchesPlan(t *testing.T) {
	t.Parallel()
	gates := gatesForPhase(PhaseRefinement)
	mustHaveGate(t, gates, "stitched-matches-plan")
}

// TestGateStitchedMatchesPlan_NoSession_NoViolations — empty context
// produces no violations (in-memory-only research sessions, pre-mount
// bootstrap). Gate is non-destructive on incomplete shapes.
func TestGateStitchedMatchesPlan_NoSession_NoViolations(t *testing.T) {
	t.Parallel()
	v := gateStitchedMatchesPlan(GateContext{})
	if len(v) != 0 {
		t.Errorf("empty context should produce no violations; got %+v", v)
	}
	v = gateStitchedMatchesPlan(GateContext{Plan: syntheticShowcasePlan()})
	if len(v) != 0 {
		t.Errorf("plan without outputRoot should produce no violations; got %+v", v)
	}
}

// TestGateStitchedMatchesPlan_FreshStitch_NoViolations is the happy
// path: after stitch-content lands the same plan state to disk as
// what's in memory, re-rendering and reading agree byte-for-byte.
func TestGateStitchedMatchesPlan_FreshStitch_NoViolations(t *testing.T) {
	t.Parallel()
	sess := buildRefinementSessionWithDisk(t)

	v := gateStitchedMatchesPlan(GateContext{
		Plan:       sess.Plan,
		OutputRoot: sess.OutputRoot,
	})
	if len(v) != 0 {
		t.Errorf("fresh-stitch should produce no divergence violations; got %+v", v)
	}
}

// TestGateStitchedMatchesPlan_EnvReadmeOnDiskDiverges fires the
// canonical regression: an env tier README on disk has been mutated
// out from under plan.json (the inverse of run-39's S1-5: there it
// was plan.json that lagged disk; the gate catches both directions
// because it compares re-render to disk symmetrically).
func TestGateStitchedMatchesPlan_EnvReadmeOnDiskDiverges(t *testing.T) {
	t.Parallel()
	sess := buildRefinementSessionWithDisk(t)

	// Overwrite tier 0 README on disk with a body that re-render
	// from plan won't produce.
	tier, _ := TierAt(0)
	path := filepath.Join(sess.OutputRoot, tier.Folder, "README.md")
	if err := os.WriteFile(path, []byte("Hand-edited tier 0 README — divergent from plan.\n"), 0o600); err != nil {
		t.Fatalf("seed divergent disk body: %v", err)
	}

	v := gateStitchedMatchesPlan(GateContext{
		Plan:       sess.Plan,
		OutputRoot: sess.OutputRoot,
	})
	if len(v) == 0 {
		t.Fatalf("expected divergence violation for tier 0 README; got none")
	}
	var found bool
	for _, x := range v {
		if x.Code == "stitched-surface-divergence" && strings.Contains(x.Path, tier.Folder) {
			found = true
			if x.Severity != SeverityBlocking {
				t.Errorf("divergence violation should be Blocking; got %v", x.Severity)
			}
			if !strings.Contains(x.Message, "tier 0") {
				t.Errorf("violation message should name the divergent surface; got %q", x.Message)
			}
		}
	}
	if !found {
		t.Errorf("no tier-0-README divergence in violations: %+v", v)
	}
}

// TestGateStitchedMatchesPlan_RootReadmeOnDiskDiverges — root README
// has the same coverage as env tiers: divergence is blocking.
func TestGateStitchedMatchesPlan_RootReadmeOnDiskDiverges(t *testing.T) {
	t.Parallel()
	sess := buildRefinementSessionWithDisk(t)

	path := filepath.Join(sess.OutputRoot, "README.md")
	if err := os.WriteFile(path, []byte("Hand-edited root README that diverges from the plan.\n"), 0o600); err != nil {
		t.Fatalf("seed divergent root README: %v", err)
	}

	v := gateStitchedMatchesPlan(GateContext{
		Plan:       sess.Plan,
		OutputRoot: sess.OutputRoot,
	})
	if len(v) == 0 {
		t.Fatalf("expected divergence violation for root README; got none")
	}
	var found bool
	for _, x := range v {
		if x.Code == "stitched-surface-divergence" && strings.Contains(x.Message, "root README") {
			found = true
		}
	}
	if !found {
		t.Errorf("no root-README divergence in violations: %+v", v)
	}
}

// TestGateStitchedMatchesPlan_MissingSurface_FailsClosed pins the
// run-40 fix-up #3 contract: when a surface re-renders to non-empty
// content but no file exists at the target path, the gate emits a
// blocking violation instead of soft-passing. Pre-fix the gate
// fail-opened on os.ReadFile error, which masked the canonical
// refinement-write-back regression class (plan updated, disk skipped
// → surface absent → gate misses it). Codex code review caught this.
func TestGateStitchedMatchesPlan_MissingSurface_FailsClosed(t *testing.T) {
	t.Parallel()
	sess := buildRefinementSessionWithDisk(t)

	// Remove tier 0 README so the gate sees an absent surface with
	// non-empty plan-derived content.
	tier, _ := TierAt(0)
	path := filepath.Join(sess.OutputRoot, tier.Folder, "README.md")
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove env tier README to seed missing surface: %v", err)
	}

	v := gateStitchedMatchesPlan(GateContext{
		Plan:       sess.Plan,
		OutputRoot: sess.OutputRoot,
	})
	var found bool
	for _, x := range v {
		if x.Code == "stitched-surface-missing" && strings.Contains(x.Path, tier.Folder) {
			found = true
			if x.Severity != SeverityBlocking {
				t.Errorf("missing-surface violation should be Blocking; got %v", x.Severity)
			}
		}
	}
	if !found {
		t.Errorf("expected stitched-surface-missing violation; got %+v", v)
	}
}

// TestGateStitchedMatchesPlan_SkipsAbsentCodebaseSourceRoot pins the
// best-effort design: a codebase whose SourceRoot path doesn't exist
// on disk is skipped silently (no violation). Tests use synthetic
// /var/www/<host>dev paths that aren't materialized; production runs
// have real apps-repo mounts.
func TestGateStitchedMatchesPlan_SkipsAbsentCodebaseSourceRoot(t *testing.T) {
	t.Parallel()
	sess := buildRefinementSessionWithDisk(t)
	// Re-point one codebase to a guaranteed-absent path.
	for i, cb := range sess.Plan.Codebases {
		if i == 0 {
			sess.Plan.Codebases[i].SourceRoot = filepath.Join(t.TempDir(), "definitely-not-here", cb.Hostname)
			break
		}
	}

	v := gateStitchedMatchesPlan(GateContext{
		Plan:       sess.Plan,
		OutputRoot: sess.OutputRoot,
	})
	for _, x := range v {
		// Surfaces from the rewritten codebase MUST NOT trip the
		// gate — the SourceRoot doesn't exist on disk so the gate
		// skips that codebase entirely.
		if strings.Contains(x.Path, "definitely-not-here") {
			t.Errorf("codebase README at absent SourceRoot should be skipped, not flagged: %+v", x)
		}
	}
}
