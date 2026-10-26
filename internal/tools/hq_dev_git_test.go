// Tests for: tools/hq_dev_git.go — a Mate's start and every new Git
// credential HQ sends keep each wired dev service's git on this Mate's HQ.
package tools

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/workflow"
)

// retiredHQ is an HQ the pair was wired at before the Mate moved to the one
// it links to now: nothing answers there.
const retiredHQ = "https://retired-hq.invalid"

// retireHQ leaves the lab's wired pair as a zcp from before the move left it:
// origin, the persisted helper and the record all on retiredHQ, with no
// record of the credential the dev service holds or of how its code is kept.
func (l *hqLab) retireHQ() {
	l.t.Helper()
	retired := retiredHQ + "/git/" + labApp + "/appdev.git"
	current := l.git("remote", "get-url", "origin")
	l.git("config", "--unset-all", "credential."+hqScopeOf(l.t, current)+".helper")
	l.git("config", "credential."+retiredHQ+".helper", "!f() { echo username=mate; echo password=$GIT_TOKEN; }; f")
	l.git("remote", "set-url", "origin", retired)
	if err := workflow.UpdateServiceMeta(l.stateDir, "appdev", func(m *workflow.ServiceMeta) error {
		m.RemoteURL = retired
		m.HQ.CredentialFingerprint, m.HQ.Origin = "", nil
		return nil
	}); err != nil {
		l.t.Fatal(err)
	}
}

// hqScopeOf is the scope git keeps a helper for remote's host under.
func hqScopeOf(t *testing.T, remote string) string {
	t.Helper()
	u, err := url.Parse(remote)
	if err != nil {
		t.Fatal(err)
	}
	return u.Scheme + "://" + u.Host
}

// gitConfig is every value the pair's checkout holds for key, "" for none.
func (l *hqLab) gitConfig(key string) string {
	l.t.Helper()
	out, _ := exec.CommandContext(l.t.Context(), "git", "-C", l.pair, "config", "--get-all", key).Output() //nolint:gosec // G204: a test's own git
	return strings.TrimSpace(string(out))
}

// mateStart is the Mate's start as it reaches the pair's dev service.
func (l *hqLab) mateStart() []string {
	l.t.Helper()
	lines, _ := startDevGit(l.t.Context(), l.mock, l.hq.srv.Client(), l.ssh, l.rt, l.stateDir)
	return lines
}

// TestDevService_WiredAtARetiredHQIsMovedToTheCurrentOne: a pair wired
// before its Mate moved HQ has origin, git's helper and its record on the
// retired one and holds a credential HQ no longer takes. The Mate's start —
// and develop's start, the repository pass — points origin at the pair's
// repository on the current HQ, gives git a helper for it and none for the
// retired one, and hands the dev service the Mate's current credential, so
// plain git in its sessions reaches HQ.
func TestDevService_WiredAtARetiredHQIsMovedToTheCurrentOne(t *testing.T) {
	t.Parallel()
	mateStart := func(l *hqLab) { l.mateStart() }
	developStart := func(l *hqLab) { l.developStart() }
	for _, tc := range []struct {
		name  string
		start func(*hqLab)
		// refused: a push with the revoked credential marked the pair.
		refused bool
	}{
		{name: "the Mate's start", start: mateStart},
		{name: "develop's start", start: developStart},
		{name: "the Mate's start, a pair a refused push marked", start: mateStart, refused: true},
		{name: "develop's start, a pair a refused push marked", start: developStart, refused: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			const rotated = "the-credential-after-the-move"
			lab := newHQLab(t)
			lab.wire()
			want := lab.git("remote", "get-url", "origin")
			lab.retireHQ()
			lab.relink(rotated)
			if tc.refused {
				degradeGitPushStateToBroken(lab.stateDir, "appdev")
			}

			tc.start(lab)

			if got := lab.git("remote", "get-url", "origin"); got != want {
				t.Errorf("origin = %q, want the repository on the current HQ %q", got, want)
			}
			if got := lab.meta().RemoteURL; got != want {
				t.Errorf("the record's remote = %q, want %q", got, want)
			}
			if got := lab.gitConfig("credential." + retiredHQ + ".helper"); got != "" {
				t.Errorf("git still holds a helper for the retired HQ: %q", got)
			}
			if held := lab.ssh.gitToken(t.Context(), "svc-appdev"); held != rotated {
				t.Errorf("the dev service holds %q, want the Mate's current credential", held)
			}
			if out, err := lab.ssh.ExecSSH(t.Context(), "appdev", "cd /var/www && GIT_TERMINAL_PROMPT=0 git fetch -q origin"); err != nil {
				t.Errorf("plain git in a dev service session cannot reach HQ: %v\n%s", err, out)
			}
			assertNoSecretOnDisk(t, lab.stateDir, rotated)
		})
	}
}

// devGitDeps is the lab's Mate as its service reaches it.
func (l *hqLab) devGitDeps(context.Context) (devGitDeps, bool) {
	return devGitDeps{client: l.mock, httpClient: l.hq.srv.Client(), ssh: l.ssh, rt: l.rt, stateDir: l.stateDir}, true
}

// TestMateService_EveryCredentialHQSendsReachesTheDevService: HQ sends the
// Mate a new Git credential on every link it opens and revokes the one
// before. The Mate's service hands each one to the wired pair's dev service
// at its next look, with no person and no agent pass, so plain git in its
// sessions keeps reaching HQ; a look with nothing new costs the dev service
// nothing.
func TestMateService_EveryCredentialHQSendsReachesTheDevService(t *testing.T) {
	t.Parallel()
	lab := newHQLab(t)
	lab.wire()
	ticks := make(chan time.Time)
	done := make(chan struct{})
	go func() {
		defer close(done)
		keepMateDevGit(t.Context(), lab.devGitDeps, time.Time{}, ticks, func(string) {})
	}()
	// look lets the service take one look and waits until it took it: the
	// second send is taken only once the first look is over.
	look := func() {
		ticks <- time.Time{}
		ticks <- time.Time{}
	}
	// The Mate's start is over once the service takes its first tick.
	ticks <- time.Time{}

	for _, credential := range []string{"sent-on-the-next-link", "sent-on-the-link-after"} {
		lab.relink(credential)
		look()
		if held := lab.ssh.gitToken(t.Context(), "svc-appdev"); held != credential {
			t.Errorf("the dev service holds %q, want %q", held, credential)
		}
		if out, err := lab.ssh.ExecSSH(t.Context(), "appdev", "cd /var/www && GIT_TERMINAL_PROMPT=0 git fetch -q origin"); err != nil {
			t.Errorf("plain git in a dev service session cannot reach HQ: %v\n%s", err, out)
		}
	}

	lab.ssh.mu.Lock()
	before := len(lab.ssh.commands)
	lab.ssh.mu.Unlock()
	look()
	lab.ssh.mu.Lock()
	idle := lab.ssh.commands[before:]
	lab.ssh.mu.Unlock()
	if len(idle) != 0 {
		t.Errorf("a look with no new credential ran %q on the dev service", idle)
	}

	close(ticks)
	<-done
}

// TestMateService_ACredentialTheServerHasNotYetReceivedIsHandedOnOnceItLands:
// the Mate's service starts before its server links to HQ, so the file holds
// no credential yet. Nothing is written onto the dev service then; the
// credential the link brings reaches it at the next look.
func TestMateService_ACredentialTheServerHasNotYetReceivedIsHandedOnOnceItLands(t *testing.T) {
	t.Parallel()
	const linked = "sent-when-the-server-linked"
	lab := newHQLab(t)
	lab.wire()
	if err := os.Remove(lab.rt.HQ.GitCredentialPath); err != nil {
		t.Fatal(err)
	}
	ticks := make(chan time.Time, 1)
	ticks <- time.Time{}
	close(ticks)
	looks := 0
	deps := func(ctx context.Context) (devGitDeps, bool) {
		if looks++; looks == 2 {
			if held := lab.ssh.gitToken(ctx, "svc-appdev"); held != labCredential {
				t.Errorf("before the server linked, the dev service was given %q", held)
			}
			lab.relink(linked)
		}
		return lab.devGitDeps(ctx)
	}

	keepMateDevGit(t.Context(), deps, time.Time{}, ticks, func(string) {})

	if held := lab.ssh.gitToken(t.Context(), "svc-appdev"); held != linked {
		t.Errorf("the dev service holds %q, want the credential the link brought", held)
	}
}

// TestMateService_AStartThatDidNotSettleIsTakenAgainOnlyOnceItsWaitIsOver: HQ
// does not say how the pair's code is kept at the Mate's start, so the start
// is taken again — not at every look, which would reach the dev service every
// few seconds while it stays stopped, but once the wait after it is over.
func TestMateService_AStartThatDidNotSettleIsTakenAgainOnlyOnceItsWaitIsOver(t *testing.T) {
	t.Parallel()
	lab := newHQLab(t)
	lab.wire()
	if err := workflow.UpdateServiceMeta(lab.stateDir, "appdev", func(m *workflow.ServiceMeta) error {
		m.HQ.Origin = nil
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	lab.hq.answerWith(http.StatusBadGateway, "bad_gateway", 1, func(r *http.Request) bool { return r.URL.Path == "/api/mate/self" })
	started := time.Date(2026, 10, 26, 20, 0, 0, 0, time.UTC)
	ticks := make(chan time.Time, 2)
	ticks <- started.Add(devGitLookEvery)
	ticks <- started.Add(hqWaitBackoff)
	close(ticks)
	var sessions []int
	deps := func(ctx context.Context) (devGitDeps, bool) {
		lab.ssh.mu.Lock()
		sessions = append(sessions, len(lab.ssh.commands))
		lab.ssh.mu.Unlock()
		return lab.devGitDeps(ctx)
	}

	keepMateDevGit(t.Context(), deps, started, ticks, func(string) {})

	if sessions[2] != sessions[1] {
		t.Errorf("a look within the wait reached the dev service %d time(s)", sessions[2]-sessions[1])
	}
	if lab.meta().HQ.Origin == nil {
		t.Error("the start taken again once the wait was over did not record how the code is kept")
	}
}
