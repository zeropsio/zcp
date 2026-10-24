// Tests for: tools/lease.go + tools/hq_wiring.go — inside a Mate, a lease of a
// repository on its HQ puts the Mate credential on the pool's GIT_TOKEN, and
// the repository pass renews it after the Mate enrolls again
// (docs/spec-workflows.md §8 DP-16).
package tools

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

// leaseToolServerIn is leaseToolServerSSH for the runtime rt, answering the
// mock platform too.
func leaseToolServerIn(t *testing.T, rt runtime.Info, ssh ops.SSHDeployer) (srv *mcp.Server, stateDir string, mock *platform.Mock) {
	t.Helper()
	hs := httptest.NewServer(&fakeOrchestrator{})
	t.Cleanup(hs.Close)
	stateDir = t.TempDir()
	mock = platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "proj"}).
		WithServices([]platform.ServiceStack{
			{ID: "svc-pool", Name: "devpool", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1", ServiceStackTypeCategoryName: "USER"}},
		}).
		WithServiceEnv("svc-pool", []platform.ServiceEnvVar{{Key: "API_TOKEN", Content: leaseTestToken, Sensitive: true}})
	factory := func(_, token string) ops.SwarmAPI { return platform.NewSwarmClient(hs.URL, token, hs.Client()) }
	srv = mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterLease(srv, mock, "proj-1", stateDir, factory, nil, ssh, rt)
	return srv, stateDir, mock
}

// serviceGitToken is the GIT_TOKEN serviceID holds on mock.
func serviceGitToken(t *testing.T, mock *platform.Mock, serviceID string) (platform.ServiceEnvVar, bool) {
	t.Helper()
	envs, err := mock.GetServiceEnv(context.Background(), serviceID)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range envs {
		if e.Key == ops.GitTokenEnvKey {
			return e, true
		}
	}
	return platform.ServiceEnvVar{}, false
}

// TestLeaseTool_HQRepoInAMate_PutsTheMateCredentialOnThePool: zerops_lease
// in a Mate gives the pool the Mate credential its enrollment holds before it
// checks out a repository on HQ — as a sensitive GIT_TOKEN, the way a wired
// dev service holds it. A container that is not a Mate leaves the pool's
// GIT_TOKEN the user's. Neither the result nor any state file carries the
// credential, and a write that fails says so without it.
func TestLeaseTool_HQRepoInAMate_PutsTheMateCredentialOnThePool(t *testing.T) {
	// Non-parallel: t.Setenv points HOME at the test's enrollment.
	tests := []struct {
		name      string
		mate      bool
		failWrite bool
		wantWrite bool
		wantError bool
	}{
		{name: "in a Mate", mate: true, wantWrite: true},
		{name: "outside a Mate", mate: false},
		{name: "in a Mate whose write fails", mate: true, failWrite: true, wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enrollWithLeaseHQ(t)
			ssh := &leaseStubSSH{}
			srv, stateDir, mock := leaseToolServerIn(t, runtime.Info{InContainer: true, ProjectID: "proj-1", MateEnabled: tt.mate}, ssh)
			if tt.failWrite {
				mock.WithError("CreateServiceEnvVar", platform.NewPlatformError(platform.ErrAPIError, "userData create refused", ""))
			}

			res := callTool(t, srv, "zerops_lease", map[string]any{"action": "acquire", "pool": "devpool", "repo": leaseHQRepo, "branch": "feat"})
			text := getTextContent(t, res)
			if res.IsError != tt.wantError {
				t.Fatalf("IsError = %v, want %v: %s", res.IsError, tt.wantError, text)
			}
			token, held := serviceGitToken(t, mock, "svc-pool")
			switch {
			case tt.wantWrite && (!held || token.Content != leaseHQCredential || !token.Sensitive):
				t.Errorf("the pool's GIT_TOKEN = %+v (held %v), want the Mate credential as a sensitive secret", token, held)
			case !tt.wantWrite && held:
				t.Errorf("zcp wrote the pool's GIT_TOKEN: %+v", token)
			}
			if tt.wantError && (!strings.Contains(text, "GIT_TOKEN") || ssh.sawCommand("fetch -q --prune origin")) {
				t.Errorf("a failed write either is not named or a checkout ran anyway: %s", text)
			}
			if strings.Contains(text, leaseHQCredential) || ssh.sawCommand(leaseHQCredential) {
				t.Errorf("the credential reached the result or a session command: %s", text)
			}
			assertNoSecretOnDisk(t, stateDir, leaseHQCredential)
		})
	}
}

// writePoolCredentialFingerprint records, the way an acquire does, that pool
// (serviceID) holds the credential fingerprint names.
func writePoolCredentialFingerprint(t *testing.T, stateDir, pool, serviceID, fingerprint string) {
	t.Helper()
	path := filepath.Join(stateDir, "leases", "credentials", pool+".json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]string{"poolServiceId": serviceID, "fingerprint": fingerprint})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestReconcileHQRepositories_RenewsThePoolsCredentialAfterTheMateEnrollsAgain:
// a redeployed zcp enrolls the Mate again and HQ revokes the credential every
// pool zcp gave it, so a lease held there loses HQ. The repository pass gives
// every pool zcp holds a fingerprint for the new credential — a lease's next
// fresh session reads it live — in a Mate with pairs or without, and never
// outside a Mate.
func TestReconcileHQRepositories_RenewsThePoolsCredentialAfterTheMateEnrollsAgain(t *testing.T) {
	// Non-parallel: newHQLab sets HOME and package tuning knobs.
	const renewed = "the-renewed-credential"
	tests := []struct {
		name      string
		mate      bool
		noPair    bool
		wantRenew bool
	}{
		{name: "a Mate with a pair", mate: true, wantRenew: true},
		{name: "a Mate whose code lives only in pools", mate: true, noPair: true, wantRenew: true},
		{name: "not a Mate", mate: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lab := newHQLab(t)
			lab.rt.MateEnabled = tt.mate
			lab.mock.WithServices([]platform.ServiceStack{
				{ID: "svc-appdev", Name: "appdev"}, {ID: "svc-appstage", Name: "appstage"},
				{ID: "svc-pool", Name: "devpool", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1"}},
			})
			if tt.noPair {
				if err := workflow.DeleteServiceMeta(lab.stateDir, "appdev"); err != nil {
					t.Fatal(err)
				}
			}
			writePoolCredentialFingerprint(t, lab.stateDir, "devpool", "svc-pool", hq.CredentialFingerprint(labCredential))
			lab.reenroll(renewed)

			lab.wire()

			token, held := serviceGitToken(t, lab.mock, "svc-pool")
			switch {
			case tt.wantRenew && (!held || token.Content != renewed || !token.Sensitive):
				t.Errorf("the pool's GIT_TOKEN = %+v (held %v), want the renewed credential as a sensitive secret", token, held)
			case !tt.wantRenew && held:
				t.Errorf("a pass outside a Mate wrote the pool's GIT_TOKEN: %+v", token)
			}
			assertNoSecretOnDisk(t, lab.stateDir, renewed)
		})
	}
}
