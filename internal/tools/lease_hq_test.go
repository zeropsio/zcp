// Tests for: tools/lease.go + tools/deploy_git_push.go — a lease of a
// repository on the Mate's HQ (docs/spec-workflows.md §8 DP-12).
package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/hq/hqtest"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
)

const (
	leaseHQAddress    = "https://hq.example.invalid"
	leaseHQRepo       = leaseHQAddress + "/git/org-1/a1/web"
	leaseHQCredential = "the-mate-credential-0001"
)

// leaseHQLink links a test's container to the HQ at leaseHQAddress, the way
// a Mate's container presents itself to its HQ: its identity, and a file of
// its own for the Git credential the Mate's server keeps, holding
// leaseHQCredential.
func leaseHQLink(t *testing.T) *runtime.HQLink {
	t.Helper()
	link := &runtime.HQLink{Origin: leaseHQAddress, ProjectID: "proj-1", Key: "the-project-key",
		GitCredentialPath: filepath.Join(t.TempDir(), ".zcp", "hq", "git-credential")}
	hqtest.GitCredential(t, link.GitCredentialPath, leaseHQCredential)
	return link
}

// TestLeaseTool_HQRepo_ChecksOutAsMate: zerops_lease reads the HQ this
// container presents itself to at the acquire, so the checkout of a
// repository on it authenticates as HQ's git user `mate`; the result never
// carries the Mate credential.
func TestLeaseTool_HQRepo_ChecksOutAsMate(t *testing.T) {
	t.Parallel()
	ssh := &leaseStubSSH{}
	srv, _, _ := leaseToolServerIn(t, runtime.Info{HQ: leaseHQLink(t)}, ssh)

	res := callTool(t, srv, "zerops_lease", map[string]any{"action": "acquire", "pool": "devpool", "repo": leaseHQRepo + ".git", "branch": "feat"})
	text := getTextContent(t, res)
	if res.IsError {
		t.Fatalf("acquire: %s", text)
	}
	if !ssh.sawCommand("echo username=mate;") || ssh.sawCommand("username=oauth2") {
		t.Errorf("the checkout of a repository on HQ does not authenticate as mate: %v", ssh.calls)
	}
	if !ssh.sawCommand("credential.https://hq.example.invalid.helper") {
		t.Errorf("the checkout keeps no helper under HQ's scope: %v", ssh.calls)
	}
	if ssh.sawCommand(leaseHQCredential) || strings.Contains(text, leaseHQCredential) {
		t.Errorf("the Mate credential reached a command or the result: %s", text)
	}
}

// TestLeaseTool_HQCheckoutFailure_SaysWhatIsMissing: a pool whose
// ZCP_HQ_GIT_TOKEN is not the Mate credential cannot reach HQ; the refusal names the missing
// credential as a prerequisite and never appends the contract that asks the
// user for a token of their own.
func TestLeaseTool_HQCheckoutFailure_SaysWhatIsMissing(t *testing.T) {
	t.Parallel()
	ssh := &leaseStubSSH{fn: func(string) ([]byte, error) {
		return nil, &platform.SSHExecError{
			Hostname: "devpool-3", Output: "fatal: could not read Username for 'https://hq.example.invalid': terminal prompts disabled",
			Err: os.ErrPermission,
		}
	}}
	srv, stateDir, _ := leaseToolServerIn(t, runtime.Info{HQ: leaseHQLink(t)}, ssh)

	res := callTool(t, srv, "zerops_lease", map[string]any{"action": "acquire", "pool": "devpool", "repo": leaseHQRepo, "branch": "feat"})
	text := getTextContent(t, res)
	if !res.IsError {
		t.Fatalf("want an error result, got: %s", text)
	}
	for _, want := range []string{platform.ErrPrerequisiteMissing, "`mate`", "HQ credential", "pool's " + ops.PoolHQCredentialEnvKey, "terminal prompts disabled"} {
		if !strings.Contains(text, want) {
			t.Errorf("result %s lacks %q", text, want)
		}
	}
	for _, banned := range []string{"user-held secret", platform.ErrGitTokenMissing, leaseHQCredential, leaseTestLease, "no zcp command puts it"} {
		if strings.Contains(text, banned) {
			t.Errorf("result %s carries %q", text, banned)
		}
	}
	if entries, _ := os.ReadDir(filepath.Join(stateDir, "leases")); len(entries) != 0 {
		t.Errorf("a failed checkout left %d entr(ies) in the lease directory", len(entries))
	}
}

// TestDeployTool_LeaseGitPushRefusal_HQRepo_NamesHQsBranchRule: the git-push
// refusal from a lease points at the agent's own push; for a lease on HQ it
// names HQ's branch rule — a branch of the agent's own, never `main`, never
// forced.
func TestDeployTool_LeaseGitPushRefusal_HQRepo_NamesHQsBranchRule(t *testing.T) {
	t.Parallel()
	rig := newLeaseDeployRig(t)
	rig.rt.HQ = leaseHQLink(t)
	record := `{"poolHostname":"webpool","poolServiceId":"svc-pool","containerId":"c-3","containerNumber":3,` +
		`"containerHostname":"node-id-3.runtime.webpool.zerops","lease":"secret-lease-value","branch":"feat-x",` +
		`"repo":"` + leaseHQRepo + `","acquiredAt":"2026-10-23T12:00:00Z"}`
	if err := os.WriteFile(filepath.Join(rig.stateDir, "leases", rig.alias+".json"), []byte(record), 0o600); err != nil {
		t.Fatal(err)
	}
	ssh := &leaseSSH{}

	result, text := rig.deploy(t, ssh, map[string]any{"sourceService": rig.alias, "targetService": "appstage", "strategy": "git-push"})
	if !result.IsError {
		t.Fatalf("a git-push from a lease ran: %s", text)
	}
	for _, want := range []string{"branch of your own", "git push -u origin", `never push \"main\"`, "never force-push", rig.alias, "preview URL"} {
		if !strings.Contains(text, want) {
			t.Errorf("refusal %s lacks %q", text, want)
		}
	}
	if len(ssh.hosts()) != 0 {
		t.Errorf("ssh calls ran before the refusal: %v", ssh.hosts())
	}
}
