// Tests for: ops/lease_checkout.go — a branch new to origin is cut from the
// group's base commit, and the checkout answers the commit it cut from
// (docs/spec-workflows.md §8 DP-6, DP-17).
package ops

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runCheckoutFrom runs the composed command with base in workDir the way ssh
// would hand it to the container's shell, and returns its output and error.
func runCheckoutFrom(t *testing.T, workDir, repo, branch, base string) (string, error) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "sh", "-c", leaseCheckoutCommand(workDir, repo, branch, base, "")) //nolint:gosec // running the composed command is the test
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// advanceMain puts one more commit on origin's main and returns it.
func advanceMain(t *testing.T, origin string) string {
	t.Helper()
	work := t.TempDir()
	gitIn(t, work, "clone", "-q", "-b", "main", origin, ".")
	if err := os.WriteFile(filepath.Join(work, "later.txt"), []byte("l"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, work, "add", "-A")
	gitIn(t, work, "commit", "-q", "-m", "later")
	gitIn(t, work, "push", "-q", "origin", "main")
	return gitIn(t, work, "rev-parse", "HEAD")
}

// TestLeaseCheckout_NewBranch_CutFromTheGroupsBase: a branch new to origin is
// cut from the base the group recorded, even after main moved on; without a
// base it is cut from origin/main. Either way the checkout answers the commit
// it cut from, and a branch already on origin is tracked and answers none.
func TestLeaseCheckout_NewBranch_CutFromTheGroupsBase(t *testing.T) {
	t.Parallel()
	origin, mainSHA, featSHA := originWithBranches(t)
	laterSHA := advanceMain(t, origin)

	fromBase := imageLeftovers(t)
	out, err := runCheckoutFrom(t, fromBase, origin, "brand-new", mainSHA)
	if err != nil {
		t.Fatalf("checkout from the base: %v\n%s", err, out)
	}
	if got := gitIn(t, fromBase, "rev-parse", "HEAD"); got != mainSHA {
		t.Errorf("HEAD = %s, want the group's base %s (main has moved on to %s)", got, mainSHA, laterSHA)
	}
	if base, ok := checkoutBase(out); !ok || base != mainSHA {
		t.Errorf("answered base = %q (%v), want %s:\n%s", base, ok, mainSHA, out)
	}

	fromMain := imageLeftovers(t)
	out, err = runCheckoutFrom(t, fromMain, origin, "brand-new", "")
	if err != nil {
		t.Fatalf("checkout without a base: %v\n%s", err, out)
	}
	if got := gitIn(t, fromMain, "rev-parse", "HEAD"); got != laterSHA {
		t.Errorf("HEAD = %s, want origin/main %s", got, laterSHA)
	}
	if base, ok := checkoutBase(out); !ok || base != laterSHA {
		t.Errorf("answered base = %q (%v), want origin/main %s:\n%s", base, ok, laterSHA, out)
	}

	tracked := imageLeftovers(t)
	out, err = runCheckoutFrom(t, tracked, origin, "feat", mainSHA)
	if err != nil {
		t.Fatalf("checkout of a branch on origin: %v\n%s", err, out)
	}
	if got := gitIn(t, tracked, "rev-parse", "HEAD"); got != featSHA {
		t.Errorf("HEAD = %s, want origin/feat %s (a branch on origin is tracked, whatever the base)", got, featSHA)
	}
	if base, ok := checkoutBase(out); ok {
		t.Errorf("a tracked branch answered base %q", base)
	}
}

// A base origin does not have (main was rewritten since) fails the checkout,
// saying so, rather than cutting the branch from another commit than the
// group's.
func TestLeaseCheckout_BaseNotOnOrigin_FailsNamingIt(t *testing.T) {
	t.Parallel()
	origin, _, _ := originWithBranches(t)
	const gone = "0123456789abcdef0123456789abcdef01234567"
	out, err := runCheckoutFrom(t, imageLeftovers(t), origin, "brand-new", gone)
	if err == nil {
		t.Fatalf("a checkout from a base origin lacks succeeded:\n%s", out)
	}
	if !strings.Contains(out, gone) {
		t.Errorf("output does not name the base:\n%s", out)
	}
}

// The base reaches the command only as a full commit id, shell-quoted.
func TestLeaseCheckoutCommand_BaseIsACommitID(t *testing.T) {
	t.Parallel()
	const base = "9f8e7d6c5b4a39281706f5e4d3c2b1a098765432"
	cmd := leaseCheckoutCommand("/var/www", "https://github.com/acme/app", "feat", base, "")
	if !strings.Contains(cmd, shellQuote(base)) {
		t.Errorf("command does not carry the quoted base:\n%s", cmd)
	}
	if strings.Contains(cmd, "--no-track origin/main") {
		t.Errorf("a command with a base still cuts from origin/main:\n%s", cmd)
	}
	if got := leaseCheckoutCommand("/var/www", "https://github.com/acme/app", "feat", "main; touch x", ""); strings.Contains(got, "touch x") {
		t.Errorf("a base that is no commit id reached the command:\n%s", got)
	}
}
