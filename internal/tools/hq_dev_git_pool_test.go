// Tests for: tools/hq_dev_git.go and hq_wiring.go — keeping a wired pair on
// this Mate's HQ when its dev half is a dev pool (docs/spec-workflows.md §8
// PS-3, §13 PM-10): no checkout of its own to move.
package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
			l.snapshotMate(l.t.Context())
			startDevGit(l.t.Context(), l.hq.srv.Client(), ssh, l.rt, l.stateDir)
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
