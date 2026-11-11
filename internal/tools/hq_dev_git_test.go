// Tests for: tools/hq_dev_git.go — a Mate's start and every new Git
// credential HQ sends keep each wired dev service's git on this Mate's HQ.
package tools

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/zeropsio/zcp/internal/ops"
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
		m.HQ.Origin = nil
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
	l.snapshotMate(l.t.Context())
	lines, _ := startDevGit(l.t.Context(), l.hq.srv.Client(), l.ssh, l.rt, l.stateDir)
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
		// refused: a refused push marked the pair.
		refused bool
	}{
		{name: "the Mate's start", start: mateStart},
		{name: "develop's start", start: developStart},
		{name: "the Mate's start, a pair a refused push marked", start: mateStart, refused: true},
		{name: "develop's start, a pair a refused push marked", start: developStart, refused: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			lab := newHQLab(t)
			lab.wire()
			want := lab.git("remote", "get-url", "origin")
			lab.retireHQ()
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
			if held := lab.ssh.gitToken(t.Context(), "svc-appdev"); held != "" {
				t.Errorf("the dev service holds GIT_TOKEN %q, want no Git credential stored there", held)
			}
			if out, err := lab.devGit().ExecSSH(t.Context(), "appdev", "cd /var/www && git fetch -q origin"); err != nil {
				t.Errorf("git in the dev service cannot reach the current HQ through the relay: %v\n%s", err, out)
			}
			assertNoSecretOnDisk(t, lab.stateDir, labKey)
		})
	}
}

// devGitDeps is the lab's Mate as its service reaches it.
func (l *hqLab) devGitDeps(ctx context.Context) (devGitDeps, bool) {
	l.snapshotMate(ctx)
	return devGitDeps{client: l.mock, httpClient: l.hq.srv.Client(), ssh: l.ssh, rt: l.rt, stateDir: l.stateDir}, true
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
		d, ok := lab.devGitDeps(ctx)
		if len(sessions) == 1 {
			if err := os.Remove(filepath.Join(filepath.Dir(lab.rt.HQ.StatePath), "state.json")); err != nil {
				t.Fatal(err)
			}
		}
		return d, ok
	}

	keepMateDevGit(t.Context(), deps, started, ticks, nil, func(string) {})

	if sessions[2] != sessions[1] {
		t.Errorf("a look within the wait reached the dev service %d time(s)", sessions[2]-sessions[1])
	}
	if lab.meta().HQ.Origin == nil {
		t.Error("the start taken again once the wait was over did not record how the code is kept")
	}
}

// TestRepositoryPass_APairARefusedPushMarkedKeepsItsBackoffAfterANewCredential:
// a new credential takes a configured pair outside the pass's backoff, since
// renewing it is one write. A pair a refused push marked is wired again
// instead — asking HQ for its repository — so it keeps the backoff: passes
// that agent tool calls drive in a burst ask HQ once.
func TestRepositoryPass_APairARefusedPushMarkedKeepsItsBackoffAfterANewCredential(t *testing.T) {
	t.Parallel()
	lab := newHQLab(t)
	lab.wire()
	degradeGitPushStateToBroken(lab.stateDir, "appdev")
	lab.relink("sent-on-the-next-link")
	lab.hq.answerWith(http.StatusBadGateway, "bad_gateway", 100, func(r *http.Request) bool { return r.URL.Path == "/api/mate/repos" })
	elapseHQBackoff(t, lab.stateDir)

	lab.wire()
	first := lab.hq.callCount("POST /api/mate/repos")
	lab.wire()

	if got := lab.hq.callCount("POST /api/mate/repos"); got != first {
		t.Errorf("a pass inside the backoff asked HQ for the repository again: %d calls, want %d", got, first)
	}
}

// TestDevService_ARemoteThatIsNotItsHQRepositoryIsNeverMoved: only an
// address of the pair's own repository on an HQ — `/git/[<org>/]<app>/<repo>.git`
// — is a retired wiring. A record naming any other remote, a GitHub
// repository an adopt found, is the person's, and its origin stays.
func TestDevService_ARemoteThatIsNotItsHQRepositoryIsNeverMoved(t *testing.T) {
	t.Parallel()
	for _, remote := range []string{
		"https://github.com/acme/app.git",
		retiredHQ + "/git/another-app/appdev.git",
		retiredHQ + "/git/" + labApp + "/another-repo.git",
	} {
		t.Run(remote, func(t *testing.T) {
			t.Parallel()
			lab := newHQLab(t)
			lab.wire()
			lab.git("remote", "set-url", "origin", remote)
			if err := workflow.UpdateServiceMeta(lab.stateDir, "appdev", func(m *workflow.ServiceMeta) error {
				m.RemoteURL = remote
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			degradeGitPushStateToBroken(lab.stateDir, "appdev")

			lab.mateStart()
			lab.developStart()

			if got := lab.git("remote", "get-url", "origin"); got != remote {
				t.Errorf("origin = %q, want %q kept", got, remote)
			}
			if got := lab.meta().RemoteURL; got != remote {
				t.Errorf("the record's remote = %q, want %q kept", got, remote)
			}
		})
	}
}

// unansweringSSH stands in for a dev service that never answers: it records
// how long each command was given and fails it.
type unansweringSSH struct {
	ops.SSHDeployer
	mu     sync.Mutex
	within []time.Duration
}

func (d *unansweringSSH) ExecSSH(ctx context.Context, _, _ string) ([]byte, error) {
	deadline, ok := ctx.Deadline()
	d.mu.Lock()
	defer d.mu.Unlock()
	if !ok {
		d.within = append(d.within, -1)
	} else {
		d.within = append(d.within, time.Until(deadline))
	}
	return nil, errors.New("the dev service does not answer")
}

// TestMateService_EveryCommandOnADevServiceIsBounded: a dev service that
// does not answer holds the Mate's service — and the pair's lock — for
// devGitSSHWithin at most, never for an SSH session's own long bound, so the
// credential keeps following for every other pair.
func TestMateService_EveryCommandOnADevServiceIsBounded(t *testing.T) {
	t.Parallel()
	lab := newHQLab(t)
	lab.wire()
	lab.retireHQ()
	ssh := &unansweringSSH{}
	ticks := make(chan time.Time)
	close(ticks)
	deps := func(ctx context.Context) (devGitDeps, bool) {
		d, ok := lab.devGitDeps(ctx)
		d.ssh = ssh
		return d, ok
	}

	client := lab.hq.srv.Client()
	transport := client.Transport.(*http.Transport).Clone()
	transport.DisableKeepAlives = true
	client.Transport = transport
	synctest.Test(t, func(t *testing.T) {
		keepMateDevGit(t.Context(), deps, time.Time{}, ticks, nil, func(string) {})
	})

	if len(ssh.within) == 0 {
		t.Fatal("the start ran no command on the dev service")
	}
	for _, within := range ssh.within {
		if within < 0 || within > devGitSSHWithin {
			t.Errorf("a command was given %v, want at most %v", within, devGitSSHWithin)
		}
	}
}
