// Tests for: tools/lease.go + tools/deploy_git_push.go — a lease of a
// repository on the Mate's HQ (docs/spec-workflows.md §8 DP-12).
package tools

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
)

const (
	leaseHQAddress = "https://hq.example.invalid"
	leaseHQRepo    = leaseHQAddress + "/git/org-1/a1/web"
	leaseHQKey     = "the-project-key-0001"
)

// leaseHQLink links a test's container to the HQ at leaseHQAddress, the way
// a Mate's container presents itself to its HQ: its identity, the project
// key leaseHQKey among it.
func leaseHQLink(t *testing.T) *runtime.HQLink {
	t.Helper()
	return &runtime.HQLink{Origin: leaseHQAddress, ProjectID: "proj-1", Key: leaseHQKey}
}

// TestLeaseAcquire_HQRepoInAMate_ClonedThroughTheRelay_NoHQCredentialInThePool:
// a pool acquire clones an HQ repository through the relay with the project
// key; no HQ Git credential is stored in the pool. The HQ takes only the
// Mate's project key, the acquire's checkout lands on HQ's main, and neither
// the pool's variables, the leased container's git config nor zcp's state
// holds a credential for it.
// witness: internal/tools/hq_lease_completion.go | deps.HQGit = relayed → _ = relayed
func TestLeaseAcquire_HQRepoInAMate_ClonedThroughTheRelay_NoHQCredentialInThePool(t *testing.T) {
	t.Parallel()
	fake := newFakeHQ(t)
	fake.credential = "a-credential-no-git-presents"
	fake.ensureRepo(t.Context(), labApp, "web")
	main := fake.git(t.Context(), fake.repoDir(labApp, "web"), "rev-parse", "main")
	repo := fake.srv.URL + "/git/" + labOrg + "/" + labApp + "/web.git"
	leased := t.TempDir()
	ssh := &labSSH{t: t, dirs: map[string]string{"devpool-3": leased, "node-id-3.runtime.devpool.zerops": leased}, home: t.TempDir(), caFile: fake.caFile}
	// A relayed session clears git's TLS variables: the lab's CA reaches its
	// git from the global configuration, as a container's CA store would.
	if err := os.WriteFile(filepath.Join(ssh.home, ".gitconfig"), []byte("[http]\n\tsslCAInfo = "+fake.caFile+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rt := runtime.Info{InContainer: true, MateEnabled: true, ProjectID: labMate, OrgID: labOrg, HQ: fake.link(t)}
	srv, stateDir, mock := leaseToolServerIn(t, rt, ssh)
	ssh.mock = mock

	res := callTool(t, srv, "zerops_lease", map[string]any{"action": "acquire", "pool": "devpool", "repo": repo, "branch": "feat"})
	text := getTextContent(t, res)
	if res.IsError {
		t.Fatalf("acquire of a repository on HQ: %s", text)
	}
	if got, _ := leasedGit(t, leased, "rev-parse", "HEAD"); got != main {
		t.Errorf("the leased checkout is at %q, want HQ's main %q", got, main)
	}
	for _, key := range []string{ops.PoolHQCredentialEnvKey, ops.GitTokenEnvKey} {
		if v, held := serviceEnvVar(t, mock, "svc-pool", key); held {
			t.Errorf("the acquire stored the pool's %s: %+v", key, v)
		}
	}
	if helpers, _ := leasedGit(t, leased, "config", "--get-regexp", `^credential\.`); helpers != "" {
		t.Errorf("the leased checkout keeps a credential helper: %s", helpers)
	}
	for _, secret := range []string{labKey, labCredential} {
		if strings.Contains(text, secret) {
			t.Errorf("the result carries a credential: %s", text)
		}
		assertNoSecretOnDisk(t, stateDir, secret)
		assertNoSecretOnDisk(t, filepath.Join(leased, ".git"), secret)
	}
}

// leasedGit is git's trimmed answer in the leased checkout dir.
func leasedGit(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...).Output() //nolint:gosec // G204: a test's own git
	return strings.TrimSpace(string(out)), err
}

// TestLeaseTool_HQCheckoutFailure_SaysWhatIsMissing: a checkout git asked a
// credential of that nothing answered cannot reach HQ; the refusal names the
// Mate's project key and the relayed `zcp git` as the prerequisite and never
// appends the contract that asks the user for a token of their own.
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
	for _, want := range []string{platform.ErrPrerequisiteMissing, "project key", "zcp git ", "never ask", "terminal prompts disabled"} {
		if !strings.Contains(text, want) {
			t.Errorf("result %s lacks %q", text, want)
		}
	}
	for _, banned := range []string{"user-held secret", platform.ErrGitTokenMissing, ops.PoolHQCredentialEnvKey, leaseHQKey, leaseTestLease, "no zcp command puts it"} {
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
