package recipe

import (
	"path/filepath"
	"strings"
	"testing"
)

// handlers_refinement_env_snapshot_test.go — Run-46 Item 3 substrate pin.
//
// Run-45 forensic — refinement-2 ACTed on a tier-yaml-comment finding
// (TY5 canonical-block strip) via `record-fragment mode=replace` against
// `env/<N>/import-comments/<host>`. The replacement landed in plan state
// + re-stitched the env README, but the new body violated the
// priority-justification gate. The violation only surfaced at finalize/
// refinement close-time, requiring a re-record cycle.
//
// The codebase fragment path is protected by the snapshot/restore
// wrapper (run-17 §9.5): record-fragment runs `refinementPreCheckScoped`
// pre-replace, then re-runs it post-replace; if the replace introduced
// a NEW blocking violation, the wrapper reverts to the pre-replace body
// and surfaces a `refinement-replace-reverted` notice.
//
// Pre-Item-3 the env fragments (`env/<N>/import-comments/...`) had no
// equivalent wrapper — "best-effort revert" per the handlers.go
// comment. Item 3 extends the wrapper to fire for env fragments too,
// running the env-tier validators (EnvGates) pre/post replace.

// TestWrapRefinement_TierImportCommentsWrapped — TY5-breaking replace
// on env/<tier>/import-comments/<host> reverts via snapshot/restore.
// The bad body never persists; the prior body is restored; a
// refinement-replace-reverted notice surfaces.
func TestWrapRefinement_TierImportCommentsWrapped(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
	sess := NewSession("synth-showcase", "dev", log, dir, nil)
	sess.Plan = syntheticShowcasePlan()
	// Inject a valid priority-justification block on the db service
	// comment so the PRE-replace state passes the priority-justification
	// gate. The gate looks for the priority block BETWEEN the first
	// service line and the first `priority: 10` line; placing the
	// justification on db (the first priority-10 service) satisfies it.
	// Without this the gate fires identically pre + post and the
	// wrapper has no reason to revert.
	ec := sess.Plan.EnvComments["0"]
	if ec.Service == nil {
		ec.Service = map[string]string{}
	}
	ec.Service["db"] = "Set higher priority for databases and storages,\nbecause the app depends on those services."
	sess.Plan.EnvComments["0"] = ec
	stageScaffoldYAMLs(t, dir, sess.Plan)
	sess.OutputRoot = dir
	if _, err := stitchContent(sess); err != nil {
		t.Fatalf("seed stitch-content: %v", err)
	}

	// Walk to PhaseRefinement.
	for _, p := range []Phase{
		PhaseResearch, PhaseProvision, PhaseScaffold, PhaseFeature,
		PhaseCodebaseContent, PhaseEnvContent, PhaseFinalize, PhaseRefinement,
	} {
		if err := sess.EnterPhase(p); err != nil {
			t.Fatalf("EnterPhase(%s): %v", p, err)
		}
		sess.Completed[p] = true
	}
	sess.Completed[PhaseRefinement] = false
	sess.Current = PhaseRefinement

	priorComment := sess.Plan.EnvComments["0"].Service["db"]
	if priorComment == "" {
		t.Fatal("test fixture: tier 0 db comment must be populated to exercise revert")
	}

	// Replace the db comment with a body that strips the priority-
	// justification block. The priority-justification gate must surface
	// a new blocking violation that wasn't present pre-replace; the
	// wrapper must then revert.
	badBody := "Postgres for the showcase storage layer."
	in := RecipeInput{
		Action:     "record-fragment",
		FragmentID: "env/0/import-comments/db",
		Fragment:   badBody,
		Mode:       modeReplace,
	}
	r := handleRecordFragment(sess, in, RecipeResult{Action: "record-fragment"})
	if !r.OK {
		// The wrapper allows the underlying recordFragment to succeed
		// then reverts via a Notice. A hard-Error here would be a
		// different failure mode — surface for diagnosis.
		t.Fatalf("expected wrapper to succeed via Notice-revert, got Error=%q", r.Error)
	}
	// The notice must name the revert.
	foundNotice := false
	for _, n := range r.Notices {
		if n.Code == "refinement-replace-reverted" {
			foundNotice = true
			break
		}
	}
	if !foundNotice {
		t.Errorf("expected refinement-replace-reverted notice on env-fragment wrapper revert; got Notices=%+v", r.Notices)
	}
	// And the prior body must be restored.
	gotComment := sess.Plan.EnvComments["0"].Service["db"]
	if gotComment != priorComment {
		t.Errorf("env fragment was NOT reverted to prior body. Got: %q\nWant: %q", gotComment, priorComment)
	}
}

// TestWrapRefinement_TierImportCommentsCleanReplace — a tier-yaml-
// comment replace that does NOT introduce a new violation MUST land
// successfully (no false-positive revert).
func TestWrapRefinement_TierImportCommentsCleanReplace(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
	sess := NewSession("synth-showcase", "dev", log, dir, nil)
	sess.Plan = syntheticShowcasePlan()
	stageScaffoldYAMLs(t, dir, sess.Plan)
	sess.OutputRoot = dir
	if _, err := stitchContent(sess); err != nil {
		t.Fatalf("seed stitch-content: %v", err)
	}
	for _, p := range []Phase{
		PhaseResearch, PhaseProvision, PhaseScaffold, PhaseFeature,
		PhaseCodebaseContent, PhaseEnvContent, PhaseFinalize, PhaseRefinement,
	} {
		if err := sess.EnterPhase(p); err != nil {
			t.Fatalf("EnterPhase(%s): %v", p, err)
		}
		sess.Completed[p] = true
	}
	sess.Completed[PhaseRefinement] = false
	sess.Current = PhaseRefinement

	// A neutral edit on the project comment — adds prose without
	// breaking any tier-yaml gate.
	cleanBody := `AI agent workspace — a dev slot per codebase for SSH iteration plus a stage slot that validates the production build path. Updated prose for clarity.`
	in := RecipeInput{
		Action:     "record-fragment",
		FragmentID: "env/0/import-comments/project",
		Fragment:   cleanBody,
		Mode:       modeReplace,
	}
	r := handleRecordFragment(sess, in, RecipeResult{Action: "record-fragment"})
	if !r.OK {
		t.Fatalf("expected ok=true on clean tier-yaml-comment replace; got Error=%q", r.Error)
	}
	// No revert notice should fire.
	for _, n := range r.Notices {
		if n.Code == "refinement-replace-reverted" {
			t.Errorf("clean replace must NOT trigger revert; got %+v", n)
		}
	}
	// And the new body must be persisted.
	gotComment := sess.Plan.EnvComments["0"].Project
	if !strings.Contains(gotComment, "Updated prose for clarity") {
		t.Errorf("env fragment was reverted on clean replace. Got: %q", gotComment)
	}
}
