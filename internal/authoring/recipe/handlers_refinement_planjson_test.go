package recipe

import (
	"testing"
)

// Run-40 ENG-1 — refinement-phase non-write-back to plan.json.
//
// Pre-fix: handleRecordFragment updated sess.Plan.Fragments[id] in
// memory and (during refinement) re-stitched the published disk
// surfaces (codebase fragments via the transactional wrapper's
// validator pass, env fragments via preStitchEnv). plan.json on disk
// was NOT re-written. Run-39's refinement landed ACT-edits on
// codebase/api/integration-guide/{3,4}, codebase/app/integration-guide/
// {2,3}, codebase/worker/integration-guide/4 — every disk-stitched
// README reflected the new body; every plan.json fragment held the
// stale pre-refinement body. Any tool that re-renders from plan.json
// regenerates pre-refinement content; refinement is lossy at the
// plan-of-record layer.
//
// Fix: handleRecordFragment persists plan.json to disk after a
// successful refinement-phase mutation lands in sess.Plan. Snapshot
// taken under sess.mu, write happens unlocked (CLAUDE.md "Hold
// mutexes during I/O — copy under lock, release, then I/O").
//
// Diagnosed in plans/run-40-evidence-grounded-plan.md §"S1-5".

// TestRefinementReplaceCodebaseFragment_WritesPlanJson pins the
// primary closure: a refinement Replace on a codebase IG slot must
// land in <outputRoot>/plan.json so re-rendering from plan.json
// returns the post-refinement body, not the pre-refinement body.
func TestRefinementReplaceCodebaseFragment_WritesPlanJson(t *testing.T) {
	t.Parallel()
	sess := buildRefinementSessionWithDisk(t)
	const fragmentID = "codebase/api/integration-guide/2"

	// Seed an initial plan.json so re-reads have a baseline.
	if err := WritePlan(sess.OutputRoot, sess.Plan); err != nil {
		t.Fatalf("seed WritePlan: %v", err)
	}

	const newBody = "### 2. Trust the L7 proxy for `request.ip` and HTTPS\n\nWithout `trust proxy`, NestJS sees the balancer's internal IP.\n"
	in := RecipeInput{
		Action:         "record-fragment",
		Slug:           sess.Slug,
		FragmentID:     fragmentID,
		Fragment:       newBody,
		Mode:           "replace",
		Classification: "platform-invariant",
	}
	r := handleRecordFragment(sess, in, RecipeResult{Action: "record-fragment", Slug: sess.Slug})
	if r.Error != "" {
		t.Fatalf("unexpected error: %s", r.Error)
	}

	// In-memory plan-state landed.
	if got := sess.SnapshotFragment(fragmentID); got != newBody {
		t.Fatalf("in-memory fragment did not update; got %q, want %q", got, newBody)
	}

	// plan.json on disk MUST mirror the in-memory fragment after a
	// refinement Replace — that's the closure of ENG-1.
	onDisk, err := ReadPlan(sess.OutputRoot)
	if err != nil {
		t.Fatalf("ReadPlan: %v", err)
	}
	if got := onDisk.Fragments[fragmentID]; got != newBody {
		t.Errorf("plan.json on disk did NOT pick up the refinement Replace.\nFragment id: %s\nWant body:\n%s\nGot body:\n%s",
			fragmentID, newBody, got)
	}
}

// TestRefinementReplaceEnvIntro_WritesPlanJson — env-intro fragments
// route through the same plan.Fragments map as codebase fragments;
// the env-stitch path is independent of the plan.json write. Pinned
// separately so a regression in either branch surfaces independently.
func TestRefinementReplaceEnvIntro_WritesPlanJson(t *testing.T) {
	t.Parallel()
	sess := buildRefinementSessionWithDisk(t)
	if err := WritePlan(sess.OutputRoot, sess.Plan); err != nil {
		t.Fatalf("seed WritePlan: %v", err)
	}

	const newIntro = "Tier 0 — refined AI-agent workspace intro for plan.json sync."
	in := RecipeInput{
		Action:     "record-fragment",
		Slug:       sess.Slug,
		FragmentID: "env/0/intro",
		Fragment:   newIntro,
		Mode:       "replace",
	}
	r := handleRecordFragment(sess, in, RecipeResult{Action: "record-fragment", Slug: sess.Slug})
	if r.Error != "" {
		t.Fatalf("unexpected error: %s", r.Error)
	}

	onDisk, err := ReadPlan(sess.OutputRoot)
	if err != nil {
		t.Fatalf("ReadPlan: %v", err)
	}
	if got := onDisk.Fragments["env/0/intro"]; got != newIntro {
		t.Errorf("plan.json missing refined env intro.\nWant:\n%s\nGot:\n%s", newIntro, got)
	}
}

// TestRefinementReplaceEnvImportComment_WritesPlanJson — env
// import-comments route through ApplyEnvComment to plan.EnvComments
// (not plan.Fragments). The plan.json persistence must mirror that
// branch too, otherwise refinement-time tier-comment edits stay
// lossy at the plan-of-record layer.
func TestRefinementReplaceEnvImportComment_WritesPlanJson(t *testing.T) {
	t.Parallel()
	sess := buildRefinementSessionWithDisk(t)
	if err := WritePlan(sess.OutputRoot, sess.Plan); err != nil {
		t.Fatalf("seed WritePlan: %v", err)
	}

	const newComment = "Postgres — refined comment for plan.json sync."
	in := RecipeInput{
		Action:     "record-fragment",
		Slug:       sess.Slug,
		FragmentID: "env/0/import-comments/db",
		Fragment:   newComment,
		Mode:       "replace",
	}
	r := handleRecordFragment(sess, in, RecipeResult{Action: "record-fragment", Slug: sess.Slug})
	if r.Error != "" {
		t.Fatalf("unexpected error: %s", r.Error)
	}

	onDisk, err := ReadPlan(sess.OutputRoot)
	if err != nil {
		t.Fatalf("ReadPlan: %v", err)
	}
	if got := onDisk.EnvComments["0"].Service["db"]; got != newComment {
		t.Errorf("plan.json missing refined env import-comment.\nWant: %s\nGot: %s", newComment, got)
	}
}

// TestRefinementReplaceRevertedFragment_PlanJsonHoldsPriorBody — when
// the refinement transactional wrapper REVERTS a Replace because the
// post-replace validator surfaces a new blocking violation, plan.json
// must end up holding the priorBody (the snapshot the wrapper
// restored), not the rejected attempted body. Pins the contract:
// plan.json persistence reads the final in-memory state, not the
// transient mid-call state.
func TestRefinementReplaceRevertedFragment_PlanJsonHoldsPriorBody(t *testing.T) {
	t.Parallel()
	sess := buildRefinementSessionWithDisk(t)
	const fragmentID = "codebase/api/integration-guide/2"
	priorBody := sess.SnapshotFragment(fragmentID)
	if priorBody == "" {
		t.Fatal("setup: precondition fragment missing")
	}
	if err := WritePlan(sess.OutputRoot, sess.Plan); err != nil {
		t.Fatalf("seed WritePlan: %v", err)
	}

	// Replace body with a slot-shape-clean H3 whose body would trip
	// codebase-ig-plain-ordered-list at post-replace surface validation.
	in := RecipeInput{
		Action:         "record-fragment",
		Slug:           sess.Slug,
		FragmentID:     fragmentID,
		Fragment:       "### 2. Trust the reverse proxy\n\n1. plain ordered\n2. list shape\n",
		Mode:           "replace",
		Classification: "platform-invariant",
	}
	r := handleRecordFragment(sess, in, RecipeResult{Action: "record-fragment", Slug: sess.Slug})
	if r.Error != "" {
		t.Fatalf("unexpected error: %s", r.Error)
	}

	// In-memory state reverted to priorBody.
	if got := sess.SnapshotFragment(fragmentID); got != priorBody {
		t.Fatalf("expected in-memory revert; got %q", got)
	}
	// plan.json reflects priorBody too — NOT the rejected attempt.
	onDisk, err := ReadPlan(sess.OutputRoot)
	if err != nil {
		t.Fatalf("ReadPlan: %v", err)
	}
	if got := onDisk.Fragments[fragmentID]; got != priorBody {
		t.Errorf("plan.json should hold reverted priorBody after a rejected refinement Replace.\nWant body bytes: %d\nGot body bytes: %d",
			len(priorBody), len(got))
	}
}

// TestRefinementReplace_OutputRootEmpty_NoPanic guards the in-memory
// test-fixture case: when sess.OutputRoot is "" (no on-disk root)
// the plan.json sync MUST be a no-op, not a panic or error. WritePlan
// already short-circuits on empty outputRoot; this pins that the
// handler call site doesn't synthesize a path.
func TestRefinementReplace_OutputRootEmpty_NoPanic(t *testing.T) {
	t.Parallel()
	sess := buildRefinementSession(t)
	sess.OutputRoot = "" // in-memory only

	in := RecipeInput{
		Action:         "record-fragment",
		Slug:           sess.Slug,
		FragmentID:     "codebase/api/integration-guide/2",
		Fragment:       "### 2. Trust the L7 proxy\n\nClean body.\n",
		Mode:           "replace",
		Classification: "platform-invariant",
	}
	r := handleRecordFragment(sess, in, RecipeResult{Action: "record-fragment", Slug: sess.Slug})
	if r.Error != "" {
		t.Fatalf("empty outputRoot should not surface a plan.json write error; got %q", r.Error)
	}
}
