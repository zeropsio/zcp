package recipe

import (
	"fmt"
	"os"
	"path/filepath"
)

// gateStitchedMatchesPlan re-renders each disk-stitched surface from
// in-memory plan state and compares byte-for-byte against the file on
// disk. Divergence is reported as a blocking violation naming the path
// and the offending surface — refinement close MUST refuse so the
// deliverable that lands on disk reflects the plan-of-record.
//
// Run-40 ENG-6 — future-proofs against the run-39 regression class
// (S1-5): plan.json carried pre-refinement bodies for 5+ codebase IG
// slots whose disk-stitched READMEs had been refined cleanly. Pre-fix
// (pre-ENG-1) this happened because refinement Replace mutated
// sess.Plan + disk but never persisted plan.json. With ENG-1 in
// place, plan.json sync is now part of every refinement Replace, but
// this gate is the safety net: should any future code path mutate
// sess.Plan without re-stitching disk (or vice versa), the gate
// surfaces the divergence at refinement close. Diagnosed in
// plans/run-40-evidence-grounded-plan.md §"ENG-6".
//
// Best-effort by design. Surfaces whose on-disk path doesn't exist
// (codebase READMEs at apps-repo paths absent in synthetic-plan
// tests, container-only deploy paths) are skipped without a
// violation; an absent surface isn't a divergence, it's a non-
// production-shape session. Production runs have OutputRoot
// materialized end-to-end and every relevant surface present.
//
// Read-only — no disk writes. Pure invariant check.
func gateStitchedMatchesPlan(ctx GateContext) []Violation {
	if ctx.Plan == nil || ctx.OutputRoot == "" {
		return nil
	}
	var out []Violation

	// Root README. Always under OutputRoot in production; the
	// not-present case is the in-memory-only test path.
	if body, _, err := AssembleRootREADME(ctx.Plan); err == nil {
		path := filepath.Join(ctx.OutputRoot, "README.md")
		if v := stitchedSurfaceDiverged(path, body, "root README"); v != nil {
			out = append(out, *v)
		}
	}

	// Env READMEs — one per tier. Tier folders sit under OutputRoot
	// and are always present in production output trees.
	for i := range Tiers() {
		body, _, err := AssembleEnvREADME(ctx.Plan, i)
		if err != nil {
			continue
		}
		tier, ok := TierAt(i)
		if !ok {
			continue
		}
		path := filepath.Join(ctx.OutputRoot, tier.Folder, "README.md")
		if v := stitchedSurfaceDiverged(path, body, fmt.Sprintf("env tier %d README", i)); v != nil {
			out = append(out, *v)
		}
	}

	// Codebase READMEs — land on the apps-repo path (cb.SourceRoot)
	// which is the dev-container mount point in production. Best-
	// effort: skip codebases whose SourceRoot doesn't exist on the
	// local filesystem (test sessions with synthetic /var/www paths,
	// pre-mount research/provision-phase sessions).
	for _, cb := range ctx.Plan.Codebases {
		if cb.SourceRoot == "" {
			continue
		}
		if _, err := os.Stat(cb.SourceRoot); err != nil {
			continue
		}
		body, _, err := AssembleCodebaseREADME(ctx.Plan, cb.Hostname)
		if err != nil {
			continue
		}
		path := filepath.Join(cb.SourceRoot, "README.md")
		if v := stitchedSurfaceDiverged(path, body, fmt.Sprintf("codebase %s README", cb.Hostname)); v != nil {
			out = append(out, *v)
		}
	}

	return out
}

// stitchedSurfaceDiverged reads path from disk and compares to the
// freshly-assembled wantBody. Returns a Violation when the bodies
// differ or when the path is absent. Run-40 fix-up #3 — pre-fix this
// returned nil on os.ReadFile failure, which made the gate fail-open
// against the canonical regression class (refinement Replace lands
// in plan but disk write was skipped → disk file absent → gate
// soft-passes → deliverable ships with no surface at all). Codex
// code review flagged the fail-open as the gate's silent bypass.
//
// Callers are responsible for not invoking the comparison on
// surfaces that legitimately don't exist (e.g. codebase READMEs
// when SourceRoot isn't materialized in test sessions). The caller-
// side os.Stat check on cb.SourceRoot in gateStitchedMatchesPlan
// scopes this gate to production shape.
//
// label names the surface in user-facing prose ("root README", "env
// tier 0 README", "codebase api README"). Path goes in Violation.Path
// so the agent can jump straight to the divergent file.
func stitchedSurfaceDiverged(path, wantBody, label string) *Violation {
	got, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Violation{
				Code:     "stitched-surface-missing",
				Path:     path,
				Severity: SeverityBlocking,
				Message: fmt.Sprintf(
					"%s missing on disk — plan.json carries fragment content that re-renders to %d bytes but no file exists at the target path. The deliverable would ship without this surface. Run `zerops_recipe action=stitch-content` to materialize from plan-of-record.",
					label, len(wantBody)),
			}
		}
		return &Violation{
			Code:     "stitched-surface-read-failed",
			Path:     path,
			Severity: SeverityBlocking,
			Message: fmt.Sprintf(
				"%s could not be read from disk (%v) — the gate cannot verify the deliverable surface matches plan.json. Fix the filesystem error and re-run complete-phase.",
				label, err),
		}
	}
	if string(got) == wantBody {
		return nil
	}
	gotLen, wantLen := len(got), len(wantBody)
	return &Violation{
		Code:     "stitched-surface-divergence",
		Path:     path,
		Severity: SeverityBlocking,
		Message: fmt.Sprintf(
			"%s on disk does not match re-render from plan.json — disk is %d bytes, re-render is %d. The deliverable would ship in a state the plan-of-record can no longer reproduce. Run `zerops_recipe action=stitch-content` to bring disk back into agreement with plan.json, or `action=record-fragment mode=replace` to re-author the fragment if disk holds the intended body.",
			label, gotLen, wantLen),
	}
}
