// Tests for: ops/lease_checkout.go — a lease of a repository on the Mate's HQ
// reaches HQ relayed and names HQ's branch rule
// (docs/spec-workflows.md §8 DP-12).
package ops

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

const (
	leaseHQURL  = "https://hq.example.invalid"
	leaseHQRepo = leaseHQURL + "/git/a1/web"
)

// TestLeaseCheckoutCommand_HQRepoNamesNoHelper_OthersKeepTheOAuth2One: a
// repository on the Mate's HQ is fetched with no helper of the command's own
// and keeps none — the relayed session's helper answers HQ, and a
// `-c credential.helper=` would clear it — while every other repository is
// fetched with, and keeps, the lease's oauth2 helper under its own scope, its
// port included.
// witness: internal/ops/lease_checkout.go | if !strings.HasPrefix(repo, httpsScheme+"://") || IsHQRemote(repo, hqURL) { → if !strings.HasPrefix(repo, httpsScheme+"://") {
func TestLeaseCheckoutCommand_HQRepoNamesNoHelper_OthersKeepTheOAuth2One(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		repo      string
		hqURL     string
		wantScope string // "" for a repository on HQ: no helper at all
	}{
		{"a repository on HQ", leaseHQRepo, leaseHQURL, ""},
		{"a repository on an HQ on its own port", "https://hq.example.invalid:3000/git/a1/web", "https://hq.example.invalid:3000", ""},
		{"a repository outside HQ", "https://github.com/acme/app", leaseHQURL, "github.com"},
		{"a container enrolled with no HQ", leaseHQRepo, "", "hq.example.invalid"},
		{"HQ's host on another port", "https://hq.example.invalid:3000/git/a1/web", leaseHQURL, "hq.example.invalid:3000"},
		{"a repository outside HQ on its own port", "https://git.example.invalid:8443/acme/app", leaseHQURL, "git.example.invalid:8443"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cmd := leaseCheckoutCommand("/var/www", tt.repo, "feat", "", tt.hqURL)
			if tt.wantScope == "" {
				if !strings.Contains(cmd, "GIT_TERMINAL_PROMPT=0 git fetch -q --prune origin") || strings.Contains(cmd, "credential.helper") || strings.Contains(cmd, "username=") {
					t.Errorf("the checkout of a repository on HQ names a helper of its own:\n%s", cmd)
				}
				return
			}
			helper := leaseCredentialHelperShell(tt.wantScope)
			fetch := "-c credential.helper= -c credential.helper=" + shellQuote(helper) + " fetch"
			persisted := "git config --replace-all " + shellQuote("credential.https://"+tt.wantScope+".helper") + " " + shellQuote(helper)
			for _, want := range []string{fetch, persisted} {
				if !strings.Contains(cmd, want) {
					t.Errorf("command lacks %q:\n%s", want, cmd)
				}
			}
		})
	}
}

// TestLeaseAcquire_HQRepo_ChecksOutAsMate: acquire reads the enrollment's HQ
// address when it checks out, runs the checkout of a repository there as a
// relayed session (HQGit), which answers HQ as the Mate, and the lease record
// keeps the canonical
// repository (never a credential) for the release guard and the git-push
// refusal to tell an HQ lease by.
func TestLeaseAcquire_HQRepo_ChecksOutAsMate(t *testing.T) {
	t.Parallel()
	fx := newLeaseFixture(t)
	ssh, relayed := &leaseSSH{}, &leaseSSH{}
	fx.deps.SSH, fx.deps.HQGit = ssh, relayed
	fx.deps.HQAddress = func() string { return leaseHQURL }
	fx.swarm.acquired = acquiredAs(3)

	if _, err := LeaseAcquire(context.Background(), fx.deps, testPoolHost, leaseHQRepo+".git", "feat"); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if cmds := relayed.checkouts(); len(cmds) != 1 || cmds[0].hostname != "devpool-3" {
		t.Fatalf("relayed checkouts = %v, want the one checkout of devpool-3, answered with the Mate's key", cmds)
	}
	if cmds := ssh.checkouts(); len(cmds) != 0 {
		t.Errorf("the checkout of a repository on HQ ran over plain ssh too: %v", cmds)
	}
	info, ok, err := LeaseTarget(fx.stateDir, "devpool-3")
	if err != nil || !ok {
		t.Fatalf("LeaseTarget = %+v, %v, %v", info, ok, err)
	}
	if info.Repo != leaseHQRepo {
		t.Errorf("record repo = %q, want the canonical %q", info.Repo, leaseHQRepo)
	}
}

// TestLeaseCheckout_HQCredentialFailure_NamesTheMateCredential: a lease of an
// HQ repository whose git reached HQ with nothing answering it — outside the
// relay — or with the Mate's project key HQ refused (DP-16) fails at
// checkout. The container goes back and the refusal says what is missing as a
// prerequisite — not as GIT_TOKEN_*, whose contract asks the user for a token:
// the Mate's key is no user's token to ask for or make up — and that git
// against HQ runs through zcp.
func TestLeaseCheckout_HQCredentialFailure_NamesTheMateCredential(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		output string
		want   string
	}{
		{"git outside the relay", "fatal: could not read Username for 'https://hq.example.invalid': terminal prompts disabled", "nothing answered it"},
		{"HQ refuses the Mate's key", "remote: Unauthorized\nfatal: Authentication failed for 'https://hq.example.invalid/git/a1/web/'", "refused"},
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
			for _, want := range []string{"project key", "zcp git <alias>", tt.want, "never ask"} {
				if !strings.Contains(pe.Suggestion, want) {
					t.Errorf("suggestion %q lacks %q", pe.Suggestion, want)
				}
			}
			if strings.Contains(pe.Suggestion, PoolHQCredentialEnvKey) {
				t.Errorf("suggestion %q still names a credential stored in the pool", pe.Suggestion)
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
			hqWords := []string{"branch of your own", "`git checkout -b <branch>`", "`zcp git <alias> push -u origin <branch>`", "HQ refuses a push of `main`", "force push"}
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
