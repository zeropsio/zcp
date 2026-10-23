// Tests for: ops/lease_checkout.go — a lease of a repository on the Mate's HQ
// authenticates as HQ's git user and names HQ's branch rule
// (docs/spec-workflows.md §8 DP-9).
package ops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

const (
	leaseHQURL  = "https://hq.example.invalid"
	leaseHQRepo = leaseHQURL + "/git/a1/web"
)

// TestLeaseCheckoutCommand_HQRepo_AuthenticatesAsMate: HQ's git takes only the
// user `mate`, so a repository on the Mate's HQ is fetched, and keeps its
// helper, as that user — under HQ's scope, its port included, as a dev
// service's checkout keeps it. Every other repository keeps the lease's oauth2
// helper.
func TestLeaseCheckoutCommand_HQRepo_AuthenticatesAsMate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		repo       string
		hqURL      string
		wantHelper string
		wantScope  string
	}{
		{"a repository on HQ", leaseHQRepo, leaseHQURL, leaseHQCredentialHelperShell, "hq.example.invalid"},
		{"a repository on an HQ on its own port", "https://hq.example.invalid:3000/git/a1/web", "https://hq.example.invalid:3000", leaseHQCredentialHelperShell, "hq.example.invalid:3000"},
		{"a repository outside HQ", "https://github.com/acme/app", leaseHQURL, leaseCredentialHelperShell, "github.com"},
		{"a container enrolled with no HQ", leaseHQRepo, "", leaseCredentialHelperShell, "hq.example.invalid"},
		{"HQ's host on another port", "https://hq.example.invalid:3000/git/a1/web", leaseHQURL, leaseCredentialHelperShell, "hq.example.invalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cmd := leaseCheckoutCommand("/var/www", tt.repo, "feat", tt.hqURL)
			fetch := "-c credential.helper= -c credential.helper=" + shellQuote(tt.wantHelper) + " fetch"
			persisted := "git config --replace-all " + shellQuote("credential.https://"+tt.wantScope+".helper") + " " + shellQuote(tt.wantHelper)
			for _, want := range []string{fetch, persisted} {
				if !strings.Contains(cmd, want) {
					t.Errorf("command lacks %q:\n%s", want, cmd)
				}
			}
			if tt.wantHelper == leaseHQCredentialHelperShell && strings.Contains(cmd, "username=oauth2") {
				t.Errorf("a repository on HQ is still asked for as oauth2:\n%s", cmd)
			}
			if strings.Contains(cmd, "zcp hq git-credential") {
				t.Errorf("the lease asks a zcp the pool container does not have:\n%s", cmd)
			}
		})
	}
}

// TestLeaseHQCredentialHelper_AnswersAsMateFromSessionEnvOnly: the helper a
// lease of an HQ repository keeps answers the pool's session from its
// GIT_TOKEN as `mate`, and without one answers nothing — it never falls back to
// `zcp hq git-credential` the way a dev service's HQ helper does, since zcp is
// not installed in a pool container. It is the lease helper with HQ's user,
// not a helper of another shape.
func TestLeaseHQCredentialHelper_AnswersAsMateFromSessionEnvOnly(t *testing.T) {
	t.Parallel()
	requireGit(t)
	if want := strings.Replace(leaseCredentialHelperShell, "username=oauth2", "username=mate", 1); leaseHQCredentialHelperShell != want {
		t.Errorf("the HQ lease helper is\n%s\nwant the lease helper with HQ's user\n%s", leaseHQCredentialHelperShell, want)
	}
	marker := filepath.Join(t.TempDir(), "zcp-was-asked")
	zcpOnPath := fakeZcpPath(t, "touch "+shellQuote(marker)+"; printf 'username=mate\\npassword=from-zcp\\n'")
	tests := []struct {
		name     string
		env      map[string]string
		wantUser string
		want     string
	}{
		{"the pool's session answers its GIT_TOKEN as mate", map[string]string{"GIT_TOKEN": "the-pool-secret", "PATH": zcpOnPath}, "mate", "the-pool-secret"},
		{"without GIT_TOKEN it answers nothing and asks no zcp", map[string]string{"PATH": zcpOnPath}, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, home := t.TempDir(), t.TempDir()
			script := "git init -q && " + leaseCredentialHelperConfig(leaseHQRepo, leaseHQURL)
			if out, err := gitShell(t, repo, home, nil, "", script); err != nil {
				t.Fatalf("persist the helper: %v\n%s", err, out)
			}
			user, got, answered := answeredCredential(t, repo, home, tt.env, leaseHQRepo)
			if got != tt.want || answered != (tt.want != "") {
				t.Errorf("the helper answered %q (answered=%v), want %q", got, answered, tt.want)
			}
			if tt.want != "" && user != tt.wantUser {
				t.Errorf("the helper answered the user %q, want %q", user, tt.wantUser)
			}
			if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("the helper ran zcp (%v); a pool container has none", err)
			}
		})
	}
}

// TestLeaseAcquire_HQRepo_ChecksOutAsMate: acquire reads the enrollment's HQ
// address when it checks out, and the lease record keeps the canonical
// repository (never a credential) for the release guard and the git-push
// refusal to tell an HQ lease by.
func TestLeaseAcquire_HQRepo_ChecksOutAsMate(t *testing.T) {
	t.Parallel()
	fx := newLeaseFixture(t)
	ssh := &leaseSSH{}
	fx.deps.SSH = ssh
	fx.deps.HQAddress = func() string { return leaseHQURL }
	fx.swarm.acquired = acquiredAs(3)

	if _, err := LeaseAcquire(context.Background(), fx.deps, testPoolHost, leaseHQRepo+".git", "feat"); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	cmds := ssh.commands()
	if len(cmds) != 1 {
		t.Fatalf("ssh commands = %v, want the one checkout", cmds)
	}
	if !strings.Contains(cmds[0], shellQuote(leaseHQCredentialHelperShell)) || strings.Contains(cmds[0], "username=oauth2") {
		t.Errorf("the checkout of a repository on HQ does not authenticate as mate:\n%s", cmds[0])
	}
	info, ok, err := LeaseTarget(fx.stateDir, "devpool-3")
	if err != nil || !ok {
		t.Fatalf("LeaseTarget = %+v, %v, %v", info, ok, err)
	}
	if info.Repo != leaseHQRepo {
		t.Errorf("record repo = %q, want the canonical %q", info.Repo, leaseHQRepo)
	}
}

// TestLeaseCheckout_HQCredentialFailure_NamesTheMateCredential: zcp gives the
// Mate credential to a wired pair's dev service only, never to a pool, so a
// lease of an HQ repository whose pool GIT_TOKEN is unset or another token
// fails at checkout. The container goes back and the refusal says what is
// missing as a prerequisite — not as GIT_TOKEN_*, whose contract asks the user
// for a token: the Mate credential is no user's token to ask for or make up.
func TestLeaseCheckout_HQCredentialFailure_NamesTheMateCredential(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		output string
		want   string
	}{
		{"no GIT_TOKEN in the pool", "fatal: could not read Username for 'https://hq.example.invalid': terminal prompts disabled", "is not set"},
		{"HQ refuses the pool's GIT_TOKEN", "remote: Unauthorized\nfatal: Authentication failed for 'https://hq.example.invalid/git/a1/web/'", "refused"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fx := newLeaseFixture(t)
			fx.deps.SSH = &leaseSSH{fn: func(string, string) ([]byte, error) {
				return nil, sshFailure(tt.output, exitError(t, 128))
			}}
			fx.deps.HQAddress = func() string { return leaseHQURL }
			fx.swarm.acquired = acquiredAs(3)

			_, err := LeaseAcquire(context.Background(), fx.deps, testPoolHost, leaseHQRepo, "feat")
			var pe *platform.PlatformError
			if !errors.As(err, &pe) {
				t.Fatalf("err = %v, want a refusal", err)
			}
			if pe.Code != platform.ErrPrerequisiteMissing {
				t.Errorf("code = %s, want %s (the Mate credential is no user's token)", pe.Code, platform.ErrPrerequisiteMissing)
			}
			for _, want := range []string{"`mate`", "HQ credential", "GIT_TOKEN", "pool", tt.want, "never ask"} {
				if !strings.Contains(pe.Suggestion, want) {
					t.Errorf("suggestion %q lacks %q", pe.Suggestion, want)
				}
			}
			if len(fx.swarm.releases) != 1 || !fx.swarm.releases[0].reset {
				t.Errorf("releases = %+v, want the container handed back with a reset", fx.swarm.releases)
			}
		})
	}
}

// TestLeaseRelease_HQRepo_HintNamesHQsBranchRule: the release guard tells the
// agent to push its work; for a lease on HQ it names the branch rule HQ
// enforces — a branch of the agent's own, never `main`, never forced — and a
// lease elsewhere keeps the plain push.
func TestLeaseRelease_HQRepo_HintNamesHQsBranchRule(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		repo string
		onHQ bool
	}{
		{"a lease on HQ", leaseHQRepo, true},
		{"a lease outside HQ", testRepo, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			hf := newHostFixture(t)
			hf.deps.HQAddress = func() string { return leaseHQURL }
			ssh := &leaseSSH{fn: func(_, command string) ([]byte, error) {
				if strings.Contains(command, "rev-list --count") {
					return []byte(strconv.Itoa(2) + "\n"), nil
				}
				return nil, nil
			}}
			hf.deps.SSH = ssh
			hf.swarm.acquired = acquiredAs(3)
			if _, err := LeaseAcquire(context.Background(), hf.deps, testPoolHost, tt.repo, "main"); err != nil {
				t.Fatalf("acquire: %v", err)
			}
			hf.events = nil

			_, err := LeaseRelease(context.Background(), hf.deps, "devpool-3", false)
			pe := assertRefusedAndKept(t, hf, err)
			hqWords := []string{"branch of your own", "`git checkout -b <branch>`", "`git push -u origin <branch>`", "HQ refuses a push of `main`", "force push"}
			for _, w := range hqWords {
				if got := strings.Contains(pe.Suggestion, w); got != tt.onHQ {
					t.Errorf("suggestion %q names %q: %v, want %v", pe.Suggestion, w, got, tt.onHQ)
				}
			}
			if !tt.onHQ && !strings.Contains(pe.Suggestion, "Commit and push it from the container") {
				t.Errorf("suggestion %q lost the plain push of a lease outside HQ", pe.Suggestion)
			}
		})
	}
}
