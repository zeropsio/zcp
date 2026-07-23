package skillpacks

import (
	"path/filepath"
	"testing"
)

func TestStatus_AbsentPack(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	st, err := Status(cwd, "superpowers")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.State != StateAbsent || st.Managed || st.Retired {
		t.Errorf("st = %+v, want absent/unmanaged/not-retired", st)
	}
}

func TestStatus_InstalledCleanPack(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	pack, _ := Lookup("superpowers")
	installCleanPackForTest(t, cwd, pack, []seedSkillSpec{
		{name: "alpha", sourcePath: "skills/alpha", files: map[string]string{"SKILL.md": "# x\n"}},
	})

	st, err := Status(cwd, "superpowers")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.State != StateInstalled || !st.Managed || st.Retired {
		t.Errorf("st = %+v, want installed/managed/not-retired", st)
	}
	if st.SkillCount != 1 || st.Commit != testCommit {
		t.Errorf("st.SkillCount/Commit = %d/%q, want 1/%q", st.SkillCount, st.Commit, testCommit)
	}
	if len(st.Warnings) != 0 {
		t.Errorf("Warnings = %v, want none for a clean install", st.Warnings)
	}
}

func TestStatus_IncompletePack(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	pack, _ := Lookup("superpowers")
	installCleanPackForTest(t, cwd, pack, []seedSkillSpec{
		{name: "alpha", sourcePath: "skills/alpha", files: map[string]string{"SKILL.md": "# x\n"}},
	})
	removeDirForTest(t, cwd, targetSkillDest(TargetClaude, "alpha"))

	st, err := Status(cwd, "superpowers")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.State != StateIncomplete {
		t.Errorf("State = %q, want %q", st.State, StateIncomplete)
	}
	if len(st.Warnings) == 0 {
		t.Error("expected a warning about the missing copy")
	}
}

func TestStatus_ModifiedPack(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	pack, _ := Lookup("superpowers")
	installCleanPackForTest(t, cwd, pack, []seedSkillSpec{
		{name: "alpha", sourcePath: "skills/alpha", files: map[string]string{"SKILL.md": "# x\n"}},
	})
	writeFile(t, filepath.Join(cwd, targetSkillDest(TargetAgents, "alpha"), "extra.txt"), "tampered\n")

	st, err := Status(cwd, "superpowers")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.State != StateModified {
		t.Errorf("State = %q, want %q", st.State, StateModified)
	}
}

// TestStatus_ModifiedBeatsIncomplete proves modified takes priority when
// BOTH a missing copy and a drifted copy are present simultaneously — a
// present-but-drifted copy is a more urgent signal than a merely-missing
// one.
func TestStatus_ModifiedBeatsIncomplete(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	pack, _ := Lookup("superpowers")
	installCleanPackForTest(t, cwd, pack, []seedSkillSpec{
		{name: "alpha", sourcePath: "skills/alpha", files: map[string]string{"SKILL.md": "# x\n"}},
	})
	removeDirForTest(t, cwd, targetSkillDest(TargetClaude, "alpha"))
	writeFile(t, filepath.Join(cwd, targetSkillDest(TargetAgents, "alpha"), "extra.txt"), "tampered\n")

	st, err := Status(cwd, "superpowers")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.State != StateModified {
		t.Errorf("State = %q, want %q (modified beats incomplete)", st.State, StateModified)
	}
}

func TestStatus_LegacyManifest_IsBroken(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	root, err := openWorkspaceRoot(cwd)
	if err != nil {
		t.Fatalf("openWorkspaceRoot: %v", err)
	}
	if err := root.MkdirAll(skillPacksStateDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, filepath.Join(cwd, manifestRelPath("superpowers")), `{"id":"superpowers","installedDirs":["superpowers"]}`)
	_ = root.Close()

	st, err := Status(cwd, "superpowers")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.State != StateBroken || !st.Managed {
		t.Errorf("st = %+v, want broken/managed", st)
	}
}

func TestStatus_RetiredPack(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	retiredPack := Pack{ID: "old-retired-pack", Repo: "owner/old", CloneURL: "https://example.invalid/old", Ref: "main"}
	installCleanPackForTest(t, cwd, retiredPack, []seedSkillSpec{
		{name: "alpha", sourcePath: "skills/alpha", files: map[string]string{"SKILL.md": "# x\n"}},
	})

	st, err := Status(cwd, "old-retired-pack")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !st.Retired {
		t.Error("expected Retired = true for an id no longer in the catalog")
	}
	if st.State != StateInstalled {
		t.Errorf("State = %q, want installed (retired doesn't mean broken)", st.State)
	}
}

func TestStatusAll_ListsEveryCatalogPackPlusRetired(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	retiredPack := Pack{ID: "old-retired-pack", Repo: "owner/old", CloneURL: "https://example.invalid/old", Ref: "main"}
	installCleanPackForTest(t, cwd, retiredPack, []seedSkillSpec{
		{name: "alpha", sourcePath: "skills/alpha", files: map[string]string{"SKILL.md": "# x\n"}},
	})

	statuses, err := StatusAll(cwd)
	if err != nil {
		t.Fatalf("StatusAll: %v", err)
	}
	ids := make(map[string]PackStatus, len(statuses))
	for _, s := range statuses {
		ids[s.ID] = s
	}
	for _, id := range ValidIDs() {
		if _, ok := ids[id]; !ok {
			t.Errorf("StatusAll missing catalog id %q", id)
		}
	}
	retired, ok := ids["old-retired-pack"]
	if !ok {
		t.Fatal("StatusAll missing the retired pack")
	}
	if !retired.Retired {
		t.Error("retired pack's Retired flag is false")
	}
}

// removeDirForTest is a small local helper: os.RemoveAll on a workspace-
// relative path, for tests that need to simulate drift by hand.
func removeDirForTest(t *testing.T, cwd, rel string) {
	t.Helper()
	root, err := openWorkspaceRoot(cwd)
	if err != nil {
		t.Fatalf("openWorkspaceRoot: %v", err)
	}
	defer func() { _ = root.Close() }()
	if err := root.RemoveAll(rel); err != nil {
		t.Fatalf("RemoveAll(%s): %v", rel, err)
	}
}
