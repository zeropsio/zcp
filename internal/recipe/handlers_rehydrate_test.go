// Tests for: Store.OpenOrCreate plan-rehydration — Fix 2 of run-25
// unblock work. Background: a sub-agent dispatch runs in a different MCP
// server instance from the main agent; previously OpenOrCreate created a
// fresh empty Plan even when <outputRoot>/plan.json had the full plan on
// disk. Run-24 evidence: sub-agent observed `Plan codebases: []` while
// main-agent had a populated plan.json — `complete-phase scaffold
// codebase=api` failed with "unknown codebase" because the rehydrate
// step didn't exist.
package recipe

import (
	"os"
	"path/filepath"
	"testing"
)

// TestOpenOrCreate_RehydratesPlanFromDisk_WhenPlanJsonExists writes a
// plan.json to disk before opening the session; the freshly-created
// session must read it on open so cross-process state holds.
func TestOpenOrCreate_RehydratesPlanFromDisk_WhenPlanJsonExists(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	outputRoot := filepath.Join(dir, "run")
	// Pre-write a plan to disk (simulating a prior process's persisted
	// state). Use raw WritePlan because it is the canonical persist path.
	persisted := &Plan{
		Slug:      "synth-showcase",
		Framework: "nestjs",
		Codebases: []Codebase{
			{Hostname: "api", Role: RoleAPI},
			{Hostname: "app", Role: RoleFrontend},
		},
	}
	// WritePlan needs the directory to exist.
	if err := mkdirRecipe(outputRoot); err != nil {
		t.Fatalf("mkdir outputRoot: %v", err)
	}
	if err := WritePlan(outputRoot, persisted); err != nil {
		t.Fatalf("WritePlan: %v", err)
	}

	store := NewStore(dir)
	sess, err := store.OpenOrCreate("synth-showcase", outputRoot)
	if err != nil {
		t.Fatalf("OpenOrCreate: %v", err)
	}
	if sess.Plan == nil {
		t.Fatal("session Plan is nil — rehydration did not run")
	}
	if got := len(sess.Plan.Codebases); got != 2 {
		t.Errorf("Plan.Codebases len = %d, want 2 (rehydrate from disk)", got)
	}
	if sess.Plan.Framework != "nestjs" {
		t.Errorf("Plan.Framework = %q, want nestjs (rehydrate from disk)", sess.Plan.Framework)
	}
}

// TestOpenOrCreate_FreshSession_WhenPlanJsonAbsent confirms the no-disk
// path remains a fresh empty plan — pre-fix-2 behavior must persist when
// no plan.json exists at outputRoot.
func TestOpenOrCreate_FreshSession_WhenPlanJsonAbsent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	outputRoot := filepath.Join(dir, "run-fresh")
	store := NewStore(dir)
	sess, err := store.OpenOrCreate("synth-showcase", outputRoot)
	if err != nil {
		t.Fatalf("OpenOrCreate: %v", err)
	}
	if sess.Plan == nil {
		t.Fatal("session Plan is nil — fresh path should still init Plan{Slug}")
	}
	if got := len(sess.Plan.Codebases); got != 0 {
		t.Errorf("Plan.Codebases len = %d, want 0 (fresh session, no disk)", got)
	}
	if sess.Plan.Slug != "synth-showcase" {
		t.Errorf("Plan.Slug = %q, want synth-showcase", sess.Plan.Slug)
	}
}

// TestOpenOrCreate_DoesNotOverwriteInMemorySession verifies idempotence:
// when the same slug is already open in-memory, a second OpenOrCreate
// returns the existing session without re-reading disk (which would
// stomp in-memory mutations not yet flushed).
func TestOpenOrCreate_DoesNotOverwriteInMemorySession(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	outputRoot := filepath.Join(dir, "run-idem")
	store := NewStore(dir)

	sess1, err := store.OpenOrCreate("synth-showcase", outputRoot)
	if err != nil {
		t.Fatalf("OpenOrCreate first: %v", err)
	}
	// Mutate in-memory plan AFTER first OpenOrCreate; nothing flushed.
	sess1.Plan = &Plan{
		Slug:      "synth-showcase",
		Codebases: []Codebase{{Hostname: "in-memory-only", Role: RoleAPI}},
	}

	sess2, err := store.OpenOrCreate("synth-showcase", outputRoot)
	if err != nil {
		t.Fatalf("OpenOrCreate second: %v", err)
	}
	if sess1 != sess2 {
		t.Fatal("second OpenOrCreate returned a different session — not idempotent")
	}
	if got := len(sess2.Plan.Codebases); got != 1 || sess2.Plan.Codebases[0].Hostname != "in-memory-only" {
		t.Errorf("second OpenOrCreate stomped in-memory state: codebases=%v", sess2.Plan.Codebases)
	}
}

// mkdirRecipe creates outputRoot so WritePlan's CreateTemp call has a
// directory to write into. OpenOrCreate also ensures this, but the
// pre-OpenOrCreate write happens first.
func mkdirRecipe(path string) error {
	return os.MkdirAll(path, 0o755)
}
