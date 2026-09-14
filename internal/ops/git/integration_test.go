// Tests for: ops/git — end-to-end against a real local git repo, proving
// the shell scripts built in sha.go/ledger.go actually run (the fakeRunner
// tests in sha_test.go/ledger_test.go only pin the command strings).
package git

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/topology"
)

// isolatedHomeRunner is a Runner that isolates HOME (and both git config
// scopes) so a test proves WriteLedger's `git tag -a` supplies its OWN
// author/committer identity rather than accidentally passing because the
// machine running the test happens to have ~/.gitconfig set.
type isolatedHomeRunner struct {
	homeDir string
}

func (r isolatedHomeRunner) Run(ctx context.Context, dir, script string) (string, string, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", script)
	cmd.Dir = dir
	cmd.Env = []string{
		"HOME=" + r.homeDir,
		"PATH=" + os.Getenv("PATH"),
		"GIT_CONFIG_GLOBAL=" + filepath.Join(r.homeDir, "no-such-gitconfig"),
		"GIT_CONFIG_SYSTEM=" + filepath.Join(r.homeDir, "no-such-gitconfig-system"),
	}
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	err := cmd.Run()
	return out.String(), errBuf.String(), err
}

// initRepo creates a git repo at dir with one commit containing file.txt,
// returning the commit's full sha.
func initRepo(t *testing.T, dir string) string {
	t.Helper()
	run := func(args ...string) {
		cmd := exec.CommandContext(context.Background(), "git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=zcp-test", "GIT_AUTHOR_EMAIL=zcp-test@example.com",
			"GIT_COMMITTER_NAME=zcp-test", "GIT_COMMITTER_EMAIL=zcp-test@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	run("init", "-q")
	run("add", "file.txt")
	run("commit", "-q", "-m", "initial")

	out, err := exec.CommandContext(context.Background(), "git", "-C", dir, "rev-parse", "HEAD").CombinedOutput()
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestOpsGit_EndToEnd_ResolveExtractLedgerRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("shells out to the real git binary")
	}
	ctx := context.Background()
	repoDir := t.TempDir()
	sha := initRepo(t, repoDir)

	r := LocalRunner{}

	resolved, err := ResolveSHA(ctx, r, repoDir, sha[:7])
	if err != nil {
		t.Fatalf("ResolveSHA: %v", err)
	}
	if resolved != sha {
		t.Fatalf("resolved = %s, want %s", resolved, sha)
	}

	tmpDir, err := MkTempDir(ctx, r)
	if err != nil {
		t.Fatalf("MkTempDir: %v", err)
	}
	defer func() { _ = RemoveTemp(ctx, r, tmpDir) }()
	if strings.HasPrefix(tmpDir, repoDir) {
		t.Fatalf("tmp dir %s must be outside repo dir %s", tmpDir, repoDir)
	}

	if err := ExtractCommitToTemp(ctx, r, repoDir, resolved, tmpDir); err != nil {
		t.Fatalf("ExtractCommitToTemp: %v", err)
	}
	extracted, err := os.ReadFile(filepath.Join(tmpDir, "file.txt"))
	if err != nil {
		t.Fatalf("read extracted file.txt: %v", err)
	}
	if string(extracted) != "hello\n" {
		t.Fatalf("extracted file.txt = %q, want %q", extracted, "hello\n")
	}
	// Extraction must not disturb the source repo's own working tree/.git.
	if _, err := os.Stat(filepath.Join(repoDir, ".git")); err != nil {
		t.Fatalf(".git missing from source repo after extraction: %v", err)
	}

	entry := topology.LedgerEntry{
		SHA:          resolved,
		AppVersionID: "av-1",
		Target:       "appstage",
		Project:      "proj-1",
		At:           "2026-09-14T12:00:00Z",
	}
	if err := WriteLedger(ctx, r, repoDir, entry); err != nil {
		t.Fatalf("WriteLedger: %v", err)
	}

	gotEntry, ok, err := LastDeployOnRecord(ctx, r, repoDir, "proj-1", "appstage")
	if err != nil {
		t.Fatalf("LastDeployOnRecord: %v", err)
	}
	if !ok {
		t.Fatal("LastDeployOnRecord: ok = false, want true")
	}
	if gotEntry != entry {
		t.Fatalf("LastDeployOnRecord = %+v, want %+v", gotEntry, entry)
	}

	// The tag is also readable directly via `git tag -l` — the mechanism
	// an eval preseed / a human replicates by hand.
	tagOut, err := exec.CommandContext(ctx, "git", "-C", repoDir, "tag", "-l", "zcp/deploy/*").CombinedOutput()
	if err != nil {
		t.Fatalf("git tag -l: %v\n%s", err, tagOut)
	}
	tags := strings.Fields(strings.TrimSpace(string(tagOut)))
	if len(tags) != 1 {
		t.Fatalf("zcp/deploy/* tags = %d, want 1: %v", len(tags), tags)
	}
	wantTag := topology.DeployTagName("proj-1", "appstage", "av-1")
	if tags[0] != wantTag {
		t.Fatalf("tag = %s, want %s", tags[0], wantTag)
	}

	// Tags are on a NEVER-checked-out namespace — the repo's own HEAD (and
	// working tree) must stay untouched.
	headOut, err := exec.CommandContext(context.Background(), "git", "-C", repoDir, "rev-parse", "HEAD").CombinedOutput()
	if err != nil {
		t.Fatalf("rev-parse HEAD after ledger write: %v\n%s", err, headOut)
	}
	if strings.TrimSpace(string(headOut)) != sha {
		t.Fatalf("HEAD moved after WriteLedger: got %s, want %s", strings.TrimSpace(string(headOut)), sha)
	}
}

func TestHeadStatus_RealRepo_CleanThenDirty(t *testing.T) {
	if testing.Short() {
		t.Skip("shells out to the real git binary")
	}
	ctx := context.Background()
	repoDir := t.TempDir()
	sha := initRepo(t, repoDir)

	r := LocalRunner{}
	gotSHA, dirty, ok, err := HeadStatus(ctx, r, repoDir)
	if err != nil {
		t.Fatalf("HeadStatus (clean): %v", err)
	}
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if gotSHA != sha {
		t.Errorf("sha = %s, want %s", gotSHA, sha)
	}
	if dirty {
		t.Error("dirty = true, want false on a freshly committed repo")
	}

	if err := os.WriteFile(filepath.Join(repoDir, "file.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatalf("dirty the tree: %v", err)
	}
	gotSHA, dirty, ok, err = HeadStatus(ctx, r, repoDir)
	if err != nil {
		t.Fatalf("HeadStatus (dirty): %v", err)
	}
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if gotSHA != sha {
		t.Errorf("sha = %s, want %s (HEAD unchanged by an uncommitted edit)", gotSHA, sha)
	}
	if !dirty {
		t.Error("dirty = false, want true after editing a tracked file")
	}
}

func TestHeadStatus_UnbornRepo_ReturnsNotOkNoError(t *testing.T) {
	if testing.Short() {
		t.Skip("shells out to the real git binary")
	}
	ctx := context.Background()
	repoDir := t.TempDir()
	if out, err := exec.CommandContext(context.Background(), "git", "init", "-q", repoDir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	_, _, ok, err := HeadStatus(ctx, LocalRunner{}, repoDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("ok = true, want false (unborn HEAD)")
	}
}

func TestLastDeployOnRecord_UnbornRepo_ReturnsNotOkNoError(t *testing.T) {
	if testing.Short() {
		t.Skip("shells out to the real git binary")
	}
	ctx := context.Background()
	repoDir := t.TempDir()
	if out, err := exec.CommandContext(context.Background(), "git", "init", "-q", repoDir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	_, ok, err := LastDeployOnRecord(ctx, LocalRunner{}, repoDir, "proj-1", "appstage")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("ok = true, want false")
	}
}

// TestWriteLedger_NoAmbientIdentity_StillSucceeds proves WriteLedger's
// `git tag -a` supplies its own identity: on a buildFromGit-provisioned
// container there is no ~/.gitconfig and no GIT_AUTHOR_*/GIT_COMMITTER_*
// env (unlike a self-deploy container, which InitServiceGit seeds), so
// without an explicit identity `git tag -a` fails "unable to auto-detect
// email address" and the ledger write is silently lost. isolatedHomeRunner
// removes every ambient identity source (HOME, both git config scopes) so
// this test cannot pass by accident on a dev machine with its own
// ~/.gitconfig.
func TestWriteLedger_NoAmbientIdentity_StillSucceeds(t *testing.T) {
	if testing.Short() {
		t.Skip("shells out to the real git binary")
	}
	ctx := context.Background()
	repoDir := t.TempDir()
	sha := initRepo(t, repoDir)
	r := isolatedHomeRunner{homeDir: t.TempDir()}

	entry := topology.LedgerEntry{
		SHA:          sha,
		AppVersionID: "av-1",
		Target:       "appstage",
		Project:      "proj-1",
		At:           "2026-09-14T12:00:00Z",
	}
	if err := WriteLedger(ctx, r, repoDir, entry); err != nil {
		t.Fatalf("WriteLedger with no ambient identity: %v", err)
	}

	got, ok, err := LastDeployOnRecord(ctx, r, repoDir, "proj-1", "appstage")
	if err != nil {
		t.Fatalf("LastDeployOnRecord: %v", err)
	}
	if !ok || got != entry {
		t.Errorf("LastDeployOnRecord = %+v, ok=%v, want %+v, ok=true", got, ok, entry)
	}
}

// TestWriteLedger_RetriedSameAppVersion_OverwritesTagWithoutError proves
// the -f (force) flag: a retried deploy of the same appVersion must not
// fail on an already-existing tag.
func TestWriteLedger_RetriedSameAppVersion_OverwritesTagWithoutError(t *testing.T) {
	if testing.Short() {
		t.Skip("shells out to the real git binary")
	}
	ctx := context.Background()
	repoDir := t.TempDir()
	sha := initRepo(t, repoDir)
	r := LocalRunner{}

	entry := topology.LedgerEntry{SHA: sha, AppVersionID: "av-1", Target: "appstage", Project: "proj-1", At: "2026-09-14T12:00:00Z"}
	if err := WriteLedger(ctx, r, repoDir, entry); err != nil {
		t.Fatalf("first WriteLedger: %v", err)
	}
	entry.At = "2026-09-14T12:05:00Z" // same appVersion, updated timestamp
	if err := WriteLedger(ctx, r, repoDir, entry); err != nil {
		t.Fatalf("second WriteLedger (retry) failed, want -f to allow overwrite: %v", err)
	}

	got, ok, err := LastDeployOnRecord(ctx, r, repoDir, "proj-1", "appstage")
	if err != nil {
		t.Fatalf("LastDeployOnRecord: %v", err)
	}
	if !ok || got.At != entry.At {
		t.Errorf("LastDeployOnRecord = %+v, want the OVERWRITTEN entry %+v", got, entry)
	}
}

// TestLastDeployOnRecord_TwoTagsForOneTarget_ReturnsNewestByTaggerDate
// writes two tags against the SAME target with distinct appVersionIds and
// asserts the newest one wins, and that both are listable via
// `git tag -l 'zcp/deploy/*'` — the mechanism an eval preseed replicates
// by hand.
func TestLastDeployOnRecord_TwoTagsForOneTarget_ReturnsNewestByTaggerDate(t *testing.T) {
	if testing.Short() {
		t.Skip("shells out to the real git binary")
	}
	ctx := context.Background()
	repoDir := t.TempDir()
	sha1 := initRepo(t, repoDir)
	// Second commit so the two tags point at genuinely distinct commits.
	if err := os.WriteFile(filepath.Join(repoDir, "file2.txt"), []byte("second\n"), 0o644); err != nil {
		t.Fatalf("write file2.txt: %v", err)
	}
	run := func(args ...string) {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = repoDir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=zcp-test", "GIT_AUTHOR_EMAIL=zcp-test@example.com",
			"GIT_COMMITTER_NAME=zcp-test", "GIT_COMMITTER_EMAIL=zcp-test@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("add", "file2.txt")
	run("commit", "-q", "-m", "second")
	shaOut, err := exec.CommandContext(ctx, "git", "-C", repoDir, "rev-parse", "HEAD").CombinedOutput()
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v\n%s", err, shaOut)
	}
	sha2 := strings.TrimSpace(string(shaOut))

	r := LocalRunner{}
	older := topology.LedgerEntry{SHA: sha1, AppVersionID: "av-1", Target: "appstage", Project: "proj-1", At: "2026-09-14T12:00:00Z"}
	if err := WriteLedger(ctx, r, repoDir, older); err != nil {
		t.Fatalf("WriteLedger (older): %v", err)
	}
	// taggerdate has 1-second resolution — sleep past it so the two tags
	// sort deterministically instead of racing on a same-second tie.
	time.Sleep(1100 * time.Millisecond)
	newer := topology.LedgerEntry{SHA: sha2, AppVersionID: "av-2", Target: "appstage", Project: "proj-1", At: "2026-09-14T12:05:00Z"}
	if err := WriteLedger(ctx, r, repoDir, newer); err != nil {
		t.Fatalf("WriteLedger (newer): %v", err)
	}

	got, ok, err := LastDeployOnRecord(ctx, r, repoDir, "proj-1", "appstage")
	if err != nil {
		t.Fatalf("LastDeployOnRecord: %v", err)
	}
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if got != newer {
		t.Errorf("LastDeployOnRecord = %+v, want the NEWER entry %+v", got, newer)
	}
	// The JSON round-trips exactly, and BOTH tags are listable.
	tagOut, err := exec.CommandContext(ctx, "git", "-C", repoDir, "tag", "-l", "zcp/deploy/*").CombinedOutput()
	if err != nil {
		t.Fatalf("git tag -l: %v\n%s", err, tagOut)
	}
	tags := strings.Fields(strings.TrimSpace(string(tagOut)))
	if len(tags) != 2 {
		t.Fatalf("zcp/deploy/* tags = %d, want 2: %v", len(tags), tags)
	}
	msgOut, err := exec.CommandContext(ctx, "git", "-C", repoDir, "for-each-ref", "--format=%(contents:subject)",
		"refs/tags/"+topology.DeployTagName("proj-1", "appstage", "av-2")).CombinedOutput()
	if err != nil {
		t.Fatalf("for-each-ref newer tag: %v\n%s", err, msgOut)
	}
	var decoded topology.LedgerEntry
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(msgOut))), &decoded); err != nil {
		t.Fatalf("decode newer tag message: %v (message: %s)", err, msgOut)
	}
	if decoded != newer {
		t.Errorf("newer tag message = %+v, want %+v", decoded, newer)
	}
}
