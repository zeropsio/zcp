// Tests for: the host a lease's credential helpers answer (review R4-2,
// docs/spec-workflows.md §8 DP-6, DP-12, DP-32) — the pool's GIT_TOKEN is the
// person's token for one git host, so the helpers a checkout, a warm clone and
// the repository a lease keeps hand git read git's host= and answer the token
// only for that host; an acquire naming another host fetches without it. The
// pool's HQ credential answers only HQ's host.
package ops

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

const (
	// foreignRepo is a repository on a host the pool's GIT_TOKEN is not for:
	// a URL an agent picked up from a README, an issue or a page.
	foreignRepo = "https://git.attacker.example.invalid/acme/app.git"
	usersToken  = "the-users-own-github-token"
)

// persistedHelperRe finds the helper a checkout persists in the repository:
// `git config --replace-all '<key>' '<helper>'`.
var persistedHelperRe = regexp.MustCompile(`git config --replace-all '(?:[^']|'\\'')*' ('(?:[^']|'\\'')*')`)

// persistedHelperAnswer asks the helper command persists — handed to git for
// every host, as if it were kept under any scope — for a credential to
// remoteURL, in a session with env; false when command persists none.
func persistedHelperAnswer(t *testing.T, command string, env map[string]string, remoteURL string) (user, password string, answered bool) {
	t.Helper()
	m := persistedHelperRe.FindStringSubmatch(command)
	if m == nil {
		return "", "", false
	}
	return inlineAnswer(t, "-c credential.helper= -c credential.helper="+m[1], env, remoteURL)
}

// TestLeaseAcquire_PoolTokenAnswersOnlyThePoolsHost: a pool whose default
// repository is on github.com — the host its GIT_TOKEN is for — and an
// acquire naming a repository on another host. Neither the checkout's fetch,
// the helper the repository keeps for the agent's push, nor the warm clone of
// that repository answers the person's token to that host; a lease of another
// repository on github.com still fetches and pushes with it.
func TestLeaseAcquire_PoolTokenAnswersOnlyThePoolsHost(t *testing.T) {
	t.Parallel()
	requireGit(t)
	session := map[string]string{GitTokenEnvKey: usersToken}

	t.Run("a repository on another host", func(t *testing.T) {
		t.Parallel()
		orch := newWarmOrch(4)
		orch.add(3, "", true, false, 1)
		wf, ssh := newWarmCloneFixture(t, orch)
		if err := storePoolDefaultRepo(wf.stateDir, testPoolHost, testRepo); err != nil {
			t.Fatal(err)
		}
		if _, err := LeaseAcquire(context.Background(), wf.deps, testPoolHost, foreignRepo, "feat"); err != nil {
			t.Fatalf("acquire: %v", err)
		}
		var checkouts []sshCall
		for _, c := range ssh.checkouts() {
			if !isWarmCloneLaunch(c.command) {
				checkouts = append(checkouts, c)
			}
		}
		launches := ssh.launches()
		if len(checkouts) != 1 || len(launches) != 1 {
			t.Fatalf("checkouts %d, clone launches %d: want one of each", len(checkouts), len(launches))
		}
		asked := map[string]func() (string, string, bool){
			"the checkout's fetch": func() (string, string, bool) {
				return inlineAnswer(t, checkouts[0].command, session, foreignRepo)
			},
			"the helper the repository keeps": func() (string, string, bool) {
				return persistedHelperAnswer(t, checkouts[0].command, session, foreignRepo)
			},
			"the warm clone of it": func() (string, string, bool) {
				return inlineAnswer(t, launches[0].command, session, foreignRepo)
			},
		}
		for what, ask := range asked {
			if _, password, answered := ask(); answered && strings.Contains(password, usersToken) {
				t.Errorf("%s answers %s with the pool's GIT_TOKEN", what, foreignRepo)
			}
		}
	})

	t.Run("another repository on the pool's host", func(t *testing.T) {
		t.Parallel()
		fx := newLeaseFixture(t)
		ssh := &leaseSSH{}
		fx.deps.SSH = ssh
		fx.swarm.acquired = acquiredAs(3)
		if err := storePoolDefaultRepo(fx.stateDir, testPoolHost, testRepo); err != nil {
			t.Fatal(err)
		}
		const other = "https://GitHub.com/acme/other.git"
		if _, err := LeaseAcquire(context.Background(), fx.deps, testPoolHost, other, "feat"); err != nil {
			t.Fatalf("acquire: %v", err)
		}
		cmd := ssh.checkouts()[0].command
		if _, password, _ := inlineAnswer(t, cmd, session, other); password != usersToken {
			t.Errorf("the checkout's fetch answers %q, want the pool's GIT_TOKEN", password)
		}
		if _, password, _ := persistedHelperAnswer(t, cmd, session, other); password != usersToken {
			t.Errorf("the helper the repository keeps answers %q, want the pool's GIT_TOKEN", password)
		}
	})
}

// TestLeaseAcquire_PoolTokenBindsAtTheFirstLeaseOutsideHQ: inside a Mate a
// pool's default repository is on HQ, which says nothing of where the
// person's GIT_TOKEN belongs. The first lease of a repository outside HQ
// fetches with it and binds it to that host; a later acquire naming another
// host gets nothing from it, the bound host still does.
func TestLeaseAcquire_PoolTokenBindsAtTheFirstLeaseOutsideHQ(t *testing.T) {
	t.Parallel()
	requireGit(t)
	fx := newLeaseFixture(t)
	inAMate(fx)
	ssh := &leaseSSH{}
	fx.deps.SSH = ssh
	if err := storePoolDefaultRepo(fx.stateDir, testPoolHost, leaseHQRepo); err != nil {
		t.Fatal(err)
	}
	session := map[string]string{GitTokenEnvKey: usersToken}
	for i, repo := range []string{testRepo, foreignRepo, "https://github.com/acme/other"} {
		fx.swarm.acquired = acquiredAs(int64(3 + i))
		if _, err := LeaseAcquire(context.Background(), fx.deps, testPoolHost, repo, "feat"); err != nil {
			t.Fatalf("acquire of %s: %v", repo, err)
		}
	}
	checkouts := ssh.checkouts()
	if len(checkouts) != 3 {
		t.Fatalf("checkouts = %d, want one per acquire", len(checkouts))
	}
	if _, password, _ := inlineAnswer(t, checkouts[0].command, session, testRepo); password != usersToken {
		t.Errorf("the first lease outside HQ fetches with %q, want the pool's GIT_TOKEN", password)
	}
	if _, password, answered := inlineAnswer(t, checkouts[1].command, session, foreignRepo); answered && strings.Contains(password, usersToken) {
		t.Errorf("an acquire on another host after the token was bound to github.com gets it")
	}
	if _, password, _ := inlineAnswer(t, checkouts[2].command, session, "https://github.com/acme/other"); password != usersToken {
		t.Errorf("a later lease on the bound host fetches with %q, want the pool's GIT_TOKEN", password)
	}
}

// TestLeaseAcquire_HQCredentialAnswersOnlyHQ: the pool's HQ credential is
// this Mate's, for HQ's git alone. The helpers a lease of an HQ repository
// hands git — the fetch's and the one the repository keeps — answer it to
// HQ's host and never to another (a redirect, or the helper kept under
// another scope).
func TestLeaseAcquire_HQCredentialAnswersOnlyHQ(t *testing.T) {
	t.Parallel()
	requireGit(t)
	fx := newLeaseFixture(t)
	inAMate(fx)
	ssh := &leaseSSH{}
	fx.deps.SSH = ssh
	fx.swarm.acquired = acquiredAs(3)
	if _, err := LeaseAcquire(context.Background(), fx.deps, testPoolHost, leaseHQRepo, "feat"); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	cmd := ssh.checkouts()[0].command
	session := map[string]string{PoolHQCredentialEnvKey: poolMateCredential}
	if user, password, _ := inlineAnswer(t, cmd, session, leaseHQRepo); user != "mate" || password != poolMateCredential {
		t.Errorf("the fetch answers HQ with %q / %q, want mate with the Mate credential", user, password)
	}
	for what, ask := range map[string]func() (string, string, bool){
		"the fetch's helper":              func() (string, string, bool) { return inlineAnswer(t, cmd, session, foreignRepo) },
		"the helper the repository keeps": func() (string, string, bool) { return persistedHelperAnswer(t, cmd, session, foreignRepo) },
	} {
		if _, password, answered := ask(); answered && strings.Contains(password, poolMateCredential) {
			t.Errorf("%s answers %s with the Mate credential", what, foreignRepo)
		}
	}
}

// TestLeaseCheckout_ForeignHostAsksForACredential_NamesTheTokensHost: a
// private repository on a host the pool's GIT_TOKEN is not for fails at its
// fetch with git's "terminal prompts disabled". It is no missing token — the
// pool holds one, for another host — so the failure is PREREQUISITE_MISSING
// naming the host the token answers, never GIT_TOKEN_MISSING, whose contract
// asks the person for a token.
func TestLeaseCheckout_ForeignHostAsksForACredential_NamesTheTokensHost(t *testing.T) {
	t.Parallel()
	fx := newLeaseFixture(t)
	fx.swarm.acquired = acquiredAs(3)
	if err := storePoolDefaultRepo(fx.stateDir, testPoolHost, testRepo); err != nil {
		t.Fatal(err)
	}
	fx.deps.SSH = &leaseSSH{fn: func(_, command string) ([]byte, error) {
		if !strings.Contains(command, "fetch -q --prune origin") {
			return nil, nil
		}
		out := "fatal: could not read Username for 'https://git.attacker.example.invalid': terminal prompts disabled"
		return []byte(out), sshFailure(out, exitError(t, 128))
	}}
	_, err := LeaseAcquire(context.Background(), fx.deps, testPoolHost, foreignRepo, "feat")
	var pe *platform.PlatformError
	if !errors.As(err, &pe) || pe.Code != platform.ErrPrerequisiteMissing {
		t.Fatalf("err = %v, want PREREQUISITE_MISSING", err)
	}
	for _, want := range []string{"git.attacker.example.invalid", "github.com", "Never ask"} {
		if !strings.Contains(pe.Suggestion, want) {
			t.Errorf("suggestion lacks %q: %s", want, pe.Suggestion)
		}
	}
}
