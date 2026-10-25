// Tests for: tools/lease.go + tools/hq_wiring.go — inside a Mate, a lease of a
// repository on its HQ puts the Mate credential on the pool's
// ZCP_HQ_GIT_TOKEN — never its GIT_TOKEN, which stays the user's — and the
// repository pass renews it after the Mate enrolls again
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
	"github.com/zeropsio/zcp/internal/topology"
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

// serviceEnvVar is the variable key serviceID holds on mock.
func serviceEnvVar(t *testing.T, mock *platform.Mock, serviceID, key string) (platform.ServiceEnvVar, bool) {
	t.Helper()
	envs, err := mock.GetServiceEnv(context.Background(), serviceID)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range envs {
		if e.Key == key {
			return e, true
		}
	}
	return platform.ServiceEnvVar{}, false
}

// serviceGitToken is the GIT_TOKEN serviceID holds on mock.
func serviceGitToken(t *testing.T, mock *platform.Mock, serviceID string) (platform.ServiceEnvVar, bool) {
	t.Helper()
	return serviceEnvVar(t, mock, serviceID, ops.GitTokenEnvKey)
}

// poolHQCredential is the HQ credential pool service serviceID holds on mock
// (ops.PoolHQCredentialEnvKey).
func poolHQCredential(t *testing.T, mock *platform.Mock, serviceID string) (platform.ServiceEnvVar, bool) {
	t.Helper()
	return serviceEnvVar(t, mock, serviceID, ops.PoolHQCredentialEnvKey)
}

// TestLeaseTool_HQRepoInAMate_PutsTheMateCredentialOnThePool: zerops_lease
// in a Mate gives the pool the Mate credential its enrollment holds before it
// checks out a repository on HQ — as the sensitive ZCP_HQ_GIT_TOKEN only its
// leases of HQ repositories read. A container that is not a Mate writes
// nothing, and no acquire writes the pool's GIT_TOKEN, the user's. Neither the
// result nor any state file carries the credential, and a write that fails
// says so without it.
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
			token, held := poolHQCredential(t, mock, "svc-pool")
			switch {
			case tt.wantWrite && (!held || token.Content != leaseHQCredential || !token.Sensitive):
				t.Errorf("the pool's %s = %+v (held %v), want the Mate credential as a sensitive secret", ops.PoolHQCredentialEnvKey, token, held)
			case !tt.wantWrite && held:
				t.Errorf("zcp wrote the pool's %s: %+v", ops.PoolHQCredentialEnvKey, token)
			}
			if token, held := serviceGitToken(t, mock, "svc-pool"); held {
				t.Errorf("zcp wrote the pool's GIT_TOKEN, the user's: %+v", token)
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

			token, held := poolHQCredential(t, lab.mock, "svc-pool")
			switch {
			case tt.wantRenew && (!held || token.Content != renewed || !token.Sensitive):
				t.Errorf("the pool's %s = %+v (held %v), want the renewed credential as a sensitive secret", ops.PoolHQCredentialEnvKey, token, held)
			case !tt.wantRenew && held:
				t.Errorf("a pass outside a Mate wrote the pool's %s: %+v", ops.PoolHQCredentialEnvKey, token)
			}
			if token, held := serviceGitToken(t, lab.mock, "svc-pool"); held {
				t.Errorf("the pass wrote the pool's GIT_TOKEN, the user's: %+v", token)
			}
			assertNoSecretOnDisk(t, lab.stateDir, renewed)
		})
	}
}

// TestLeaseTool_HQRepo_PoolKeyingAPair_NoWriteOfTheCredentialItHolds: once a
// pool keys a pair, the repository pass takes the acquire's per-pool record
// over — the pair keeps the fingerprint of the credential the pool holds —
// and removes it. An acquire then finds no per-pool record, yet the pool
// already holds the credential the enrollment holds now, so it writes
// nothing: a write is a delete and a create of the variable, and while the
// two run every fresh session of every container of the pool reads none, so
// git in the pool's other leases fails "could not read Username" (live,
// 2026-10-25: a sync from one lease failed 2 s after another conversation's
// acquire rewrote the credential its pool already held). A pair recording
// another credential, or another pool service, still gets the write.
func TestLeaseTool_HQRepo_PoolKeyingAPair_NoWriteOfTheCredentialItHolds(t *testing.T) {
	// Non-parallel: t.Setenv points HOME at the test's enrollment.
	tests := []struct {
		name          string
		pairServiceID string
		pairHolds     string
		wantWrites    int
	}{
		{name: "the pair records the credential held now", pairServiceID: "svc-pool", pairHolds: leaseHQCredential, wantWrites: 0},
		{name: "the pair records a revoked credential", pairServiceID: "svc-pool", pairHolds: "a-revoked-credential", wantWrites: 1},
		{name: "the pair records another pool service", pairServiceID: "svc-an-older-pool", pairHolds: leaseHQCredential, wantWrites: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enrollWithLeaseHQ(t)
			srv, stateDir, mock := leaseToolServerIn(t, runtime.Info{InContainer: true, ProjectID: "proj-1", MateEnabled: true}, &leaseStubSSH{})
			// The pool holds the credential an earlier acquire wrote; the
			// Mate's key reads it back REDACTED.
			mock.WithServiceEnv("svc-pool", []platform.ServiceEnvVar{
				{ID: "e-api", Key: "API_TOKEN", Content: leaseTestToken, Sensitive: true},
				{ID: "e-hq", Key: ops.PoolHQCredentialEnvKey, Content: "REDACTED", Sensitive: true},
			})
			if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
				Hostname: "devpool", Mode: topology.PlanModeStandard, Stages: []topology.StageRef{{Hostname: "devstage"}},
				BootstrapSession: "test", BootstrappedAt: "2026-10-25",
				DevPool: &workflow.DevPoolRef{ServiceID: tt.pairServiceID},
				HQ:      &workflow.HQRepoRef{AppID: "a1", Repo: "web", CredentialFingerprint: hq.CredentialFingerprint(tt.pairHolds)},
			}); err != nil {
				t.Fatal(err)
			}

			res := callTool(t, srv, "zerops_lease", map[string]any{"action": "acquire", "pool": "devpool", "repo": leaseHQRepo, "branch": "feat"})
			if text := getTextContent(t, res); res.IsError {
				t.Fatalf("acquire failed: %s", text)
			}
			if n := mock.CallCount("CreateServiceEnvVar"); n != tt.wantWrites {
				t.Errorf("credential writes = %d, want %d", n, tt.wantWrites)
			}
			if tt.wantWrites == 0 && mock.CallCount("DeleteUserData") != 0 {
				t.Errorf("the acquire deleted a variable of the pool (%d), leaving its sessions without the credential", mock.CallCount("DeleteUserData"))
			}
		})
	}
}
