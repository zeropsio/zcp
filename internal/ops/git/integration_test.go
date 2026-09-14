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

	"github.com/zeropsio/zcp/internal/topology"
)

// isolatedHomeRunner is a Runner that isolates HOME (and both git config
// scopes) so a test proves WriteLedger's `git commit-tree` supplies its OWN
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

	gotSHA, err := ReadEnvRef(ctx, r, repoDir, "appstage")
	if err != nil {
		t.Fatalf("ReadEnvRef: %v", err)
	}
	if gotSHA != resolved {
		t.Fatalf("ReadEnvRef = %s, want %s", gotSHA, resolved)
	}

	// The ledger's history is read directly via `git log
	// refs/zcp/deploy/*` (no Go-side reader — nothing calls one): confirm
	// exactly one entry exists and its commit message round-trips the
	// exact JSON WriteLedger wrote.
	refOut, err := exec.CommandContext(ctx, "git", "-C", repoDir, "for-each-ref",
		"--format=%(objectname)", topology.DeployRefPrefix).CombinedOutput()
	if err != nil {
		t.Fatalf("for-each-ref %s: %v\n%s", topology.DeployRefPrefix, err, refOut)
	}
	refs := strings.Fields(strings.TrimSpace(string(refOut)))
	if len(refs) != 1 {
		t.Fatalf("refs/zcp/deploy/* entries = %d, want 1: %v", len(refs), refs)
	}
	msgOut, err := exec.CommandContext(ctx, "git", "-C", repoDir, "log", "-1", "--format=%B", refs[0]).CombinedOutput()
	if err != nil {
		t.Fatalf("log -1 --format=%%B %s: %v\n%s", refs[0], err, msgOut)
	}
	var gotEntry topology.LedgerEntry
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(msgOut))), &gotEntry); err != nil {
		t.Fatalf("decode ledger message: %v (message: %s)", err, msgOut)
	}
	if gotEntry != entry {
		t.Fatalf("ledger message = %+v, want %+v", gotEntry, entry)
	}

	// Ledger refs are on a NEVER-checked-out branch — the repo's own HEAD
	// (and working tree) must stay untouched.
	headOut, err := exec.CommandContext(context.Background(), "git", "-C", repoDir, "rev-parse", "HEAD").CombinedOutput()
	if err != nil {
		t.Fatalf("rev-parse HEAD after ledger write: %v\n%s", err, headOut)
	}
	if strings.TrimSpace(string(headOut)) != sha {
		t.Fatalf("HEAD moved after WriteLedger: got %s, want %s", strings.TrimSpace(string(headOut)), sha)
	}
}

func TestReadEnvRef_UnbornRepo_ReturnsEmptyNoError(t *testing.T) {
	if testing.Short() {
		t.Skip("shells out to the real git binary")
	}
	ctx := context.Background()
	repoDir := t.TempDir()
	if out, err := exec.CommandContext(context.Background(), "git", "init", "-q", repoDir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	got, err := ReadEnvRef(ctx, LocalRunner{}, repoDir, "appstage")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("got = %q, want empty", got)
	}
}

// TestWriteLedger_NoAmbientIdentity_StillSucceeds proves WriteLedger's
// `git commit-tree` supplies its own identity: on a buildFromGit-provisioned
// container there is no ~/.gitconfig and no GIT_AUTHOR_*/GIT_COMMITTER_* env
// (unlike a self-deploy container, which InitServiceGit seeds), so without
// an explicit identity commit-tree fails "unable to auto-detect email
// address" and the ledger write is silently lost. isolatedHomeRunner
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

	got, err := ReadEnvRef(ctx, r, repoDir, "appstage")
	if err != nil {
		t.Fatalf("ReadEnvRef: %v", err)
	}
	if got != sha {
		t.Errorf("ReadEnvRef = %s, want %s", got, sha)
	}
}
