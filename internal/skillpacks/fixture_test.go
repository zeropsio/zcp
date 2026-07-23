package skillpacks

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// isolatedGitEnv builds a git-only environment pointed at a throwaway HOME
// (mirrors internal/ops/deploy_ssh_test.go's helper) so fixture repo
// creation never reads or writes the developer's real global gitconfig, and
// commits always have an identity via GIT_AUTHOR_*/GIT_COMMITTER_* rather
// than depending on one being configured.
func isolatedGitEnv(t *testing.T) []string {
	t.Helper()
	home := t.TempDir()
	env := os.Environ()
	filtered := make([]string, 0, len(env)+6)
	for _, e := range env {
		switch {
		case strings.HasPrefix(e, "HOME="),
			strings.HasPrefix(e, "GIT_CONFIG_GLOBAL="),
			strings.HasPrefix(e, "GIT_CONFIG_SYSTEM="),
			strings.HasPrefix(e, "GIT_CONFIG_NOSYSTEM="):
			continue
		}
		filtered = append(filtered, e)
	}
	return append(filtered,
		"HOME="+home,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=zcp-test",
		"GIT_AUTHOR_EMAIL=zcp-test@example.com",
		"GIT_COMMITTER_NAME=zcp-test",
		"GIT_COMMITTER_EMAIL=zcp-test@example.com",
	)
}

func mustRunGit(t *testing.T, dir string, env []string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = dir
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// newFixtureRepo git-inits dir (which the caller has already populated with
// files) and commits everything as one commit. dir itself is then usable
// directly as a `git clone` source — a plain filesystem path, no file://
// scheme needed and no network access.
func newFixtureRepo(t *testing.T, dir string) {
	t.Helper()
	env := isolatedGitEnv(t)
	mustRunGit(t, dir, env, "init", "-q", "-b", "main")
	mustRunGit(t, dir, env, "add", "-A")
	mustRunGit(t, dir, env, "commit", "-q", "-m", "fixture")
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// testCommit is a syntactically valid (40 hex char) commit SHA used
// wherever a test needs a well-formed commit that was never actually
// produced by git.
const testCommit = "0123456789abcdef0123456789abcdef01234567"

// writeSkillMD writes a minimal, valid SKILL.md declaring name/description
// at path.
func writeSkillMD(t *testing.T, path, name, description string) {
	t.Helper()
	writeFile(t, path, fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n\n# %s\n", name, description, name))
}

// writeSymlinkOrSkip creates a symlink, skipping the calling test outright
// when the filesystem doesn't support them (mirrors every other symlink
// test in this package).
func writeSymlinkOrSkip(t *testing.T, target, linkPath string) {
	t.Helper()
	if err := os.Symlink(target, linkPath); err != nil {
		t.Skipf("symlinks unsupported on this filesystem: %v", err)
	}
}

// testPack builds a Pack whose CloneURL/Ref point at a local fixture repo
// (a plain filesystem path on the "main" branch newFixtureRepo always
// creates) — `git clone` accepts that directly, so tests using it never
// touch the network. id must not collide with a real catalog id so a bug
// that accidentally calls Add(id) instead of the addFresh test seam fails
// loudly instead of silently hitting the real catalog entry.
func testPack(id, repoDir string) Pack {
	return Pack{ID: id, Repo: "local/" + id, CloneURL: repoDir, Ref: "main", Title: id, Description: "test pack"}
}

// addPackForTest exercises Add's absent→installed flow against an explicit
// Pack (bypassing catalog Lookup) so tests can point CloneURL at a local
// fixture repo. Unlike the public Add, it does not acquire the
// cross-process lock — tests that need lock behavior exercise
// acquirePackLock directly (see lock_test.go).
func addPackForTest(t *testing.T, cwd string, pack Pack) (Result, error) {
	t.Helper()
	root, err := openWorkspaceRoot(cwd)
	if err != nil {
		return Result{Operation: "add", PackID: pack.ID}, err
	}
	defer func() { _ = root.Close() }()
	return addFresh(context.Background(), root, pack, Result{Operation: "add", PackID: pack.ID})
}

// seedSkillSpec describes one skill's source content for
// installCleanPackForTest.
type seedSkillSpec struct {
	name       string
	sourcePath string
	files      map[string]string // relative path -> content, e.g. "SKILL.md": "# x\n"
}

// installCleanPackForTest builds a fully-valid installed pack (both target
// copies, correct markers, correct manifest, pinned at testCommit) directly
// on disk — without cloning or discovery — so remove/status tests can seed
// a known-clean starting state and then perturb specific copies by hand.
func installCleanPackForTest(t *testing.T, cwd string, pack Pack, skills []seedSkillSpec) {
	t.Helper()
	root, err := openWorkspaceRoot(cwd)
	if err != nil {
		t.Fatalf("openWorkspaceRoot: %v", err)
	}
	defer func() { _ = root.Close() }()

	generation := uuid.NewString()
	entries := make([]SkillEntry, 0, len(skills))
	for _, sk := range skills {
		srcDir := t.TempDir()
		for relPath, content := range sk.files {
			writeFile(t, filepath.Join(srcDir, relPath), content)
		}

		digest := ""
		for _, tg := range targets {
			if err := root.MkdirAll(targetSkillsDir(tg), 0o755); err != nil {
				t.Fatalf("mkdir %s: %v", targetSkillsDir(tg), err)
			}
			destRel := targetSkillDest(tg, sk.name)
			if err := copyTreeIntoRoot(root, srcDir, destRel); err != nil {
				t.Fatalf("copyTreeIntoRoot: %v", err)
			}
			d, err := treeDigest(root, destRel)
			if err != nil {
				t.Fatalf("treeDigest: %v", err)
			}
			if digest == "" {
				digest = d
			}
			marker := Marker{
				SchemaVersion: markerSchemaVersion, PackID: pack.ID, Generation: generation,
				Target: string(tg), SkillName: sk.name, SourcePath: sk.sourcePath, Commit: testCommit, Digest: d,
			}
			if err := writeMarker(root, destRel, marker); err != nil {
				t.Fatalf("writeMarker: %v", err)
			}
		}
		entries = append(entries, SkillEntry{Name: sk.name, SourcePath: sk.sourcePath, Digest: digest})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })

	m := Manifest{
		SchemaVersion: manifestSchemaVersion, ID: pack.ID, Generation: generation,
		Source:  SourceRef{Repo: pack.Repo, CloneURL: pack.CloneURL, Ref: pack.Ref, Commit: testCommit},
		Targets: []string{string(TargetAgents), string(TargetClaude)},
		Skills:  entries,
	}
	if err := writeManifest(root, m); err != nil {
		t.Fatalf("writeManifest: %v", err)
	}
}
