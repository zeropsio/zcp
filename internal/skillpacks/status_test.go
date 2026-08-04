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

// TestPackStatus_Revision_StableForIdenticalState proves computeRevision is
// a pure function of persisted state: two reads with nothing changed in
// between must report byte-identical revisions (spec-skill-packs.md §3.1).
func TestPackStatus_Revision_StableForIdenticalState(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	pack, _ := Lookup("superpowers")
	installCleanPackForTest(t, cwd, pack, []seedSkillSpec{
		{name: "alpha", sourcePath: "skills/alpha", files: map[string]string{"SKILL.md": "# x\n"}},
	})

	st1, err := Status(cwd, "superpowers")
	if err != nil {
		t.Fatalf("Status (1st): %v", err)
	}
	st2, err := Status(cwd, "superpowers")
	if err != nil {
		t.Fatalf("Status (2nd): %v", err)
	}
	if st1.Revision == "" {
		t.Fatal("Revision must not be empty for an installed pack")
	}
	if st1.Revision != st2.Revision {
		t.Errorf("Revision changed across two reads of identical state: %q vs %q", st1.Revision, st2.Revision)
	}
}

// TestPackStatus_Revision_ChangesWhenSelectionChanges proves the revision is
// sensitive to the installed selection: growing the installed set from one
// skill to two must change the reported revision (spec-skill-packs.md §3.1's
// "any change to the installed selection yields a different one").
func TestPackStatus_Revision_ChangesWhenSelectionChanges(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	pack, _ := Lookup("superpowers")
	installCleanPackForTest(t, cwd, pack, []seedSkillSpec{
		{name: "alpha", sourcePath: "skills/alpha", files: map[string]string{"SKILL.md": "# x\n"}},
	})
	before, err := Status(cwd, "superpowers")
	if err != nil {
		t.Fatalf("Status (before): %v", err)
	}

	removeDirForTest(t, cwd, targetSkillDest(TargetAgents, "alpha"))
	removeDirForTest(t, cwd, targetSkillDest(TargetClaude, "alpha"))
	installCleanPackForTest(t, cwd, pack, []seedSkillSpec{
		{name: "alpha", sourcePath: "skills/alpha", files: map[string]string{"SKILL.md": "# x\n"}},
		{name: "beta", sourcePath: "skills/beta", files: map[string]string{"SKILL.md": "# y\n"}},
	})
	after, err := Status(cwd, "superpowers")
	if err != nil {
		t.Fatalf("Status (after): %v", err)
	}

	if before.Revision == after.Revision {
		t.Errorf("Revision unchanged after the installed selection grew: %q", before.Revision)
	}
}

// TestPackStatus_SkillLevelPack_ReportsSelectionAndCatalog proves a read
// against a skill-level catalog pack carries everything a picker needs
// without a second source of truth (spec-skill-packs.md §3.1): the exact
// installed names AND the pack's catalog metadata (name/category/
// description), even before anything is installed.
func TestPackStatus_SkillLevelPack_ReportsSelectionAndCatalog(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()

	// Absent: catalog metadata must still be present (the picker needs it to
	// render an as-yet-uninstalled pack's selection UI).
	absent, err := Status(cwd, "matt-pocock-skills")
	if err != nil {
		t.Fatalf("Status (absent): %v", err)
	}
	if len(absent.Catalog) == 0 {
		t.Fatal("expected catalog metadata for an absent skill-level pack")
	}
	if len(absent.Selected) != 0 {
		t.Errorf("Selected = %v, want none for an absent pack", absent.Selected)
	}

	pack, ok := Lookup("matt-pocock-skills")
	if !ok {
		t.Fatal("test setup: matt-pocock-skills must be a real catalog id")
	}
	installCleanPackForTest(t, cwd, pack, []seedSkillSpec{
		{name: "tdd", sourcePath: "skills/engineering/tdd", files: map[string]string{"SKILL.md": "# x\n"}},
		{name: "handoff", sourcePath: "skills/productivity/handoff", files: map[string]string{"SKILL.md": "# y\n"}},
	})

	installed, err := Status(cwd, "matt-pocock-skills")
	if err != nil {
		t.Fatalf("Status (installed): %v", err)
	}
	if !equalStrings(installed.Selected, []string{"handoff", "tdd"}) {
		t.Errorf("Selected = %v, want [handoff tdd]", installed.Selected)
	}
	if len(installed.Catalog) != len(pack.Skills) {
		t.Errorf("Catalog has %d entries, want the full %d-entry catalog", len(installed.Catalog), len(pack.Skills))
	}
	var sawTDD bool
	for _, c := range installed.Catalog {
		if c.Name == "tdd" {
			sawTDD = true
			if c.Category == "" || c.Description == "" {
				t.Errorf("catalog entry for tdd is missing category/description: %+v", c)
			}
		}
	}
	if !sawTDD {
		t.Error("catalog metadata is missing the tdd entry")
	}

	// Repository-level packs never carry per-skill catalog metadata (§1: only
	// a skill-level pack ever offers a subset).
	repoLevel, err := Status(cwd, "andrej-karpathy-skills")
	if err != nil {
		t.Fatalf("Status (repository-level): %v", err)
	}
	if len(repoLevel.Catalog) != 0 {
		t.Errorf("Catalog = %v, want none for a repository-level pack", repoLevel.Catalog)
	}
}

// TestPackStatus_NonClosedInstalledSet_Warns proves pack-status's third
// migration bucket (spec-skill-packs.md §3.1's closing bullet): an installed
// selection that predates a pack's Requires edges and violates them is
// reported as a warning — never auto-installed, never detached. The warning
// wording is hand-derived from §4.2's edge table (implement Requires tdd,
// code-review; violations only reports direct edges of skills actually
// present in the selection, spec-skill-packs.md §7 proof 16 shares the same
// (missing, requiredBy) rendering pack-set's own dependency auto-close
// report uses — formatViolations/formatAutoClosedAdditions differ only in
// prefix), independent of the module's own traversal.
func TestPackStatus_NonClosedInstalledSet_Warns(t *testing.T) {
	t.Parallel()
	pack, ok := Lookup("matt-pocock-skills")
	if !ok {
		t.Fatal("test setup: matt-pocock-skills must be a real catalog id")
	}

	// implement Requires tdd, code-review (§4.2 table); tdd is present, so
	// the only direct violation is code-review required by implement.
	closureWarning := "selection is not dependency-closed: missing code-review (required by implement), setup-matt-pocock-skills (required by code-review)"

	tests := []struct {
		name         string
		seeds        []seedSkillSpec
		wantSelected []string
		wantWarnings []string
	}{
		{
			name: "implement without code-review warns naming the missing dependency",
			seeds: []seedSkillSpec{
				{name: "implement", sourcePath: "skills/engineering/implement", files: map[string]string{"SKILL.md": "# implement\n"}},
				{name: "tdd", sourcePath: "skills/engineering/tdd", files: map[string]string{"SKILL.md": "# tdd\n"}},
			},
			wantSelected: []string{"implement", "tdd"},
			wantWarnings: []string{closureWarning},
		},
		{
			name: "fully closed installed set has no closure warning",
			seeds: []seedSkillSpec{
				{name: "implement", sourcePath: "skills/engineering/implement", files: map[string]string{"SKILL.md": "# implement\n"}},
				{name: "tdd", sourcePath: "skills/engineering/tdd", files: map[string]string{"SKILL.md": "# tdd\n"}},
				{name: "code-review", sourcePath: "skills/engineering/code-review", files: map[string]string{"SKILL.md": "# code-review\n"}},
				{name: "setup-matt-pocock-skills", sourcePath: "skills/engineering/setup-matt-pocock-skills", files: map[string]string{"SKILL.md": "# setup\n"}},
			},
			wantSelected: []string{"code-review", "implement", "setup-matt-pocock-skills", "tdd"},
			wantWarnings: nil,
		},
		{
			name: "out-of-catalog skill keeps existing behavior; closure computed over the in-catalog remainder",
			seeds: []seedSkillSpec{
				{name: "grill-me", sourcePath: "skills/productivity/grill-me", files: map[string]string{"SKILL.md": "# grill-me\n"}},
				{name: "implement", sourcePath: "skills/engineering/implement", files: map[string]string{"SKILL.md": "# implement\n"}},
				{name: "tdd", sourcePath: "skills/engineering/tdd", files: map[string]string{"SKILL.md": "# tdd\n"}},
			},
			wantSelected: []string{"grill-me", "implement", "tdd"},
			wantWarnings: []string{closureWarning},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cwd := t.TempDir()
			installCleanPackForTest(t, cwd, pack, tt.seeds)

			st, err := Status(cwd, "matt-pocock-skills")
			if err != nil {
				t.Fatalf("Status: %v", err)
			}
			if st.Retired {
				t.Error("Retired = true, want false: matt-pocock-skills is still catalogued")
			}
			if !equalStrings(st.Selected, tt.wantSelected) {
				t.Errorf("Selected = %v, want %v", st.Selected, tt.wantSelected)
			}
			if !equalStrings(st.Warnings, tt.wantWarnings) {
				t.Errorf("Warnings = %v, want %v", st.Warnings, tt.wantWarnings)
			}
		})
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
