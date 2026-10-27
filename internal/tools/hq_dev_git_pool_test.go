// Tests for: tools/hq_dev_git.go and hq_wiring.go — keeping a wired pair on
// this Mate's HQ when its dev half is a dev pool (docs/spec-workflows.md §8
// PS-3, §13 PM-10): no checkout of its own to move, its HQ credential renewed
// on the pool.
package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// TestDevGit_PoolKeyingAPairWiredAtARetiredHQ_MovesOnlyTheRecord: a pair
// keyed by its dev pool whose record names its repository on an HQ the Mate
// no longer links to is moved by the Mate's start and by the repository pass
// as a dev service's pair is — but the pool's hostname reaches a random
// container of it, possibly someone's lease, and holds no checkout: nothing
// runs over ssh there, and only the record follows to the current HQ.
func TestDevGit_PoolKeyingAPairWiredAtARetiredHQ_MovesOnlyTheRecord(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		run  func(l *hqLab, ssh *hostLogSSH)
	}{
		{name: "the Mate's start", run: func(l *hqLab, ssh *hostLogSSH) {
			startDevGit(l.t.Context(), l.mock, l.hq.srv.Client(), ssh, l.rt, l.stateDir)
		}},
		{name: "the repository pass", run: func(l *hqLab, ssh *hostLogSSH) {
			reconcileHQRepositories(l.t.Context(), l.mock, l.hq.srv.Client(), ssh, l.rt, l.stateDir, hqPassNow)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			lab := newHQLab(t)
			lab.wire()
			current := lab.git("remote", "get-url", "origin")
			ssh := lab.keyPairOnPool()
			retired := retiredHQ + "/git/" + labApp + "/appdev.git"
			if err := workflow.UpdateServiceMeta(lab.stateDir, testPool, func(m *workflow.ServiceMeta) error {
				m.RemoteURL = retired
				return nil
			}); err != nil {
				t.Fatal(err)
			}

			tc.run(lab, ssh)

			if n := ssh.reached(testPool); n != 0 {
				t.Errorf("the move reached the pool %s over ssh %d time(s)", testPool, n)
			}
			if got := lab.poolMeta().RemoteURL; got != current {
				t.Errorf("the pool-keyed pair's record names %q, want its repository on the current HQ %q", got, current)
			}
		})
	}
}

// TestMateService_ACredentialHQSendsReachesEveryPool (D9, live 2026-10-27):
// HQ sends the Mate a new Git credential on every link it opens and revokes
// the one before, so a lease of an HQ repository pushes with a revoked
// ZCP_HQ_GIT_TOKEN until its pool holds the new one. The Mate's service, at
// its next look after the credential lands — with no agent pass — writes it
// to every pool zcp put the credential on: one an acquire recorded and one
// keying a pair. A look with nothing new writes nothing.
func TestMateService_ACredentialHQSendsReachesEveryPool(t *testing.T) {
	t.Parallel()
	const rotated = "sent-on-the-next-link"
	for _, tc := range []struct {
		name     string
		pool     string
		prepare  func(*hqLab)
		recorded bool
	}{
		{name: "a pool an acquire recorded", pool: "svc-pool", recorded: true, prepare: func(l *hqLab) {
			l.mock.WithServices([]platform.ServiceStack{
				{ID: "svc-appdev", Name: "appdev"}, {ID: "svc-appstage", Name: "appstage"},
				{ID: "svc-pool", Name: "devpool", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1"}},
			})
			writePoolCredentialFingerprint(l.t, l.stateDir, "devpool", "svc-pool", hq.GitCredentialFingerprint(labCredential))
		}},
		{name: "a pool keying a pair", pool: testPoolID, prepare: func(l *hqLab) {
			l.wire()
			l.keyPairOnPool()
			if err := workflow.UpdateServiceMeta(l.stateDir, testPool, func(m *workflow.ServiceMeta) error {
				m.HQ.CredentialFingerprint = hq.GitCredentialFingerprint(labCredential)
				return nil
			}); err != nil {
				l.t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			lab := newHQLab(t)
			tc.prepare(lab)
			ticks := make(chan time.Time, 2)
			ticks <- time.Time{}
			ticks <- time.Time{}
			close(ticks)
			looks, writesAfterRenewal := 0, -1
			deps := func(ctx context.Context) (devGitDeps, bool) {
				switch looks++; looks {
				case 2:
					lab.relink(rotated)
				case 3:
					writesAfterRenewal = lab.mock.CallCount("CreateServiceEnvVar")
				}
				return lab.devGitDeps(ctx)
			}

			keepMateDevGit(t.Context(), deps, time.Time{}, ticks, func(string) {})

			if token, held := poolHQCredential(t, lab.mock, tc.pool); !held || token.Content != rotated || !token.Sensitive {
				t.Errorf("the pool's %s = %+v (held %v), want the credential HQ sent, as a sensitive secret", ops.PoolHQCredentialEnvKey, token, held)
			}
			if n := lab.mock.CallCount("CreateServiceEnvVar"); n != writesAfterRenewal {
				t.Errorf("a look with no new credential wrote %d variable(s)", n-writesAfterRenewal)
			}
			if _, fingerprint, found := ops.PoolCredentialRecord(lab.stateDir, "devpool"); tc.recorded && (!found || fingerprint != hq.GitCredentialFingerprint(rotated)) {
				t.Errorf("the pool's record names %q (found %v), want the credential HQ sent", fingerprint, found)
			}
			assertNoSecretOnDisk(t, lab.stateDir, rotated)
		})
	}
}

// TestHQPass_PoolKeyingAPair_ARefusedRecordIsRenewedOnce: HQ refused the
// credential a lease of a pool keying a pair presented, and the lease's call
// could not write it afresh, so zcp marked the pool's record refused (D9).
// The pair's record still names the Mate's current credential, so the pair's
// own renewal skips the pool; the pass's takeover leaves the marked record to
// the pools' renewal, which writes the pool once, and the next pass writes
// nothing.
func TestHQPass_PoolKeyingAPair_ARefusedRecordIsRenewedOnce(t *testing.T) {
	t.Parallel()
	lab := newHQLab(t)
	lab.wire()
	ssh := lab.keyPairOnPool()
	current := hq.GitCredentialFingerprint(labCredential)
	if err := workflow.UpdateServiceMeta(lab.stateDir, testPool, func(m *workflow.ServiceMeta) error {
		m.HQ.CredentialFingerprint = current
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	writeRefusedPoolCredential(t, lab.stateDir, testPool, testPoolID, current)
	before := lab.mock.CallCount("CreateServiceEnvVar")

	lab.poolPass(ssh)
	first := lab.mock.CallCount("CreateServiceEnvVar") - before
	lab.poolPass(ssh)

	if token, held := poolHQCredential(t, lab.mock, testPoolID); !held || token.Content != labCredential {
		t.Errorf("the pool's %s = %+v (held %v), want the Mate's credential written afresh", ops.PoolHQCredentialEnvKey, token, held)
	}
	if second := lab.mock.CallCount("CreateServiceEnvVar") - before; first != 1 || second != first {
		t.Errorf("writes = %d after the first pass, %d after the second; want one, then none", first, second)
	}
}

// writeRefusedPoolCredential records, as a lease's refused git leaves it,
// that pool (serviceID) was given the credential fingerprint names and HQ
// refused what it holds.
func writeRefusedPoolCredential(t *testing.T, stateDir, pool, serviceID, fingerprint string) {
	t.Helper()
	path := filepath.Join(stateDir, "leases", "credentials", pool+".json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]any{"poolServiceId": serviceID, "fingerprint": fingerprint, "refused": true})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

// writePoolDefault records repo as pool's default repository, as its first
// acquire left it.
func writePoolDefault(t *testing.T, stateDir, pool, repo string) {
	t.Helper()
	path := filepath.Join(stateDir, "leases", "pools", pool+".json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]string{"repo": repo})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

// poolDefault is pool's default repository as recorded, "" for none.
func poolDefault(t *testing.T, stateDir, pool string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(stateDir, "leases", "pools", pool+".json"))
	if err != nil {
		return ""
	}
	var rec struct {
		Repo string `json:"repo"`
	}
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatal(err)
	}
	return rec.Repo
}

// TestDevGit_APoolsDefaultRepositoryFollowsItsPair (D8, live 2026-10-27): a
// pool's default repository is recorded at its first acquire, and an acquire
// naming no repository clones it. Recorded while the pair was on an HQ the
// Mate has since left, it named the retired HQ, and every acquire without a
// repository went there. A default naming the pair's repository moves with
// the pair: off a retired HQ at the Mate's start and at the repository pass,
// and to the application HQ holds the Mate in now when the pass wires the
// pair again there. A default naming anything else stays.
func TestDevGit_APoolsDefaultRepositoryFollowsItsPair(t *testing.T) {
	t.Parallel()
	retired := retiredHQ + "/git/" + labApp + "/appdev.git"
	for _, tc := range []struct {
		name string
		run  func(*hqLab)
	}{
		{name: "the Mate's start", run: func(l *hqLab) { l.mateStart() }},
		{name: "develop's start", run: func(l *hqLab) { l.developStart() }},
	} {
		t.Run("off a retired HQ, "+tc.name, func(t *testing.T) {
			t.Parallel()
			lab := newHQLab(t)
			lab.wire()
			current := lab.git("remote", "get-url", "origin")
			lab.retireHQ()
			writePoolDefault(t, lab.stateDir, "webpool", retired)
			writePoolDefault(t, lab.stateDir, "docspool", "https://github.com/acme/docs.git")

			tc.run(lab)

			if got := poolDefault(t, lab.stateDir, "webpool"); got != current {
				t.Errorf("webpool's default repository = %q, want the pair's repository on the current HQ %q", got, current)
			}
			if got := poolDefault(t, lab.stateDir, "docspool"); got != "https://github.com/acme/docs.git" {
				t.Errorf("a default naming another repository moved to %q", got)
			}
		})
	}
	t.Run("to the application HQ holds the Mate in now", func(t *testing.T) {
		t.Parallel()
		lab := newHQLab(t)
		lab.wire()
		writePoolDefault(t, lab.stateDir, "webpool", lab.git("remote", "get-url", "origin"))
		lab.hq.moveMate("app-2")

		lab.developStart()

		moved := lab.meta().RemoteURL
		if !strings.Contains(moved, "/app-2/") {
			t.Fatalf("the pair was not wired again in app-2: remote %q", moved)
		}
		if got := poolDefault(t, lab.stateDir, "webpool"); got != moved {
			t.Errorf("webpool's default repository = %q, want the pair's repository in app-2 %q", got, moved)
		}
	})
}
