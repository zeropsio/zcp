// Tests for: ops/git/repo.go — AdoptBaseline against a real local git repo,
// proving the shell scripts built in repo.go actually run (the fakeRunner
// tests in repo_test.go only pin the command strings). Reuses
// isolatedHomeRunner from integration_test.go so the snapshot commit's
// inline identity is proven independent of the machine's own ~/.gitconfig.
package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
)

// gitOutput runs a plain git command against dir and returns trimmed
// combined output, failing the test on a non-zero exit.
func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", dir}, args...)
	out, err := exec.CommandContext(context.Background(), "git", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// baselineTreeLineCount returns the number of entries `git ls-tree -r`
// reports for a revision on dir.
func baselineTreeLineCount(t *testing.T, dir, revision string) int {
	t.Helper()
	out := gitOutput(t, dir, "ls-tree", "-r", revision)
	if out == "" {
		return 0
	}
	return len(strings.Split(out, "\n"))
}

func TestAdoptBaseline_Integration_FilesNoRepo_SnapshotsNonEmptyTreeWithRobotIdentity(t *testing.T) {
	if testing.Short() {
		t.Skip("shells out to the real git binary")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app.js"), []byte("console.log('hi')\n"), 0o644); err != nil {
		t.Fatalf("write app.js: %v", err)
	}
	r := isolatedHomeRunner{homeDir: t.TempDir()}

	result, err := AdoptBaseline(context.Background(), r, dir, "av-real-1", topology.RuntimeDynamic)
	if err != nil {
		t.Fatalf("AdoptBaseline: %v", err)
	}
	if result.Case != AdoptCaseSnapshot {
		t.Errorf("Case = %q, want %q", result.Case, AdoptCaseSnapshot)
	}

	revision := "HEAD"
	if n := baselineTreeLineCount(t, dir, revision); n == 0 {
		t.Error("snapshot tree is empty, want app.js to be present")
	}
	msg := gitOutput(t, dir, "log", "-1", "--format=%s", revision)
	if !strings.HasPrefix(msg, "zcp: snapshot") {
		t.Errorf("commit message = %q, want it to start with %q", msg, "zcp: snapshot")
	}
	authorEmail := gitOutput(t, dir, "log", "-1", "--format=%ae", revision)
	if authorEmail != robotIdentityEmail {
		t.Errorf("author email = %q, want %q (robot identity, no ambient ~/.gitconfig)", authorEmail, robotIdentityEmail)
	}
}

func TestAdoptBaseline_Integration_EmptyTreeMarkerHEAD_SkipsInitStillSnapshots(t *testing.T) {
	if testing.Short() {
		t.Skip("shells out to the real git binary")
	}
	dir := t.TempDir()
	// Simulate ops.InitServiceGit's GLC-1 marker commit: a repo whose HEAD
	// is a commit over the empty tree, built exactly the way
	// gitHeadEnsureFragment does (git mktree </dev/null + commit-tree +
	// update-ref HEAD) — no working-tree files staged, no index touched.
	run := func(args ...string) string {
		cmd := exec.CommandContext(context.Background(), "git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=zcp-test", "GIT_AUTHOR_EMAIL=zcp-test@example.com",
			"GIT_COMMITTER_NAME=zcp-test", "GIT_COMMITTER_EMAIL=zcp-test@example.com")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("init", "-q", "-b", "main")

	cmd := exec.CommandContext(context.Background(), "git", "mktree")
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader("")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git mktree: %v\n%s", err, out)
	}
	treeSHA := strings.TrimSpace(string(out))
	if treeSHA != emptyTreeSHA {
		t.Fatalf("git mktree </dev/null = %q, want the well-known empty tree %q", treeSHA, emptyTreeSHA)
	}
	commitSHA := run("-c", "user.name=zcp-test", "-c", "user.email=zcp-test@example.com", "commit-tree", treeSHA, "-m", "zcp init")
	run("update-ref", "HEAD", commitSHA)

	// Now the "application files" land next to the marker HEAD — exactly
	// what a buildFromGit-deployed, adopt-without-prior-git service looks
	// like once GLC-1 has run.
	if err := os.WriteFile(filepath.Join(dir, "app.js"), []byte("console.log('hi')\n"), 0o644); err != nil {
		t.Fatalf("write app.js: %v", err)
	}

	r := isolatedHomeRunner{homeDir: t.TempDir()}
	result, err := AdoptBaseline(context.Background(), r, dir, "av-real-2", topology.RuntimeDynamic)
	if err != nil {
		t.Fatalf("AdoptBaseline: %v", err)
	}
	if result.Case != AdoptCaseSnapshot {
		t.Errorf("Case = %q, want %q", result.Case, AdoptCaseSnapshot)
	}

	revision := "HEAD"
	if n := baselineTreeLineCount(t, dir, revision); n == 0 {
		t.Error("snapshot tree is empty, want app.js to be present")
	}
	// Exactly one commit reachable from the revision beyond the marker — no
	// re-init happened (a re-init would have produced a fresh unborn repo
	// with no marker commit to be "beyond").
	revCount := gitOutput(t, dir, "rev-list", "--count", revision)
	if revCount != "2" {
		t.Errorf("rev-list --count %s = %q, want 2 (marker commit + snapshot commit)", revision, revCount)
	}
}

func TestAdoptBaseline_Integration_RealContentHEAD_PreservesHEADWithoutNewCommit(t *testing.T) {
	if testing.Short() {
		t.Skip("shells out to the real git binary")
	}
	dir := t.TempDir()
	sha := initRepo(t, dir) // integration_test.go helper: repo with one real commit (file.txt)

	r := isolatedHomeRunner{homeDir: t.TempDir()}
	result, err := AdoptBaseline(context.Background(), r, dir, "av-real-3", topology.RuntimeDynamic)
	if err != nil {
		t.Fatalf("AdoptBaseline: %v", err)
	}
	if result.Case != AdoptCaseExisting {
		t.Errorf("Case = %q, want %q", result.Case, AdoptCaseExisting)
	}

	headSHA := gitOutput(t, dir, "rev-parse", "HEAD")
	if headSHA != sha {
		t.Errorf("HEAD moved to %q, want it to stay at %q — AdoptBaseline must not commit on the existing-content path", headSHA, sha)
	}
}

func TestAdoptBaseline_Integration_PreservesTagsAndExcludes(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "snapshot", true: "existing"}[existing], func(t *testing.T) {
			dir := t.TempDir()
			if existing {
				initRepo(t, dir)
			} else {
				gitOutput(t, dir, "init", "-q", "-b", "main")
				gitOutput(t, dir, "-c", "user.name=test", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-m", "init")
			}
			gitOutput(t, dir, "tag", "v1.0.0")
			gitOutput(t, dir, "tag", "zcp/baseline/av-existing")
			before := gitOutput(t, dir, "show-ref", "--tags")
			for name, content := range map[string]string{"app.js": "app", "credentials.json": "private", ".git/info/exclude": "credentials.json"} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := AdoptBaseline(context.Background(), isolatedHomeRunner{homeDir: t.TempDir()}, dir, "av-new", topology.RuntimeDynamic); err != nil {
				t.Fatal(err)
			}
			if after := gitOutput(t, dir, "show-ref", "--tags"); after != before {
				t.Errorf("tags changed: before %s, after %s", before, after)
			}
			if tracked := gitOutput(t, dir, "ls-files", "credentials.json"); tracked != "" {
				t.Error("adoption committed an excluded credential file")
			}
			if ignored := gitOutput(t, dir, "check-ignore", "credentials.json"); ignored != "credentials.json" {
				t.Error("custom exclusion lost")
			}
		})
	}
}
