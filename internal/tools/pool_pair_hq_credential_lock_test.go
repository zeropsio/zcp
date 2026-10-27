// Tests for: tools/hq_wiring.go — the repository pass renews a pool keying a
// pair under the pool's credential lock, the one an acquire on the pool
// decides and writes under (docs/spec-workflows.md §8 DP-16).
package tools

import (
	"testing"

	"github.com/zeropsio/zcp/internal/hq"
)

// TestRenewHQPairCredential_PoolWrittenByAnAcquireMeanwhile_NoSecondWrite: the
// pass read the pair's stale fingerprint, and an acquire on the pool then
// wrote the credential the enrollment holds now and recorded it. Under the
// pool's credential lock the pass reads that record again and takes it over,
// so it never writes the variable a second time: a write deletes it and
// creates it anew, under the fresh sessions of the pool's leases.
func TestRenewHQPairCredential_PoolWrittenByAnAcquireMeanwhile_NoSecondWrite(t *testing.T) {
	// Non-parallel: newHQLab sets HOME and package tuning knobs.
	lab := newHQLab(t)
	lab.wire()
	lab.keyPairOnPool()
	lab.rt.MateEnabled = true
	hqc, ok := openHQ(lab.hq.srv.Client(), lab.rt)
	if !ok {
		t.Fatal("the lab's Mate is not enrolled")
	}
	m := lab.poolMeta()
	current := hq.GitCredentialFingerprint(labCredential)
	if m.HQ.CredentialFingerprint == current {
		t.Fatalf("precondition: the pair already records the current fingerprint")
	}
	writePoolCredentialFingerprint(t, lab.stateDir, testPool, testPoolID, current)
	before := lab.mock.CallCount("CreateServiceEnvVar")

	if line := renewHQPairCredential(t.Context(), lab.mock, labMate, lab.stateDir, hqc, m); line != "" {
		t.Fatalf("the renewal failed: %s", line)
	}

	if n := lab.mock.CallCount("CreateServiceEnvVar") - before; n != 0 {
		t.Errorf("HQ credential writes = %d, want 0: the acquire's write was made again", n)
	}
	if got := lab.poolMeta().HQ.CredentialFingerprint; got != current {
		t.Errorf("the pair records fingerprint %q, want the enrollment's %q", got, current)
	}
}
