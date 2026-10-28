package ops

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/ops/origintest"
)

// ownEditShell runs zcp's container commands on this machine with /var/www
// moved under root, with git reading no global or system configuration.
type ownEditShell struct{ root string }

func (s ownEditShell) ExecSSH(ctx context.Context, _, command string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", strings.ReplaceAll(command, "'"+DevRepoRoot, "'"+s.root)) //nolint:gosec // G204: the commands under test, in a temp repository
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("%w: %s", err, out)
	}
	return out, nil
}

func (s ownEditShell) ExecSSHStdin(ctx context.Context, hostname, command string, _ []byte) ([]byte, error) {
	return s.ExecSSH(ctx, hostname, command)
}

func (ownEditShell) ExecSSHBackground(context.Context, string, string, time.Duration) ([]byte, error) {
	return nil, nil
}

// TestCommitOwnEdit_CommitsOnlyZcpsFilesOnTheWorkBranchAsZcp runs zcp's read
// and commit of its own edit against a real repository (docs/spec-mate.md
// §2.8a.2): the read names the branch and which of the asked-about files hold
// uncommitted changes; the commit holds exactly zcp's files — a new one
// included — signed as zcp, past a commit check that would refuse it, and
// leaves the person's own staged and unstaged work as it was.
func TestCommitOwnEdit_CommitsOnlyZcpsFilesOnTheWorkBranchAsZcp(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	origintest.Git(t, root, "init", "-q", "-b", "feat-x")
	write("zerops.yaml", "zerops:\n  - setup: dev\n")
	write("app.txt", "app\n")
	origintest.Git(t, root, "add", "-A")
	origintest.Git(t, root, "commit", "-q", "-m", "app")
	// The repository's commit check refuses every commit: zcp's commit holds
	// only its own lines and never runs it.
	write(".git/hooks/pre-commit", "#!/bin/sh\necho typecheck failed >&2\nexit 1\n")
	if err := os.Chmod(filepath.Join(root, ".git/hooks/pre-commit"), 0o755); err != nil {
		t.Fatal(err)
	}
	// The person's own work: an unstaged change and a staged new file.
	write("app.txt", "app, changed\n")
	write("staged.txt", "staged\n")
	origintest.Git(t, root, "add", "staged.txt")
	ssh := ownEditShell{root: root}

	base, err := ReadOwnEditBase(ctx, ssh, "appdev", "", []string{"zerops.yaml", "zerops.yml", ".zcp/dev-servers.json"})
	if err != nil {
		t.Fatalf("ReadOwnEditBase: %v", err)
	}
	if !base.Repo || base.Branch != "feat-x" || len(base.Uncommitted) != 0 {
		t.Fatalf("base = %+v, want the work branch with zcp's files clean", base)
	}

	write("zerops.yaml", "zerops:\n  - setup: dev\n    run:\n      start: zcp-devsup run\n")
	write(".zcp/dev-servers.json", `{"version":1,"servers":[]}`+"\n")
	sha, err := CommitOwnEdit(ctx, ssh, "appdev", "", []string{"zerops.yaml", ".zcp/dev-servers.json"}, "zcp: run setup dev under the dev-server supervisor", "Only zcp's lines.")
	if err != nil {
		t.Fatalf("CommitOwnEdit: %v", err)
	}
	if head := origintest.Git(t, root, "rev-parse", "--short", "HEAD"); sha == "" || !strings.HasPrefix(head, sha) {
		t.Errorf("sha = %q, HEAD = %q", sha, head)
	}
	files := strings.Fields(origintest.Git(t, root, "show", "--name-only", "--format=", "HEAD"))
	slices.Sort(files)
	if want := []string{".zcp/dev-servers.json", "zerops.yaml"}; !slices.Equal(files, want) {
		t.Errorf("zcp's commit holds %v, want %v", files, want)
	}
	if who := origintest.Git(t, root, "show", "-s", "--format=%an <%ae>|%cn <%ce>|%s", "HEAD"); who != "Zerops Agent <agent@zerops.io>|Zerops Agent <agent@zerops.io>|zcp: run setup dev under the dev-server supervisor" {
		t.Errorf("zcp's commit = %q, want signed as zcp with its subject", who)
	}
	status := origintest.Git(t, root, "status", "--porcelain")
	if !strings.Contains(status, "M app.txt") || !strings.Contains(status, "A  staged.txt") || strings.Contains(status, "zerops.yaml") {
		t.Errorf("status after zcp's commit:\n%s\nwant the person's work as it was and zcp's files committed", status)
	}

	write("zerops.yaml", "zerops:\n  - setup: dev\n    # the person's line\n")
	if base, err = ReadOwnEditBase(ctx, ssh, "appdev", "", []string{"zerops.yaml", ".zcp/dev-servers.json"}); err != nil || !slices.Equal(base.Uncommitted, []string{"zerops.yaml"}) {
		t.Errorf("base = %+v, %v; want zerops.yaml named uncommitted", base, err)
	}
	origintest.Git(t, root, "checkout", "-q", "--detach")
	if base, err = ReadOwnEditBase(ctx, ssh, "appdev", "", []string{"zerops.yaml"}); err != nil || !base.Repo || base.Branch != "" {
		t.Errorf("base on a detached HEAD = %+v, %v; want a repository with no branch", base, err)
	}
	if base, err = ReadOwnEditBase(ctx, ownEditShell{root: t.TempDir()}, "appdev", "", []string{"zerops.yaml"}); err != nil || base.Repo {
		t.Errorf("base of a folder without git = %+v, %v; want no repository", base, err)
	}
}
