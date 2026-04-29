package recipe

import (
	"path/filepath"
	"strings"
	"testing"
)

// Run-17 §9.5 — refinement-phase Replace transactional wrapper.
// On PhaseRefinement Replace of a codebase/<host>/... fragment the
// engine snapshots the prior body, applies the Replace, runs surface
// validators, and reverts to the snapshot if a new blocking violation
// surfaces. Per-fragment edit cap = 1; the agent does not loop.

func TestRefinementReplace_ValidatorsViolate_FragmentReverts(t *testing.T) {
	t.Parallel()
	sess := buildRefinementSession(t)
	const fragmentID = "codebase/api/integration-guide/2"
	priorBody := sess.Plan.Fragments[fragmentID]
	if priorBody == "" {
		t.Fatal("setup: precondition fragment missing")
	}

	// Replace body with a slot-shape-clean H3 whose body carries a
	// plain ordered list — slot_shape passes (one ### heading) but
	// validateCodebaseIG fires `codebase-ig-plain-ordered-list` on the
	// assembled README post-replace.
	in := RecipeInput{
		Action:     "record-fragment",
		Slug:       sess.Slug,
		FragmentID: fragmentID,
		Fragment:   "### 2. Trust the reverse proxy\n\n1. plain ordered\n2. list shape\n",
		Mode:       "replace",
	}
	r := handleRecordFragment(sess, in, RecipeResult{Action: "record-fragment", Slug: sess.Slug})

	if r.Error != "" {
		t.Fatalf("unexpected error: %s", r.Error)
	}
	if got := sess.SnapshotFragment(fragmentID); got != priorBody {
		t.Errorf("fragment was not reverted; got %q, want priorBody", got)
	}
	found := false
	for _, n := range r.Notices {
		if n.Code == "refinement-replace-reverted" && strings.Contains(n.Message, "codebase-ig-plain-ordered-list") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected refinement-replace-reverted notice naming codebase-ig-plain-ordered-list; got %+v", r.Notices)
	}
	if r.BodyBytes != len(priorBody) {
		t.Errorf("BodyBytes after revert = %d, want %d (priorBody length)", r.BodyBytes, len(priorBody))
	}
}

func TestRefinementReplace_ValidatorsPass_FragmentChanged(t *testing.T) {
	t.Parallel()
	sess := buildRefinementSession(t)
	const fragmentID = "codebase/api/integration-guide/2"

	// Replace body with a clean H3 that introduces no new violations.
	newBody := "### 2. Trust the L7 proxy for `request.ip` and HTTPS\n\nWithout `trust proxy`, NestJS sees the balancer's internal IP.\n"
	in := RecipeInput{
		Action:     "record-fragment",
		Slug:       sess.Slug,
		FragmentID: fragmentID,
		Fragment:   newBody,
		Mode:       "replace",
	}
	r := handleRecordFragment(sess, in, RecipeResult{Action: "record-fragment", Slug: sess.Slug})

	if r.Error != "" {
		t.Fatalf("unexpected error: %s", r.Error)
	}
	if got := sess.SnapshotFragment(fragmentID); got != newBody {
		t.Errorf("clean refinement Replace should persist new body; got %q, want %q", got, newBody)
	}
	for _, n := range r.Notices {
		if n.Code == "refinement-replace-reverted" {
			t.Errorf("clean Replace should not produce refinement-replace-reverted notice; got %+v", n)
		}
	}
}

func TestRefinementReplace_NonCodebaseFragment_BypassesWrapper(t *testing.T) {
	t.Parallel()
	sess := buildRefinementSession(t)

	// env/<N>/intro fragments don't bind to a single codebase; the
	// transactional wrapper skips them and the unwrapped recordFragment
	// path applies the Replace directly. Slot-shape is the safety net.
	in := RecipeInput{
		Action:     "record-fragment",
		Slug:       sess.Slug,
		FragmentID: "env/0/intro",
		Fragment:   "Tier 0 — agent workspace.",
		Mode:       "replace",
	}
	r := handleRecordFragment(sess, in, RecipeResult{Action: "record-fragment", Slug: sess.Slug})
	if r.Error != "" {
		t.Fatalf("unexpected error: %s", r.Error)
	}
	for _, n := range r.Notices {
		if n.Code == "refinement-replace-reverted" {
			t.Errorf("non-codebase fragment should bypass the wrapper; got %+v", n)
		}
	}
	if got := sess.SnapshotFragment("env/0/intro"); got != "Tier 0 — agent workspace." {
		t.Errorf("env intro should be applied; got %q", got)
	}
}

func TestRefinementReplace_NonRefinementPhase_BypassesWrapper(t *testing.T) {
	t.Parallel()
	sess := buildRefinementSession(t)
	// Pretend we're back at codebase-content phase. The wrapper only
	// fires at PhaseRefinement; at other phases the slot-shape +
	// classification refusals are the only contracts at record time.
	sess.Current = PhaseCodebaseContent

	in := RecipeInput{
		Action:     "record-fragment",
		Slug:       sess.Slug,
		FragmentID: "codebase/api/integration-guide/2",
		Fragment:   "### 2. Trust the reverse proxy\n\n1. plain\n2. ordered\n",
		Mode:       "replace",
	}
	r := handleRecordFragment(sess, in, RecipeResult{Action: "record-fragment", Slug: sess.Slug})
	if r.Error != "" {
		t.Fatalf("unexpected error: %s", r.Error)
	}
	for _, n := range r.Notices {
		if n.Code == "refinement-replace-reverted" {
			t.Errorf("non-refinement phase should bypass the wrapper; got %+v", n)
		}
	}
	// Body persisted without rollback even though it would have fired
	// surface-validator violations at finalize.
	if !strings.Contains(sess.SnapshotFragment("codebase/api/integration-guide/2"), "1. plain\n2. ordered") {
		t.Error("body should persist outside the wrapper")
	}
}

// buildRefinementSession returns a Session at PhaseRefinement with a
// clean baseline plan: every codebase has slotted IG + KB + CLAUDE +
// intro fragments that pass surface validators when assembled. Used
// by the transactional wrapper tests.
func buildRefinementSession(t *testing.T) *Session {
	t.Helper()
	dir := t.TempDir()
	plan := syntheticShowcasePlan()
	stageScaffoldYAMLs(t, dir, plan)

	plan.Fragments = map[string]string{}
	for _, cb := range plan.Codebases {
		base := "codebase/" + cb.Hostname
		plan.Fragments[base+"/intro"] = "Codebase intro paragraph.\n"
		// Two slotted IG items so codebase-ig-too-few-items doesn't
		// fire (engine injects IG #1 at assemble; we plant slots 2 + 3).
		plan.Fragments[base+"/integration-guide/2"] = "### 2. Trust the reverse proxy\n\nSet trust proxy.\n"
		plan.Fragments[base+"/integration-guide/3"] = "### 3. Drain on SIGTERM\n\nGraceful shutdown.\n"
		plan.Fragments[base+"/knowledge-base"] = "- **404 on Topic** — explanation that satisfies the bullet contract.\n"
		plan.Fragments[base+"/claude-md"] = "# " + cb.Hostname + "\n\nApplication scaffold authored by the framework's stock generator.\n\n## Build & run\n\n- npm install\n- npm test\n- npm run start:dev\n\n## Architecture\n\n- src/main.ts — bootstrap entry\n- src/app.module.ts — root module\n- src/items/ — items domain\n"
	}

	sess := &Session{
		Slug:       plan.Slug,
		Current:    PhaseRefinement,
		Plan:       plan,
		OutputRoot: filepath.Join(dir, "run"),
		Completed: map[Phase]bool{
			PhaseResearch:        true,
			PhaseProvision:       true,
			PhaseScaffold:        true,
			PhaseFeature:         true,
			PhaseCodebaseContent: true,
			PhaseEnvContent:      true,
			PhaseFinalize:        true,
		},
	}
	return sess
}
