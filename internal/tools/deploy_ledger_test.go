// Tests for: tools/deploy_ledger.go — writeDeployLedgerLocal/
// writeDeployLedgerSSH thread result.Dirty into the ledger entry
// (docs/spec-workflows.md §4.9).
package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/ops"
	git "github.com/zeropsio/zcp/internal/ops/git"
)

// initGitRepoForLedgerTest creates a real git repo at a temp dir with one
// commit, returning (dir, full commit sha).
func initGitRepoForLedgerTest(t *testing.T) (dir, sha string) {
	t.Helper()
	dir = t.TempDir()
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
	return dir, strings.TrimSpace(string(out))
}

func TestWriteDeployLedgerLocal_DirtyResult_TagRecordsDirtyTrue(t *testing.T) {
	if testing.Short() {
		t.Skip("shells out to the real git binary")
	}
	dir, sha := initGitRepoForLedgerTest(t)

	result := &ops.DeployResult{
		SHA:           sha,
		AppVersionID:  "av-1",
		TargetService: "app",
		Dirty:         true,
	}
	writeDeployLedgerLocal(context.Background(), dir, "proj-1", result)
	if len(result.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", result.Warnings)
	}

	entry, ok, err := git.LastDeployOnRecord(context.Background(), git.LocalRunner{}, dir, "proj-1", "app")
	if err != nil {
		t.Fatalf("LastDeployOnRecord: %v", err)
	}
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if !entry.Dirty {
		t.Error("entry.Dirty = false, want true")
	}
}

func TestWriteDeployLedgerLocal_CleanResult_TagOmitsDirty(t *testing.T) {
	if testing.Short() {
		t.Skip("shells out to the real git binary")
	}
	dir, sha := initGitRepoForLedgerTest(t)

	result := &ops.DeployResult{
		SHA:           sha,
		AppVersionID:  "av-1",
		TargetService: "app",
		Dirty:         false,
	}
	writeDeployLedgerLocal(context.Background(), dir, "proj-1", result)

	entry, ok, err := git.LastDeployOnRecord(context.Background(), git.LocalRunner{}, dir, "proj-1", "app")
	if err != nil {
		t.Fatalf("LastDeployOnRecord: %v", err)
	}
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if entry.Dirty {
		t.Error("entry.Dirty = true, want false")
	}
}

// fakeLedgerSSHDeployer records every ExecSSH command it receives, for
// asserting the ledger tag write over SSH carries "dirty":true.
type fakeLedgerSSHDeployer struct {
	calls []string
}

func (f *fakeLedgerSSHDeployer) ExecSSH(_ context.Context, _, command string) ([]byte, error) {
	f.calls = append(f.calls, command)
	return []byte("ok"), nil
}

func (f *fakeLedgerSSHDeployer) ExecSSHBackground(_ context.Context, _, _ string, _ time.Duration) ([]byte, error) {
	return []byte("ok"), nil
}

func TestWriteDeployLedgerSSH_DirtyResult_TagCommandCarriesDirtyTrue(t *testing.T) {
	ssh := &fakeLedgerSSHDeployer{}
	result := &ops.DeployResult{
		SHA:           "abc123",
		AppVersionID:  "av-1",
		TargetService: "app",
		SourceService: "app",
		Dirty:         true,
	}
	writeDeployLedgerSSH(context.Background(), ssh, "", "proj-1", result)
	if len(result.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", result.Warnings)
	}
	if len(ssh.calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(ssh.calls))
	}
	if !strings.Contains(ssh.calls[0], `"dirty":true`) {
		t.Errorf("tag command = %q, want the JSON message to carry \"dirty\":true", ssh.calls[0])
	}
	if !strings.Contains(ssh.calls[0], "zcp/deploy/proj-1/app/av-1") {
		t.Errorf("tag command = %q, want the zcp/deploy/proj-1/app/av-1 tag name", ssh.calls[0])
	}
}

func TestWriteDeployLedgerSSH_CleanResult_TagCommandOmitsDirty(t *testing.T) {
	ssh := &fakeLedgerSSHDeployer{}
	result := &ops.DeployResult{
		SHA:           "abc123",
		AppVersionID:  "av-1",
		TargetService: "app",
		SourceService: "app",
		Dirty:         false,
	}
	writeDeployLedgerSSH(context.Background(), ssh, "", "proj-1", result)
	if len(ssh.calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(ssh.calls))
	}
	if strings.Contains(ssh.calls[0], `"dirty"`) {
		t.Errorf("tag command = %q, want no \"dirty\" key when Dirty is false (omitempty)", ssh.calls[0])
	}
}
