// Tests for: ops/lease_warm_clone.go and the steps of lease_warm.go,
// lease_group.go and lease_checkout.go that use it — a pool's warm container
// already holds the pool's repository: right after zcp creates a pool's warm
// container, keeps one at a release or finds one at an acquire, it starts —
// detached in the container — a clone of the repository the pool's last lease
// checked out, only into a container still free, only for a pool zcp leased a
// repository from, and for a repository on the Mate's HQ only once the pool's
// ZCP_HQ_GIT_TOKEN holds this Mate's HQ credential; status reports what it did
// (docs/spec-workflows.md §8 DP-32).
package ops

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/platform"
)

// warmCloneSSH is the pool's containers as zcp reaches them over ssh: a clone's
// launch answers started (or answer, when set), the release guard finds
// nothing unsaved, and everything else answers nothing. onLaunch, when set,
// runs at each launch with the host it went to.
type warmCloneSSH struct {
	leaseSSH
	answer   string
	onLaunch func(host string)
}

func newWarmCloneSSH() *warmCloneSSH {
	s := &warmCloneSSH{}
	s.fn = func(host, command string) ([]byte, error) {
		switch {
		case isWarmCloneLaunch(command):
			if s.onLaunch != nil {
				s.onLaunch(host)
			}
			if s.answer != "" {
				return []byte(s.answer), nil
			}
			return []byte("@@zcp-warm started\n"), nil
		case strings.Contains(command, "rev-list --count"):
			return []byte("0\n"), nil
		}
		return nil, nil
	}
	return s
}

func isWarmCloneLaunch(command string) bool { return strings.Contains(command, "@@zcp-warm started") }

// launches are the clone launches, in order.
func (s *warmCloneSSH) launches() []sshCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []sshCall
	for _, c := range s.calls {
		if isWarmCloneLaunch(c.command) {
			out = append(out, c)
		}
	}
	return out
}

func newWarmCloneFixture(t *testing.T, orch *warmOrch) (*warmFixture, *warmCloneSSH) {
	t.Helper()
	wf := newWarmFixture(t, orch)
	ssh := newWarmCloneSSH()
	wf.deps.SSH = ssh
	return wf, ssh
}

func warmCloneOf(t *testing.T, wf *warmFixture) *LeaseWarmClone {
	t.Helper()
	status, err := LeaseStatus(context.Background(), wf.deps, testPoolHost)
	if err != nil {
		t.Fatal(err)
	}
	return poolWarmOf(t, status).Clone
}

// TestLeaseWarmClone_ContainerTheTopUpCreated_GetsThePoolsRepository: after an
// acquire the pool's new warm container, once the orchestrator's create
// answers, gets the clone of the repository the acquire checked out — the URL
// as it was given (DP-18) — and status reports it started there.
func TestLeaseWarmClone_ContainerTheTopUpCreated_GetsThePoolsRepository(t *testing.T) {
	t.Parallel()
	orch := newWarmOrch(4)
	orch.add(3, "", true, false, 1)
	wf, ssh := newWarmCloneFixture(t, orch)

	wf.acquire(t, "feat-a")

	launches := ssh.launches()
	if len(launches) != 1 {
		t.Fatalf("clone launches = %+v, want one, into the created container", launches)
	}
	if launches[0].hostname != nodeHostname(4) {
		t.Errorf("the clone went to %s, want the created container %s", launches[0].hostname, nodeHostname(4))
	}
	if !strings.Contains(launches[0].command, testRepo) || !strings.Contains(launches[0].command, leaseWarmRepoKey(testRepo)) {
		t.Errorf("the clone does not name the acquired repository as given: %s", launches[0].command)
	}
	clone := warmCloneOf(t, wf)
	if clone == nil || clone.Outcome != LeaseWarmCloneStarted || clone.Container != nodeHostname(4) || clone.Repo != "https://github.com/acme/app" {
		t.Errorf("status clone = %+v, want started in %s for the repository's identity", clone, nodeHostname(4))
	}
}

// TestLeaseWarmClone_ContainerAReleaseKeeps_GetsItAfterTheReset: a release
// that keeps its container as the pool's warm one starts the clone there once
// the reset release made it free again — a reset takes any clone away.
func TestLeaseWarmClone_ContainerAReleaseKeeps_GetsItAfterTheReset(t *testing.T) {
	t.Parallel()
	orch := newWarmOrch(1) // no room for a create: the released container is the warm one
	orch.add(3, "", true, false, 1)
	wf, ssh := newWarmCloneFixture(t, orch)
	lease := wf.acquire(t, "feat-a")
	var lockAtLaunch []string
	ssh.onLaunch = func(string) {
		lock, _, _ := orch.state("c-3")
		lockAtLaunch = append(lockAtLaunch, lock)
	}

	view := wf.release(t, lease.Alias)

	if !strings.Contains(view.Note, "kept as the pool's warm container") {
		t.Fatalf("release note %q: want the container kept warm", view.Note)
	}
	launches := ssh.launches()
	if len(launches) != 1 || launches[0].hostname != nodeHostname(3) {
		t.Fatalf("clone launches = %+v, want one into the kept %s", launches, nodeHostname(3))
	}
	if len(lockAtLaunch) != 1 || lockAtLaunch[0] != "" {
		t.Errorf("the container's lock at the launch = %v, want free (after the reset release)", lockAtLaunch)
	}
}

// TestLeaseWarmClone_FoundWarmContainer_GetsIt: an acquire that leaves the
// pool its warm container — no create — starts the clone in that one (a zcp
// restarted since it was created, or one created before any lease).
func TestLeaseWarmClone_FoundWarmContainer_GetsIt(t *testing.T) {
	t.Parallel()
	orch := newWarmOrch(4)
	orch.add(3, "", true, false, 1)
	orch.add(6, "", true, false, 1)
	wf, ssh := newWarmCloneFixture(t, orch)

	wf.acquire(t, "feat-a")

	if _, _, creates := orch.log(); len(creates) != 0 {
		t.Errorf("creates = %v, want none: the pool kept its warm container", creates)
	}
	launches := ssh.launches()
	if len(launches) != 1 || launches[0].hostname != nodeHostname(6) {
		t.Errorf("clone launches = %+v, want one into the warm %s", launches, nodeHostname(6))
	}
}

// TestLeaseWarmClone_PoolNeverLeasedARepository_ClonesNothing: a pool zcp
// never checked a repository out on — here a lease it holds from before it
// kept such a record — gets no clone, its warm container stays as the image
// left it.
func TestLeaseWarmClone_PoolNeverLeasedARepository_ClonesNothing(t *testing.T) {
	t.Parallel()
	orch := newWarmOrch(4)
	wf, ssh := newWarmCloneFixture(t, orch)
	wf.hold(t, 5, true)

	view := wf.release(t, "devpool-5")

	if !strings.Contains(view.Note, "kept as the pool's warm container") {
		t.Fatalf("release note %q: want the container kept warm", view.Note)
	}
	if launches := ssh.launches(); len(launches) != 0 {
		t.Errorf("clone launches = %+v, want none for a pool zcp never leased a repository from", launches)
	}
	if clone := warmCloneOf(t, wf); clone != nil {
		t.Errorf("status clone = %+v, want none", clone)
	}
}

// TestLeaseWarmClone_ContainerTakenMeanwhile_Untouched: a container another
// acquire took before zcp could start the clone — read by id right before —
// is never written into; status says the clone was skipped and why.
func TestLeaseWarmClone_ContainerTakenMeanwhile_Untouched(t *testing.T) {
	t.Parallel()
	orch := newWarmOrch(4)
	orch.add(3, "", true, false, 1)
	orch.onCreate = func(o *warmOrch, created []*warmBox) {
		o.mu.Lock()
		defer o.mu.Unlock()
		created[0].c.LockState, created[0].lease = leaseLockReserved, "another consumer's lease"
	}
	wf, ssh := newWarmCloneFixture(t, orch)

	wf.acquire(t, "feat-a")

	if launches := ssh.launches(); len(launches) != 0 {
		t.Errorf("clone launches = %+v, want none into a container another holder took", launches)
	}
	clone := warmCloneOf(t, wf)
	if clone == nil || clone.Outcome != LeaseWarmCloneSkipped || clone.Container != nodeHostname(4) || !strings.Contains(clone.Note, "no longer free") {
		t.Errorf("status clone = %+v, want skipped for %s, no longer free", clone, nodeHostname(4))
	}
}

// TestLeaseWarmClone_LaunchAnswers_RecordedAsTheyAre: what the container
// answers is what status reports — started, a tree that already holds a
// repository or an image without the tools (skipped, saying which), and a
// launch ssh could not run (failed).
func TestLeaseWarmClone_LaunchAnswers_RecordedAsTheyAre(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, answer string
		err          error
		outcome      string
		note         string
	}{
		{"started", "@@zcp-warm started\n", nil, LeaseWarmCloneStarted, ""},
		{"a repository is there", "@@zcp-warm present\n", nil, LeaseWarmCloneSkipped, "already holds a repository"},
		{"no flock in the image", "@@zcp-warm tools\n", nil, LeaseWarmCloneSkipped, "flock"},
		{"ssh failed", "", sshFailure("connection refused", errors.New("exit status 255")), LeaseWarmCloneFailed, "connection refused"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			orch := newWarmOrch(4)
			orch.add(3, "", true, false, 1)
			wf, ssh := newWarmCloneFixture(t, orch)
			ssh.answer = tt.answer
			if tt.err != nil {
				base := ssh.fn
				ssh.setHandler(func(host, command string) ([]byte, error) {
					if isWarmCloneLaunch(command) {
						return nil, tt.err
					}
					return base(host, command)
				})
			}

			wf.acquire(t, "feat-a")

			clone := warmCloneOf(t, wf)
			if clone == nil || clone.Outcome != tt.outcome || !strings.Contains(clone.Note, tt.note) {
				t.Errorf("status clone = %+v, want %s noting %q", clone, tt.outcome, tt.note)
			}
		})
	}
}

// TestLeaseWarmClone_SSHFailingAsTheAliasFileIsRewritten_TriedAgain: ssh
// itself failing (exit 255) — as it does for every call while a lease's
// release or acquire reinstalls the ssh alias file, which the release that
// keeps a warm container does right beside the clone (live, 2026-10-24: "Can't
// open user config file /etc/ssh/ssh_config.d/zcp-leases.conf: Permission
// denied") — or a try that ran out of time before ssh got in — as the first
// connection to a container its create just answered did, before the
// container admitted zcp (live, 2026-10-24: killed after 30 s, the container's
// first accepted connection a second later) — is tried again: the launch
// changes nothing until it runs, and a second launch of one that did run finds
// the clone there. A remote command's own failure is not retried, and the
// tries are bounded.
func TestLeaseWarmClone_SSHFailingAsTheAliasFileIsRewritten_TriedAgain(t *testing.T) {
	t.Parallel()
	refused := func(t *testing.T) error {
		t.Helper()
		return sshFailure("Can't open user config file /etc/ssh/ssh_config.d/zcp-leases.conf: Permission denied", exitError(t, 255))
	}
	timedOut := func(*testing.T) error {
		return sshFailure("", fmt.Errorf("ssh stopped: %w (%w)", context.DeadlineExceeded, errors.New("signal: killed")))
	}
	remote := func(t *testing.T) error {
		t.Helper()
		return sshFailure("mkdir: cannot create directory", exitError(t, 1))
	}
	tests := []struct {
		name      string
		failure   func(*testing.T) error
		failures  int
		wantTries int
		outcome   string
	}{
		{"ssh fails once", refused, 1, 2, LeaseWarmCloneStarted},
		{"ssh keeps failing", refused, 99, leaseWarmLaunchTries, LeaseWarmCloneFailed},
		{"a try runs out of time before ssh gets in", timedOut, 1, 2, LeaseWarmCloneStarted},
		{"the remote command fails", remote, 99, 1, LeaseWarmCloneFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			orch := newWarmOrch(4)
			orch.add(3, "", true, false, 1)
			wf, ssh := newWarmCloneFixture(t, orch)
			wf.deps.Warm.LaunchRetry = time.Millisecond
			failure := tt.failure(t)
			tries := 0
			base := ssh.fn
			ssh.setHandler(func(host, command string) ([]byte, error) {
				if isWarmCloneLaunch(command) {
					tries++
					if tries <= tt.failures {
						return nil, failure
					}
				}
				return base(host, command)
			})

			wf.acquire(t, "feat-a")

			if tries != tt.wantTries {
				t.Errorf("launch tries = %d, want %d", tries, tt.wantTries)
			}
			if clone := warmCloneOf(t, wf); clone == nil || clone.Outcome != tt.outcome {
				t.Errorf("status clone = %+v, want %s", clone, tt.outcome)
			}
		})
	}
}

// TestLeaseWarmClone_HQRepository_OnlyWithTheMateCredentialOnThePool: a
// repository on the Mate's HQ is fetched with this Mate's HQ credential on the
// pool's ZCP_HQ_GIT_TOKEN, which an acquire writes (DP-16): the clone starts only when
// zcp's record says the pool holds the credential the enrollment holds now —
// never outside a Mate, never before an acquire wrote it, never after the Mate
// enrolled again — and never writes it itself.
func TestLeaseWarmClone_HQRepository_OnlyWithTheMateCredentialOnThePool(t *testing.T) {
	t.Parallel()
	current := hq.GitCredentialFingerprint(poolMateCredential)
	tests := []struct {
		name      string
		inAMate   bool
		recordFor string
		recorded  string
		wantClone bool
	}{
		{"outside a Mate", false, "svc-pool", current, false},
		{"no credential written yet", true, "", "", false},
		{"the Mate enrolled again", true, "svc-pool", hq.GitCredentialFingerprint(poolOldCredential), false},
		{"the pool was re-created", true, "svc-gone", current, false},
		{"the pool holds the credential", true, "svc-pool", current, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			orch := newWarmOrch(4)
			wf, ssh := newWarmCloneFixture(t, orch)
			wf.deps.HQAddress = func() string { return leaseHQURL }
			if tt.inAMate {
				wf.deps.PoolCredential = PoolHQCredential{Read: func() (string, bool) { return poolMateCredential, true }}
			}
			if err := storeWarmCloneRepo(wf.stateDir, testPoolHost, leaseHQRepo); err != nil {
				t.Fatal(err)
			}
			if tt.recordFor != "" {
				writePoolCredentialRecord(t, wf.stateDir, testPoolHost, tt.recordFor, tt.recorded)
			}
			wf.hold(t, 5, true)
			mock := wf.deps.Client.(*platform.Mock)

			wf.release(t, "devpool-5")

			launches := ssh.launches()
			if tt.wantClone != (len(launches) == 1) || len(launches) > 1 {
				t.Fatalf("clone launches = %+v, want a clone: %v", launches, tt.wantClone)
			}
			if tt.wantClone && !strings.Contains(launches[0].command, "username=mate") {
				t.Errorf("an HQ repository's clone must authenticate as mate: %s", launches[0].command)
			}
			if writes := mock.CallCount("CreateServiceEnvVar"); writes != 0 {
				t.Errorf("HQ credential writes = %d, want none: only an acquire writes the credential", writes)
			}
			clone := warmCloneOf(t, wf)
			if !tt.wantClone && (clone == nil || clone.Outcome != LeaseWarmCloneSkipped || !strings.Contains(clone.Note, "HQ")) {
				t.Errorf("status clone = %+v, want skipped naming HQ's credential", clone)
			}
		})
	}
}

// TestLeaseWarmClone_WithoutTheWarmPolicy_ClonesNothing: a zcp without the
// warm policy (the envelope's reader, the scale gate) starts no clone.
func TestLeaseWarmClone_WithoutTheWarmPolicy_ClonesNothing(t *testing.T) {
	t.Parallel()
	fx := newLeaseFixture(t)
	ssh := newWarmCloneSSH()
	fx.deps.SSH = ssh
	fx.swarm.acquired = acquiredAs(3)

	if _, err := LeaseAcquire(context.Background(), fx.deps, testPoolHost, testRepo, "feat"); err != nil {
		t.Fatal(err)
	}

	if launches := ssh.launches(); len(launches) != 0 {
		t.Errorf("clone launches = %+v, want none", launches)
	}
}

// TestLeaseCheckout_RemovedWarmClone_SaysSo: a checkout that removed a warm
// clone (of another repository, or unfinished) before it checked out says so
// on the lease.
func TestLeaseCheckout_RemovedWarmClone_SaysSo(t *testing.T) {
	t.Parallel()
	fx := newLeaseFixture(t)
	ssh := &leaseSSH{fn: func(_, command string) ([]byte, error) {
		if strings.Contains(command, "fetch -q --prune origin") {
			return []byte(leaseWarmCheckoutMarker + leaseWarmDiscarded + "\n"), nil
		}
		return nil, nil
	}}
	fx.deps.SSH = ssh
	fx.swarm.acquired = acquiredAs(3)

	view, err := LeaseAcquire(context.Background(), fx.deps, testPoolHost, testRepo, "feat")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(view.Note, "removed before the checkout") {
		t.Errorf("note %q: want it to say the warm clone was removed before the checkout", view.Note)
	}
}

// TestClassifyCheckoutFailure_WarmCloneThatWouldNotStop: a checkout refused
// because a warm clone would not stop or could not be removed is no
// credential or repository problem: the container went back with a reset
// (which takes the clone away), so the step is to acquire again.
func TestClassifyCheckoutFailure_WarmCloneThatWouldNotStop(t *testing.T) {
	t.Parallel()
	for _, msg := range []string{leaseWarmBusy, leaseWarmUnreadable} {
		code, suggestion := classifyCheckoutFailure(sshFailure(msg+"\n", exitError(t, 4)), false)
		if code != platform.ErrSSHDeployFailed || !strings.Contains(suggestion, "acquire again") || strings.Contains(suggestion, "repo URL") {
			t.Errorf("%q: code %s, suggestion %q — want SSH_DEPLOY_FAILED saying to acquire again", msg, code, suggestion)
		}
	}
}

// TestLeaseWarmCloneCommand_CredentialNeverInIt: the clone authenticates as a
// checkout does — the inline helper answering from the session's GIT_TOKEN,
// or as HQ's user `mate` from the pool's HQ credential for a repository on the
// Mate's HQ — and persists no helper, identity or credential: the checkout
// that takes it does.
func TestLeaseWarmCloneCommand_CredentialNeverInIt(t *testing.T) {
	t.Parallel()
	plain := leaseWarmCloneCommand(LeaseWorkDir, testRepo, leaseHQURL, leaseHelperScope(testRepo))
	onHQ := leaseWarmCloneCommand(LeaseWorkDir, leaseHQRepo, leaseHQURL, "")
	for name, c := range map[string]string{"plain": plain, "hq": onHQ} {
		reads := "$" + GitTokenEnvKey
		if name == "hq" {
			reads = "$" + PoolHQCredentialEnvKey
		}
		if !strings.Contains(c, "fetch -q --prune origin") || !strings.Contains(c, reads) {
			t.Errorf("%s: the clone must fetch through the session-env helper reading %s: %s", name, reads, c)
		}
		for _, never := range []string{"git config", poolMateCredential, testPoolToken, "credential.https"} {
			if strings.Contains(c, never) {
				t.Errorf("%s: the clone carries %q: %s", name, never, c)
			}
		}
	}
	if !strings.Contains(plain, "username=oauth2") || !strings.Contains(onHQ, "username=mate") {
		t.Error("the plain clone answers oauth2 and the HQ clone answers mate")
	}
}

// TestLeaseWarmRepoKey_IsTheRepositorysIdentity: the key a clone and a
// checkout compare is the repository's identity (topology.CanonicalRepoURL,
// case folded), so `…/repo.git` and `…/repo` are one repository.
func TestLeaseWarmRepoKey_IsTheRepositorysIdentity(t *testing.T) {
	t.Parallel()
	if leaseWarmRepoKey("https://github.com/acme/app.git") != leaseWarmRepoKey("https://GitHub.com/acme/app/") {
		t.Error("one repository spelled two ways must have one key")
	}
	if leaseWarmRepoKey("https://github.com/acme/app") == leaseWarmRepoKey("https://github.com/acme/other") {
		t.Error("two repositories must have two keys")
	}
}
