package farm

import (
	"debug/buildinfo"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestBuildCandidate_LinkedWorktree_StampsItsOwnHead pins that a farm
// binary built from a linked git worktree carries that worktree's own HEAD
// as its revision, never the revision of an unrelated repository the
// worktree happens to sit inside, and that uncommitted changes mark it
// modified — so the console names the source a batch really ran.
func TestBuildCandidate_LinkedWorktree_StampsItsOwnHead(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a real Go binary via the go toolchain; skipped under -short")
	}
	for _, tool := range []string{"sh", "git", "go", "tar"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not on PATH", tool)
		}
	}
	script := filepath.Join(filepath.Dir(wrapperScriptPath(t)), "build-candidate.sh")

	// outer is an unrelated repository; the module's worktree sits inside it.
	outer := t.TempDir()
	gitRun(t, outer, "init", "-q")
	writeFile(t, outer, "README", "outer")
	gitRun(t, outer, "add", ".")
	gitRun(t, outer, "commit", "-qm", "outer")
	outerHead := gitRun(t, outer, "rev-parse", "HEAD")

	main := filepath.Join(t.TempDir(), "main")
	gitRun(t, "", "init", "-q", main)
	writeFile(t, main, "go.mod", "module example.com/candidate\n\ngo 1.21\n")
	writeFile(t, main, "main.go", "package main\n\nfunc main() {}\n")
	writeFile(t, main, ".gitignore", "internal/knowledge/recipes/*.md\n")
	gitRun(t, main, "add", ".")
	gitRun(t, main, "commit", "-qm", "candidate")
	worktree := filepath.Join(outer, "wt")
	gitRun(t, main, "worktree", "add", "-q", "-b", "farm", worktree)
	writeFile(t, worktree, "extra.go", "package main\n\nvar extra = 1\n")
	gitRun(t, worktree, "add", "extra.go")
	gitRun(t, worktree, "commit", "-qm", "worktree commit")
	worktreeHead := gitRun(t, worktree, "rev-parse", "HEAD")
	if worktreeHead == outerHead {
		t.Fatal("fixture: worktree and outer repository share a HEAD")
	}

	out := filepath.Join(t.TempDir(), "zcp-linux")
	build := func() map[string]string {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "sh", script, out, ".")
		cmd.Dir = worktree
		cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build-candidate.sh: %v\n%s", err, output)
		}
		bi, err := buildinfo.ReadFile(out)
		if err != nil {
			t.Fatalf("read build info: %v", err)
		}
		settings := map[string]string{}
		for _, s := range bi.Settings {
			settings[s.Key] = s.Value
		}
		return settings
	}

	clean := build()
	if clean["vcs.revision"] != worktreeHead || clean["vcs.modified"] != "false" {
		t.Errorf("clean worktree build stamped revision %q modified %q, want %q modified false (outer repository is %q)", clean["vcs.revision"], clean["vcs.modified"], worktreeHead, outerHead)
	}

	writeFile(t, worktree, "extra.go", "package main\n\nvar extra = 2\n")
	dirty := build()
	if dirty["vcs.revision"] != worktreeHead || dirty["vcs.modified"] != "true" {
		t.Errorf("dirty worktree build stamped revision %q modified %q, want %q modified true", dirty["vcs.revision"], dirty["vcs.modified"], worktreeHead)
	}
}

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=farm", "GIT_AUTHOR_EMAIL=farm@example.com", "GIT_COMMITTER_NAME=farm", "GIT_COMMITTER_EMAIL=farm@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}
