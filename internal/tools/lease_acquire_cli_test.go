// Tests for: internal/tools/lease_acquire_cli.go — the writer lease wire (DP-39).
package tools

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

func acquireCLIDeps(t *testing.T, orch http.Handler) ops.LeaseDeps {
	t.Helper()
	client := platform.NewMock().
		WithServices([]platform.ServiceStack{swarmPoolService("svc-pool", "devpool", "ACTIVE")}).
		WithServiceEnv("svc-pool", []platform.ServiceEnvVar{{Key: "API_TOKEN", Content: leaseTestToken, Sensitive: true}})
	return ops.LeaseDeps{ProjectID: "proj-1", Client: client, StateDir: t.TempDir(),
		Swarm: ops.NewSwarmFactory(&http.Client{Transport: leaseHandlerTransport{handler: orch}}),
		Env:   func(string) (string, bool) { return "", false },
	}
}

// witness: internal/tools/lease_acquire_cli.go | Reused: v.Reused, → Reused: false,
func TestAcquireLeaseAnswer_WriterCopy_ReportsIdentityAndReuse(t *testing.T) {
	t.Parallel()
	deps := acquireCLIDeps(t, &fakeOrchestrator{})
	var id string
	for _, reused := range []bool{false, true} {
		answer, code := AcquireLeaseAnswer(t.Context(), deps, "devpool", leaseTestRepo, "crew/writer")
		raw, err := json.Marshal(answer)
		if err != nil || code != 0 {
			t.Fatalf("exit %d, answer %s, marshal %v", code, raw, err)
		}
		stamp := acquiredAtOf(t, deps.StateDir, "devpool-3")
		if !reused {
			id = answer.Lease.ID
			decoded, err := hex.DecodeString(id)
			if err != nil || len(decoded) != 16 {
				t.Fatal("the writer did not receive a 16-byte hex lease id")
			}
		}
		want := acquiredLeaseIdentity{ID: id, Alias: "devpool-3", Pool: "devpool", Branch: "crew/writer", Repo: leaseTestRepo,
			AcquiredAt: stamp, ContainerHostname: "node-id-3.runtime.devpool.zerops"}
		var wire struct {
			Lease  acquiredLeaseIdentity `json:"lease"`
			Reused *bool                 `json:"reused"`
		}
		if err := json.Unmarshal(raw, &wire); err != nil || stamp == "" || wire.Lease != want || wire.Reused == nil || *wire.Reused != reused {
			t.Fatalf("want lease %+v, reused %t, got %s", want, reused, raw)
		}
		var fields map[string]json.RawMessage
		var lease map[string]string
		if json.Unmarshal(raw, &fields) != nil || json.Unmarshal(fields["lease"], &lease) != nil || len(fields) != 2 || len(lease) != 7 {
			t.Fatalf("unexpected fields in writer answer")
		}
		if _, err := os.Stat(filepath.Join(deps.StateDir, leaseSessionsDir)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("CLI acquire created an MCP session directory or could not check it: %v", err)
		}
	}
}

// witness: internal/tools/lease_acquire_cli.go | return r.declined → return true
func TestAcquireLeaseAnswer_FailedAcquire_DistinguishesReservation(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name     string
		orch     http.Handler
		ssh      ops.SSHDeployer
		wantExit int
		wantCode string
	}{
		{"full pool", fullPoolOrchestrator{}, nil, 3, platform.ErrAPIError},
		{"checkout after reservation", &fakeOrchestrator{}, &leaseStubSSH{fn: func(string) ([]byte, error) {
			return nil, errors.New("checkout failed")
		}}, 1, platform.ErrSSHDeployFailed},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			deps := acquireCLIDeps(t, tt.orch)
			deps.SSH = tt.ssh
			answer, exit := AcquireLeaseAnswer(t.Context(), deps, "devpool", leaseTestRepo, "crew/writer")
			raw, err := json.Marshal(answer)
			if exit != tt.wantExit || err != nil || strings.Contains(string(raw), leaseTestToken) || strings.Contains(string(raw), leaseTestLease) {
				t.Fatalf("unexpected exit %d, marshal error %v, or secret in answer", exit, err)
			}
			switch exit {
			case 3:
				if answer.Refused == nil || answer.Refused.Code != tt.wantCode {
					t.Fatalf("full pool refusal: %s", raw)
				}
			case 1:
				if answer.Refused != nil || answer.Error == "" || answer.LeaseReleaseFailure == nil || answer.Code != tt.wantCode {
					t.Fatalf("failure after reservation: %s", raw)
				}
			}
		})
	}
}

// witness: internal/ops/lease_group.go | case r.HolderID == "" && d.mateAcquire: → case r.HolderID != "" && d.mateAcquire:
func TestAcquireLeaseAnswer_AgentHeldKey_RefusesTakeover(t *testing.T) {
	t.Parallel()
	deps := acquireCLIDeps(t, &fakeOrchestrator{})
	for _, branch := range []string{"shared", "next"} {
		_, err := ops.LeaseAcquireGroup(t.Context(), deps, []string{"devpool"}, leaseTestRepo, branch)
		if err != nil {
			t.Fatal(err)
		}
		answer, code := AcquireLeaseAnswer(t.Context(), deps, "devpool", leaseTestRepo, branch)
		if code != 3 || answer.Refused == nil || answer.Refused.Code != platform.ErrPermissionDenied {
			t.Fatalf("unexpected takeover answer, exit %d", code)
		}
		change, err := ops.LeaseReleaseChange(t.Context(), deps, "devpool-3", true)
		if err != nil || len(change.Leases) != 1 {
			t.Fatalf("agent lost its lease: %v", err)
		}
	}
}
