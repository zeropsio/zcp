package skillpacks

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestAdd_UnknownID_Errors(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()

	_, err := Add(context.Background(), cwd, "not-a-real-pack")
	if err == nil {
		t.Fatal("expected an error for an unknown id")
	}
	if code := codeOf(t, err); code != CodeUnknownPack {
		t.Errorf("code = %q, want %q", code, CodeUnknownPack)
	}
}

func TestAddPack_NestedLayout_InstallsBothTargetsWithMatchingMarkers(t *testing.T) {
	t.Parallel()
	repoDir := t.TempDir()
	writeSkillMD(t, filepath.Join(repoDir, "skills", "typescript", "foo", "SKILL.md"), "foo", "does foo")
	writeSkillMD(t, filepath.Join(repoDir, "skills", "ai-sdk", "bar", "SKILL.md"), "bar", "does bar")
	newFixtureRepo(t, repoDir)

	cwd := t.TempDir()
	pack := testPack("nested-pack", repoDir)
	result, err := addPackForTest(t, cwd, pack)
	if err != nil {
		t.Fatalf("addPackForTest: %v", err)
	}
	if !result.Changed || result.State != StateInstalled || result.SkillCount != 2 {
		t.Fatalf("result = %+v, want Changed=true State=installed SkillCount=2", result)
	}

	for _, tg := range targets {
		for _, name := range []string{"foo", "bar"} {
			skillMD := filepath.Join(cwd, targetSkillDest(tg, name), "SKILL.md")
			if _, err := os.Stat(skillMD); err != nil {
				t.Errorf("expected %s to exist: %v", skillMD, err)
			}
			markerPath := filepath.Join(cwd, targetSkillDest(tg, name), markerFileName)
			data, err := os.ReadFile(markerPath)
			if err != nil {
				t.Fatalf("read marker %s: %v", markerPath, err)
			}
			if !strings.Contains(string(data), `"target": "`+string(tg)+`"`) {
				t.Errorf("marker %s does not declare target %q: %s", markerPath, tg, data)
			}
		}
	}

	root, err := openWorkspaceRoot(cwd)
	if err != nil {
		t.Fatalf("openWorkspaceRoot: %v", err)
	}
	defer func() { _ = root.Close() }()
	m, state, err := loadManifest(root, "nested-pack")
	if err != nil || state != manifestValid {
		t.Fatalf("loadManifest: state=%v err=%v", state, err)
	}
	if len(m.Skills) != 2 {
		t.Fatalf("manifest has %d skills, want 2", len(m.Skills))
	}
}

func TestAddPack_RootSkillLayout_InstallsUnderDeclaredName(t *testing.T) {
	t.Parallel()
	repoDir := t.TempDir()
	writeSkillMD(t, filepath.Join(repoDir, "SKILL.md"), "whole-repo", "the whole repo is one skill")
	writeFile(t, filepath.Join(repoDir, "resources", "helper.txt"), "helper\n")
	newFixtureRepo(t, repoDir)

	cwd := t.TempDir()
	pack := testPack("root-pack", repoDir)
	result, err := addPackForTest(t, cwd, pack)
	if err != nil {
		t.Fatalf("addPackForTest: %v", err)
	}
	if result.SkillCount != 1 {
		t.Fatalf("SkillCount = %d, want 1", result.SkillCount)
	}
	for _, tg := range targets {
		if _, err := os.Stat(filepath.Join(cwd, targetSkillDest(tg, "whole-repo"), "resources", "helper.txt")); err != nil {
			t.Errorf("expected nested resources copied under %s: %v", tg, err)
		}
	}
}

func TestAddPack_NoSkillMDAnywhere_ErrorsWithZeroWrites(t *testing.T) {
	t.Parallel()
	repoDir := t.TempDir()
	writeFile(t, filepath.Join(repoDir, "README.md"), "just a readme\n")
	newFixtureRepo(t, repoDir)

	cwd := t.TempDir()
	pack := testPack("no-skills-pack", repoDir)
	_, err := addPackForTest(t, cwd, pack)
	if err == nil {
		t.Fatal("expected an error when no SKILL.md exists anywhere")
	}
	if code := codeOf(t, err); code != CodeNoSkills {
		t.Errorf("code = %q, want %q", code, CodeNoSkills)
	}
	for _, tg := range targets {
		if _, statErr := os.Stat(filepath.Join(cwd, targetRootDir(tg))); !os.IsNotExist(statErr) {
			t.Errorf("%s should not have been created at all, stat err = %v", targetRootDir(tg), statErr)
		}
	}
}

// TestAdd_SkillLevelPack_InstallsOnlyCataloguedSkills proves the catalog is
// applied as an intersection over discovery output (spec-skill-packs.md
// §1/§3): a fixture repo carries two catalogued skills (foo, bar) and one
// excluded-looking skill (personal/baz) that discovery itself would happily
// find — only foo and bar must land on disk.
func TestAdd_SkillLevelPack_InstallsOnlyCataloguedSkills(t *testing.T) {
	t.Parallel()
	repoDir := t.TempDir()
	writeSkillMD(t, filepath.Join(repoDir, "skills", "foo", "SKILL.md"), "foo", "catalogued")
	writeSkillMD(t, filepath.Join(repoDir, "skills", "bar", "SKILL.md"), "bar", "catalogued")
	writeSkillMD(t, filepath.Join(repoDir, "skills", "personal", "baz", "SKILL.md"), "baz", "not catalogued")
	newFixtureRepo(t, repoDir)

	cwd := t.TempDir()
	pack := testPack("skill-level-fixture-pack", repoDir)
	pack.Review = ReviewSkillLevel
	pack.Skills = []CatalogSkill{
		{Name: "foo", SourcePath: "skills/foo", Category: "Engineering", Description: "does foo"},
		{Name: "bar", SourcePath: "skills/bar", Category: "Engineering", Description: "does bar"},
	}

	result, err := addPackForTest(t, cwd, pack)
	if err != nil {
		t.Fatalf("addPackForTest: %v", err)
	}
	if result.SkillCount != 2 {
		t.Fatalf("SkillCount = %d, want 2 (baz must be excluded)", result.SkillCount)
	}
	for _, tg := range targets {
		for _, name := range []string{"foo", "bar"} {
			if _, statErr := os.Stat(filepath.Join(cwd, targetSkillDest(tg, name), "SKILL.md")); statErr != nil {
				t.Errorf("expected catalogued skill %q under %s: %v", name, tg, statErr)
			}
		}
		if _, statErr := os.Stat(filepath.Join(cwd, targetSkillDest(tg, "baz"))); !os.IsNotExist(statErr) {
			t.Errorf("uncatalogued skill %q must not be installed under %s, stat err = %v", "baz", tg, statErr)
		}
	}
}

// TestAdd_SkillLevelPack_MissingCataloguedSkill_RefusesWithoutMutation
// proves a catalogued skill absent from the upstream clone is a hard error,
// not a silent partial install: the catalog promises the skill exists at a
// specific location, so "installation succeeds only when the complete
// supported set is installed" (spec-skill-packs.md §5) requires refusing
// with zero writes rather than quietly installing the subset that was
// found.
func TestAdd_SkillLevelPack_MissingCataloguedSkill_RefusesWithoutMutation(t *testing.T) {
	t.Parallel()
	repoDir := t.TempDir()
	writeSkillMD(t, filepath.Join(repoDir, "skills", "foo", "SKILL.md"), "foo", "catalogued and present")
	newFixtureRepo(t, repoDir)

	cwd := t.TempDir()
	pack := testPack("skill-level-missing-pack", repoDir)
	pack.Review = ReviewSkillLevel
	pack.Skills = []CatalogSkill{
		{Name: "foo", SourcePath: "skills/foo", Category: "Engineering", Description: "does foo"},
		{Name: "bar", SourcePath: "skills/bar", Category: "Engineering", Description: "catalogued but missing upstream"},
	}

	_, err := addPackForTest(t, cwd, pack)
	if err == nil {
		t.Fatal("expected an error for a catalogued skill missing upstream")
	}
	if code := codeOf(t, err); code != CodeInvalidSource {
		t.Errorf("code = %q, want %q", code, CodeInvalidSource)
	}
	if !strings.Contains(err.Error(), "bar") {
		t.Errorf("error = %v, want it to name the missing skill %q", err, "bar")
	}
	for _, tg := range targets {
		if _, statErr := os.Stat(filepath.Join(cwd, targetRootDir(tg))); !os.IsNotExist(statErr) {
			t.Errorf("%s should not have been created at all (zero writes on missing catalogued skill), stat err = %v", targetRootDir(tg), statErr)
		}
	}
}

// TestAdd_RepositoryLevelPack_InstallsFullDiscoveredSet pins the
// repository-level review path: with no declared catalog skill list, the
// complete discovered set installs together, exactly as before the
// skill-level catalog was introduced (spec-skill-packs.md §1).
func TestAdd_RepositoryLevelPack_InstallsFullDiscoveredSet(t *testing.T) {
	t.Parallel()
	repoDir := t.TempDir()
	writeSkillMD(t, filepath.Join(repoDir, "skills", "foo", "SKILL.md"), "foo", "does foo")
	writeSkillMD(t, filepath.Join(repoDir, "skills", "bar", "SKILL.md"), "bar", "does bar")
	newFixtureRepo(t, repoDir)

	cwd := t.TempDir()
	pack := testPack("repo-level-fixture-pack", repoDir)
	pack.Review = ReviewRepositoryLevel

	result, err := addPackForTest(t, cwd, pack)
	if err != nil {
		t.Fatalf("addPackForTest: %v", err)
	}
	if result.SkillCount != 2 {
		t.Fatalf("SkillCount = %d, want 2 (the full discovered set)", result.SkillCount)
	}
	for _, tg := range targets {
		for _, name := range []string{"foo", "bar"} {
			if _, statErr := os.Stat(filepath.Join(cwd, targetSkillDest(tg, name), "SKILL.md")); statErr != nil {
				t.Errorf("expected discovered skill %q under %s: %v", name, tg, statErr)
			}
		}
	}
}

func TestAddPack_MissingGitBinary_ClearError(t *testing.T) {
	// Non-parallel: mutates process-wide PATH.
	emptyPathDir := t.TempDir()
	t.Setenv("PATH", emptyPathDir)

	cwd := t.TempDir()
	pack := testPack("no-git-pack", "irrelevant")
	_, err := addPackForTest(t, cwd, pack)
	if err == nil {
		t.Fatal("expected an error when git is not on PATH")
	}
	if code := codeOf(t, err); code != CodeGitMissing {
		t.Errorf("code = %q, want %q", code, CodeGitMissing)
	}
}

// TestAddPack_CollisionInEitherTarget_ZeroWrites proves the collision
// preflight checks BOTH roots before any write: a name already taken in
// EITHER target aborts the whole pack, with nothing installed in either.
func TestAddPack_CollisionInEitherTarget_ZeroWrites(t *testing.T) {
	t.Parallel()
	for _, collideIn := range targets {
		t.Run(string(collideIn), func(t *testing.T) {
			t.Parallel()
			repoDir := t.TempDir()
			writeSkillMD(t, filepath.Join(repoDir, "skills", "foo", "SKILL.md"), "foo", "does foo")
			newFixtureRepo(t, repoDir)

			cwd := t.TempDir()
			writeFile(t, filepath.Join(cwd, targetSkillDest(collideIn, "foo"), "unrelated.txt"), "pre-existing, unrelated\n")

			pack := testPack("collide-pack-"+string(collideIn), repoDir)
			_, err := addPackForTest(t, cwd, pack)
			if err == nil {
				t.Fatal("expected a collision error")
			}
			if code := codeOf(t, err); code != CodeCollision {
				t.Errorf("code = %q, want %q", code, CodeCollision)
			}

			for _, tg := range targets {
				dest := filepath.Join(cwd, targetSkillDest(tg, "foo"))
				if tg == collideIn {
					if _, statErr := os.Stat(filepath.Join(dest, "unrelated.txt")); statErr != nil {
						t.Errorf("pre-existing content under %s must survive untouched: %v", tg, statErr)
					}
					if _, statErr := os.Stat(filepath.Join(dest, "SKILL.md")); !os.IsNotExist(statErr) {
						t.Errorf("the pre-existing dir under %s must not have been written into", tg)
					}
				} else {
					if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
						t.Errorf("%s must not have been created at all (zero writes on collision)", dest)
					}
				}
			}
		})
	}
}

// TestAddPack_BothTargetsEqualContent proves the two physical copies have
// identical non-marker content.
func TestAddPack_BothTargetsEqualContent(t *testing.T) {
	t.Parallel()
	repoDir := t.TempDir()
	writeSkillMD(t, filepath.Join(repoDir, "skills", "foo", "SKILL.md"), "foo", "does foo")
	writeFile(t, filepath.Join(repoDir, "skills", "foo", "sub", "helper.txt"), "helper\n")
	newFixtureRepo(t, repoDir)

	cwd := t.TempDir()
	pack := testPack("equal-pack", repoDir)
	if _, err := addPackForTest(t, cwd, pack); err != nil {
		t.Fatalf("addPackForTest: %v", err)
	}

	agentsSkillMD, err := os.ReadFile(filepath.Join(cwd, targetSkillDest(TargetAgents, "foo"), "SKILL.md"))
	if err != nil {
		t.Fatalf("read agents SKILL.md: %v", err)
	}
	claudeSkillMD, err := os.ReadFile(filepath.Join(cwd, targetSkillDest(TargetClaude, "foo"), "SKILL.md"))
	if err != nil {
		t.Fatalf("read claude SKILL.md: %v", err)
	}
	if string(agentsSkillMD) != string(claudeSkillMD) {
		t.Error("SKILL.md content differs between .agents and .claude copies")
	}

	agentsHelper, err := os.ReadFile(filepath.Join(cwd, targetSkillDest(TargetAgents, "foo"), "sub", "helper.txt"))
	if err != nil {
		t.Fatalf("read agents helper.txt: %v", err)
	}
	claudeHelper, err := os.ReadFile(filepath.Join(cwd, targetSkillDest(TargetClaude, "foo"), "sub", "helper.txt"))
	if err != nil {
		t.Fatalf("read claude helper.txt: %v", err)
	}
	if string(agentsHelper) != string(claudeHelper) {
		t.Error("nested content differs between .agents and .claude copies")
	}
}

// TestAdd_HealthyInstall_NoOpWithoutNetwork seeds a real, fully-clean
// install directly on disk (no clone) and proves Add's state-check happens
// BEFORE any network access: this uses the real "superpowers" catalog id,
// and if Add reached its clone step it would either hang or fail in this
// offline test environment — it must not, because the manifest is already
// healthy.
func TestAdd_HealthyInstall_NoOpWithoutNetwork(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	pack, ok := Lookup("superpowers")
	if !ok {
		t.Fatal("superpowers missing from catalog")
	}
	installCleanPackForTest(t, cwd, pack, []seedSkillSpec{
		{name: "alpha", sourcePath: "skills/alpha", files: map[string]string{"SKILL.md": "---\nname: alpha\ndescription: x\n---\n"}},
	})

	result, err := Add(context.Background(), cwd, "superpowers")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if result.Changed {
		t.Error("Changed = true, want false (healthy re-add is a no-op)")
	}
	if result.State != StateInstalled {
		t.Errorf("State = %q, want %q", result.State, StateInstalled)
	}
	if result.Commit != testCommit {
		t.Errorf("Commit = %q, want %q", result.Commit, testCommit)
	}
}

func TestAdd_ModifiedInstall_RefusesWithoutWrites(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	pack, ok := Lookup("superpowers")
	if !ok {
		t.Fatal("superpowers missing from catalog")
	}
	installCleanPackForTest(t, cwd, pack, []seedSkillSpec{
		{name: "alpha", sourcePath: "skills/alpha", files: map[string]string{"SKILL.md": "---\nname: alpha\ndescription: x\n---\n"}},
	})
	// Tamper with one copy after install.
	writeFile(t, filepath.Join(cwd, targetSkillDest(TargetAgents, "alpha"), "extra.txt"), "user added this\n")

	result, err := Add(context.Background(), cwd, "superpowers")
	if err == nil {
		t.Fatal("expected an error for a modified install")
	}
	if code := codeOf(t, err); code != CodeLocalChanges {
		t.Errorf("code = %q, want %q", code, CodeLocalChanges)
	}
	if result.Changed {
		t.Error("Changed = true, want false")
	}
	// The tampered file must survive untouched (refuse, don't overwrite).
	if _, statErr := os.Stat(filepath.Join(cwd, targetSkillDest(TargetAgents, "alpha"), "extra.txt")); statErr != nil {
		t.Errorf("expected the local edit to survive: %v", statErr)
	}
}

func TestAdd_IncompleteInstall_RefusesWithRecoveryInstructions(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	pack, ok := Lookup("superpowers")
	if !ok {
		t.Fatal("superpowers missing from catalog")
	}
	installCleanPackForTest(t, cwd, pack, []seedSkillSpec{
		{name: "alpha", sourcePath: "skills/alpha", files: map[string]string{"SKILL.md": "---\nname: alpha\ndescription: x\n---\n"}},
	})
	if err := os.RemoveAll(filepath.Join(cwd, targetSkillDest(TargetClaude, "alpha"))); err != nil {
		t.Fatalf("remove claude copy: %v", err)
	}

	result, err := Add(context.Background(), cwd, "superpowers")
	if err == nil {
		t.Fatal("expected an error for an incomplete install")
	}
	if code := codeOf(t, err); code != CodeIncomplete {
		t.Errorf("code = %q, want %q", code, CodeIncomplete)
	}
	if !strings.Contains(err.Error(), "pack-remove") || !strings.Contains(err.Error(), "pack-add") {
		t.Errorf("error = %v, want it to mention both pack-remove and pack-add", err)
	}
	if result.Changed {
		t.Error("Changed = true, want false")
	}
}

func TestAdd_LegacyManifest_RefusesWithoutMutation(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	root, err := openWorkspaceRoot(cwd)
	if err != nil {
		t.Fatalf("openWorkspaceRoot: %v", err)
	}
	if err := root.MkdirAll(skillPacksStateDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, filepath.Join(cwd, manifestRelPath("superpowers")), `{"id":"superpowers","repo":"obra/superpowers","commit":"deadbeef","installedDirs":["superpowers"]}`)
	_ = root.Close()

	_, err = Add(context.Background(), cwd, "superpowers")
	if err == nil {
		t.Fatal("expected an error for a legacy manifest")
	}
	if code := codeOf(t, err); code != CodeLegacyState {
		t.Errorf("code = %q, want %q", code, CodeLegacyState)
	}
	for _, tg := range targets {
		if _, statErr := os.Stat(filepath.Join(cwd, targetRootDir(tg))); !os.IsNotExist(statErr) {
			t.Errorf("%s must not be created by a refused legacy-state add", targetRootDir(tg))
		}
	}
}

func TestAdd_CorruptManifest_Refuses(t *testing.T) {
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

	_, err = Add(context.Background(), cwd, "superpowers")
	if err == nil {
		t.Fatal("expected an error for a corrupt manifest")
	}
	if code := codeOf(t, err); code != CodeCorruptState {
		t.Errorf("code = %q, want %q", code, CodeCorruptState)
	}
}

// TestPublishPack_MidCopyFailure_RollsBackEverythingPublishedSoFar is the
// touched-tracking cleanup proof: skill-a fully publishes to both targets,
// then skill-b fails partway through copying (an unreadable file). Every
// directory this call itself published — skill-a under both targets — must
// be rolled back, and no staging remnants may survive.
func TestPublishPack_MidCopyFailure_RollsBackEverythingPublishedSoFar(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("skipping under root: permission bits are not enforced")
	}
	t.Parallel()
	srcRoot := t.TempDir()
	writeSkillMD(t, filepath.Join(srcRoot, "skill-a", "SKILL.md"), "skill-a", "will succeed")
	writeSkillMD(t, filepath.Join(srcRoot, "skill-b", "SKILL.md"), "skill-b", "will fail")
	blocked := filepath.Join(srcRoot, "skill-b", "blocked.txt")
	writeFile(t, blocked, "unreadable\n")
	if err := os.Chmod(blocked, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(blocked, 0o644) })

	cwd := t.TempDir()
	root, err := openWorkspaceRoot(cwd)
	if err != nil {
		t.Fatalf("openWorkspaceRoot: %v", err)
	}
	defer func() { _ = root.Close() }()

	pack := testPack("midfail-pack", "unused")
	candidates := []Candidate{
		{Name: "skill-a", SourcePath: "skill-a", SourceDir: filepath.Join(srcRoot, "skill-a")},
		{Name: "skill-b", SourcePath: "skill-b", SourceDir: filepath.Join(srcRoot, "skill-b")},
	}
	_, err = publishPack(root, pack, candidates, uuid.NewString(), testCommit)
	if err == nil {
		t.Fatal("expected a mid-copy failure")
	}

	for _, tg := range targets {
		for _, name := range []string{"skill-a", "skill-b"} {
			dest := filepath.Join(cwd, targetSkillDest(tg, name))
			if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
				t.Errorf("expected %s to be rolled back, stat err = %v", dest, statErr)
			}
		}
		stagingParent := filepath.Join(cwd, targetRootDir(tg))
		entries, _ := os.ReadDir(stagingParent)
		for _, e := range entries {
			if strings.Contains(e.Name(), "zcp-skillpacks-staging") {
				t.Errorf("staging directory leaked under %s: %s", tg, e.Name())
			}
		}
	}
	if m := loadManifestOrFatal(t, cwd, "midfail-pack"); m != nil {
		t.Error("no manifest should exist after a mid-copy failure")
	}
}

// loadManifestOrFatal opens root and loads id's manifest, failing the test
// on any unexpected error (an absent manifest is a normal nil return, not a
// failure).
func loadManifestOrFatal(t *testing.T, cwd, id string) *Manifest {
	t.Helper()
	root, err := openWorkspaceRoot(cwd)
	if err != nil {
		t.Fatalf("openWorkspaceRoot: %v", err)
	}
	defer func() { _ = root.Close() }()
	m, _, err := loadManifest(root, id)
	if err != nil {
		t.Fatalf("loadManifest: %v", err)
	}
	return m
}
