//go:build e2e

// Tests for: e2e — negative bootstrap scenarios verifying provision checker failures.
//
// Tests 11-13: provision checker should correctly FAIL when requirements are not met.
//
// Run: go test ./e2e/ -tags e2e -count=1 -v -run TestE2E_Bootstrap_ProvisionFail -timeout 600s

package e2e_test

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestE2E_Bootstrap_ProvisionFail_MissingDep imports only dev (no db), but plan declares
// a postgresql dependency. Provision should fail with a "service not found" check.
func TestE2E_Bootstrap_ProvisionFail_MissingDep(t *testing.T) {
	h := newHarness(t)
	s := newSession(t, h.srv)

	suffix := randomSuffix()[:4]
	devHostname := "bn" + suffix + "dev"
	dbHostname := "bnd" + suffix

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		s.callTool("zerops_workflow", map[string]any{"action": "reset"})
		cleanupServices(ctx, h.client, h.projectID, devHostname)
	})

	plan := []any{
		map[string]any{
			"runtime": map[string]any{
				"devHostname":   devHostname,
				"type":          "nodejs@22",
				"bootstrapMode": "simple",
			},
			"dependencies": []any{
				map[string]any{
					"hostname":   dbHostname,
					"type":       "postgresql@16",
					"mode":       "NON_HA",
					"resolution": "CREATE",
				},
			},
		},
	}

	// Only import dev — deliberately omit the db.
	importYAML := buildImportYAML([]importService{
		{Hostname: devHostname, Type: "nodejs@22", StartWithoutCode: true, MinContainers: 1},
	})

	resp := bootstrapAndProvisionExpectFail(t, s, plan, importYAML, []string{devHostname})
	assertProvisionFailed(t, resp)

	// Verify the specific failure: db hostname should have an _exists check that failed.
	found := false
	for _, c := range resp.CheckResult.Checks {
		if c.Name == dbHostname+"_exists" && c.Status == "fail" {
			found = true
			if !strings.Contains(c.Detail, "not found") {
				t.Errorf("expected 'not found' in detail, got %q", c.Detail)
			}
		}
	}
	if !found {
		t.Errorf("expected %s_exists fail check, got checks: %v", dbHostname, resp.CheckResult.Checks)
	}
	t.Log("  MissingDep: provision correctly failed — dependency not imported")
}

// TestE2E_Bootstrap_ProvisionFail_DevNotReady imports dev WITHOUT startWithoutCode.
// Dev stays NEW/READY_TO_DEPLOY. Provision checker expects RUNNING/ACTIVE and should fail.
func TestE2E_Bootstrap_ProvisionFail_DevNotReady(t *testing.T) {
	h := newHarness(t)
	s := newSession(t, h.srv)

	suffix := randomSuffix()[:4]
	devHostname := "bn" + suffix + "dev"

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		s.callTool("zerops_workflow", map[string]any{"action": "reset"})
		cleanupServices(ctx, h.client, h.projectID, devHostname)
	})

	plan := []any{
		map[string]any{
			"runtime": map[string]any{
				"devHostname":   devHostname,
				"type":          "nodejs@22",
				"bootstrapMode": "simple",
			},
		},
	}

	// Import dev without startWithoutCode — it stays NEW/READY_TO_DEPLOY.
	importYAML := buildImportYAML([]importService{
		{Hostname: devHostname, Type: "nodejs@22"},
	})

	resp := bootstrapAndProvisionExpectFail(t, s, plan, importYAML, []string{devHostname})
	assertProvisionFailed(t, resp)

	// Verify the specific failure: dev hostname should have a _status check that failed.
	found := false
	for _, c := range resp.CheckResult.Checks {
		if c.Name == devHostname+"_status" && c.Status == "fail" {
			found = true
			t.Logf("  Dev status check detail: %s", c.Detail)
		}
	}
	if !found {
		t.Errorf("expected %s_status fail check, got checks: %v", devHostname, resp.CheckResult.Checks)
	}
	t.Log("  DevNotReady: provision correctly failed — dev not RUNNING/ACTIVE")
}

// TestE2E_Bootstrap_ProvisionFail_WrongStageStatus tests standard mode where stage
// is imported with startWithoutCode (becomes ACTIVE), but plan has isExisting=false
// which expects stage to be NEW/READY_TO_DEPLOY. Provision should fail.
func TestE2E_Bootstrap_ProvisionFail_WrongStageStatus(t *testing.T) {
	h := newHarness(t)
	s := newSession(t, h.srv)

	suffix := randomSuffix()[:4]
	devHostname := "bn" + suffix + "dev"
	stageHostname := "bn" + suffix + "stage"

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
		defer cancel()
		s.callTool("zerops_workflow", map[string]any{"action": "reset"})
		cleanupServices(ctx, h.client, h.projectID, devHostname, stageHostname)
	})

	plan := []any{
		map[string]any{
			"runtime": map[string]any{
				"devHostname": devHostname,
				"type":        "nodejs@22",
				// No isExisting, no bootstrapMode → standard mode, isExisting=false.
				// Checker expects stage to be NEW/READY_TO_DEPLOY.
			},
		},
	}

	// Import both with startWithoutCode — stage becomes ACTIVE, but checker expects NEW.
	importYAML := buildImportYAML([]importService{
		{Hostname: devHostname, Type: "nodejs@22", StartWithoutCode: true, MinContainers: 1},
		{Hostname: stageHostname, Type: "nodejs@22", StartWithoutCode: true, MinContainers: 1},
	})

	resp := bootstrapAndProvisionExpectFail(t, s, plan, importYAML, []string{devHostname, stageHostname})
	assertProvisionFailed(t, resp)

	// Verify the specific failure: stage should have a _status check that failed.
	found := false
	for _, c := range resp.CheckResult.Checks {
		if c.Name == stageHostname+"_status" && c.Status == "fail" {
			found = true
			t.Logf("  Stage status check detail: %s", c.Detail)
		}
	}
	if !found {
		t.Errorf("expected %s_status fail check, got checks: %v", stageHostname, resp.CheckResult.Checks)
	}
	t.Log("  WrongStageStatus: provision correctly failed — stage ACTIVE but expected NEW/READY_TO_DEPLOY")
}
