// Tests for: tools/hq_wiring.go — every bootstrapped pair gets its
// repository in HQ and the Mate's own branch descending from its main, run
// against a fake HQ that serves real git (hq_lab_test.go).
package tools

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// TestReconcileHQRepositories_GivesThePairItsRepositoryAndBranch: the pass
// asks HQ for the repository named after the dev half, points the checkout's
// origin at it with the Mate credential where git-push-setup keeps it — a
// sensitive service env on the push source, never a file zcp writes — and
// puts the checkout on the Mate's branch, cut from HQ's main.
func TestReconcileHQRepositories_GivesThePairItsRepositoryAndBranch(t *testing.T) {
	lab := newHQLab(t)

	report := lab.wire()

	if len(report) != 1 || !strings.Contains(report[0], `repository "appdev" wired in HQ`) || !strings.Contains(report[0], `"mate/p-mate"`) {
		t.Fatalf("report = %q", report)
	}
	meta := lab.meta()
	if meta.HQ == nil || meta.HQ.AppID != labApp || meta.HQ.Repo != "appdev" || meta.HQ.Branch != "mate/p-mate" {
		t.Fatalf("the pair's HQ record = %+v", meta.HQ)
	}
	if meta.GitPushState != topology.GitPushConfigured || meta.RemoteURL != lab.hq.srv.URL+"/git/"+labApp+"/appdev.git" {
		t.Errorf("git-push = %s to %q", meta.GitPushState, meta.RemoteURL)
	}
	if got := lab.git("rev-parse", "--abbrev-ref", "HEAD"); got != "mate/p-mate" {
		t.Errorf("the checkout is on %q, want the Mate's branch", got)
	}
	if got := lab.git("remote", "get-url", "origin"); got != meta.RemoteURL {
		t.Errorf("origin = %q, want the repository in HQ", got)
	}
	if lab.git("rev-parse", "HEAD") != lab.remoteHead("main") {
		t.Errorf("a pair fresh from zcp init is cut AT HQ's main")
	}
	if held := lab.ssh.gitToken(t.Context(), "svc-appdev"); held != labCredential {
		t.Errorf("the push source holds %q, want the Mate credential", held)
	}
	assertNoSecretOnDisk(t, lab.stateDir, labCredential)

	// One repository per pair: the next pass asks HQ for none.
	elapseHQBackoff(t, lab.stateDir, "appdev")
	before := len(lab.hq.calls)
	lab.wire()
	for _, call := range lab.hq.calls[before:] {
		if strings.Contains(call, "/api/mate/repos") {
			t.Errorf("a wired pair asked HQ for its repository again: %v", lab.hq.calls[before:])
		}
	}
}

// elapseHQBackoff makes the pair's next pass due.
func elapseHQBackoff(t *testing.T, stateDir, hostname string) {
	t.Helper()
	writeHQPairState(stateDir, hostname, hqPairState{})
}

// TestReconcileHQRepositories_RewiresAPairToTheRepositoryItsRecordNames: a
// pair whose push an earlier refusal marked is wired again — to the
// repository its record names, which a stand-up took from the group recipe,
// never to one named after its hostname.
func TestReconcileHQRepositories_RewiresAPairToTheRepositoryItsRecordNames(t *testing.T) {
	lab := newHQLab(t)
	if err := workflow.UpdateServiceMeta(lab.stateDir, "appdev", func(m *workflow.ServiceMeta) error {
		m.GitPushState = topology.GitPushBroken
		m.HQ = &workflow.HQRepoRef{AppID: labApp, Repo: "medusa", Branch: "mate/" + labMate}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	lab.wire()

	meta := lab.meta()
	if meta.HQ == nil || meta.HQ.Repo != "medusa" || !strings.HasSuffix(meta.RemoteURL, "/git/"+labApp+"/medusa.git") {
		t.Fatalf("the pair is wired to %+v at %q, want the repository its record names", meta.HQ, meta.RemoteURL)
	}
	for _, call := range lab.hq.calls {
		if strings.Contains(call, "/git/"+labApp+"/appdev.git") {
			t.Errorf("the pass reached a repository named after the hostname: %s", call)
		}
	}
}
