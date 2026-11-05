package ops

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/ops/origintest"
)

// ownEditShell runs zcp's container commands on this machine with /var/www
// moved under root, with git reading no global or system configuration.
type ownEditShell struct {
	root, path     string
	processContext context.Context //nolint:containedctx // test controls the subprocess lifetime
}

//nolint:contextcheck // a controlled test may supply the process lifetime
func (s ownEditShell) ExecSSH(ctx context.Context, _, command string) ([]byte, error) {
	if s.processContext != nil {
		ctx = s.processContext
	}
	cmd := exec.CommandContext(ctx, "sh", "-c", strings.ReplaceAll(command, "'"+DevRepoRoot, "'"+s.root)) //nolint:gosec // G204: the commands under test, in a temp repository
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
	if s.path != "" {
		cmd.Env = append(cmd.Env, "PATH="+s.path+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
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
// uncommitted changes; the commit runs the repository's commit checks — the
// person's hooks are their policy — and a check that refuses leaves nothing
// committed, with its reason; once the checks pass the commit holds exactly
// zcp's files — a new one included — signed as zcp, and leaves the person's
// own staged and unstaged work as it was.
func TestCommitOwnEdit_CommitsOnlyZcpsFilesOnTheWorkBranchAsZcp(t *testing.T) {
	// non-parallel: isolate fixture identities from the caller's Git hook environment.
	for _, name := range []string{"GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL", "GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL"} {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}
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
	hook := func(body string) {
		t.Helper()
		write(".git/hooks/pre-commit", "#!/bin/sh\n"+body)
		if err := os.Chmod(filepath.Join(root, ".git/hooks/pre-commit"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	origintest.Git(t, root, "init", "-q", "-b", "feat-x")
	write("zerops.yaml", "zerops:\n  - setup: dev\n")
	write("app.txt", "app\n")
	origintest.Git(t, root, "add", "-A")
	origintest.Git(t, root, "commit", "-q", "-m", "app")
	first := origintest.Git(t, root, "rev-parse", "HEAD")
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
	paths := []string{"zerops.yaml", ".zcp/dev-servers.json"}
	const subject = "zcp: run setup dev under the dev-server supervisor"

	// A commit check that refuses: nothing is committed, and its reason is
	// the answer.
	hook("echo 'typecheck: 3 errors in app/src/main.ts' >&2\nexit 1\n")
	if _, err := CommitOwnEdit(ctx, ssh, "appdev", "", paths, subject, "Only zcp's lines."); err == nil || !strings.Contains(err.Error(), "typecheck: 3 errors in app/src/main.ts") {
		t.Fatalf("CommitOwnEdit past a refusing commit check = %v, want its reason", err)
	}
	if head := origintest.Git(t, root, "rev-parse", "HEAD"); head != first {
		t.Fatalf("a refusing commit check let a commit through: HEAD %s, was %s", head, first)
	}

	// Checks that pass run, and the commit goes through.
	hook("touch .git/zcp-hook-ran\n")
	sha, err := CommitOwnEdit(ctx, ssh, "appdev", "", paths, subject, "Only zcp's lines.")
	if err != nil {
		t.Fatalf("CommitOwnEdit: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".git/zcp-hook-ran")); err != nil {
		t.Errorf("the repository's commit check did not run for zcp's commit: %v", err)
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

// TestCommitOwnEdit_CommitChecksPastTheBudgetCommitNothing: the container
// bounds zcp's commit, the repository's commit checks included; checks that
// run past the budget end there, nothing is committed, and the answer says
// they ran past it.
func TestCommitOwnEdit_CommitChecksPastTheBudgetCommitNothing(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	bin := t.TempDir()
	receipt, hookStarted := filepath.Join(bin, "timeout-started"), filepath.Join(bin, "hook-started")
	expire, killed, hold := filepath.Join(bin, "expire"), filepath.Join(bin, "killed"), filepath.Join(bin, "hold")
	if out, err := exec.CommandContext(t.Context(), "mkfifo", receipt, hookStarted, expire, killed, hold).CombinedOutput(); err != nil {
		t.Fatalf("mkfifo: %v: %s", err, out)
	}
	// The timeout wrapper starts real git, holds the deadline until the hook is
	// running, then delivers its timeout and reports the exact requested bound.
	stub := "#!/bin/sh\nbound=$1\nshift\n\"$@\" &\npid=$!\nprintf '%s %s\\n' \"$bound\" \"$pid\" > " + shellQuote(receipt) +
		"\nread expiry < " + shellQuote(expire) + "\nkill \"$pid\" 2>/dev/null || :\nprintf killed > " + shellQuote(killed) + "\nwait \"$pid\" 2>/dev/null || :\nexit 124\n"
	if err := os.WriteFile(filepath.Join(bin, "timeout"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	origintest.Git(t, root, "init", "-q", "-b", "feat-x")
	if err := os.WriteFile(filepath.Join(root, "zerops.yaml"), []byte("zerops:\n  - setup: dev\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	origintest.Git(t, root, "add", "-A")
	origintest.Git(t, root, "commit", "-q", "-m", "app")
	first := origintest.Git(t, root, "rev-parse", "HEAD")
	hook := "#!/bin/sh\nprintf '%s\\n' \"$$\" > " + shellQuote(hookStarted) + "\nread release < " + shellQuote(hold) + "\n"
	if err := os.WriteFile(filepath.Join(root, ".git/hooks/pre-commit"), []byte(hook), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "zerops.yaml"), []byte("zerops:\n  - setup: dev\n    run:\n      start: zcp-devsup run\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := make(chan error, 1)
	go func() {
		_, err := commitOwnEdit(t.Context(), ownEditShell{root: root, path: bin, processContext: t.Context()}, "appdev", "", []string{"zerops.yaml"}, "zcp: subject", "body", time.Second)
		result <- err
	}()
	readFIFO := func(path string) string {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return strings.TrimSpace(string(data))
	}
	start := strings.Fields(readFIFO(receipt))
	if len(start) != 2 || start[0] != "1" {
		t.Fatalf("timeout request = %v, want a one-second budget and git's pid", start)
	}
	gitPID, err := strconv.Atoi(start[1])
	if err != nil {
		t.Fatal(err)
	}
	var hookPID int
	t.Cleanup(func() {
		for _, pid := range []int{gitPID, hookPID} {
			if pid > 0 {
				if p, err := os.FindProcess(pid); err == nil {
					_ = p.Kill()
				}
			}
		}
	})
	hookPID, err = strconv.Atoi(readFIFO(hookStarted))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(expire, []byte("expire\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := readFIFO(killed); got != "killed" {
		t.Fatalf("timeout receipt = %q", got)
	}
	// The real timeout signals its command's process group; the controlled
	// wrapper reports killing git so the test can deliver the same signal to
	// its held hook before collecting the result.
	if p, err := os.FindProcess(hookPID); err == nil {
		_ = p.Kill()
	}
	err = <-result
	gitPID, hookPID = 0, 0
	if err == nil || !strings.Contains(err.Error(), "the repository's commit checks ran past 1s") {
		t.Fatalf("commitOwnEdit past its budget = %v, want the checks named as running past it", err)
	}
	if head := origintest.Git(t, root, "rev-parse", "HEAD"); head != first {
		t.Errorf("checks past the budget let a commit through: HEAD %s, was %s", head, first)
	}
}
