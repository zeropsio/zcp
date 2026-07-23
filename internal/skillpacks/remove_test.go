package skillpacks

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemove_AbsentPack_NoOpSuccess(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()

	result, err := Remove(context.Background(), cwd, "superpowers")
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if result.Changed {
		t.Error("Changed = true, want false")
	}
	if result.State != StateAbsent {
		t.Errorf("State = %q, want %q", result.State, StateAbsent)
	}
}

func TestRemove_CleanInstall_RemovesEverything(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	pack, _ := Lookup("superpowers")
	installCleanPackForTest(t, cwd, pack, []seedSkillSpec{
		{name: "alpha", sourcePath: "skills/alpha", files: map[string]string{"SKILL.md": "# x\n"}},
	})

	result, err := Remove(context.Background(), cwd, "superpowers")
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if !result.Changed || result.State != StateAbsent {
		t.Fatalf("result = %+v, want Changed=true State=absent", result)
	}
	for _, tg := range targets {
		if _, statErr := os.Stat(filepath.Join(cwd, targetSkillDest(tg, "alpha"))); !os.IsNotExist(statErr) {
			t.Errorf("expected %s copy removed, stat err = %v", tg, statErr)
		}
	}
	if m := loadManifestOrFatal(t, cwd, "superpowers"); m != nil {
		t.Error("expected manifest removed")
	}
}

// TestRemove_Idempotent_SecondCallIsAlsoNoOp proves remove is idempotent —
// unlike the old implementation, a second call on an already-removed pack
// succeeds as a no-op rather than erroring "not installed".
func TestRemove_Idempotent_SecondCallIsAlsoNoOp(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	pack, _ := Lookup("superpowers")
	installCleanPackForTest(t, cwd, pack, []seedSkillSpec{
		{name: "alpha", sourcePath: "skills/alpha", files: map[string]string{"SKILL.md": "# x\n"}},
	})

	if _, err := Remove(context.Background(), cwd, "superpowers"); err != nil {
		t.Fatalf("first Remove: %v", err)
	}
	result, err := Remove(context.Background(), cwd, "superpowers")
	if err != nil {
		t.Fatalf("second Remove should be a no-op, not an error: %v", err)
	}
	if result.Changed {
		t.Error("second Remove: Changed = true, want false")
	}
}

func TestRemove_MissingCopyOnOneTarget_WarnsAndRemovesOther(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	pack, _ := Lookup("superpowers")
	installCleanPackForTest(t, cwd, pack, []seedSkillSpec{
		{name: "alpha", sourcePath: "skills/alpha", files: map[string]string{"SKILL.md": "# x\n"}},
	})
	removeDirForTest(t, cwd, targetSkillDest(TargetClaude, "alpha"))

	result, err := Remove(context.Background(), cwd, "superpowers")
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if !result.Changed {
		t.Error("Changed = false, want true")
	}
	if len(result.Warnings) == 0 {
		t.Error("expected a warning about the already-missing copy")
	}
	if _, statErr := os.Stat(filepath.Join(cwd, targetSkillDest(TargetAgents, "alpha"))); !os.IsNotExist(statErr) {
		t.Errorf("expected the agents copy removed too, stat err = %v", statErr)
	}
	if m := loadManifestOrFatal(t, cwd, "superpowers"); m != nil {
		t.Error("expected manifest removed even when one copy was already missing")
	}
}

// TestRemove_ModifiedCopy_PreservedAndDetached is the user-preserving
// proof: a copy whose content has drifted from what the marker/manifest
// digest records is preserved (content untouched) and only detached (its
// ZCP marker deleted) — never silently deleted, never left falsely
// ZCP-owned.
func TestRemove_ModifiedCopy_PreservedAndDetached(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	pack, _ := Lookup("superpowers")
	installCleanPackForTest(t, cwd, pack, []seedSkillSpec{
		{name: "alpha", sourcePath: "skills/alpha", files: map[string]string{"SKILL.md": "# x\n"}},
	})
	writeFile(t, filepath.Join(cwd, targetSkillDest(TargetAgents, "alpha"), "extra.txt"), "user's own edit\n")

	result, err := Remove(context.Background(), cwd, "superpowers")
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if !strings.Contains(result.Message, "preserved") {
		t.Errorf("Message = %q, want it to mention preservation", result.Message)
	}
	// Content survives.
	if _, statErr := os.Stat(filepath.Join(cwd, targetSkillDest(TargetAgents, "alpha"), "extra.txt")); statErr != nil {
		t.Errorf("expected the modified copy's content to survive: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(cwd, targetSkillDest(TargetAgents, "alpha"), "SKILL.md")); statErr != nil {
		t.Errorf("expected the modified copy's original content to survive too: %v", statErr)
	}
	// Marker is detached (removed) so the directory is no longer ZCP-owned.
	if _, statErr := os.Stat(filepath.Join(cwd, targetSkillDest(TargetAgents, "alpha"), markerFileName)); !os.IsNotExist(statErr) {
		t.Errorf("expected the marker to be detached (deleted), stat err = %v", statErr)
	}
	// The clean claude copy is still removed.
	if _, statErr := os.Stat(filepath.Join(cwd, targetSkillDest(TargetClaude, "alpha"))); !os.IsNotExist(statErr) {
		t.Errorf("expected the clean claude copy removed, stat err = %v", statErr)
	}
	if m := loadManifestOrFatal(t, cwd, "superpowers"); m != nil {
		t.Error("expected manifest removed")
	}
}

// TestRemove_ForeignMarkerCopy_PreservedUntouched proves a copy whose
// marker belongs to a DIFFERENT pack/generation is left completely
// untouched — content and marker alike.
func TestRemove_ForeignMarkerCopy_PreservedUntouched(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	pack, _ := Lookup("superpowers")
	installCleanPackForTest(t, cwd, pack, []seedSkillSpec{
		{name: "alpha", sourcePath: "skills/alpha", files: map[string]string{"SKILL.md": "# x\n"}},
	})
	// Overwrite the agents-copy marker so it belongs to a different pack.
	root, err := openWorkspaceRoot(cwd)
	if err != nil {
		t.Fatalf("openWorkspaceRoot: %v", err)
	}
	foreign := validMarker()
	foreign.PackID = "a-totally-different-pack"
	if err := writeMarker(root, targetSkillDest(TargetAgents, "alpha"), foreign); err != nil {
		t.Fatalf("writeMarker: %v", err)
	}
	_ = root.Close()

	result, err := Remove(context.Background(), cwd, "superpowers")
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if len(result.Warnings) == 0 {
		t.Error("expected a warning about the foreign marker")
	}
	if _, statErr := os.Stat(filepath.Join(cwd, targetSkillDest(TargetAgents, "alpha"), markerFileName)); statErr != nil {
		t.Errorf("expected the foreign marker to survive untouched: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(cwd, targetSkillDest(TargetAgents, "alpha"), "SKILL.md")); statErr != nil {
		t.Errorf("expected the content to survive untouched: %v", statErr)
	}
}

// TestRemove_MissingMarkerCopy_PreservedUntouched proves a directory
// present at the expected path but carrying NO marker at all (the user
// replaced it by hand) is left completely untouched.
func TestRemove_MissingMarkerCopy_PreservedUntouched(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	pack, _ := Lookup("superpowers")
	installCleanPackForTest(t, cwd, pack, []seedSkillSpec{
		{name: "alpha", sourcePath: "skills/alpha", files: map[string]string{"SKILL.md": "# x\n"}},
	})
	if err := os.Remove(filepath.Join(cwd, targetSkillDest(TargetAgents, "alpha"), markerFileName)); err != nil {
		t.Fatalf("remove marker: %v", err)
	}

	result, err := Remove(context.Background(), cwd, "superpowers")
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if len(result.Warnings) == 0 {
		t.Error("expected a warning about the missing marker")
	}
	if _, statErr := os.Stat(filepath.Join(cwd, targetSkillDest(TargetAgents, "alpha"), "SKILL.md")); statErr != nil {
		t.Errorf("expected the content to survive untouched: %v", statErr)
	}
}

// TestRemove_RetiredPack_StillRemovable proves removal validates against
// the manifest+syntax, not catalog membership — a pack retired from the
// catalog must remain removable.
func TestRemove_RetiredPack_StillRemovable(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	if _, ok := Lookup("old-retired-pack"); ok {
		t.Fatal("test setup: old-retired-pack must not be a real catalog id")
	}
	retiredPack := Pack{ID: "old-retired-pack", Repo: "owner/old", CloneURL: "https://example.invalid/old", Ref: "main"}
	installCleanPackForTest(t, cwd, retiredPack, []seedSkillSpec{
		{name: "alpha", sourcePath: "skills/alpha", files: map[string]string{"SKILL.md": "# x\n"}},
	})

	result, err := Remove(context.Background(), cwd, "old-retired-pack")
	if err != nil {
		t.Fatalf("Remove of a retired pack should succeed: %v", err)
	}
	if !result.Changed {
		t.Error("Changed = false, want true")
	}
	for _, tg := range targets {
		if _, statErr := os.Stat(filepath.Join(cwd, targetSkillDest(tg, "alpha"))); !os.IsNotExist(statErr) {
			t.Errorf("expected %s copy removed, stat err = %v", tg, statErr)
		}
	}
}

func TestRemove_LegacyManifest_RefusesWithoutMutation(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	root, err := openWorkspaceRoot(cwd)
	if err != nil {
		t.Fatalf("openWorkspaceRoot: %v", err)
	}
	if err := root.MkdirAll(skillPacksStateDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Seed a v1 manifest AND the directory it once claimed to manage, to
	// prove a refused legacy remove touches neither.
	writeFile(t, filepath.Join(cwd, targetSkillDest(TargetClaude, "superpowers"), "SKILL.md"), "# x\n")
	writeFile(t, filepath.Join(cwd, manifestRelPath("superpowers")), `{"id":"superpowers","repo":"obra/superpowers","commit":"deadbeef","installedDirs":["superpowers"]}`)
	_ = root.Close()

	_, err = Remove(context.Background(), cwd, "superpowers")
	if err == nil {
		t.Fatal("expected an error for a legacy manifest")
	}
	if code := codeOf(t, err); code != CodeLegacyState {
		t.Errorf("code = %q, want %q", code, CodeLegacyState)
	}
	if _, statErr := os.Stat(filepath.Join(cwd, targetSkillDest(TargetClaude, "superpowers"), "SKILL.md")); statErr != nil {
		t.Errorf("expected the directory to survive an aborted legacy remove: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(cwd, manifestRelPath("superpowers"))); statErr != nil {
		t.Errorf("expected the legacy manifest to be retained for manual cleanup: %v", statErr)
	}
}

func TestRemove_CorruptManifest_Refuses(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	root, err := openWorkspaceRoot(cwd)
	if err != nil {
		t.Fatalf("openWorkspaceRoot: %v", err)
	}
	if err := root.MkdirAll(skillPacksStateDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, filepath.Join(cwd, manifestRelPath("superpowers")), `{"schemaVersion":2,"id":"superpowers"}`)
	_ = root.Close()

	_, err = Remove(context.Background(), cwd, "superpowers")
	if err == nil {
		t.Fatal("expected an error for a corrupt manifest")
	}
	if code := codeOf(t, err); code != CodeCorruptState {
		t.Errorf("code = %q, want %q", code, CodeCorruptState)
	}
}

func TestRemove_ContextAlreadyCanceled_ErrorsImmediately(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Remove(ctx, cwd, "superpowers")
	if err == nil {
		t.Fatal("expected an error for an already-canceled context")
	}
}

// TestRemove_SymlinkedClaudeAncestor_FailsClosed reuses the workspace-guard
// proof at the Remove entry point.
func TestRemove_SymlinkedClaudeAncestor_FailsClosed(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	pack, _ := Lookup("superpowers")
	installCleanPackForTest(t, cwd, pack, []seedSkillSpec{
		{name: "alpha", sourcePath: "skills/alpha", files: map[string]string{"SKILL.md": "# x\n"}},
	})

	outside := t.TempDir()
	realClaude := filepath.Join(cwd, ".claude")
	if err := os.RemoveAll(realClaude); err != nil {
		t.Fatalf("remove real .claude: %v", err)
	}
	writeSymlinkOrSkip(t, outside, realClaude)

	_, err := Remove(context.Background(), cwd, "superpowers")
	if err == nil {
		t.Fatal("expected an error when .claude is a symlinked ancestor")
	}
	// openWorkspaceRoot itself refuses while .claude is a symlink, so check
	// the manifest survived via a plain stat rather than loadManifestOrFatal
	// (which would also hit the same guard).
	if _, statErr := os.Stat(filepath.Join(cwd, manifestRelPath("superpowers"))); statErr != nil {
		t.Errorf("manifest should be retained after a fail-closed abort: %v", statErr)
	}
}
