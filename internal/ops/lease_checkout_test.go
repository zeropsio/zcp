// Tests for: ops/lease_checkout.go — a lease's /var/www is put on its branch
// at acquire, and a release refuses to lose work (DP-4, DP-6).
package ops

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// leaseSSH is the leased container as zcp reaches it: a handler answers each
// command run over ssh to an alias.
type leaseSSH struct {
	mu    sync.Mutex
	fn    func(host, command string) ([]byte, error)
	calls []sshCall
}

func (s *leaseSSH) ExecSSH(_ context.Context, host, command string) ([]byte, error) {
	s.mu.Lock()
	s.calls = append(s.calls, sshCall{hostname: host, command: command})
	fn := s.fn
	s.mu.Unlock()
	if fn == nil {
		return nil, nil
	}
	return fn(host, command)
}

// ExecSSHStdin answers one command as ExecSSH does; the input is not read.
func (s *leaseSSH) ExecSSHStdin(ctx context.Context, host, command string, _ []byte) ([]byte, error) {
	return s.ExecSSH(ctx, host, command)
}

func (s *leaseSSH) ExecSSHBackground(ctx context.Context, host, command string, _ time.Duration) ([]byte, error) {
	return s.ExecSSH(ctx, host, command)
}

func (s *leaseSSH) commands() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.calls))
	for _, c := range s.calls {
		out = append(out, c.command)
	}
	return out
}

// checkouts are the checkout commands, in order (an acquire also ensures the
// lease's supervisor).
func (s *leaseSSH) checkouts() []sshCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []sshCall
	for _, c := range s.calls {
		if strings.Contains(c.command, "fetch -q --prune origin") {
			out = append(out, c)
		}
	}
	return out
}

func (s *leaseSSH) setHandler(fn func(host, command string) ([]byte, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fn = fn
}

func (s *leaseSSH) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = nil
}

// sshFailure is what SystemSSHDeployer returns when the remote command fails.
func sshFailure(output string, err error) error {
	return &platform.SSHExecError{Hostname: "devpool-3", Output: output, Err: err}
}

func exitError(t *testing.T, code int) error {
	t.Helper()
	err := exec.CommandContext(context.Background(), "sh", "-c", "exit "+strconv.Itoa(code)).Run() //nolint:gosec // the exit status is a test constant
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("sh exit %d gave %v, want an *exec.ExitError", code, err)
	}
	return err
}

func readPoolDefault(t *testing.T, stateDir, pool string) (string, bool) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(stateDir, "leases", "pools", pool+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return "", false
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(raw), true
}

func TestLeaseAcquire_NoRepoNoDefault_RefusedBeforeLeasing(t *testing.T) {
	t.Parallel()
	fx := newLeaseFixture(t)
	fx.swarm.acquired = acquiredAs(3)
	fx.deps.SSH = &leaseSSH{}

	_, err := LeaseAcquire(context.Background(), fx.deps, testPoolHost, "", "feature")
	var pe *platform.PlatformError
	if !errors.As(err, &pe) || pe.Code != platform.ErrInvalidParameter {
		t.Fatalf("err = %v, want an INVALID_PARAMETER refusal", err)
	}
	if !strings.Contains(pe.Message, "repo") {
		t.Errorf("message %q should ask for repo", pe.Message)
	}
	if len(fx.swarm.acquireReqs) != 0 {
		t.Errorf("the orchestrator was asked to acquire %d time(s); a refusal must take no container", len(fx.swarm.acquireReqs))
	}
	if entries, _ := os.ReadDir(filepath.Join(fx.stateDir, "leases")); len(entries) != 0 {
		t.Errorf("a refused acquire left %d entr(ies) in the lease directory", len(entries))
	}
}

func TestLeaseAcquire_StoresPoolDefaultRepo(t *testing.T) {
	t.Parallel()
	fx := newLeaseFixture(t)
	ssh := &leaseSSH{}
	fx.deps.SSH = ssh
	fx.swarm.acquired = acquiredAs(3)
	ctx := context.Background()

	if _, err := LeaseAcquire(ctx, fx.deps, testPoolHost, testRepo, "feature"); err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	stored, ok := readPoolDefault(t, fx.stateDir, testPoolHost)
	if !ok {
		t.Fatal("the first acquire with a repo must store it as the pool's default")
	}
	// The default is a clone URL a later acquire clones: kept as given
	// (testRepo ends in .git), never its canonical form.
	if !strings.Contains(stored, `"`+testRepo+`"`) {
		t.Errorf("stored default = %s, want the URL as given, %s", stored, testRepo)
	}
	if info, err := os.Stat(filepath.Join(fx.stateDir, "leases", "pools", testPoolHost+".json")); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("default record stat = %v, %v; want mode 600", info, err)
	}

	// A later acquire without repo uses the default; one with another repo
	// uses that repo and leaves the default alone.
	fx.swarm.acquired = acquiredAs(4)
	fx.swarm.acquired.Container.ID = "c-def"
	if _, err := LeaseAcquire(ctx, fx.deps, testPoolHost, "", "other"); err != nil {
		t.Fatalf("second acquire without repo: %v", err)
	}
	fx.swarm.acquired = acquiredAs(5)
	fx.swarm.acquired.Container.ID = "c-ghi"
	if _, err := LeaseAcquire(ctx, fx.deps, testPoolHost, "https://github.com/acme/other", "third"); err != nil {
		t.Fatalf("third acquire: %v", err)
	}
	cmds := ssh.checkouts()
	if len(cmds) != 3 {
		t.Fatalf("checkouts = %d, want one per acquire", len(cmds))
	}
	if !strings.Contains(cmds[1].command, shellQuote(testRepo)) {
		t.Errorf("second checkout does not use the pool default %s:\n%s", testRepo, cmds[1].command)
	}
	if !strings.Contains(cmds[2].command, shellQuote("https://github.com/acme/other")) {
		t.Errorf("third checkout does not use the repo it was given:\n%s", cmds[2].command)
	}
	if again, _ := readPoolDefault(t, fx.stateDir, testPoolHost); again != stored {
		t.Errorf("an acquire with another repo changed the default:\n%s\nto\n%s", stored, again)
	}
}

// TestLeaseAcquire_RepoAsGiven_ClonedVerbatim: a plain git server answers a
// repository only at the address it serves it under — `…/repo.git` clones,
// `…/repo` is "repository not found" (live Mate run) — so the checkout clones
// the URL exactly as the caller gave it, an acquire that takes the pool's
// default clones that same URL, and a failed checkout names it. The
// canonical form is identity only: the lease record keeps it (DP-18).
func TestLeaseAcquire_RepoAsGiven_ClonedVerbatim(t *testing.T) {
	t.Parallel()
	const canonical = "https://git.example.com/team/repo"
	tests := []struct{ name, given string }{
		{".git suffix", canonical + ".git"},
		{"trailing slash", canonical + "/"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fx := newLeaseFixture(t)
			ssh := &leaseSSH{}
			fx.deps.SSH = ssh
			fx.swarm.acquired = acquiredAs(3)
			ctx := context.Background()

			if _, err := LeaseAcquire(ctx, fx.deps, testPoolHost, tt.given, "feature"); err != nil {
				t.Fatalf("acquire with repo: %v", err)
			}
			fx.swarm.acquired = acquiredAs(4)
			fx.swarm.acquired.Container.ID = "c-def"
			if _, err := LeaseAcquire(ctx, fx.deps, testPoolHost, "", "other"); err != nil {
				t.Fatalf("acquire with the pool's default: %v", err)
			}
			cmds := ssh.checkouts()
			if len(cmds) != 2 {
				t.Fatalf("checkouts = %d, want one per acquire", len(cmds))
			}
			for i, c := range cmds {
				if !strings.Contains(c.command, "git remote add origin "+shellQuote(tt.given)) ||
					!strings.Contains(c.command, "git remote set-url origin "+shellQuote(tt.given)) {
					t.Errorf("checkout %d does not clone the URL as given (%s):\n%s", i+1, tt.given, c.command)
				}
				if strings.Contains(c.command, shellQuote(canonical)) {
					t.Errorf("checkout %d clones the canonical form %s instead of %s:\n%s", i+1, canonical, tt.given, c.command)
				}
			}
			rec, err := loadLeaseRecord(fx.stateDir, "devpool-3")
			if err != nil {
				t.Fatal(err)
			}
			if rec.Repo != canonical {
				t.Errorf("record repo = %q, want the canonical identity %q", rec.Repo, canonical)
			}

			// A failed checkout names the URL it tried, as given.
			fail := newLeaseFixture(t)
			fail.swarm.acquired = acquiredAs(5)
			fail.deps.SSH = &leaseSSH{fn: func(string, string) ([]byte, error) {
				out := "fatal: repository '" + canonical + "/' not found"
				return []byte(out), sshFailure(out, exitError(t, 128))
			}}
			_, err = LeaseAcquire(ctx, fail.deps, testPoolHost, tt.given, "feature")
			var pe *platform.PlatformError
			if !errors.As(err, &pe) || !strings.Contains(pe.Message, " of "+tt.given) {
				t.Errorf("failed checkout = %v, want its message to name %s", err, tt.given)
			}
		})
	}
}

func TestLeaseAcquire_BadRepoOrBranch_RefusedBeforeLeasing(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, repo, branch string }{
		{"userinfo carries a credential", "https://oauth2:ghp_secret@github.com/acme/app", "main"},
		{"option-shaped url", "--upload-pack=touch /tmp/x", "main"},
		{"local path", "/srv/repo", "main"},
		{"ext transport", "ext::sh -c touch% /tmp/x", "main"},
		{"scp form", "git@github.com:acme/app.git", "main"},
		{"url with a space", "https://github.com/acme/app x", "main"},
		{"plain http would send GIT_TOKEN in clear", "http://github.com/acme/app", "main"},
		{"option-shaped branch", testRepo, "--orphan"},
		{"branch with a space", testRepo, "a b"},
		{"branch with a quote", testRepo, "a'b"},
		{"branch with dotdot", testRepo, "a..b"},
		{"branch with a semicolon", testRepo, "a;touch"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fx := newLeaseFixture(t)
			fx.swarm.acquired = acquiredAs(3)
			fx.deps.SSH = &leaseSSH{}
			_, err := LeaseAcquire(context.Background(), fx.deps, testPoolHost, tt.repo, tt.branch)
			var pe *platform.PlatformError
			if !errors.As(err, &pe) || pe.Code != platform.ErrInvalidParameter {
				t.Fatalf("err = %v, want INVALID_PARAMETER", err)
			}
			if strings.Contains(err.Error(), "ghp_secret") {
				t.Errorf("the refusal echoes the credential: %v", err)
			}
			if len(fx.swarm.acquireReqs) != 0 {
				t.Errorf("acquired %d time(s) for a refused input", len(fx.swarm.acquireReqs))
			}
		})
	}
}

// gitIn runs git in dir with a fixed identity (the test's commits only).
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// originWithBranches is a bare repo with main (README, app.txt) and feat
// (main + feat.txt), the oracle for what a checkout must leave behind.
func originWithBranches(t *testing.T) (origin, mainSHA, featSHA string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	origin = filepath.Join(t.TempDir(), "origin.git")
	work := t.TempDir()
	gitIn(t, work, "init", "-q", "-b", "main")
	for name, body := range map[string]string{"README": "r", "app.txt": "a"} {
		if err := os.WriteFile(filepath.Join(work, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitIn(t, work, "add", "-A")
	gitIn(t, work, "commit", "-q", "-m", "main")
	mainSHA = gitIn(t, work, "rev-parse", "HEAD")
	gitIn(t, work, "checkout", "-q", "-b", "feat")
	if err := os.WriteFile(filepath.Join(work, "feat.txt"), []byte("f"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, work, "add", "-A")
	gitIn(t, work, "commit", "-q", "-m", "feat")
	featSHA = gitIn(t, work, "rev-parse", "HEAD")
	gitIn(t, filepath.Dir(origin), "init", "-q", "--bare", "-b", "main", origin)
	gitIn(t, work, "push", "-q", origin, "main", "feat")
	return origin, mainSHA, featSHA
}

// runCheckout runs the composed command in workDir the way ssh would hand it
// to the container's shell.
func runCheckout(t *testing.T, workDir, repo, branch string) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "sh", "-c", leaseCheckoutCommand(workDir, repo, branch, "", "")) //nolint:gosec // running the composed command is the test
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("checkout command failed: %v\n%s\ncommand: %s", err, out, leaseCheckoutCommand(workDir, repo, branch, "", ""))
	}
}

// imageLeftovers is what a never-used container's /var/www holds: files of the
// pool image, no .git.
func imageLeftovers(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range map[string]string{"stale.txt": "image", "app.txt": "from the image"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "stale-dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stale-dir", "x"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLeaseCheckout_ExistingBranch_TracksIt(t *testing.T) {
	t.Parallel()
	origin, _, featSHA := originWithBranches(t)
	wd := imageLeftovers(t)

	runCheckout(t, wd, origin, "feat")
	runCheckout(t, wd, origin, "feat") // idempotent: the container may be re-prepared

	if got := gitIn(t, wd, "rev-parse", "--abbrev-ref", "HEAD"); got != "feat" {
		t.Errorf("branch = %q, want feat", got)
	}
	if got := gitIn(t, wd, "rev-parse", "HEAD"); got != featSHA {
		t.Errorf("HEAD = %s, want origin/feat %s", got, featSHA)
	}
	if got := gitIn(t, wd, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}"); got != "origin/feat" {
		t.Errorf("upstream = %q, want origin/feat", got)
	}
	if got := gitIn(t, wd, "remote", "get-url", "origin"); got != origin {
		t.Errorf("origin = %q, want %q", got, origin)
	}
	for _, gone := range []string{"stale.txt", "stale-dir"} {
		if _, err := os.Stat(filepath.Join(wd, gone)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s left by the image survives the checkout (%v)", gone, err)
		}
	}
	if body, err := os.ReadFile(filepath.Join(wd, "app.txt")); err != nil || string(body) != "a" {
		t.Errorf("app.txt = %q, %v; want the branch's content (a checkout -f overwrites the image's file)", body, err)
	}
	if status := gitIn(t, wd, "status", "--porcelain"); status != "" {
		t.Errorf("tree is dirty after the checkout:\n%s", status)
	}
}

func TestLeaseCheckout_NewBranch_CutFromMain(t *testing.T) {
	t.Parallel()
	origin, mainSHA, _ := originWithBranches(t)
	wd := imageLeftovers(t)
	// An ignored file the image left stays: clean never uses -x.
	if err := os.WriteFile(filepath.Join(wd, ".gitignore"), []byte("cache.bin\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wd, "cache.bin"), []byte("c"), 0o644); err != nil {
		t.Fatal(err)
	}

	runCheckout(t, wd, origin, "brand-new")

	if got := gitIn(t, wd, "rev-parse", "--abbrev-ref", "HEAD"); got != "brand-new" {
		t.Errorf("branch = %q, want brand-new", got)
	}
	if got := gitIn(t, wd, "rev-parse", "HEAD"); got != mainSHA {
		t.Errorf("HEAD = %s, want main %s (DP-6: a new branch is cut from main)", got, mainSHA)
	}
	cmd := exec.CommandContext(context.Background(), "git", "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
	cmd.Dir = wd
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Errorf("the new branch tracks %q; it must not track main (a push would target main)", strings.TrimSpace(string(out)))
	}
	if _, err := os.Stat(filepath.Join(wd, "stale.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("stale.txt survives (%v)", err)
	}
	if _, err := os.Stat(filepath.Join(wd, "feat.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("feat.txt is on a branch cut from main (%v)", err)
	}
	if _, err := os.Stat(filepath.Join(wd, "cache.bin")); err != nil {
		t.Errorf("an ignored file was removed although clean never takes -x: %v", err)
	}
}

func TestLeaseCheckout_CredentialNeverInCommand(t *testing.T) {
	// Non-parallel: t.Setenv plants a GIT_TOKEN in zcp's own env, which must
	// never reach the command.
	t.Setenv("GIT_TOKEN", "ghp_planted_in_the_zcp_env")
	fx := newLeaseFixture(t)
	ssh := &leaseSSH{}
	fx.deps.SSH = ssh
	fx.swarm.acquired = acquiredAs(3)

	if _, err := LeaseAcquire(context.Background(), fx.deps, testPoolHost, testRepo, "feat'ure; touch x"); err == nil {
		t.Fatal("a branch with shell metacharacters must be refused")
	}
	if _, err := LeaseAcquire(context.Background(), fx.deps, testPoolHost, testRepo, "feature/x-1.2"); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	cmds := ssh.checkouts()
	if len(cmds) != 1 {
		t.Fatalf("checkouts = %d, want 1", len(cmds))
	}
	cmd := cmds[0].command
	if cmds[0].hostname != "devpool-3" {
		t.Errorf("command ran on %q, want the alias devpool-3", cmds[0].hostname)
	}
	for _, want := range []string{
		"-c credential.helper= -c credential.helper=" + shellQuote(leaseCredentialHelperShell), "GIT_TERMINAL_PROMPT=0",
		shellQuote("feature/x-1.2"), shellQuote(testRepo), "cd '/var/www'", "git clean -fd",
	} {
		if !strings.Contains(cmd, want) {
			t.Errorf("command lacks %q:\n%s", want, cmd)
		}
	}
	for _, banned := range []string{"ghp_", "planted", "password=ghp", "https://oauth2", " -x", "-fdx", "-xfd"} {
		if strings.Contains(cmd, banned) {
			t.Errorf("command contains %q:\n%s", banned, cmd)
		}
	}
	// The only credential references are the helpers reading the session env.
	inHelpers := strings.Count(cmd, gitCredentialHelperArgs())*strings.Count(gitCredentialHelperArgs(), "GIT_TOKEN") +
		strings.Count(cmd, shellQuote(leaseCredentialHelperShell))*strings.Count(leaseCredentialHelperShell, "GIT_TOKEN")
	if n := strings.Count(cmd, "GIT_TOKEN"); n != inHelpers {
		t.Errorf("GIT_TOKEN appears %d times, want only inside credential helpers:\n%s", n, cmd)
	}
}

func TestLeaseCheckout_EmptyBranch_IsMain(t *testing.T) {
	t.Parallel()
	fx := newLeaseFixture(t)
	ssh := &leaseSSH{}
	fx.deps.SSH = ssh
	fx.swarm.acquired = acquiredAs(3)

	view, err := LeaseAcquire(context.Background(), fx.deps, testPoolHost, testRepo, "")
	if err != nil {
		t.Fatal(err)
	}
	if view.Branch != "main" {
		t.Errorf("view branch = %q, want main", view.Branch)
	}
	if cmds := ssh.checkouts(); len(cmds) != 1 || !strings.Contains(cmds[0].command, shellQuote("main")) {
		t.Errorf("checkouts = %v, want a checkout of main", cmds)
	}
}

func TestLeaseCheckout_Failure_ReleasesWithResetAndCleansUp(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		output   string
		wantCode string
	}{
		{"rejected token", "fatal: Authentication failed for 'https://github.com/acme/app/'", platform.ErrGitTokenInvalid},
		{"no token in the session", "fatal: could not read Username for 'https://github.com': terminal prompts disabled", platform.ErrGitTokenMissing},
		{"repository missing", "fatal: repository 'https://github.com/acme/app/' not found", platform.ErrSSHDeployFailed},
		{"network", "fatal: unable to access 'https://github.com/acme/app/': Could not resolve host: github.com", platform.ErrSSHDeployFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			hf := newHostFixture(t)
			hf.swarm.acquired = acquiredAs(3)
			hf.deps.SSH = &leaseSSH{fn: func(string, string) ([]byte, error) {
				return []byte(tt.output), sshFailure(tt.output, exitError(t, 128))
			}}

			_, err := LeaseAcquire(context.Background(), hf.deps, testPoolHost, testRepo, "feature")
			var pe *platform.PlatformError
			if !errors.As(err, &pe) {
				t.Fatalf("err = %v, want a *PlatformError", err)
			}
			if pe.Code != tt.wantCode {
				t.Errorf("Code = %q, want %q", pe.Code, tt.wantCode)
			}
			var sshErr *platform.SSHExecError
			if !errors.As(err, &sshErr) || sshErr.Output != tt.output {
				t.Errorf("the ssh error with git's stderr must stay reachable for the tool to surface (got %v)", err)
			}
			if strings.Contains(err.Error(), testLease) || strings.Contains(pe.Suggestion, testLease) {
				t.Errorf("error leaks the lease: %v", err)
			}
			if len(hf.swarm.releases) != 1 || hf.swarm.releases[0] != (fakeRelease{id: "c-abc", lease: testLease, reset: true}) {
				t.Errorf("releases = %+v, want the container released with reset=true", hf.swarm.releases)
			}
			if _, statErr := os.Stat(filepath.Join(hf.stateDir, "leases", "devpool-3.json")); !errors.Is(statErr, os.ErrNotExist) {
				t.Errorf("the record survives a failed checkout: %v", statErr)
			}
			raw, rErr := os.ReadFile(hf.aliasFile)
			if rErr != nil {
				t.Fatal(rErr)
			}
			if strings.Contains(string(raw), "devpool-3") {
				t.Errorf("the stanza of the released lease is still in the alias file:\n%s", raw)
			}
			if st := hf.mounter.states["/var/www/devpool-3"]; st == platform.MountStateActive {
				t.Errorf("devpool-3 is still mounted after the failed checkout")
			}
			if _, ok := readPoolDefault(t, hf.stateDir, testPoolHost); ok {
				t.Error("a failed checkout must not store the repo as the pool's default")
			}
			want := "install-aliases,mount,unmount,orchestrator-release,install-aliases"
			if got := strings.Join(hf.events, ","); got != want {
				t.Errorf("host events = %s, want %s", got, want)
			}
		})
	}
}

func TestLeaseCheckout_FailureAndReleaseFails_RecordKept(t *testing.T) {
	t.Parallel()
	fx := newLeaseFixture(t)
	fx.swarm.acquired = acquiredAs(3)
	fx.swarm.releaseErr = platform.NewPlatformError(platform.ErrNetworkError, "the orchestrator did not answer", "")
	fx.deps.SSH = &leaseSSH{fn: func(string, string) ([]byte, error) {
		return nil, sshFailure("fatal: Authentication failed", exitError(t, 128))
	}}

	_, err := LeaseAcquire(context.Background(), fx.deps, testPoolHost, testRepo, "feature")
	if err == nil {
		t.Fatal("want the checkout failure")
	}
	// The container could not be handed back: the record stays so that
	// zerops_lease can still release it.
	if _, statErr := os.Stat(filepath.Join(fx.stateDir, "leases", "devpool-3.json")); statErr != nil {
		t.Errorf("record dropped although the container is still reserved: %v", statErr)
	}
	if !strings.Contains(err.Error(), "devpool-3") || !strings.Contains(err.Error(), "release") {
		t.Errorf("error %q should tell the agent the container is still held and how to release it", err)
	}
}

// unsavedFixture is a leased devpool-3 whose container answers the two guard
// reads (git status, unpushed count) as given.
func unsavedFixture(t *testing.T, status string, unpushed int) (*hostFixture, *leaseSSH) {
	t.Helper()
	hf := newHostFixture(t)
	ssh := &leaseSSH{fn: func(_, command string) ([]byte, error) {
		switch {
		case strings.Contains(command, "status --porcelain"):
			return []byte(status), nil
		case strings.Contains(command, "rev-list --count"):
			return []byte(strconv.Itoa(unpushed) + "\n"), nil
		}
		return nil, nil
	}}
	hf.deps.SSH = ssh
	hf.swarm.acquired = acquiredAs(3)
	if _, err := LeaseAcquire(context.Background(), hf.deps, testPoolHost, testRepo, "feature"); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	ssh.reset()
	hf.events = nil
	return hf, ssh
}

func assertRefusedAndKept(t *testing.T, hf *hostFixture, err error) *platform.PlatformError {
	t.Helper()
	var pe *platform.PlatformError
	if !errors.As(err, &pe) {
		t.Fatalf("err = %v, want a refusal", err)
	}
	if !strings.Contains(pe.Message+pe.Suggestion, "force") {
		t.Errorf("refusal %q / %q should name force=true", pe.Message, pe.Suggestion)
	}
	if len(hf.swarm.releases) != 0 {
		t.Errorf("a refused release reached the orchestrator: %+v", hf.swarm.releases)
	}
	// Re-rendering the alias file from the records (the lease's own stanza
	// included) is the guard's preparation; nothing else may touch the host.
	if touched := slices.DeleteFunc(slices.Clone(hf.events), func(e string) bool { return e == "install-aliases" }); len(touched) != 0 {
		t.Errorf("a refused release touched the host (%v); the mount must stay", touched)
	}
	if _, statErr := os.Stat(filepath.Join(hf.stateDir, "leases", "devpool-3.json")); statErr != nil {
		t.Errorf("record gone after a refused release: %v", statErr)
	}
	return pe
}

func TestLeaseRelease_Unpushed_Refused(t *testing.T) {
	t.Parallel()
	hf, ssh := unsavedFixture(t, "", 2)

	_, err := LeaseRelease(context.Background(), hf.deps, "devpool-3", false)
	pe := assertRefusedAndKept(t, hf, err)
	if !strings.Contains(pe.Message, "2 commit") {
		t.Errorf("message %q should count the 2 commits on no remote branch", pe.Message)
	}
	var sawStatus, sawRevList bool
	for _, c := range ssh.commands() {
		sawStatus = sawStatus || strings.Contains(c, "git -C '/var/www' status --porcelain")
		sawRevList = sawRevList || strings.Contains(c, "git -C '/var/www' rev-list --count --branches HEAD --not --remotes=origin")
	}
	if !sawStatus || !sawRevList {
		t.Errorf("guard commands = %v, want git status --porcelain and rev-list --count --branches HEAD --not --remotes=origin on /var/www", ssh.commands())
	}
	if ssh.calls[0].hostname != "devpool-3" {
		t.Errorf("guard ran on %q, want the alias", ssh.calls[0].hostname)
	}
}

func TestLeaseRelease_Uncommitted_Refused(t *testing.T) {
	t.Parallel()
	var lines []string
	for i := 1; i <= 25; i++ {
		lines = append(lines, fmt.Sprintf("?? file%02d.txt", i))
	}
	hf, _ := unsavedFixture(t, " M src/main.go\n"+strings.Join(lines, "\n")+"\n", 0)

	_, err := LeaseRelease(context.Background(), hf.deps, "devpool-3", false)
	pe := assertRefusedAndKept(t, hf, err)
	if !strings.Contains(pe.Message, "src/main.go") || !strings.Contains(pe.Message, "file19.txt") {
		t.Errorf("message %q should name the first 20 files", pe.Message)
	}
	if strings.Contains(pe.Message, "file20.txt") || strings.Contains(pe.Message, "file25.txt") {
		t.Errorf("message %q names more than 20 files", pe.Message)
	}
	if !strings.Contains(pe.Message, "26") {
		t.Errorf("message %q should give the total (26)", pe.Message)
	}
}

func TestLeaseRelease_Clean_Releases(t *testing.T) {
	t.Parallel()
	hf, _ := unsavedFixture(t, "", 0)

	view, err := LeaseRelease(context.Background(), hf.deps, "devpool-3", false)
	if err != nil {
		t.Fatalf("a clean, pushed container must release: %v", err)
	}
	if view.State != LeaseStateReleased {
		t.Errorf("view = %+v, want released", view)
	}
	if len(hf.swarm.releases) != 1 || !hf.swarm.releases[0].reset {
		t.Errorf("releases = %+v, want one with reset=true", hf.swarm.releases)
	}
}

func TestLeaseRelease_Force_Resets(t *testing.T) {
	t.Parallel()
	hf, ssh := unsavedFixture(t, "?? work.txt\n", 3)

	view, err := LeaseRelease(context.Background(), hf.deps, "devpool-3", true)
	if err != nil {
		t.Fatalf("force must release: %v", err)
	}
	if view.State != LeaseStateReleased {
		t.Errorf("view = %+v, want released", view)
	}
	if len(hf.swarm.releases) != 1 || hf.swarm.releases[0] != (fakeRelease{id: "c-abc", lease: testLease, reset: true}) {
		t.Errorf("releases = %+v, want one with reset=true", hf.swarm.releases)
	}
	if cmds := ssh.commands(); len(cmds) != 0 {
		t.Errorf("force still read the container: %v", cmds)
	}
}

// TestLeaseRelease_Unreachable_Lost: a container ssh cannot reach is asked
// about again; once the orchestrator no longer reserves it — the lease ended
// while the guard ran — the record is dropped as lost, unchecked, and nothing
// is released: a release of a free container resets it whatever the lease.
func TestLeaseRelease_Unreachable_Lost(t *testing.T) {
	t.Parallel()
	hf, ssh := unsavedFixture(t, "", 0)
	hf.swarm.containers = []platform.SwarmContainer{reservedContainer("c-abc", 3)}
	ssh.setHandler(func(string, string) ([]byte, error) {
		hf.swarm.containers[0].LockState = "" // released outside zcp meanwhile
		return []byte("ssh: connect to host node-id-3.runtime.devpool.zerops port 22: Connection timed out"),
			sshFailure("ssh: connect to host ... Connection timed out", exitError(t, 255))
	})

	view, err := LeaseRelease(context.Background(), hf.deps, "devpool-3", false)
	if err != nil {
		t.Fatalf("an unreachable container the orchestrator no longer holds must be dropped: %v", err)
	}
	if view.State != LeaseStateLost || !strings.Contains(view.Note, "reach") || !strings.Contains(view.Note, "record is dropped") {
		t.Errorf("view = %+v, want state lost with a note that the container could not be reached and the record is dropped", view)
	}
	if len(hf.swarm.releases) != 0 {
		t.Errorf("releases = %+v, want none: the container is free", hf.swarm.releases)
	}
	if _, statErr := os.Stat(filepath.Join(hf.stateDir, "leases", "devpool-3.json")); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("record survives: %v", statErr)
	}
}

func TestLeaseRelease_GuardCannotRead_Refused(t *testing.T) {
	t.Parallel()
	hf, ssh := unsavedFixture(t, "", 0)
	ssh.setHandler(func(string, string) ([]byte, error) {
		return nil, sshFailure("fatal: not a git repository (or any of the parent directories): .git", exitError(t, 128))
	})

	_, err := LeaseRelease(context.Background(), hf.deps, "devpool-3", false)
	_ = assertRefusedAndKept(t, hf, err)
}

// TestLeaseRelease_NoSSH_RefusedUnlessForced: a release refuses while the
// container holds work a reset would lose (DP-4), and a zcp with no ssh to the
// container (local mode) cannot tell whether it does. So an unforced release
// is refused — nothing released, the record kept — naming force=true, and a
// forced one releases with a reset.
func TestLeaseRelease_NoSSH_RefusedUnlessForced(t *testing.T) {
	t.Parallel()
	for _, force := range []bool{false, true} {
		t.Run(fmt.Sprintf("force=%v", force), func(t *testing.T) {
			t.Parallel()
			fx := newLeaseFixture(t)
			fx.swarm.acquired = acquiredAs(3)
			if _, err := LeaseAcquire(context.Background(), fx.deps, testPoolHost, testRepo, "feature"); err != nil {
				t.Fatal(err)
			}
			view, err := LeaseRelease(context.Background(), fx.deps, "devpool-3", force)
			_, statErr := os.Stat(filepath.Join(fx.stateDir, "leases", "devpool-3.json"))
			if force {
				if err != nil || view.State != LeaseStateReleased || len(fx.swarm.releases) != 1 || !fx.swarm.releases[0].reset {
					t.Errorf("forced release = %+v, %v (releases %+v), want released with a reset", view, err, fx.swarm.releases)
				}
				return
			}
			var pe *platform.PlatformError
			if !errors.As(err, &pe) || pe.Code != platform.ErrPrerequisiteMissing {
				t.Fatalf("err = %v, want a %s refusal", err, platform.ErrPrerequisiteMissing)
			}
			if !strings.Contains(pe.Message, "no ssh") || !strings.Contains(pe.Suggestion, "force=true") {
				t.Errorf("refusal %q / %q should say zcp has no ssh to check it and name force=true", pe.Message, pe.Suggestion)
			}
			if len(fx.swarm.releases) != 0 {
				t.Errorf("an unchecked release reached the orchestrator: %+v", fx.swarm.releases)
			}
			if statErr != nil {
				t.Errorf("record gone after a refused release: %v", statErr)
			}
		})
	}
}

// TestLeaseReleaseGroup_NoSSH_RefusedUnlessForced: a branch's release reads
// every lease before it releases any (DP-23); without ssh none can be read, so
// an unforced one releases none and keeps every record.
func TestLeaseReleaseGroup_NoSSH_RefusedUnlessForced(t *testing.T) {
	t.Parallel()
	gf := newGroupFixture(t)
	gf.heldGroup(t)
	gf.deps.SSH = nil

	_, err := LeaseReleaseGroup(context.Background(), gf.deps, "", "feat", false)
	var pe *platform.PlatformError
	if !errors.As(err, &pe) || pe.Code != platform.ErrPrerequisiteMissing {
		t.Fatalf("err = %v, want a %s refusal", err, platform.ErrPrerequisiteMissing)
	}
	if !strings.Contains(pe.Message, "no ssh") {
		t.Errorf("message %q should say zcp has no ssh to check the leases", pe.Message)
	}
	for name, o := range map[string]*poolOrch{"hqpool": gf.hq, "webpool": gf.web} {
		if rel := o.releaseLog(); len(rel) != 0 {
			t.Errorf("%s releases = %+v: an unchecked branch releases nothing", name, rel)
		}
	}
	if recs := gf.records(t); len(recs) != 2 {
		t.Errorf("records = %v, want both kept", recs)
	}
}

// The checkout leaves a credential helper in the repository, scoped to the
// clone's host, so the agent's own `git push` over the alias authenticates
// from the session's GIT_TOKEN without a credential ever being stored.
func TestLeaseCheckoutCommand_PersistsHostScopedHelper(t *testing.T) {
	t.Parallel()
	want := "git config --replace-all " + shellQuote("credential.https://github.com.helper") + " " + shellQuote(leaseCredentialHelperShell)
	if cmd := leaseCheckoutCommand("/var/www", "https://github.com/acme/app", "main", "", ""); !strings.Contains(cmd, want) {
		t.Errorf("command lacks the persisted helper %q:\n%s", want, cmd)
	}
	if cmd := leaseCheckoutCommand("/var/www", "/srv/origin.git", "main", "", ""); strings.Contains(cmd, "git config --replace-all 'credential.") {
		t.Errorf("a non-https origin got a persisted helper:\n%s", cmd)
	}
}

// TestLeaseCredentialHelper_OriginOnItsOwnPort_AnswersThere: git matches a
// helper's scope by port exactly (live, git 2.50), so the helper a lease's
// repository keeps for an origin on a port of its own is stored under that
// port: the agent's push there gets the session's GIT_TOKEN, and the host's
// default port gets nothing from it.
func TestLeaseCredentialHelper_OriginOnItsOwnPort_AnswersThere(t *testing.T) {
	t.Parallel()
	requireGit(t)
	const repo = "https://git.example.invalid:8443/acme/app.git"
	session := map[string]string{"GIT_TOKEN": "the-users-token"}
	if _, password, _ := persistedAnswer(t, repo, session); password != "the-users-token" {
		t.Errorf("a push to %s gets %q, want the session's GIT_TOKEN", repo, password)
	}
	dir, home := t.TempDir(), t.TempDir()
	if out, err := gitShell(t, dir, home, nil, "", "git init -q && "+leaseCredentialHelperConfig(repo, leaseHQURL)); err != nil {
		t.Fatalf("persist the helper: %v\n%s", err, out)
	}
	if _, password, answered := answeredCredential(t, dir, home, session, "https://git.example.invalid/acme/app.git"); answered {
		t.Errorf("the host's default port gets %q from a helper kept for port 8443", password)
	}
}

// The persisted helper answers only a get, and only from a session that holds
// GIT_TOKEN: git run where there is none (the zcp side of the mount) goes on to
// its next helper instead of sending an empty password.
func TestLeaseCredentialHelper_AnswersFromSessionEnvOnly(t *testing.T) {
	t.Parallel()
	body := strings.TrimPrefix(leaseCredentialHelperShell, "!")
	run := func(token, action string) string {
		t.Helper()
		// git runs a "!" helper as: sh -c '<helper> "$@"' <helper> <action>
		cmd := exec.CommandContext(context.Background(), "sh", "-c", body+` "$@"`, body, action) //nolint:gosec // running the helper is the test
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "GIT_TOKEN=" + token}
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("helper %s (token %t): %v\n%s", action, token != "", err, out)
		}
		return string(out)
	}
	if got := run("tok-1", "get"); got != "username=oauth2\npassword=tok-1\n" {
		t.Errorf("get with GIT_TOKEN = %q, want the oauth2 credential", got)
	}
	if got := run("", "get"); got != "" {
		t.Errorf("get without GIT_TOKEN = %q, want nothing", got)
	}
	for _, action := range []string{"store", "erase"} {
		if got := run("tok-1", action); got != "" {
			t.Errorf("%s = %q, want nothing", action, got)
		}
	}
}

// A container that ssh cannot reach is released unchecked only when the
// orchestrator no longer holds it for this lease: a reserved container may
// hold work, and an alias that does not resolve is not a gone container.
func TestLeaseRelease_UnreachableButReserved_Refused(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		output string
	}{
		{"alias not resolvable", "ssh: Could not resolve hostname devpool-3: Name or service not known"},
		{"connection timed out", "ssh: connect to host node-id-3.runtime.devpool.zerops port 22: Connection timed out"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			hf, ssh := unsavedFixture(t, "", 0)
			hf.swarm.containers = []platform.SwarmContainer{{ID: "c-abc", Number: 3, LockState: leaseLockReserved}}
			ssh.setHandler(func(string, string) ([]byte, error) {
				return []byte(tt.output), sshFailure(tt.output, exitError(t, 255))
			})
			_, err := LeaseRelease(context.Background(), hf.deps, "devpool-3", false)
			pe := assertRefusedAndKept(t, hf, err)
			if !strings.Contains(pe.Message, "still reserved") {
				t.Errorf("refusal %q should say the container is still reserved", pe.Message)
			}
		})
	}
}

// TestLeaseRelease_UnreachableOrchestratorSilent_Refused: whose the
// container is comes first (check before mutate); an orchestrator that does
// not answer while its pool is still there refuses the release — plain or
// forced — nothing read over ssh, released or dropped (DP-20).
func TestLeaseRelease_UnreachableOrchestratorSilent_Refused(t *testing.T) {
	t.Parallel()
	for _, force := range []bool{false, true} {
		t.Run(fmt.Sprintf("force=%v", force), func(t *testing.T) {
			t.Parallel()
			hf, ssh := unsavedFixture(t, "", 0)
			hf.swarm.listErr = platform.NewPlatformError(platform.ErrNetworkError, "dial tcp: i/o timeout", "")
			_, err := LeaseRelease(context.Background(), hf.deps, "devpool-3", force)
			var pe *platform.PlatformError
			if !errors.As(err, &pe) || !strings.Contains(pe.Message, "devpool-3") {
				t.Fatalf("err = %v, want a refusal naming the lease", err)
			}
			if len(hf.swarm.releases) != 0 || len(hf.swarm.deletes) != 0 {
				t.Errorf("a refused release reached the orchestrator: releases %+v, deletes %+v", hf.swarm.releases, hf.swarm.deletes)
			}
			if cmds := ssh.commands(); len(cmds) != 0 {
				t.Errorf("a release zcp could not check read the container: %v", cmds)
			}
			if _, statErr := os.Stat(filepath.Join(hf.stateDir, "leases", "devpool-3.json")); statErr != nil {
				t.Errorf("record gone after a refused release: %v", statErr)
			}
		})
	}
}

// The guard reaches the container through its alias, so the alias file is
// re-rendered from the records first: a stanza another call dropped, or one
// a redeploy lost, is back before the check runs.
func TestLeaseRelease_RestoresAliasesBeforeTheGuard(t *testing.T) {
	t.Parallel()
	hf, ssh := unsavedFixture(t, "", 0)
	if err := os.WriteFile(hf.aliasFile, []byte(leaseAliasFileHeader), 0o644); err != nil {
		t.Fatal(err)
	}
	var stanzaAtCheck bool
	ssh.setHandler(func(_, command string) ([]byte, error) {
		raw, _ := os.ReadFile(hf.aliasFile)
		stanzaAtCheck = strings.Contains(string(raw), "Host devpool-3\n")
		if strings.Contains(command, "rev-list --count") {
			return []byte("0\n"), nil
		}
		return nil, nil
	})
	if _, err := LeaseRelease(context.Background(), hf.deps, "devpool-3", false); err != nil {
		t.Fatalf("release: %v", err)
	}
	if !stanzaAtCheck {
		t.Error("the guard ran while the alias file lacked the lease's stanza")
	}
}

// The checkout's fetch answers git only from a session that holds GIT_TOKEN,
// so a missing token surfaces as git's own "terminal prompts disabled" (the
// credential contract's missing case), never as a rejected empty password.
func TestLeaseCheckoutCommand_FetchHelperAnswersNothingWithoutToken(t *testing.T) {
	t.Parallel()
	cmd := leaseCheckoutCommand("/var/www", "https://github.com/acme/app", "main", "", "")
	if want := "-c credential.helper= -c credential.helper=" + shellQuote(leaseCredentialHelperShell) + " fetch"; !strings.Contains(cmd, want) {
		t.Errorf("fetch does not use the token-guarded helper %q:\n%s", want, cmd)
	}
	if strings.Contains(cmd, gitCredentialHelperArgs()) {
		t.Errorf("fetch still uses the helper that sends an empty password:\n%s", cmd)
	}
}

// TestLeaseRelease_Unreachable_SearchMissesPool_StillRefused: the service
// search lags writes, so a pool absent from one search is not a pool that is
// gone. The guard reads the pool by id; while it exists and its orchestrator
// holds the container, the release is refused, never reset.
func TestLeaseRelease_Unreachable_SearchMissesPool_StillRefused(t *testing.T) {
	t.Parallel()
	hf, ssh := unsavedFixture(t, "", 0)
	pool := platform.ServiceStack{ID: "svc-pool", Name: testPoolHost, Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1", ServiceStackTypeCategoryName: "USER"}}
	hf.deps.Client = platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "proj"}).
		WithServices(nil). // the search has not caught up with the pool
		WithService(&pool).
		WithServiceEnv("svc-pool", []platform.ServiceEnvVar{{Key: "API_TOKEN", Content: testPoolToken, Sensitive: true}})
	hf.swarm.containers = []platform.SwarmContainer{{ID: "c-abc", Number: 3, LockState: leaseLockReserved}}
	ssh.setHandler(func(string, string) ([]byte, error) {
		return nil, sshFailure("ssh: connect to host ... Connection timed out", exitError(t, 255))
	})
	_, err := LeaseRelease(context.Background(), hf.deps, "devpool-3", false)
	pe := assertRefusedAndKept(t, hf, err)
	if !strings.Contains(pe.Message, "still reserved") {
		t.Errorf("refusal %q should say the container is still reserved", pe.Message)
	}
}

// TestLeaseRelease_Unreachable_PoolGoneByID_Lost: a pool the direct read no
// longer finds holds nothing of the lease; the release drops it as lost.
func TestLeaseRelease_Unreachable_PoolGoneByID_Lost(t *testing.T) {
	t.Parallel()
	hf, ssh := unsavedFixture(t, "", 0)
	hf.deps.Client = platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "proj"}).
		WithServices(nil)
	ssh.setHandler(func(string, string) ([]byte, error) {
		return nil, sshFailure("ssh: connect to host ... Connection timed out", exitError(t, 255))
	})
	view, err := LeaseRelease(context.Background(), hf.deps, "devpool-3", false)
	if err != nil {
		t.Fatalf("a lease of a pool that is gone must be dropped: %v", err)
	}
	if view.State != LeaseStateLost {
		t.Errorf("state = %q, want lost", view.State)
	}
}

// TestLeaseRelease_PoolGoneOrBeingDeleted_DroppedAsLost: a pool service the
// read by id no longer finds, or one being deleted, holds nothing of the
// lease, and its orchestrator does not answer. A release — without force or
// with it — drops the record as lost and says why (live: a release after the
// pool was deleted was refused "the orchestrator did not answer" until
// force=true). A pool that exists but whose orchestrator does not answer is
// still refused (TestLeaseRelease_UnreachableOrchestratorSilent_Refused).
func TestLeaseRelease_PoolGoneOrBeingDeleted_DroppedAsLost(t *testing.T) {
	t.Parallel()
	pool := func(status string) platform.ServiceStack {
		return platform.ServiceStack{ID: "svc-pool", Name: testPoolHost, Status: status,
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1", ServiceStackTypeCategoryName: "USER"}}
	}
	notFound := platform.NewPlatformError(platform.ErrServiceNotFound, "service stack not found", "")
	// What the platform answers a by-id read of a deleted service with
	// (live-verified): HTTP 400 serviceStackNotFound, not a 404.
	stackNotFound := platform.NewPlatformError(platform.ErrAPIError, "Service stack not found.", "")
	stackNotFound.APICode = "serviceStackNotFound"
	tests := []struct {
		name    string
		listed  []platform.ServiceStack // what the service search still lists
		byID    string                  // the status the read by id answers; "" = not found
		force   bool
		wantWhy string
		byIDErr error // the not-found answer of the read by id; nil = notFound
	}{
		{"deleted, gone from the search", nil, "DELETED", false, "no longer exists", nil},
		{"deleted, still in the search", []platform.ServiceStack{pool("DELETED")}, "DELETED", false, "no longer exists", nil},
		{"being deleted, still in the search", []platform.ServiceStack{pool("DELETING")}, "DELETING", false, "being deleted", nil},
		{"being deleted, the search still says active", []platform.ServiceStack{pool("ACTIVE")}, "DELETING", false, "being deleted", nil},
		{"being deleted, forced", []platform.ServiceStack{pool("DELETING")}, "DELETING", true, "being deleted", nil},
		{"not found by id, the search still lists it", []platform.ServiceStack{pool("ACTIVE")}, "", false, "no longer exists", nil},
		{"400 serviceStackNotFound by id, the search still lists it", []platform.ServiceStack{pool("ACTIVE")}, "", false, "no longer exists", stackNotFound},
		{"400 serviceStackNotFound by id, gone from the search", nil, "", false, "no longer exists", stackNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			hf, ssh := unsavedFixture(t, "", 0)
			ssh.setHandler(func(string, string) ([]byte, error) {
				out := "ssh: Could not resolve hostname node-id-3.runtime.devpool.zerops: No address associated with hostname"
				return []byte(out), sshFailure(out, exitError(t, 255))
			})
			mock := platform.NewMock().
				WithProject(&platform.Project{ID: "p1", Name: "proj"}).
				WithServices(tt.listed).
				WithServiceEnv("svc-pool", []platform.ServiceEnvVar{{Key: "API_TOKEN", Content: testPoolToken, Sensitive: true}})
			if tt.byID == "" {
				mock = mock.WithError("GetService", cmp.Or(tt.byIDErr, error(notFound)))
			} else {
				byID := pool(tt.byID)
				mock = mock.WithService(&byID)
			}
			hf.deps.Client = mock
			// The orchestrator of a pool on its way out answers nothing.
			unanswered := platform.NewPlatformError(platform.ErrNetworkError, "dial tcp: lookup orch.devpool.zerops: no such host", "")
			hf.swarm.listErr, hf.swarm.releaseErr = unanswered, unanswered

			view, err := LeaseRelease(context.Background(), hf.deps, "devpool-3", tt.force)
			if err != nil {
				t.Fatalf("a lease of a pool that is gone or being deleted must be dropped: %v", err)
			}
			if view.State != LeaseStateLost || !strings.Contains(view.Note, tt.wantWhy) {
				t.Errorf("view = %+v, want state lost with a note saying the pool %s", view, tt.wantWhy)
			}
			if _, statErr := os.Stat(filepath.Join(hf.stateDir, "leases", "devpool-3.json")); !errors.Is(statErr, os.ErrNotExist) {
				t.Errorf("record survives: %v", statErr)
			}
		})
	}
}

// TestLeaseCheckoutCommand_SetsACommitIdentityOnlyWhenAbsent pins that a
// lease's checkout leaves the repository commit-ready (docs/spec-workflows.md
// §8 DP-6): a fresh pool container carries no git identity, so the agent's
// first commit failed "Author identity unknown" (live, 2026-10-24). zcp fills
// the robot identity set-if-absent, after the checkout, as every other site
// that needs a commit-ready repository does — never over one the image sets.
func TestLeaseCheckoutCommand_SetsACommitIdentityOnlyWhenAbsent(t *testing.T) {
	t.Parallel()
	cmd := leaseCheckoutCommand("/var/www", testRepo, "feature", "", "")
	identity := gitIdentityEnsureFragment()
	at := strings.Index(cmd, identity)
	if at < 0 {
		t.Fatalf("checkout command sets no commit identity:\n%s", cmd)
	}
	if checkout := strings.Index(cmd, "git checkout -q -f -B"); checkout < 0 || checkout > at {
		t.Errorf("the identity is set before the checkout, in a repository not yet on its branch:\n%s", cmd)
	}
}
