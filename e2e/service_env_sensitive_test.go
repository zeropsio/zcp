//go:build e2e

// Tests for: service userData sensitive round-trip against the
// live 2026-08 model (docs/spec-zerops-env-lifecycle.md §1/§7): the
// service env Type enum collapsed to USER|SYSTEM and `sensitive` became a
// required per-write flag. This is the ONE test at any tier that writes a
// SERVICE-scope env against the real API and proves the write → read →
// yaml-baked-subtraction → delete contract end to end — no test at a
// lower tier can (they all run against the mock).
//
// MUTATING (one service userData write + one delete on the target
// service) — opt-in via ZCP_E2E_ENV_SERVICE:
//
//	export ZCP_E2E_ENV_SERVICE=appdev
//	export ZCP_API_KEY=<token>
//	go test ./e2e/ -tags e2e -run TestE2E_ServiceEnv_SensitiveRoundTrip -v -timeout 120s
//
// SKIPS when ZCP_E2E_ENV_SERVICE or ZCP_API_KEY is unset, or under -short.

package e2e_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// waitServiceEnvProcessStrict polls a process to a terminal state and
// FAILS the test on FAILED/CANCELED. Deliberately NOT
// waitForProcessDirect (process_helpers_test.go), which treats those as
// completion — a sensitive-flag write silently failing must not be
// mistaken for success in this round-trip proof.
func waitServiceEnvProcessStrict(t *testing.T, ctx context.Context, client platform.Client, processID string) {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		p, err := client.GetProcess(ctx, processID)
		if err != nil {
			t.Fatalf("GetProcess %s: %v", processID, err)
		}
		switch p.Status {
		case platform.ProcessStatusFinished:
			return
		case platform.ProcessStatusFailed:
			t.Fatalf("process %s FAILED (%v)", processID, p.FailReason)
		case platform.ProcessStatusCanceled:
			t.Fatalf("process %s CANCELED", processID)
		}
		time.Sleep(3 * time.Second)
	}
	t.Fatalf("process %s did not reach FINISHED within timeout", processID)
}

// TestE2E_ServiceEnv_SensitiveRoundTrip proves the live write/read
// contract for a service-scope sensitive var: ops.EnvSetSecretService
// (CreateServiceEnvVar) writes Type=USER, sensitive=true; the slim /env
// reflects it verbatim to an owner token; when the target has an active
// app version, the yaml-baked layer (ops.AppVersionEnvVars) never
// contains the probe key while the user-set subtraction
// (ops.FetchServiceUserEnvs) does; ops.EnvDelete removes it cleanly. No
// env value is ever logged.
func TestE2E_ServiceEnv_SensitiveRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E live-platform test in short mode")
	}
	hostname := os.Getenv("ZCP_E2E_ENV_SERVICE")
	if hostname == "" {
		t.Skip("ZCP_E2E_ENV_SERVICE not set — skipping live service-env round-trip")
	}
	token := os.Getenv("ZCP_API_KEY")
	if token == "" {
		t.Skip("ZCP_API_KEY not set — skipping E2E test")
	}
	apiHost := os.Getenv("ZCP_API_HOST")
	if apiHost == "" {
		apiHost = "api.app-prg1.zerops.io"
	}
	client, err := platform.NewZeropsClient(token, apiHost)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	authInfo, err := auth.Resolve(ctx, client)
	if err != nil {
		t.Fatalf("auth resolve: %v", err)
	}
	projectID := authInfo.ProjectID

	svc, err := ops.LookupService(ctx, client, projectID, hostname)
	if err != nil {
		t.Fatalf("lookup service %q: %v", hostname, err)
	}

	suffix := make([]byte, 4)
	if _, randErr := rand.Read(suffix); randErr != nil {
		t.Fatalf("rand: %v", randErr)
	}
	probeKey := "ZCP_E2E_ENV_PROBE_" + hex.EncodeToString(suffix)

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cleanupCancel()
		// Best-effort — also runs on test failure, so a probe that never
		// got past a partial write doesn't leak into the project forever.
		_, _ = ops.EnvDelete(cleanupCtx, client, projectID, hostname, false, []string{probeKey})
	})

	setProc, err := ops.EnvSetSecretService(ctx, client, svc.ID, probeKey, "v1")
	if err != nil {
		t.Fatalf("EnvSetSecretService: %v", err)
	}
	if setProc == nil {
		t.Fatal("EnvSetSecretService returned nil process")
	}
	waitServiceEnvProcessStrict(t, ctx, client, setProc.ID)

	envs, err := ops.FetchServiceEnv(ctx, client, svc.ID)
	if err != nil {
		t.Fatalf("FetchServiceEnv: %v", err)
	}
	var probe *platform.ServiceEnvVar
	for i := range envs {
		if envs[i].Key == probeKey {
			probe = &envs[i]
			break
		}
	}
	if probe == nil {
		t.Fatalf("probe key %q not present in slim /env after write", probeKey)
	}
	if probe.Type != platform.ServiceEnvUser {
		t.Errorf("Type = %q, want %q", probe.Type, platform.ServiceEnvUser)
	}
	if !probe.Sensitive {
		t.Error("Sensitive = false, want true — ZCP writes every service-scope var sensitive:true")
	}
	if probe.Content != "v1" {
		t.Error("Content mismatch after write (value intentionally not logged)")
	}

	svcFresh, err := client.GetService(ctx, svc.ID)
	if err != nil {
		t.Fatalf("GetService: %v", err)
	}
	if svcFresh.ActiveAppVersion != nil && svcFresh.ActiveAppVersion.ID != "" {
		yamlBaked, ybErr := ops.AppVersionEnvVars(ctx, client, *svcFresh)
		if ybErr != nil {
			t.Fatalf("AppVersionEnvVars: %v", ybErr)
		}
		for _, v := range yamlBaked {
			if v.Key == probeKey {
				t.Fatalf("probe key %q must NEVER appear in the yaml-baked app-version layer", probeKey)
			}
		}

		userEnvs, ueErr := ops.FetchServiceUserEnvs(ctx, client, svc.ID)
		if ueErr != nil {
			t.Fatalf("FetchServiceUserEnvs: %v", ueErr)
		}
		found := false
		for _, v := range userEnvs {
			if v.Key == probeKey {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("probe key %q must be present in the user-set layer (FetchServiceUserEnvs)", probeKey)
		}
	} else {
		t.Log("target has no active app version — yaml-baked subtraction not exercised")
	}

	delResult, err := ops.EnvDelete(ctx, client, projectID, hostname, false, []string{probeKey})
	if err != nil {
		t.Fatalf("EnvDelete: %v", err)
	}
	if delResult == nil || delResult.Process == nil {
		t.Fatal("EnvDelete returned no process")
	}
	waitServiceEnvProcessStrict(t, ctx, client, delResult.Process.ID)

	envsAfter, err := ops.FetchServiceEnv(ctx, client, svc.ID)
	if err != nil {
		t.Fatalf("FetchServiceEnv after delete: %v", err)
	}
	for _, e := range envsAfter {
		if e.Key == probeKey {
			t.Fatalf("probe key %q still present after delete", probeKey)
		}
	}
}
