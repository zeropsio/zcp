// Tests for: tools/hq_wiring.go — the repository pass in a Mate whose
// server holds no Git credential from HQ: what it cannot renew on the pools
// is said (docs/spec-workflows.md §8 DP-16).
package tools

import (
	"os"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/workflow"
)

// unlink is the Mate's server losing its Git credential from HQ — not linked
// yet, or HQ revoked it: the file the server keeps it in is gone.
func (l *hqLab) unlink() {
	l.t.Helper()
	if err := os.Remove(l.rt.HQ.GitCredentialPath); err != nil && !os.IsNotExist(err) {
		l.t.Fatal(err)
	}
}

// poolNotRenewedLines are the report's lines that say a pool's HQ credential
// was not renewed.
func poolNotRenewedLines(report []string) []string {
	var out []string
	for _, line := range report {
		if strings.Contains(line, ops.PoolHQCredentialEnvKey) && strings.Contains(line, "not renewed") {
			out = append(out, line)
		}
	}
	return out
}

// TestHQPass_NoGitCredential_SaysThePoolsCredentialWasNotRenewed: a pool an
// acquire gave the Mate's Git credential keeps what it holds while the
// Mate's server holds none — no renewal can run — and the pass says so once:
// which pool's ZCP_HQ_GIT_TOKEN was not renewed, and why. Nothing is written.
func TestHQPass_NoGitCredential_SaysThePoolsCredentialWasNotRenewed(t *testing.T) {
	t.Parallel()
	lab := newHQLab(t)
	writePoolCredentialFingerprint(t, lab.stateDir, "devpool", "svc-pool", hq.GitCredentialFingerprint("a-credential-hq-since-revoked"))
	lab.unlink()

	report := lab.wire()

	lines := poolNotRenewedLines(report)
	if len(lines) != 1 {
		t.Fatalf("report says %d time(s) that a pool's credential was not renewed, want once: %v", len(lines), report)
	}
	if !strings.Contains(lines[0], "devpool") || !strings.Contains(lines[0], "holds no Git credential") {
		t.Errorf("the line %q does not name devpool and why", lines[0])
	}
	if n := lab.mock.CallCount("CreateServiceEnvVar"); n != 0 {
		t.Errorf("the pass wrote %d variable(s) with no credential to write", n)
	}
}

// TestHQPass_NoGitCredential_PoolKeyingAPair_SaysItWasNotRenewed: a pool
// that keys a pair is renewed with the pair, and with no Git credential in
// the Mate's file that renewal cannot run either: the pass names the pool in
// the same one line, never a second one. An acquire's per-pool record naming
// another credential than the pair's cannot be weighed against a credential
// the pass cannot read, so it is kept, never dropped — once one is readable,
// the pass still knows what the pool holds.
func TestHQPass_NoGitCredential_PoolKeyingAPair_SaysItWasNotRenewed(t *testing.T) {
	t.Parallel()
	lab := newHQLab(t)
	lab.wire()
	ssh := lab.keyPairOnPool()
	before := hq.GitCredentialFingerprint(labCredential)
	if err := workflow.UpdateServiceMeta(lab.stateDir, testPool, func(m *workflow.ServiceMeta) error {
		m.HQ.CredentialFingerprint = before
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	acquired := hq.GitCredentialFingerprint("a-credential-an-acquire-wrote")
	writePoolCredentialFingerprint(t, lab.stateDir, testPool, testPoolID, acquired)
	lab.unlink()

	report := lab.poolPass(ssh)

	lines := poolNotRenewedLines(report)
	if len(lines) != 1 || !strings.Contains(lines[0], testPool) || !strings.Contains(lines[0], "holds no Git credential") {
		t.Fatalf("report = %v, want one line naming %s's credential not renewed, and why", report, testPool)
	}
	if _, fingerprint, found := ops.PoolCredentialRecord(lab.stateDir, testPool); !found || fingerprint != acquired {
		t.Errorf("the per-pool record = %q (found %v), want the acquire's kept until a credential is readable", fingerprint, found)
	}
	if got := lab.poolMeta().HQ.CredentialFingerprint; got != before {
		t.Errorf("the pair's fingerprint moved from %q to %q with no credential to weigh it against", before, got)
	}
}
