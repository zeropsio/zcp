package skillpacks

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func candidateNames(cs []Candidate) []string {
	names := make([]string, len(cs))
	for i, c := range cs {
		names[i] = c.Name
	}
	sort.Strings(names)
	return names
}

func codeOf(t *testing.T, err error) string {
	t.Helper()
	var ce *CodedError
	if !errors.As(err, &ce) {
		t.Fatalf("error %v is not a *CodedError", err)
	}
	return ce.Code
}

// TestDiscoverSkills_NestedCategoryLayout reproduces the confirmed Matt
// Pocock failure mode this whole redesign responds to: skills nested under
// a category directory (skills/<category>/<skill>/SKILL.md), which the old
// discovery (only skills/<name>/SKILL.md) never found at all.
func TestDiscoverSkills_NestedCategoryLayout(t *testing.T) {
	t.Parallel()
	repo := t.TempDir()
	writeSkillMD(t, filepath.Join(repo, "skills", "typescript", "foo", "SKILL.md"), "foo", "does foo")
	writeSkillMD(t, filepath.Join(repo, "skills", "ai-sdk", "bar", "SKILL.md"), "bar", "does bar")

	got, err := discoverSkills(repo)
	if err != nil {
		t.Fatalf("discoverSkills: %v", err)
	}
	if !equalStrings(candidateNames(got), []string{"bar", "foo"}) {
		t.Fatalf("names = %v, want [bar foo]", candidateNames(got))
	}
	for _, c := range got {
		var want string
		switch c.Name {
		case "foo":
			want = "skills/typescript/foo"
		case "bar":
			want = "skills/ai-sdk/bar"
		}
		if c.SourcePath != want {
			t.Errorf("candidate %q sourcePath = %q, want %q", c.Name, c.SourcePath, want)
		}
	}
}

// TestDiscoverSkills_RootSkillDominance proves a root SKILL.md flattens the
// entire repository into one skill and suppresses nested discovery — even
// when nested skill roots also exist.
func TestDiscoverSkills_RootSkillDominance(t *testing.T) {
	t.Parallel()
	repo := t.TempDir()
	writeSkillMD(t, filepath.Join(repo, "SKILL.md"), "whole-repo", "the whole repo is one skill")
	writeSkillMD(t, filepath.Join(repo, "skills", "foo", "SKILL.md"), "foo", "does foo")

	got, err := discoverSkills(repo)
	if err != nil {
		t.Fatalf("discoverSkills: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("candidates = %v, want exactly 1 (root dominance)", got)
	}
	if got[0].Name != "whole-repo" || got[0].SourcePath != "." {
		t.Errorf("candidate = %+v, want Name=whole-repo SourcePath=.", got[0])
	}
}

// TestDiscoverSkills_DotDirectory proves dot-directories are traversed
// (".agents"/".claude" are legitimate upstream source layouts too) — only
// .git/.hg/.svn/.zcp are skipped.
func TestDiscoverSkills_DotDirectory(t *testing.T) {
	t.Parallel()
	repo := t.TempDir()
	writeSkillMD(t, filepath.Join(repo, ".claude", "skills", "nested", "SKILL.md"), "nested", "lives under a dot dir")

	got, err := discoverSkills(repo)
	if err != nil {
		t.Fatalf("discoverSkills: %v", err)
	}
	if !equalStrings(candidateNames(got), []string{"nested"}) {
		t.Fatalf("names = %v, want [nested]", candidateNames(got))
	}
}

// TestDiscoverSkills_PersonalAndInProgressIncluded proves there is no
// semantic category filtering: "personal" and "in-progress" directory
// names are installed like any other category.
func TestDiscoverSkills_PersonalAndInProgressIncluded(t *testing.T) {
	t.Parallel()
	repo := t.TempDir()
	writeSkillMD(t, filepath.Join(repo, "skills", "personal", "x", "SKILL.md"), "x", "personal skill")
	writeSkillMD(t, filepath.Join(repo, "skills", "in-progress", "y", "SKILL.md"), "y", "wip skill")

	got, err := discoverSkills(repo)
	if err != nil {
		t.Fatalf("discoverSkills: %v", err)
	}
	if !equalStrings(candidateNames(got), []string{"x", "y"}) {
		t.Fatalf("names = %v, want [x y]", candidateNames(got))
	}
}

func TestDiscoverSkills_VCSAndStateDirsSkipped(t *testing.T) {
	t.Parallel()
	repo := t.TempDir()
	writeSkillMD(t, filepath.Join(repo, "skills", "foo", "SKILL.md"), "foo", "does foo")
	writeFile(t, filepath.Join(repo, ".git", "SKILL.md"), "---\nname: git\ndescription: x\n---\n")
	writeFile(t, filepath.Join(repo, ".zcp", "SKILL.md"), "---\nname: zcp\ndescription: x\n---\n")

	got, err := discoverSkills(repo)
	if err != nil {
		t.Fatalf("discoverSkills: %v", err)
	}
	if !equalStrings(candidateNames(got), []string{"foo"}) {
		t.Fatalf("names = %v, want [foo] (.git/.zcp must be skipped)", candidateNames(got))
	}
}

func TestDiscoverSkills_NoSkillMDAnywhere_NoSkillsCode(t *testing.T) {
	t.Parallel()
	repo := t.TempDir()
	writeFile(t, filepath.Join(repo, "README.md"), "just a readme\n")

	_, err := discoverSkills(repo)
	if err == nil {
		t.Fatal("expected an error when no SKILL.md exists anywhere")
	}
	if code := codeOf(t, err); code != CodeNoSkills {
		t.Errorf("code = %q, want %q", code, CodeNoSkills)
	}
}

func TestDiscoverSkills_InvalidFrontmatter_MissingName_Errors(t *testing.T) {
	t.Parallel()
	repo := t.TempDir()
	writeFile(t, filepath.Join(repo, "skills", "foo", "SKILL.md"), "---\ndescription: no name here\n---\n")

	_, err := discoverSkills(repo)
	if err == nil {
		t.Fatal("expected an error for missing frontmatter name")
	}
	if code := codeOf(t, err); code != CodeInvalidSource {
		t.Errorf("code = %q, want %q", code, CodeInvalidSource)
	}
}

func TestDiscoverSkills_DuplicateNames_Rejected(t *testing.T) {
	t.Parallel()
	repo := t.TempDir()
	// Two different upstream locations flattening to the same destination
	// name "shared" — the portable-name charset is lowercase-only by
	// construction, so a genuinely case-differing collision can never reach
	// this check; a plain duplicate is the exercisable form of "case-folded
	// duplicate rejection".
	writeSkillMD(t, filepath.Join(repo, "skills", "cat-a", "shared", "SKILL.md"), "shared", "first")
	writeSkillMD(t, filepath.Join(repo, "skills", "cat-b", "shared", "SKILL.md"), "shared", "second")

	_, err := discoverSkills(repo)
	if err == nil {
		t.Fatal("expected an error for duplicate destination names")
	}
	if code := codeOf(t, err); code != CodeInvalidSource {
		t.Errorf("code = %q, want %q", code, CodeInvalidSource)
	}
}

func TestDiscoverSkills_NameNotEqualToDirectory_Rejected(t *testing.T) {
	t.Parallel()
	repo := t.TempDir()
	writeSkillMD(t, filepath.Join(repo, "skills", "foo", "SKILL.md"), "not-foo", "declares a different name")

	_, err := discoverSkills(repo)
	if err == nil {
		t.Fatal("expected an error when declared name != directory name")
	}
	if code := codeOf(t, err); code != CodeInvalidSource {
		t.Errorf("code = %q, want %q", code, CodeInvalidSource)
	}
}

func TestDiscoverSkills_ReservedGuidedName_Rejected(t *testing.T) {
	t.Parallel()
	repo := t.TempDir()
	writeSkillMD(t, filepath.Join(repo, "skills", "guided", "SKILL.md"), "guided", "tries to shadow the reserved name")

	_, err := discoverSkills(repo)
	if err == nil {
		t.Fatal(`expected an error for the reserved "guided" name`)
	}
	if code := codeOf(t, err); code != CodeInvalidSource {
		t.Errorf("code = %q, want %q", code, CodeInvalidSource)
	}
}

func TestDiscoverSkills_SymlinkedSkillMD_HardError(t *testing.T) {
	t.Parallel()
	repo := t.TempDir()
	realSkillMD := filepath.Join(t.TempDir(), "SKILL.md")
	writeFile(t, realSkillMD, "---\nname: foo\ndescription: x\n---\n")
	linkPath := filepath.Join(repo, "skills", "foo", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(linkPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeSymlinkOrSkip(t, realSkillMD, linkPath)

	_, err := discoverSkills(repo)
	if err == nil {
		t.Fatal("expected a hard error for a symlinked SKILL.md, not a silent skip")
	}
	if code := codeOf(t, err); code != CodeInvalidSource {
		t.Errorf("code = %q, want %q", code, CodeInvalidSource)
	}
}

// TestDiscoverSkills_SymlinkInsideSelectedTree_HardError proves the
// deliberate behavior change from the old implementation: a symlink
// anywhere inside a SELECTED skill's subtree is now a hard error, not a
// silently-skipped entry.
func TestDiscoverSkills_SymlinkInsideSelectedTree_HardError(t *testing.T) {
	t.Parallel()
	repo := t.TempDir()
	writeSkillMD(t, filepath.Join(repo, "skills", "foo", "SKILL.md"), "foo", "has a stray symlink")
	outside := filepath.Join(t.TempDir(), "secret.txt")
	writeFile(t, outside, "must never be reachable\n")
	writeSymlinkOrSkip(t, outside, filepath.Join(repo, "skills", "foo", "escape-link"))

	_, err := discoverSkills(repo)
	if err == nil {
		t.Fatal("expected a hard error for a symlink inside a selected skill tree")
	}
	if code := codeOf(t, err); code != CodeInvalidSource {
		t.Errorf("code = %q, want %q", code, CodeInvalidSource)
	}
}

// TestDiscoverSkills_DepthCapExceeded proves a SKILL.md nested past
// maxWalkDepth is never found: walkForSkillRoots returns at function entry
// once depth > maxWalkDepth, so a directory 9 levels deep (depth 9) is
// never even read.
func TestDiscoverSkills_DepthCapExceeded(t *testing.T) {
	t.Parallel()
	repo := t.TempDir()
	deep := repo
	for i := range 9 {
		deep = filepath.Join(deep, fmt.Sprintf("d%d", i+1))
	}
	writeSkillMD(t, filepath.Join(deep, "SKILL.md"), "too-deep", "buried past the depth cap")

	_, err := discoverSkills(repo)
	if err == nil {
		t.Fatal("expected no-skills: a SKILL.md past the depth cap must never be found")
	}
	if code := codeOf(t, err); code != CodeNoSkills {
		t.Errorf("code = %q, want %q", code, CodeNoSkills)
	}
}

func TestDiscoverSkills_WithinDepthCap_Found(t *testing.T) {
	t.Parallel()
	repo := t.TempDir()
	shallow := repo
	for i := range 7 {
		shallow = filepath.Join(shallow, fmt.Sprintf("d%d", i+1))
	}
	shallow = filepath.Join(shallow, "just-shallow-enough")
	writeSkillMD(t, filepath.Join(shallow, "SKILL.md"), "just-shallow-enough", "at exactly the depth cap")

	got, err := discoverSkills(repo)
	if err != nil {
		t.Fatalf("discoverSkills: %v", err)
	}
	if !equalStrings(candidateNames(got), []string{"just-shallow-enough"}) {
		t.Fatalf("names = %v, want [just-shallow-enough]", candidateNames(got))
	}
}

// TestScanAndCapSkillTree_PerFileCapExceeded_Errors proves the 32MiB
// per-file cap is enforced.
func TestScanAndCapSkillTree_PerFileCapExceeded_Errors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "SKILL.md"), "---\nname: big\ndescription: x\n---\n")
	big := make([]byte, maxFileBytes+1)
	if err := os.WriteFile(filepath.Join(dir, "big.bin"), big, 0o644); err != nil {
		t.Fatalf("write big file: %v", err)
	}

	_, _, err := scanAndCapSkillTree(dir)
	if err == nil {
		t.Fatal("expected an error for a file exceeding the per-file byte cap")
	}
	if code := codeOf(t, err); code != CodeInvalidSource {
		t.Errorf("code = %q, want %q", code, CodeInvalidSource)
	}
}

// TestDiscoverSkills_TooManySkills_Errors proves the 512-skill cap.
func TestDiscoverSkills_TooManySkills_Errors(t *testing.T) {
	t.Parallel()
	repo := t.TempDir()
	for i := range maxSkills + 1 {
		name := fmt.Sprintf("skill-%04d", i)
		writeSkillMD(t, filepath.Join(repo, "skills", name, "SKILL.md"), name, "one of many")
	}

	_, err := discoverSkills(repo)
	if err == nil {
		t.Fatal("expected an error exceeding the skill-count cap")
	}
	if code := codeOf(t, err); code != CodeInvalidSource {
		t.Errorf("code = %q, want %q", code, CodeInvalidSource)
	}
}
