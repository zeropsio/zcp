package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// writeInitLeaseRecord writes the record zerops_lease keeps of apppool-3
// (<stateDir>/leases/<alias>.json, mode 0600).
func writeInitLeaseRecord(t *testing.T, stateDir string) {
	t.Helper()
	dir := filepath.Join(stateDir, "leases")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := `{"poolHostname":"apppool","poolServiceId":"svc-pool","containerId":"c-3","containerNumber":3,"containerHostname":"node-id-3.runtime.apppool.zerops","lease":"0123456789abcdef0123456789abcdef","acquiredAt":"2026-10-23T10:00:00Z"}`
	if err := os.WriteFile(filepath.Join(dir, "apppool-3.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestInitLeaseCheck_NoAPIClient_UnverifiedSaysWhy: when zcp init cannot build
// the API client a lease check asks with, every lease is unverified — never
// lost, so nothing names its release — and the refusal says which lease, what
// was not done and why; the client is tried once however many leases ask.
func TestInitLeaseCheck_NoAPIClient_UnverifiedSaysWhy(t *testing.T) {
	t.Parallel()
	var built atomic.Int32
	check := initLeaseCheck(func(context.Context) (ops.LeaseDeps, error) {
		built.Add(1)
		return ops.LeaseDeps{}, errors.New("auth: No authentication found: set ZCP_API_KEY or log in with zcli")
	})
	stateDir := t.TempDir()
	writeInitLeaseRecord(t, stateDir)

	var wg sync.WaitGroup
	errs := make([]error, 3)
	for i := range errs {
		wg.Go(func() {
			_, _, errs[i] = check(context.Background(), stateDir, "apppool-3", "mounted at boot")
		})
	}
	wg.Wait()

	for _, err := range errs {
		if err == nil {
			t.Fatal("check = nil error, want the lease unverified: there is no client to ask with")
		}
		if ops.IsLeaseLost(err) {
			t.Errorf("check = %v, reported lost: a lease zcp could not ask about is unverified", err)
		}
		for _, w := range []string{"apppool-3", "mounted at boot", "API client", "set ZCP_API_KEY"} {
			if !strings.Contains(err.Error(), w) {
				t.Errorf("refusal %q lacks %q", err, w)
			}
		}
	}
	if got := built.Load(); got != 1 {
		t.Errorf("client built %d times, want once", got)
	}
}

// TestInitLeaseCheck_BuildsTheClientOnlyWhenAsked: a container with no lease
// to mount never reaches the API — the client is built on the first check.
func TestInitLeaseCheck_BuildsTheClientOnlyWhenAsked(t *testing.T) {
	t.Parallel()
	var built atomic.Int32
	_ = initLeaseCheck(func(context.Context) (ops.LeaseDeps, error) {
		built.Add(1)
		return ops.LeaseDeps{}, nil
	})
	if got := built.Load(); got != 0 {
		t.Errorf("client built %d times before any lease asked, want none", got)
	}
}

// TestInitLeaseCheck_AsksThroughLeaseTargetChecked: the check is
// ops.LeaseTargetChecked over the resolved client and orchestrators and the
// records in the state dir zcp init reads — the one every tool that reaches a
// lease's container asks (docs/spec-workflows.md §8 DP-2): with no
// orchestrator wired, the recorded lease is refused unverified.
func TestInitLeaseCheck_AsksThroughLeaseTargetChecked(t *testing.T) {
	t.Parallel()
	check := initLeaseCheck(func(context.Context) (ops.LeaseDeps, error) {
		return ops.LeaseDeps{Client: platform.NewMock(), ProjectID: "p1"}, nil
	})
	stateDir := t.TempDir()
	writeInitLeaseRecord(t, stateDir)

	lease, ok, err := check(context.Background(), stateDir, "apppool-3", "mounted at boot")

	if !ok || lease.Alias != "apppool-3" {
		t.Fatalf("check = (%+v, %v), want the recorded lease apppool-3", lease, ok)
	}
	var pe *platform.PlatformError
	if !errors.As(err, &pe) || !strings.Contains(pe.Suggestion, "zerops_lease action=status") || ops.IsLeaseLost(err) {
		t.Errorf("check err = %v, want the unverified refusal of a lease no orchestrator can be asked about", err)
	}
	if _, ok, _ := check(context.Background(), stateDir, "apppool-4", "mounted at boot"); ok {
		t.Error("a lease with no record in the state dir was found")
	}
}

// TestResolveInitLeaseDeps_NoCredentials_SaysAuth: with neither ZCP_API_KEY
// nor a zcli login, zcp init builds no client and says why, as serve mode does.
func TestResolveInitLeaseDeps_NoCredentials_SaysAuth(t *testing.T) {
	// Not parallel — t.Setenv.
	t.Setenv("ZCP_API_KEY", "")
	t.Setenv("ZCP_ZCLI_DATA_DIR", t.TempDir())

	_, err := resolveInitLeaseDeps(context.Background())

	if err == nil || !strings.Contains(err.Error(), "ZCP_API_KEY") {
		t.Errorf("resolveInitLeaseDeps = %v, want the missing authentication named", err)
	}
}
