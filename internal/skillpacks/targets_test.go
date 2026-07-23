package skillpacks

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenWorkspaceRoot_Success(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	root, err := openWorkspaceRoot(cwd)
	if err != nil {
		t.Fatalf("openWorkspaceRoot: %v", err)
	}
	_ = root.Close()
}

// TestOpenWorkspaceRoot_SymlinkedGuardedAncestor_Rejected proves each of the
// five workspace-guarded paths is checked: a symlink there — even one
// pointing INSIDE the workspace, which os.Root's own escape-detection would
// never catch — must abort before any mutation, closing the case where
// ".claude -> .agents" would silently collapse the two-physical-copies
// invariant.
func TestOpenWorkspaceRoot_SymlinkedGuardedAncestor_Rejected(t *testing.T) {
	t.Parallel()
	guarded := []string{
		".agents",
		filepath.Join(".agents", "skills"),
		".claude",
		filepath.Join(".claude", "skills"),
		".zcp",
	}
	for _, rel := range guarded {
		t.Run(rel, func(t *testing.T) {
			t.Parallel()
			cwd := t.TempDir()
			outside := t.TempDir()
			full := filepath.Join(cwd, rel)
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				t.Fatalf("mkdir parent: %v", err)
			}
			writeSymlinkOrSkip(t, outside, full)

			_, err := openWorkspaceRoot(cwd)
			if err == nil {
				t.Fatalf("expected an error when %s is a symlink", rel)
			}
		})
	}
}

func TestOpenWorkspaceRoot_SymlinkedInWorkspaceAncestor_Rejected(t *testing.T) {
	t.Parallel()
	// The in-workspace case os.Root's own escape-detection does NOT catch:
	// .claude points at .agents, both real directories inside the same
	// workspace.
	cwd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cwd, ".agents"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeSymlinkOrSkip(t, filepath.Join(cwd, ".agents"), filepath.Join(cwd, ".claude"))

	_, err := openWorkspaceRoot(cwd)
	if err == nil {
		t.Fatal("expected an error when .claude is a symlink to .agents (in-workspace collapse)")
	}
}

func TestRootExists_AbsentIsFalseNoError(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	root, err := openWorkspaceRoot(cwd)
	if err != nil {
		t.Fatalf("openWorkspaceRoot: %v", err)
	}
	defer func() { _ = root.Close() }()

	exists, err := rootExists(root, filepath.Join(".agents", "skills", "nope"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exists {
		t.Error("expected false for an absent path")
	}
}

func TestRootExists_PresentIsTrue(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	writeFile(t, filepath.Join(cwd, ".agents", "skills", "present", "SKILL.md"), "# x\n")
	root, err := openWorkspaceRoot(cwd)
	if err != nil {
		t.Fatalf("openWorkspaceRoot: %v", err)
	}
	defer func() { _ = root.Close() }()

	exists, err := rootExists(root, filepath.Join(".agents", "skills", "present"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exists {
		t.Error("expected true for a present path")
	}
}

func TestTargetSkillsDir_AndDest(t *testing.T) {
	t.Parallel()
	if got := targetSkillsDir(TargetAgents); got != filepath.Join(".agents", "skills") {
		t.Errorf("targetSkillsDir(agents) = %q", got)
	}
	if got := targetSkillsDir(TargetClaude); got != filepath.Join(".claude", "skills") {
		t.Errorf("targetSkillsDir(claude) = %q", got)
	}
	if got := targetSkillDest(TargetAgents, "foo"); got != filepath.Join(".agents", "skills", "foo") {
		t.Errorf("targetSkillDest(agents, foo) = %q", got)
	}
}
