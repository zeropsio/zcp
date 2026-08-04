package skillpacks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// snapshotWorkspace independently captures cwd's complete on-disk state
// (every file's content hash, every directory's presence, every symlink's
// target), keyed by path relative to cwd — the "byte-identical" oracle for
// the mid-reconciliation-failure and stale-revision tests below. It walks
// with io/fs and os directly, deliberately NOT calling this package's own
// treeDigest (the same production code the tests are verifying), and
// excludes only the cross-process advisory lock file, whose mere creation is
// an expected concurrency-control side effect, not a selection mutation.
func snapshotWorkspace(t *testing.T, cwd string) map[string]string {
	t.Helper()
	snap := make(map[string]string)
	err := filepath.WalkDir(cwd, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, relErr := filepath.Rel(cwd, path)
		if relErr != nil {
			return relErr
		}
		if rel == "." || rel == lockRelPath {
			return nil
		}
		if d.IsDir() {
			snap[rel] = "DIR"
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return infoErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, linkErr := os.Readlink(path)
			if linkErr != nil {
				return linkErr
			}
			snap[rel] = "LINK:" + target
			return nil
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		sum := sha256.Sum256(content)
		snap[rel] = "FILE:" + hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatalf("snapshotWorkspace: %v", err)
	}
	return snap
}

// assertWorkspaceUnchanged fails the test with every discrepancy between two
// snapshotWorkspace results — added, removed, or content-changed entries.
func assertWorkspaceUnchanged(t *testing.T, before, after map[string]string) {
	t.Helper()
	for path, sum := range before {
		got, ok := after[path]
		if !ok {
			t.Errorf("workspace entry %q was removed", path)
			continue
		}
		if got != sum {
			t.Errorf("workspace entry %q changed: before=%q after=%q", path, sum, got)
		}
	}
	for path := range after {
		if _, ok := before[path]; !ok {
			t.Errorf("workspace entry %q appeared that was not present before", path)
		}
	}
}

// buildTestSkillLevelPack creates a local fixture git repo containing every
// skill in skills (each committed once) and returns a ReviewSkillLevel Pack
// (catalog == skills, id not colliding with a real catalog id) plus the
// resulting commit SHA — for tests that need PackSet to actually fetch real
// content rather than seed a manifest directly.
func buildTestSkillLevelPack(t *testing.T, id string, skills []CatalogSkill) (Pack, string) {
	t.Helper()
	repoDir := t.TempDir()
	for _, sk := range skills {
		writeSkillMD(t, filepath.Join(repoDir, sk.SourcePath, "SKILL.md"), sk.Name, sk.Description)
	}
	newFixtureRepo(t, repoDir)
	commit := commitAtHEAD(t, repoDir)
	pack := testPack(id, repoDir)
	pack.Review = ReviewSkillLevel
	pack.Skills = skills
	return pack, commit
}

// seedPackWithCommit installs pack's given skills as a clean, valid pack
// (via installCleanPackForTest) and then overwrites the persisted manifest's
// Source.Commit to commit — so a test can seed a manifest pinned at a REAL,
// fetchable commit (installCleanPackForTest itself always pins the
// syntactically-valid but unfetchable testCommit constant). Returns the
// manifest's generation for tests that need to predict a quarantine path.
func seedPackWithCommit(t *testing.T, cwd string, pack Pack, commit string, skills []seedSkillSpec) string {
	t.Helper()
	installCleanPackForTest(t, cwd, pack, skills)

	root, err := openWorkspaceRoot(cwd)
	if err != nil {
		t.Fatalf("openWorkspaceRoot: %v", err)
	}
	defer func() { _ = root.Close() }()
	m, _, err := loadManifest(root, pack.ID)
	if err != nil {
		t.Fatalf("loadManifest: %v", err)
	}
	m.Source.Commit = commit
	if err := writeManifest(root, *m); err != nil {
		t.Fatalf("writeManifest: %v", err)
	}
	return m.Generation
}

// TestPackSet_RepositoryLevelPack_Refused proves pack-set against a
// repository-level pack is an error, not a silent whole-pack install
// (spec-skill-packs.md §1: only a skill-level pack can take a selection).
func TestPackSet_RepositoryLevelPack_Refused(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()

	_, err := PackSet(context.Background(), cwd, "andrej-karpathy-skills", []string{"whatever"}, "irrelevant-revision")
	if err == nil {
		t.Fatal("expected an error for a repository-level pack")
	}
	if code := codeOf(t, err); code != CodeNotSkillLevel {
		t.Errorf("code = %q, want %q", code, CodeNotSkillLevel)
	}
}

// TestPackSet_UnknownSkillName_RefusedWithoutMutation proves a --skills name
// outside the pack's reviewed catalog is refused before any write.
func TestPackSet_UnknownSkillName_RefusedWithoutMutation(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()

	_, err := PackSet(context.Background(), cwd, "matt-pocock-skills", []string{"tdd", "not-a-real-skill"}, "irrelevant-revision")
	if err == nil {
		t.Fatal("expected an error for an unknown skill name")
	}
	if code := codeOf(t, err); code != CodeUnknownSkill {
		t.Errorf("code = %q, want %q", code, CodeUnknownSkill)
	}
	st, statErr := Status(cwd, "matt-pocock-skills")
	if statErr != nil {
		t.Fatalf("Status: %v", statErr)
	}
	if st.State != StateAbsent {
		t.Errorf("State = %q, want %q (zero mutation)", st.State, StateAbsent)
	}
}

// TestPackSet_StaleRevision_ConflictWithoutMutation proves a stale
// --expected-revision is refused with a stable, machine-readable conflict
// and truly zero writes — verified via an independent full-tree snapshot,
// not merely "an error came back" (spec-skill-packs.md §3.1 proof 9).
func TestPackSet_StaleRevision_ConflictWithoutMutation(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	pack, ok := Lookup("matt-pocock-skills")
	if !ok {
		t.Fatal("test setup: matt-pocock-skills must be a real catalog id")
	}
	installCleanPackForTest(t, cwd, pack, []seedSkillSpec{
		{name: "tdd", sourcePath: "skills/engineering/tdd", files: map[string]string{"SKILL.md": "# x\n"}},
	})

	before := snapshotWorkspace(t, cwd)

	_, err := PackSet(context.Background(), cwd, "matt-pocock-skills", []string{"handoff"}, "definitely-not-the-real-revision")
	if err == nil {
		t.Fatal("expected a conflict error for a stale revision")
	}
	if code := codeOf(t, err); code != CodeConflict {
		t.Errorf("code = %q, want %q", code, CodeConflict)
	}

	after := snapshotWorkspace(t, cwd)
	assertWorkspaceUnchanged(t, before, after)
}

// TestPackSet_EmptySelection_RemovesPack proves an empty --skills selection
// is routed through the same delete/detach/preserve classification as a
// direct pack-remove (spec-skill-packs.md §3): a clean copy is deleted and
// the manifest is removed entirely.
func TestPackSet_EmptySelection_RemovesPack(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	pack, ok := Lookup("matt-pocock-skills")
	if !ok {
		t.Fatal("test setup: matt-pocock-skills must be a real catalog id")
	}
	installCleanPackForTest(t, cwd, pack, []seedSkillSpec{
		{name: "tdd", sourcePath: "skills/engineering/tdd", files: map[string]string{"SKILL.md": "# x\n"}},
	})
	before, err := Status(cwd, "matt-pocock-skills")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}

	result, err := PackSet(context.Background(), cwd, "matt-pocock-skills", nil, before.Revision)
	if err != nil {
		t.Fatalf("PackSet: %v", err)
	}
	if !result.Changed || result.State != StateAbsent {
		t.Fatalf("result = %+v, want Changed=true State=absent", result)
	}
	for _, tg := range targets {
		if _, statErr := os.Stat(filepath.Join(cwd, targetSkillDest(tg, "tdd"))); !os.IsNotExist(statErr) {
			t.Errorf("expected %s copy of tdd removed, stat err = %v", tg, statErr)
		}
	}
	if m := loadManifestOrFatal(t, cwd, "matt-pocock-skills"); m != nil {
		t.Error("expected manifest removed")
	}

	after, err := Status(cwd, "matt-pocock-skills")
	if err != nil {
		t.Fatalf("Status (after): %v", err)
	}
	if after.State != StateAbsent {
		t.Errorf("after.State = %q, want absent", after.State)
	}
}

// TestPackSet_AddsAndRemovesToMatchDesiredSet proves one apply both adds and
// removes to reach exactly the desired set (spec-skill-packs.md §3.1: an
// additive verb cannot express this). alpha is kept, beta is deselected
// (clean → deleted), gamma is newly selected (fetched and installed).
func TestPackSet_AddsAndRemovesToMatchDesiredSet(t *testing.T) {
	t.Parallel()
	skills := []CatalogSkill{
		{Name: "alpha", SourcePath: "skills/alpha", Category: "Engineering", Description: "alpha"},
		{Name: "beta", SourcePath: "skills/beta", Category: "Engineering", Description: "beta"},
		{Name: "gamma", SourcePath: "skills/gamma", Category: "Engineering", Description: "gamma"},
	}
	pack, commit := buildTestSkillLevelPack(t, "addrm-fixture-pack", skills)

	cwd := t.TempDir()
	seedPackWithCommit(t, cwd, pack, commit, []seedSkillSpec{
		{name: "alpha", sourcePath: "skills/alpha", files: map[string]string{"SKILL.md": "# alpha\n"}},
		{name: "beta", sourcePath: "skills/beta", files: map[string]string{"SKILL.md": "# beta\n"}},
	})
	before, err := Status(cwd, "addrm-fixture-pack")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}

	result, err := packSetForPack(context.Background(), cwd, pack, []string{"alpha", "gamma"}, before.Revision)
	if err != nil {
		t.Fatalf("packSetForPack: %v", err)
	}
	if !result.Changed || result.State != StateInstalled || result.SkillCount != 2 {
		t.Fatalf("result = %+v, want Changed=true State=installed SkillCount=2", result)
	}
	if !equalStrings(result.Selected, []string{"alpha", "gamma"}) {
		t.Errorf("Selected = %v, want [alpha gamma]", result.Selected)
	}

	for _, tg := range targets {
		if _, statErr := os.Stat(filepath.Join(cwd, targetSkillDest(tg, "beta"))); !os.IsNotExist(statErr) {
			t.Errorf("expected %s copy of beta removed, stat err = %v", tg, statErr)
		}
		for _, name := range []string{"alpha", "gamma"} {
			if _, statErr := os.Stat(filepath.Join(cwd, targetSkillDest(tg, name), "SKILL.md")); statErr != nil {
				t.Errorf("expected %s copy of %s present: %v", tg, name, statErr)
			}
		}
	}

	m := loadManifestOrFatal(t, cwd, "addrm-fixture-pack")
	if m == nil {
		t.Fatal("expected manifest to still exist")
	}
	if len(m.Skills) != 2 {
		t.Fatalf("manifest has %d skills, want 2", len(m.Skills))
	}
}

// TestPackSet_DeselectedModifiedCopy_PreservedAndDetached proves a
// deselected skill with local changes is preserved (content untouched) and
// only detached (marker removed) rather than deleted (spec-skill-packs.md
// §3).
func TestPackSet_DeselectedModifiedCopy_PreservedAndDetached(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	pack, ok := Lookup("matt-pocock-skills")
	if !ok {
		t.Fatal("test setup: matt-pocock-skills must be a real catalog id")
	}
	installCleanPackForTest(t, cwd, pack, []seedSkillSpec{
		{name: "tdd", sourcePath: "skills/engineering/tdd", files: map[string]string{"SKILL.md": "# x\n"}},
	})
	writeFile(t, filepath.Join(cwd, targetSkillDest(TargetAgents, "tdd"), "extra.txt"), "user's own edit\n")
	before, err := Status(cwd, "matt-pocock-skills")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}

	result, err := PackSet(context.Background(), cwd, "matt-pocock-skills", nil, before.Revision)
	if err != nil {
		t.Fatalf("PackSet: %v", err)
	}
	if !result.Changed || result.State != StateAbsent {
		t.Fatalf("result = %+v, want Changed=true State=absent", result)
	}
	if !strings.Contains(result.Message, "detach") {
		t.Errorf("Message = %q, want it to mention detachment", result.Message)
	}

	// Content survives on both targets.
	if _, statErr := os.Stat(filepath.Join(cwd, targetSkillDest(TargetAgents, "tdd"), "extra.txt")); statErr != nil {
		t.Errorf("expected the modified copy's content to survive: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(cwd, targetSkillDest(TargetAgents, "tdd"), "SKILL.md")); statErr != nil {
		t.Errorf("expected the modified copy's original content to survive too: %v", statErr)
	}
	// Marker is detached (removed) so the directory is no longer ZCP-owned.
	if _, statErr := os.Stat(filepath.Join(cwd, targetSkillDest(TargetAgents, "tdd"), markerFileName)); !os.IsNotExist(statErr) {
		t.Errorf("expected the marker to be detached (deleted), stat err = %v", statErr)
	}
	// The clean claude copy is fully removed (it never drifted).
	if _, statErr := os.Stat(filepath.Join(cwd, targetSkillDest(TargetClaude, "tdd"))); !os.IsNotExist(statErr) {
		t.Errorf("expected the clean claude copy removed, stat err = %v", statErr)
	}
	if m := loadManifestOrFatal(t, cwd, "matt-pocock-skills"); m != nil {
		t.Error("expected manifest removed")
	}
}

// TestPackSet_AdditionUsesPinnedCommit_NotBranchTip proves an addition to an
// already-installed pack fetches the manifest's PINNED commit, never
// current upstream HEAD (spec-skill-packs.md §3.1 proof 10). The fixture
// repo moves on to a second commit with DIFFERENT content for the added
// skill after the pin was recorded; installing the tip would produce
// observably different content.
func TestPackSet_AdditionUsesPinnedCommit_NotBranchTip(t *testing.T) {
	t.Parallel()
	skills := []CatalogSkill{
		{Name: "alpha", SourcePath: "skills/alpha", Category: "Engineering", Description: "alpha"},
		{Name: "gamma", SourcePath: "skills/gamma", Category: "Engineering", Description: "gamma"},
	}
	repoDir := t.TempDir()
	writeSkillMD(t, filepath.Join(repoDir, "skills", "alpha", "SKILL.md"), "alpha", "alpha")
	writeSkillMD(t, filepath.Join(repoDir, "skills", "gamma", "SKILL.md"), "gamma", "pinned version")
	newFixtureRepo(t, repoDir)
	pinnedCommit := commitAtHEAD(t, repoDir)

	// Upstream moves on: gamma's content changes after the pin.
	writeSkillMD(t, filepath.Join(repoDir, "skills", "gamma", "SKILL.md"), "gamma", "tip version, never installed")
	env := isolatedGitEnv(t)
	mustRunGit(t, repoDir, env, "add", "-A")
	mustRunGit(t, repoDir, env, "commit", "-q", "-m", "moved on")
	tipCommit := commitAtHEAD(t, repoDir)
	if tipCommit == pinnedCommit {
		t.Fatal("test setup: tip must differ from the pinned commit")
	}

	pack := testPack("pinned-fixture-pack", repoDir)
	pack.Review = ReviewSkillLevel
	pack.Skills = skills

	cwd := t.TempDir()
	seedPackWithCommit(t, cwd, pack, pinnedCommit, []seedSkillSpec{
		{name: "alpha", sourcePath: "skills/alpha", files: map[string]string{"SKILL.md": "# alpha\n"}},
	})
	before, err := Status(cwd, "pinned-fixture-pack")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}

	result, err := packSetForPack(context.Background(), cwd, pack, []string{"alpha", "gamma"}, before.Revision)
	if err != nil {
		t.Fatalf("packSetForPack: %v", err)
	}
	if result.Commit != pinnedCommit {
		t.Errorf("result.Commit = %q, want the pinned commit %q", result.Commit, pinnedCommit)
	}

	for _, tg := range targets {
		got, readErr := os.ReadFile(filepath.Join(cwd, targetSkillDest(tg, "gamma"), "SKILL.md"))
		if readErr != nil {
			t.Fatalf("read gamma SKILL.md under %s: %v", tg, readErr)
		}
		if !strings.Contains(string(got), "pinned version") {
			t.Errorf("%s gamma content = %q, want the PINNED commit's content, not the tip's", tg, got)
		}
		if strings.Contains(string(got), "tip version") {
			t.Errorf("%s gamma content contains the tip's content: %q", tg, got)
		}
	}
}

// TestPackSet_PinnedCommitUnfetchable_ErrorsWithoutFallback proves that when
// the manifest's pinned commit can no longer be fetched (e.g. the upstream
// history was rewritten), PackSet refuses with a named error rather than
// silently falling back to the branch tip — that fallback would install
// unreviewed content (spec-skill-packs.md §3.1).
func TestPackSet_PinnedCommitUnfetchable_ErrorsWithoutFallback(t *testing.T) {
	t.Parallel()
	skills := []CatalogSkill{
		{Name: "alpha", SourcePath: "skills/alpha", Category: "Engineering", Description: "alpha"},
		{Name: "gamma", SourcePath: "skills/gamma", Category: "Engineering", Description: "gamma"},
	}
	pack, _ := buildTestSkillLevelPack(t, "unfetchable-fixture-pack", skills)
	unfetchableCommit := "0000000000000000000000000000000000000000"

	cwd := t.TempDir()
	seedPackWithCommit(t, cwd, pack, unfetchableCommit, []seedSkillSpec{
		{name: "alpha", sourcePath: "skills/alpha", files: map[string]string{"SKILL.md": "# alpha\n"}},
	})
	before, err := Status(cwd, "unfetchable-fixture-pack")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	beforeSnapshot := snapshotWorkspace(t, cwd)

	_, err = packSetForPack(context.Background(), cwd, pack, []string{"alpha", "gamma"}, before.Revision)
	if err == nil {
		t.Fatal("expected an error for an unfetchable pinned commit")
	}
	if code := codeOf(t, err); code != CodeDownloadFailed {
		t.Errorf("code = %q, want %q", code, CodeDownloadFailed)
	}
	if !strings.Contains(err.Error(), unfetchableCommit) {
		t.Errorf("error = %v, want it to name the unfetchable commit %q", err, unfetchableCommit)
	}

	afterSnapshot := snapshotWorkspace(t, cwd)
	assertWorkspaceUnchanged(t, beforeSnapshot, afterSnapshot)
}

// TestPackSet_MidReconciliationFailure_LeavesWorkspaceByteIdentical injects
// the failure in the REMOVAL half, after the addition (gamma) has already
// been planned AND published: beta's removal-quarantine step is made to
// fail by pre-creating its exact quarantine destination directory with no
// write permission (MkdirAll on an already-existing directory is a no-op —
// it does not repair the permission — so the very next atomic rename into
// it fails cleanly, before anything is moved). PackSet must then roll back
// the already-published addition and leave beta and alpha exactly as they
// were, proving the whole reconciliation — not just preflight — is
// all-or-nothing (spec-skill-packs.md §3.1 proof 11).
func TestPackSet_MidReconciliationFailure_LeavesWorkspaceByteIdentical(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("skipping under root: permission bits are not enforced")
	}
	t.Parallel()
	skills := []CatalogSkill{
		{Name: "alpha", SourcePath: "skills/alpha", Category: "Engineering", Description: "alpha"},
		{Name: "beta", SourcePath: "skills/beta", Category: "Engineering", Description: "beta"},
		{Name: "gamma", SourcePath: "skills/gamma", Category: "Engineering", Description: "gamma"},
	}
	pack, commit := buildTestSkillLevelPack(t, "midfail-set-pack", skills)

	cwd := t.TempDir()
	generation := seedPackWithCommit(t, cwd, pack, commit, []seedSkillSpec{
		{name: "alpha", sourcePath: "skills/alpha", files: map[string]string{"SKILL.md": "# alpha\n"}},
		{name: "beta", sourcePath: "skills/beta", files: map[string]string{"SKILL.md": "# beta\n"}},
	})
	before, err := Status(cwd, "midfail-set-pack")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}

	qDir := filepath.Join(cwd, targetQuarantineDir(TargetAgents, generation))
	if err := os.MkdirAll(qDir, 0o755); err != nil {
		t.Fatalf("mkdir quarantine dir: %v", err)
	}
	if err := os.Chmod(qDir, 0o500); err != nil {
		t.Fatalf("chmod quarantine dir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(qDir, 0o755) })

	snapshot := snapshotWorkspace(t, cwd)

	_, err = packSetForPack(context.Background(), cwd, pack, []string{"alpha", "gamma"}, before.Revision)
	if err == nil {
		t.Fatal("expected the removal-quarantine step for beta to fail")
	}

	after := snapshotWorkspace(t, cwd)
	assertWorkspaceUnchanged(t, snapshot, after)

	m := loadManifestOrFatal(t, cwd, "midfail-set-pack")
	if m == nil {
		t.Fatal("expected the manifest to survive untouched")
	}
	if !equalStrings(selectedSkillNames(m), []string{"alpha", "beta"}) {
		t.Errorf("manifest skills = %v, want unchanged [alpha beta]", selectedSkillNames(m))
	}
}

// TestPackSet_LegacyWholeRepoManifest_MigratesAndDetachesExtras proves
// spec-skill-packs.md §3.1/§6's migration rule: a valid v2 manifest recorded
// before this pack became skill-level-reviewed carries a skill ("legacy")
// outside the current catalog. PackSet must detach it (preserve content,
// drop ownership) and report it — never silently delete it, and never
// silently carry it forward as selected, even though its content is
// perfectly clean/unmodified (proof 12).
func TestPackSet_LegacyWholeRepoManifest_MigratesAndDetachesExtras(t *testing.T) {
	t.Parallel()
	// The pack's CURRENT catalog only reviews foo and bar; "legacy" is not
	// catalogued at all, simulating a whole-repository install that predates
	// skill-level review for this pack.
	skills := []CatalogSkill{
		{Name: "foo", SourcePath: "skills/foo", Category: "Engineering", Description: "foo"},
		{Name: "bar", SourcePath: "skills/bar", Category: "Engineering", Description: "bar"},
	}
	pack, commit := buildTestSkillLevelPack(t, "legacy-migrate-pack", skills)

	cwd := t.TempDir()
	seedPackWithCommit(t, cwd, pack, commit, []seedSkillSpec{
		{name: "foo", sourcePath: "skills/foo", files: map[string]string{"SKILL.md": "# foo\n"}},
		{name: "bar", sourcePath: "skills/bar", files: map[string]string{"SKILL.md": "# bar\n"}},
		{name: "legacy", sourcePath: "skills/legacy", files: map[string]string{"SKILL.md": "# legacy\n"}},
	})
	before, err := Status(cwd, "legacy-migrate-pack")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !equalStrings(before.Selected, []string{"bar", "foo", "legacy"}) {
		t.Fatalf("test setup: Selected = %v, want [bar foo legacy] (the true on-disk state)", before.Selected)
	}

	result, err := packSetForPack(context.Background(), cwd, pack, []string{"foo"}, before.Revision)
	if err != nil {
		t.Fatalf("packSetForPack: %v", err)
	}
	if !result.Changed || result.State != StateInstalled || result.SkillCount != 1 {
		t.Fatalf("result = %+v, want Changed=true State=installed SkillCount=1", result)
	}
	if !equalStrings(result.Selected, []string{"foo"}) {
		t.Errorf("Selected = %v, want [foo]", result.Selected)
	}
	foundLegacyWarning := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "legacy") && strings.Contains(w, "detach") {
			foundLegacyWarning = true
		}
	}
	if !foundLegacyWarning {
		t.Errorf("Warnings = %v, want one naming legacy's detachment", result.Warnings)
	}

	// bar (deselected, clean, catalogued) is fully deleted.
	for _, tg := range targets {
		if _, statErr := os.Stat(filepath.Join(cwd, targetSkillDest(tg, "bar"))); !os.IsNotExist(statErr) {
			t.Errorf("expected %s copy of bar deleted, stat err = %v", tg, statErr)
		}
	}
	// legacy (outside the catalog) survives, content intact, marker gone.
	for _, tg := range targets {
		if _, statErr := os.Stat(filepath.Join(cwd, targetSkillDest(tg, "legacy"), "SKILL.md")); statErr != nil {
			t.Errorf("expected %s copy of legacy's content to survive: %v", tg, statErr)
		}
		if _, statErr := os.Stat(filepath.Join(cwd, targetSkillDest(tg, "legacy"), markerFileName)); !os.IsNotExist(statErr) {
			t.Errorf("expected %s copy of legacy's marker detached, stat err = %v", tg, statErr)
		}
	}
	// foo (kept, catalogued) survives untouched.
	for _, tg := range targets {
		if _, statErr := os.Stat(filepath.Join(cwd, targetSkillDest(tg, "foo"), "SKILL.md")); statErr != nil {
			t.Errorf("expected %s copy of foo to survive: %v", tg, statErr)
		}
	}

	m := loadManifestOrFatal(t, cwd, "legacy-migrate-pack")
	if m == nil {
		t.Fatal("expected the manifest to still exist")
	}
	if !equalStrings(selectedSkillNames(m), []string{"foo"}) {
		t.Errorf("manifest skills = %v, want [foo] (legacy must never be silently carried forward as selected)", selectedSkillNames(m))
	}
}

// TestSkillPacks_GrillMeOutOfCatalog_StatusSetRemove pins the concrete,
// real-catalog case behind grill-me's removal from mattPocockSkills
// (catalog.go): a project that installed the pack while grill-me was still
// supported now holds a manifest naming a skill absent from the current
// matt-pocock-skills catalog, even though the pack itself remains reviewed
// and installable (unlike the fully-retired-pack case covered by
// TestStatus_RetiredPack / TestPackSet_RetiredPack_RemovableViaSet). This is
// the same "legacy extra" mechanism TestPackSet_LegacyWholeRepoManifest_
// MigratesAndDetachesExtras proves generically; this test pins it against
// the real production catalog and all three surfaces spec-skill-packs.md
// §3.1 governs: pack-status must not error out or hide the skill,
// pack-set must detach (never silently delete) it while still applying the
// rest of the selection, and pack-remove must not brick on it.
func TestSkillPacks_GrillMeOutOfCatalog_StatusSetRemove(t *testing.T) {
	t.Parallel()
	pack, ok := Lookup("matt-pocock-skills")
	if !ok {
		t.Fatal("test setup: matt-pocock-skills must be a real catalog id")
	}
	for _, sk := range pack.Skills {
		if sk.Name == "grill-me" {
			t.Fatal("test setup: grill-me must already be absent from the matt-pocock-skills catalog for this test to exercise the out-of-catalog case")
		}
	}
	seed := []seedSkillSpec{
		{name: "grill-me", sourcePath: "skills/productivity/grill-me", files: map[string]string{"SKILL.md": "# grill-me\n"}},
		{name: "tdd", sourcePath: "skills/engineering/tdd", files: map[string]string{"SKILL.md": "# tdd\n"}},
	}

	t.Run("pack-status surfaces grill-me rather than erroring or hiding it", func(t *testing.T) {
		t.Parallel()
		cwd := t.TempDir()
		installCleanPackForTest(t, cwd, pack, seed)

		st, err := Status(cwd, "matt-pocock-skills")
		if err != nil {
			t.Fatalf("Status: %v", err)
		}
		if st.Retired {
			t.Error("Retired = true, want false: matt-pocock-skills itself is still catalogued, only grill-me was dropped from it")
		}
		if !equalStrings(st.Selected, []string{"grill-me", "tdd"}) {
			t.Errorf("Selected = %v, want [grill-me tdd] — grill-me must not be hidden from a status read", st.Selected)
		}
	})

	t.Run("pack-set detaches grill-me and reports it while applying the rest of the selection", func(t *testing.T) {
		t.Parallel()
		cwd := t.TempDir()
		installCleanPackForTest(t, cwd, pack, seed)

		before, err := Status(cwd, "matt-pocock-skills")
		if err != nil {
			t.Fatalf("Status: %v", err)
		}

		result, err := PackSet(context.Background(), cwd, "matt-pocock-skills", []string{"tdd"}, before.Revision)
		if err != nil {
			t.Fatalf("PackSet: %v", err)
		}
		if !result.Changed || result.State != StateInstalled {
			t.Fatalf("result = %+v, want Changed=true State=installed", result)
		}
		if !equalStrings(result.Selected, []string{"tdd"}) {
			t.Errorf("Selected = %v, want [tdd] — grill-me must never be silently carried forward as selected", result.Selected)
		}
		foundGrillMeWarning := false
		for _, w := range result.Warnings {
			if strings.Contains(w, "grill-me") && strings.Contains(w, "detach") {
				foundGrillMeWarning = true
			}
		}
		if !foundGrillMeWarning {
			t.Errorf("Warnings = %v, want one naming grill-me's detachment", result.Warnings)
		}

		// grill-me's content survives (preserved, not deleted); only its
		// ownership marker is gone.
		for _, tg := range targets {
			if _, statErr := os.Stat(filepath.Join(cwd, targetSkillDest(tg, "grill-me"), "SKILL.md")); statErr != nil {
				t.Errorf("expected %s copy of grill-me's content to survive: %v", tg, statErr)
			}
			if _, statErr := os.Stat(filepath.Join(cwd, targetSkillDest(tg, "grill-me"), markerFileName)); !os.IsNotExist(statErr) {
				t.Errorf("expected %s copy of grill-me's marker detached, stat err = %v", tg, statErr)
			}
		}

		m := loadManifestOrFatal(t, cwd, "matt-pocock-skills")
		if m == nil {
			t.Fatal("expected the manifest to still exist")
		}
		if !equalStrings(selectedSkillNames(m), []string{"tdd"}) {
			t.Errorf("manifest skills = %v, want [tdd]", selectedSkillNames(m))
		}
	})

	t.Run("pack-remove does not brick on grill-me", func(t *testing.T) {
		t.Parallel()
		cwd := t.TempDir()
		installCleanPackForTest(t, cwd, pack, seed)

		result, err := Remove(context.Background(), cwd, "matt-pocock-skills")
		if err != nil {
			t.Fatalf("Remove: %v", err)
		}
		if result.State != StateAbsent || !result.Changed {
			t.Fatalf("result = %+v, want State=absent Changed=true", result)
		}
		if m := loadManifestOrFatal(t, cwd, "matt-pocock-skills"); m != nil {
			t.Error("expected the manifest to be gone after Remove")
		}
		for _, tg := range targets {
			if _, statErr := os.Stat(filepath.Join(cwd, targetSkillDest(tg, "grill-me"))); !os.IsNotExist(statErr) {
				t.Errorf("expected %s copy of grill-me removed (clean content, whole-pack removal), stat err = %v", tg, statErr)
			}
		}
	})
}

// TestPackSet_AtomicPack_PartialSelectionRefused proves Superpowers (atomic,
// spec-skill-packs.md §5) refuses any selection that is neither the complete
// supported set nor empty.
func TestPackSet_AtomicPack_PartialSelectionRefused(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	pack, ok := Lookup("superpowers")
	if !ok {
		t.Fatal("test setup: superpowers must be a real catalog id")
	}

	_, err := PackSet(context.Background(), cwd, "superpowers", []string{pack.Skills[0].Name}, "irrelevant-revision")
	if err == nil {
		t.Fatal("expected an error for a partial Superpowers selection")
	}
	if code := codeOf(t, err); code != CodeAtomicPartial {
		t.Errorf("code = %q, want %q", code, CodeAtomicPartial)
	}
}

// TestPackSet_RetiredPack_RemovableViaSet is written FRESH on this branch:
// it pins docs/spec-welcome-mode.md §7's explicit requirement ("a retired
// ReviewSkillLevel pack must still be removable via pack-set") against a
// pack id that is NOT in the current catalog at all — mirroring Remove's
// own documented "no catalog gate" contract (remove.go: "Unlike Add, Remove
// does not require id to be in the active catalog"). Only the declarative
// EMPTY selection is meaningful for a retired id (there is no catalog left
// to validate a non-empty --skills list against); a non-empty selection and
// a stale revision must still be refused.
func TestPackSet_RetiredPack_RemovableViaSet(t *testing.T) {
	t.Parallel()
	retiredPack := testPack("old-retired-pack", "irrelevant-unused-clone-url")

	t.Run("non-empty selection still refused (no catalog to validate against)", func(t *testing.T) {
		t.Parallel()
		cwd := t.TempDir()
		installCleanPackForTest(t, cwd, retiredPack, []seedSkillSpec{
			{name: "leftover", sourcePath: "skills/leftover", files: map[string]string{"SKILL.md": "# x\n"}},
		})
		before, err := Status(cwd, "old-retired-pack")
		if err != nil {
			t.Fatalf("Status: %v", err)
		}
		if !before.Retired {
			t.Fatal("test setup: expected Retired=true for an uncatalogued id with a manifest")
		}

		_, err = PackSet(context.Background(), cwd, "old-retired-pack", []string{"leftover"}, before.Revision)
		if err == nil {
			t.Fatal("expected an error: a retired pack has no catalog to validate a non-empty selection against")
		}
		if code := codeOf(t, err); code != CodeUnknownPack {
			t.Errorf("code = %q, want %q", code, CodeUnknownPack)
		}
	})

	t.Run("stale revision still refused with zero writes", func(t *testing.T) {
		t.Parallel()
		cwd := t.TempDir()
		installCleanPackForTest(t, cwd, retiredPack, []seedSkillSpec{
			{name: "leftover", sourcePath: "skills/leftover", files: map[string]string{"SKILL.md": "# x\n"}},
		})
		before := snapshotWorkspace(t, cwd)

		_, err := PackSet(context.Background(), cwd, "old-retired-pack", nil, "definitely-not-the-real-revision")
		if err == nil {
			t.Fatal("expected a conflict error for a stale revision")
		}
		if code := codeOf(t, err); code != CodeConflict {
			t.Errorf("code = %q, want %q", code, CodeConflict)
		}
		after := snapshotWorkspace(t, cwd)
		assertWorkspaceUnchanged(t, before, after)
	})

	t.Run("empty selection removes the retired pack cleanly", func(t *testing.T) {
		t.Parallel()
		cwd := t.TempDir()
		installCleanPackForTest(t, cwd, retiredPack, []seedSkillSpec{
			{name: "leftover", sourcePath: "skills/leftover", files: map[string]string{"SKILL.md": "# x\n"}},
		})
		before, err := Status(cwd, "old-retired-pack")
		if err != nil {
			t.Fatalf("Status: %v", err)
		}

		result, err := PackSet(context.Background(), cwd, "old-retired-pack", nil, before.Revision)
		if err != nil {
			t.Fatalf("PackSet: %v", err)
		}
		if !result.Changed || result.State != StateAbsent {
			t.Fatalf("result = %+v, want Changed=true State=absent", result)
		}
		for _, tg := range targets {
			if _, statErr := os.Stat(filepath.Join(cwd, targetSkillDest(tg, "leftover"))); !os.IsNotExist(statErr) {
				t.Errorf("expected %s copy of leftover removed, stat err = %v", tg, statErr)
			}
		}
		if m := loadManifestOrFatal(t, cwd, "old-retired-pack"); m != nil {
			t.Error("expected manifest removed")
		}

		after, err := Status(cwd, "old-retired-pack")
		if err != nil {
			t.Fatalf("Status (after): %v", err)
		}
		if after.State != StateAbsent {
			t.Errorf("after.State = %q, want absent", after.State)
		}
	})
}

// TestPackSet_UnclosedSelection_RefusedWithoutMutation proves a --skills
// selection that is not dependency-closed over the pack's declared Requires
// edges is refused with CodeUnclosedSelection, naming every missing skill
// and its direct requirer, before any workspace write — including no
// .zcp state/lock artifacts (spec-skill-packs.md §3.1, §7 proof 14).
// Expected missing-skill sets are hand-derived from §4.2's edge table
// (implement -> tdd, code-review; code-review -> setup-matt-pocock-skills;
// wayfinder -> grilling, domain-modeling, research, setup-matt-pocock-skills;
// triage -> grilling, setup-matt-pocock-skills), never recomputed via
// closure/violations themselves.
func TestPackSet_UnclosedSelection_RefusedWithoutMutation(t *testing.T) {
	t.Parallel()
	pack, ok := Lookup("matt-pocock-skills")
	if !ok {
		t.Fatal("test setup: matt-pocock-skills must be a real catalog id")
	}

	refusalCases := []struct {
		name         string
		desired      []string
		wantExact    string // "" means don't check exact equality, only wantContains
		wantContains []string
	}{
		{
			// The literal CLI Outcome: `--skills implement` alone. implement
			// requires tdd, code-review; code-review itself (transitively
			// reachable, though not itself selected) requires
			// setup-matt-pocock-skills. All three must be named in one
			// refusal, not discovered one layer at a time across repeated
			// calls.
			name:    "implement alone names the full transitive gap",
			desired: []string{"implement"},
			wantExact: "selection is not dependency-closed: missing code-review (required by implement), " +
				"setup-matt-pocock-skills (required by code-review), tdd (required by implement)",
		},
		{
			// code-review and setup-matt-pocock-skills are both already
			// selected (so code-review's own dependency is satisfied);
			// tdd is the one isolated direct miss.
			name:      "isolated direct miss: only tdd missing",
			desired:   []string{"implement", "code-review", "setup-matt-pocock-skills"},
			wantExact: "selection is not dependency-closed: missing tdd (required by implement)",
		},
		{
			// tdd and code-review are both already selected; only
			// code-review's own dependency (setup-matt-pocock-skills) is
			// missing — the transitive layer beyond implement's direct edges.
			name:      "transitive miss: only setup-matt-pocock-skills missing",
			desired:   []string{"implement", "tdd", "code-review"},
			wantExact: "selection is not dependency-closed: missing setup-matt-pocock-skills (required by code-review)",
		},
		{
			// wayfinder and triage share grilling as a dependency; every
			// OTHER dependency of both is already selected, isolating a
			// single multi-parent violation that must collapse to one
			// entry, not one per requirer.
			name:      "multi-parent: wayfinder+triage share the missing grilling",
			desired:   []string{"wayfinder", "triage", "domain-modeling", "research", "setup-matt-pocock-skills"},
			wantExact: "selection is not dependency-closed: missing grilling (required by triage, wayfinder)",
		},
	}

	for _, tc := range refusalCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cwd := t.TempDir()
			before := snapshotWorkspace(t, cwd)

			_, err := PackSet(context.Background(), cwd, "matt-pocock-skills", tc.desired, "irrelevant-revision")
			if err == nil {
				t.Fatal("expected an unclosed-selection error")
			}
			if code := codeOf(t, err); code != CodeUnclosedSelection {
				t.Errorf("code = %q, want %q", code, CodeUnclosedSelection)
			}
			if tc.wantExact != "" {
				var ce *CodedError
				if !errors.As(err, &ce) {
					t.Fatalf("err = %v, want a *CodedError", err)
				}
				if ce.Message != tc.wantExact {
					t.Errorf("message = %q, want %q", ce.Message, tc.wantExact)
				}
			}
			for _, want := range tc.wantContains {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q, want it to contain %q", err.Error(), want)
				}
			}

			after := snapshotWorkspace(t, cwd)
			assertWorkspaceUnchanged(t, before, after)
			if _, statErr := os.Stat(filepath.Join(cwd, ".zcp")); !os.IsNotExist(statErr) {
				t.Errorf("expected no .zcp state/lock directory to have been created, stat err = %v", statErr)
			}
		})
	}

	t.Run("closed set is not refused", func(t *testing.T) {
		t.Parallel()
		cwd := t.TempDir()
		installCleanPackForTest(t, cwd, pack, []seedSkillSpec{
			{name: "code-review", sourcePath: "skills/engineering/code-review", files: map[string]string{"SKILL.md": "# x\n"}},
			{name: "setup-matt-pocock-skills", sourcePath: "skills/engineering/setup-matt-pocock-skills", files: map[string]string{"SKILL.md": "# x\n"}},
		})
		before, err := Status(cwd, "matt-pocock-skills")
		if err != nil {
			t.Fatalf("Status: %v", err)
		}

		result, err := PackSet(context.Background(), cwd, "matt-pocock-skills", []string{"code-review", "setup-matt-pocock-skills"}, before.Revision)
		if err != nil {
			t.Fatalf("PackSet: unexpected error for a closed selection: %v", err)
		}
		if result.Changed {
			t.Errorf("Changed = true, want false (the closed subset is already exactly installed)")
		}
	})

	t.Run("empty set is trivially closed", func(t *testing.T) {
		t.Parallel()
		cwd := t.TempDir()
		before, err := Status(cwd, "matt-pocock-skills")
		if err != nil {
			t.Fatalf("Status: %v", err)
		}

		result, err := PackSet(context.Background(), cwd, "matt-pocock-skills", nil, before.Revision)
		if err != nil {
			t.Fatalf("PackSet: unexpected error for an empty selection: %v", err)
		}
		if result.Changed {
			t.Errorf("Changed = true, want false (nothing installed, nothing desired)")
		}
	})
}

// TestPackSet_StaleRevisionUnclosedSet_UnclosedWins proves the pinned
// precedence (spec-skill-packs.md §3.1, §7 proof 14): the closure check is
// pure input validation over the desired set and the catalog only, and runs
// before the lock and the revision compare, so an unclosed desired set
// combined with a deliberately stale --expected-revision still returns
// CodeUnclosedSelection, never CodeConflict — with a byte-identical
// workspace either way.
func TestPackSet_StaleRevisionUnclosedSet_UnclosedWins(t *testing.T) {
	t.Parallel()
	pack, ok := Lookup("matt-pocock-skills")
	if !ok {
		t.Fatal("test setup: matt-pocock-skills must be a real catalog id")
	}
	cwd := t.TempDir()
	installCleanPackForTest(t, cwd, pack, []seedSkillSpec{
		{name: "tdd", sourcePath: "skills/engineering/tdd", files: map[string]string{"SKILL.md": "# x\n"}},
	})
	before := snapshotWorkspace(t, cwd)

	// "implement" is not dependency-closed (misses tdd's sibling
	// code-review and transitively setup-matt-pocock-skills), AND the
	// revision below is deliberately not the real one.
	_, err := PackSet(context.Background(), cwd, "matt-pocock-skills", []string{"implement"}, "definitely-not-the-real-revision")
	if err == nil {
		t.Fatal("expected an error for an unclosed selection with a stale revision")
	}
	if code := codeOf(t, err); code != CodeUnclosedSelection {
		t.Errorf("code = %q, want %q (unclosed-selection must win over a stale revision — checked before the lock and the revision compare)", code, CodeUnclosedSelection)
	}

	after := snapshotWorkspace(t, cwd)
	assertWorkspaceUnchanged(t, before, after)
}

// TestPackSet_ClosedSet_AppliesUnchanged proves the closure check does not
// alter behavior for an already-closed selection (spec-skill-packs.md §3.1,
// §7 proof 9 under closure): a closed subset still applies its
// additions/removals exactly as it did before the closure check existed, and
// a CLOSED expanded set with a stale revision still returns CodeConflict
// byte-identically — the closure check passes silently and the existing
// revision-compare gate still catches the stale value.
func TestPackSet_ClosedSet_AppliesUnchanged(t *testing.T) {
	t.Parallel()
	pack, ok := Lookup("matt-pocock-skills")
	if !ok {
		t.Fatal("test setup: matt-pocock-skills must be a real catalog id")
	}

	t.Run("closed subset removal applies exactly as before", func(t *testing.T) {
		t.Parallel()
		cwd := t.TempDir()
		installCleanPackForTest(t, cwd, pack, []seedSkillSpec{
			{name: "tdd", sourcePath: "skills/engineering/tdd", files: map[string]string{"SKILL.md": "# x\n"}},
			{name: "code-review", sourcePath: "skills/engineering/code-review", files: map[string]string{"SKILL.md": "# x\n"}},
			{name: "setup-matt-pocock-skills", sourcePath: "skills/engineering/setup-matt-pocock-skills", files: map[string]string{"SKILL.md": "# x\n"}},
		})
		before, err := Status(cwd, "matt-pocock-skills")
		if err != nil {
			t.Fatalf("Status: %v", err)
		}
		if !equalStrings(before.Selected, []string{"code-review", "setup-matt-pocock-skills", "tdd"}) {
			t.Fatalf("test setup: Selected = %v, want all three seeded skills", before.Selected)
		}

		// code-review's own dependency (setup-matt-pocock-skills) stays
		// selected: the desired set is closed. Dropping tdd is a plain
		// removal.
		result, err := PackSet(context.Background(), cwd, "matt-pocock-skills", []string{"code-review", "setup-matt-pocock-skills"}, before.Revision)
		if err != nil {
			t.Fatalf("PackSet: unexpected error for a closed selection: %v", err)
		}
		if !result.Changed || result.State != StateInstalled || result.SkillCount != 2 {
			t.Fatalf("result = %+v, want Changed=true State=installed SkillCount=2", result)
		}
		if !equalStrings(result.Selected, []string{"code-review", "setup-matt-pocock-skills"}) {
			t.Errorf("Selected = %v, want [code-review setup-matt-pocock-skills]", result.Selected)
		}
		for _, tg := range targets {
			if _, statErr := os.Stat(filepath.Join(cwd, targetSkillDest(tg, "tdd"))); !os.IsNotExist(statErr) {
				t.Errorf("expected %s copy of tdd removed, stat err = %v", tg, statErr)
			}
		}
	})

	t.Run("closed expanded set with a stale revision still conflicts byte-identically", func(t *testing.T) {
		t.Parallel()
		cwd := t.TempDir()
		installCleanPackForTest(t, cwd, pack, []seedSkillSpec{
			{name: "tdd", sourcePath: "skills/engineering/tdd", files: map[string]string{"SKILL.md": "# x\n"}},
		})
		before := snapshotWorkspace(t, cwd)

		// tdd and setup-matt-pocock-skills are both leaves (no Requires of
		// their own) — this desired set is closed, and would add
		// setup-matt-pocock-skills if the revision were correct. It is not.
		_, err := PackSet(context.Background(), cwd, "matt-pocock-skills", []string{"tdd", "setup-matt-pocock-skills"}, "definitely-not-the-real-revision")
		if err == nil {
			t.Fatal("expected a conflict error for a stale revision")
		}
		if code := codeOf(t, err); code != CodeConflict {
			t.Errorf("code = %q, want %q (closure passes silently for a closed set; the revision compare must still catch the stale value)", code, CodeConflict)
		}

		after := snapshotWorkspace(t, cwd)
		assertWorkspaceUnchanged(t, before, after)
	})
}

// TestMattDetach_WholeRepoInstallThenSubset_MigratesCleanly is written
// FRESH on this branch (not ported from the archive): it builds its
// starting state with THIS branch's actual Add — a whole-repository install
// of matt-pocock-skills against a local fixture upstream that predates
// skill-level review (the fixture carries both catalogued and
// non-catalogued skills, exactly like the real upstream repo before
// spec-skill-packs.md's catalog existed) — rather than trusting a
// synthetic, hand-seeded manifest. It then applies a PackSet subset and
// proves the detach-migration semantics hold end to end starting from a
// real Add-produced manifest (spec-skill-packs.md §3.1/§6).
func TestMattDetach_WholeRepoInstallThenSubset_MigratesCleanly(t *testing.T) {
	t.Parallel()

	// A local fixture standing in for Matt's real upstream repo BEFORE
	// skill-level review existed: it carries both a skill the current
	// catalog still reviews (tdd) and one it deliberately excludes
	// (personal/scratch) — real upstream shape (spec-skill-packs.md §4.1).
	repoDir := t.TempDir()
	writeSkillMD(t, filepath.Join(repoDir, "skills", "engineering", "tdd", "SKILL.md"), "tdd", "test-driven development")
	writeSkillMD(t, filepath.Join(repoDir, "skills", "engineering", "code-review", "SKILL.md"), "code-review", "reviews changes")
	writeSkillMD(t, filepath.Join(repoDir, "skills", "personal", "scratch", "SKILL.md"), "scratch", "Matt's personal notes, never catalogued")
	newFixtureRepo(t, repoDir)

	// wholeRepoPack mirrors the REAL matt-pocock-skills catalog id but with
	// Review left at its zero value (ReviewRepositoryLevel) — exactly the
	// pre-catalog shape this branch's Add still supports for any pack whose
	// Review field isn't set, and exactly what a manifest recorded before
	// skill-level review would look like.
	wholeRepoPack := testPack("matt-pocock-skills", repoDir)

	cwd := t.TempDir()
	addResult, err := addPackForTest(t, cwd, wholeRepoPack)
	if err != nil {
		t.Fatalf("addPackForTest (whole-repo install): %v", err)
	}
	if addResult.SkillCount != 3 {
		t.Fatalf("whole-repo install SkillCount = %d, want 3 (tdd, code-review, scratch — the complete discovered set)", addResult.SkillCount)
	}
	for _, tg := range targets {
		for _, name := range []string{"tdd", "code-review", "scratch"} {
			if _, statErr := os.Stat(filepath.Join(cwd, targetSkillDest(tg, name), "SKILL.md")); statErr != nil {
				t.Fatalf("test setup: expected whole-repo-installed skill %q under %s: %v", name, tg, statErr)
			}
		}
	}

	// The REAL catalog's matt-pocock-skills pack (skill-level reviewed,
	// tdd/code-review catalogued, scratch and personal/* excluded) —
	// PackSet must migrate the whole-repo install against IT, not against
	// wholeRepoPack.
	realPack, ok := Lookup("matt-pocock-skills")
	if !ok {
		t.Fatal("matt-pocock-skills missing from the real catalog")
	}
	before, err := Status(cwd, "matt-pocock-skills")
	if err != nil {
		t.Fatalf("Status (before): %v", err)
	}
	if !equalStrings(before.Selected, []string{"code-review", "scratch", "tdd"}) {
		t.Fatalf("test setup: Selected = %v, want [code-review scratch tdd] (the true whole-repo on-disk state)", before.Selected)
	}

	result, err := packSetForPack(context.Background(), cwd, realPack, []string{"tdd"}, before.Revision)
	if err != nil {
		t.Fatalf("packSetForPack: %v", err)
	}
	if !result.Changed || result.State != StateInstalled || result.SkillCount != 1 {
		t.Fatalf("result = %+v, want Changed=true State=installed SkillCount=1", result)
	}
	if !equalStrings(result.Selected, []string{"tdd"}) {
		t.Errorf("Selected = %v, want [tdd]", result.Selected)
	}

	// code-review (catalogued, deselected, clean) is fully deleted.
	for _, tg := range targets {
		if _, statErr := os.Stat(filepath.Join(cwd, targetSkillDest(tg, "code-review"))); !os.IsNotExist(statErr) {
			t.Errorf("expected %s copy of code-review deleted, stat err = %v", tg, statErr)
		}
	}
	// scratch (never catalogued at all) survives, detached rather than
	// deleted — never silently dropped, never silently kept selected.
	for _, tg := range targets {
		if _, statErr := os.Stat(filepath.Join(cwd, targetSkillDest(tg, "scratch"), "SKILL.md")); statErr != nil {
			t.Errorf("expected %s copy of scratch's content to survive detachment: %v", tg, statErr)
		}
		if _, statErr := os.Stat(filepath.Join(cwd, targetSkillDest(tg, "scratch"), markerFileName)); !os.IsNotExist(statErr) {
			t.Errorf("expected %s copy of scratch's marker detached, stat err = %v", tg, statErr)
		}
	}
	// tdd (catalogued, kept) survives untouched.
	for _, tg := range targets {
		if _, statErr := os.Stat(filepath.Join(cwd, targetSkillDest(tg, "tdd"), "SKILL.md")); statErr != nil {
			t.Errorf("expected %s copy of tdd to survive: %v", tg, statErr)
		}
	}

	m := loadManifestOrFatal(t, cwd, "matt-pocock-skills")
	if m == nil {
		t.Fatal("expected the manifest to still exist")
	}
	if !equalStrings(selectedSkillNames(m), []string{"tdd"}) {
		t.Errorf("manifest skills = %v, want [tdd] (scratch must never be silently carried forward as selected)", selectedSkillNames(m))
	}
}
