package ops

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/platform"
)

// syncFixtureOnHQ is newSyncFixture with both leases of a repository on the
// Mate's HQ; mate makes zcp a Mate, which writes the pool's HQ credential.
func syncFixtureOnHQ(t *testing.T, mate bool) (*groupFixture, *syncRemote, *platform.Mock) {
	t.Helper()
	gf, remote := newSyncFixture(t, syncBranch)
	gf.deps.HQAddress = func() string { return leaseHQURL }
	if mate {
		gf.deps.PoolCredential = PoolHQCredential{Read: func() (string, bool) { return poolMateCredential, true }}
	}
	for _, alias := range []string{"hqpool-3", "webpool-7"} {
		rec, err := loadLeaseRecord(gf.stateDir, alias)
		if err != nil {
			t.Fatal(err)
		}
		rec.Repo = leaseHQRepo
		if err := writeLeaseRecord(gf.stateDir, rec); err != nil {
			t.Fatal(err)
		}
	}
	mock, ok := gf.deps.Client.(*platform.Mock)
	if !ok {
		t.Fatal("the fixture's client is not a *platform.Mock")
	}
	return gf, remote, mock
}

// hqRefusedPush is the source's push answered by HQ refusing the credential
// the session presented.
func hqRefusedPush(t *testing.T) syncAnswer {
	t.Helper()
	out := "@@zcp-sync commit " + syncTo + "\nremote: invalid credentials\nfatal: Authentication failed for '" + leaseHQRepo + "/'\n"
	return syncAnswer{out: out, err: sshFailure(out, exitError(t, 128))}
}

// hqCredentialWrites are the writes of the pool's HQ credential the mock saw.
func hqCredentialWrites(t *testing.T, mock *platform.Mock, serviceID string) int {
	t.Helper()
	envs, err := mock.GetServiceEnv(context.Background(), serviceID)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range envs {
		if e.Key == PoolHQCredentialEnvKey && e.Content == poolMateCredential {
			n++
		}
	}
	return n
}

// TestLeaseSync_HQRefusesThePoolsCredential (D9, live 2026-10-27): HQ
// redeployed, the Mate's link brought a new Git credential, and a held
// lease's push failed "Authentication failed": the pool's ZCP_HQ_GIT_TOKEN
// was no longer one HQ takes, and an acquire of the held branch is reused and
// writes nothing. Inside a Mate the sync writes the Mate's current credential
// to the pool in the same call, waits for a fresh session, and pushes once
// more. When HQ refuses that too, the answer names what renews it — never
// "acquire again" — and asks no one for a token; when the write itself
// fails, the pool is marked so the next renewal writes it even where zcp's
// record names it already. Outside a Mate zcp writes no pool credential.
func TestLeaseSync_HQRefusesThePoolsCredential(t *testing.T) {
	t.Parallel()
	t.Run("renewed in the call, the push goes through", func(t *testing.T) {
		t.Parallel()
		gf, remote, mock := syncFixtureOnHQ(t, true)
		writePoolCredentialRecord(t, gf.stateDir, groupHQPool, "svc-hq", hq.GitCredentialFingerprint(poolOldCredential))
		remote.pushes = []syncAnswer{hqRefusedPush(t)}

		view, err := LeaseSync(context.Background(), gf.deps, "hqpool-3")

		if err != nil {
			t.Fatalf("sync after the renewal: %v", err)
		}
		if view.Push.Outcome == "" {
			t.Errorf("view = %+v, want the push's outcome", view)
		}
		if n := hqCredentialWrites(t, mock, "svc-hq"); n != 1 {
			t.Errorf("the pool's %s was written %d time(s) with the Mate's credential, want once", PoolHQCredentialEnvKey, n)
		}
		if n := countContaining(remote.sent("hqpool-3"), "push --porcelain"); n != 2 {
			t.Errorf("pushes = %d, want the refused one and one more after the renewal", n)
		}
		if rec, _, found := readPoolCredentialRecord(t, gf.stateDir, groupHQPool); !found || rec["fingerprint"] != hq.GitCredentialFingerprint(poolMateCredential) {
			t.Errorf("the pool's record = %v, want the Mate's current credential", rec)
		}
	})
	t.Run("refused again, the answer names what renews it", func(t *testing.T) {
		t.Parallel()
		gf, remote, mock := syncFixtureOnHQ(t, true)
		a := hqRefusedPush(t)
		remote.push = &a

		_, err := LeaseSync(context.Background(), gf.deps, "hqpool-3")

		var pe *platform.PlatformError
		if !errors.As(err, &pe) || pe.Code != platform.ErrPrerequisiteMissing {
			t.Fatalf("err = %v, want PREREQUISITE_MISSING", err)
		}
		for _, want := range []string{`zerops_workflow action="start" workflow="develop"`, "zerops_lease action=sync lease=hqpool-3"} {
			if !strings.Contains(pe.Suggestion, want) {
				t.Errorf("suggestion %q lacks %q", pe.Suggestion, want)
			}
		}
		if strings.Contains(pe.Suggestion, "Acquire again") {
			t.Errorf("suggestion %q says to acquire again: a held branch's acquire is reused and writes nothing", pe.Suggestion)
		}
		if n := countContaining(remote.sent("hqpool-3"), "push --porcelain"); n != 2 {
			t.Errorf("pushes = %d, want exactly one more after the renewal", n)
		}
		if n := hqCredentialWrites(t, mock, "svc-hq"); n != 1 {
			t.Errorf("credential writes = %d, want one", n)
		}
	})
	t.Run("the write fails, the pool is marked for renewal", func(t *testing.T) {
		t.Parallel()
		gf, remote, mock := syncFixtureOnHQ(t, true)
		writePoolCredentialRecord(t, gf.stateDir, groupHQPool, "svc-hq", hq.GitCredentialFingerprint(poolMateCredential))
		mock.WithError("CreateServiceEnvVar", platform.NewPlatformError(platform.ErrAPIError, "userData create refused", ""))
		a := hqRefusedPush(t)
		remote.push = &a

		_, err := LeaseSync(context.Background(), gf.deps, "hqpool-3")

		var pe *platform.PlatformError
		if !errors.As(err, &pe) || !strings.Contains(pe.Message+pe.Suggestion, "could not be written") {
			t.Fatalf("err = %v, want the refusal naming the write that failed", err)
		}
		pool := &platform.ServiceStack{ID: "svc-hq", Name: groupHQPool}
		if gf.deps.PoolCredential.holds(gf.stateDir, pool, hq.GitCredentialFingerprint(poolMateCredential)) {
			t.Error("a pool HQ refused still reads as holding the Mate's credential, so the next acquire would not write it")
		}
		mock.WithError("CreateServiceEnvVar", nil)
		if err := RenewPoolHQCredential(context.Background(), mock, gf.stateDir, groupHQPool, poolMateCredential); err != nil {
			t.Fatal(err)
		}
		if n := hqCredentialWrites(t, mock, "svc-hq"); n != 1 {
			t.Errorf("the renewal of the marked pool wrote %d time(s), want once", n)
		}
		if err := RenewPoolHQCredential(context.Background(), mock, gf.stateDir, groupHQPool, poolMateCredential); err != nil || hqCredentialWrites(t, mock, "svc-hq") != 1 {
			t.Errorf("a renewal after the pool was written wrote again (%v): the mark outlived the write", err)
		}
	})
	t.Run("outside a Mate nothing is written", func(t *testing.T) {
		t.Parallel()
		gf, remote, mock := syncFixtureOnHQ(t, false)
		a := hqRefusedPush(t)
		remote.push = &a

		_, err := LeaseSync(context.Background(), gf.deps, "hqpool-3")

		var pe *platform.PlatformError
		if !errors.As(err, &pe) || strings.Contains(pe.Suggestion, "Acquire again") {
			t.Errorf("err = %v, want a refusal that does not say to acquire again", err)
		}
		if n := mock.CallCount("CreateServiceEnvVar"); n != 0 {
			t.Errorf("outside a Mate the sync wrote %d variable(s)", n)
		}
		if n := countContaining(remote.sent("hqpool-3"), "push --porcelain"); n != 1 {
			t.Errorf("pushes = %d, want the one, with nothing renewed to push again with", n)
		}
	})
}

// TestLeaseSync_HQRefusesAMembersFetch_RenewsItsPoolAndFastForwards: a
// member of the group fetches the pushed branch from HQ with its own pool's
// credential; HQ refusing it renews that pool in the same call, and the
// member is fast-forwarded on the one more try.
func TestLeaseSync_HQRefusesAMembersFetch_RenewsItsPoolAndFastForwards(t *testing.T) {
	t.Parallel()
	gf, remote, mock := syncFixtureOnHQ(t, true)
	out := "fatal: Authentication failed for '" + leaseHQRepo + "/'\n"
	remote.members["webpool-7"] = []syncAnswer{{out: out, err: sshFailure(out, exitError(t, 128))}}

	view, err := LeaseSync(context.Background(), gf.deps, "hqpool-3")

	if err != nil {
		t.Fatal(err)
	}
	if m := syncMemberOf(t, view, "webpool-7"); m.Outcome != LeaseSyncFastForwarded {
		t.Errorf("webpool-7 = %+v, want fast-forwarded after its pool's renewal", m)
	}
	if n := hqCredentialWrites(t, mock, "svc-web"); n != 1 {
		t.Errorf("the member's pool was written %d time(s), want once", n)
	}
}

func countContaining(cmds []string, sub string) int {
	n := 0
	for _, c := range cmds {
		if strings.Contains(c, sub) {
			n++
		}
	}
	return n
}
