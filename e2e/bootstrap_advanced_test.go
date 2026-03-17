//go:build e2e

// Tests for: e2e — advanced bootstrap scenarios (multi-dep, expansion, multi-target).
//
// Tests 6, 8, 10 from the bootstrap test matrix. These involve multiple
// dependencies, phase transitions, or multiple runtime targets.
//
// Run: go test ./e2e/ -tags e2e -count=1 -v -run TestE2E_Bootstrap_ -timeout 900s

package e2e_test

import (
	"context"
	"testing"
	"time"
)

// TestE2E_Bootstrap_StandardJavaMultiDep tests standard mode with java + two deps.
// Verifies provision checker handles multiple managed dependencies (postgresql + valkey).
func TestE2E_Bootstrap_StandardJavaMultiDep(t *testing.T) {
	h := newHarness(t)
	s := newSession(t, h.srv)

	suffix := randomSuffix()[:4]
	devHostname := "b6" + suffix + "dev"
	stageHostname := "b6" + suffix + "stage"
	dbHostname := "b6d" + suffix
	cacheHostname := "b6c" + suffix

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		s.callTool("zerops_workflow", map[string]any{"action": "reset"})
		cleanupServices(ctx, h.client, h.projectID, devHostname, stageHostname, dbHostname, cacheHostname)
	})

	plan := []any{
		map[string]any{
			"runtime": map[string]any{
				"devHostname":   devHostname,
				"type":          "java@21",
				"bootstrapMode": "standard",
			},
			"dependencies": []any{
				map[string]any{
					"hostname":   dbHostname,
					"type":       "postgresql@16",
					"mode":       "NON_HA",
					"resolution": "CREATE",
				},
				map[string]any{
					"hostname":   cacheHostname,
					"type":       "valkey@7.2",
					"mode":       "NON_HA",
					"resolution": "CREATE",
				},
			},
		},
	}

	importYAML := buildImportYAML([]importService{
		{Hostname: dbHostname, Type: "postgresql@16", Mode: "NON_HA", Priority: 10},
		{Hostname: cacheHostname, Type: "valkey@7.2", Mode: "NON_HA", Priority: 10},
		{Hostname: devHostname, Type: "java@21", StartWithoutCode: true, MinContainers: 1, EnableSubdomain: true},
		{Hostname: stageHostname, Type: "java@21", EnableSubdomain: true},
	})

	resp := bootstrapAndProvision(t, s, plan, importYAML, []string{devHostname, stageHostname, dbHostname, cacheHostname})
	assertProvisionPassed(t, resp)
	assertEnvVarCheck(t, resp, dbHostname)
	assertEnvVarCheck(t, resp, cacheHostname)
	assertHasStageCheck(t, resp, stageHostname)
	t.Log("  Standard mode java + postgresql + valkey provision passed")
}

// TestE2E_Bootstrap_SimpleToStandardExpansion tests expanding from simple to standard mode.
// Phase 1: simple mode (dev + db only). Phase 2: standard mode adds stage service.
func TestE2E_Bootstrap_SimpleToStandardExpansion(t *testing.T) {
	h := newHarness(t)
	s := newSession(t, h.srv)

	suffix := randomSuffix()[:4]
	devHostname := "b8" + suffix + "dev"
	stageHostname := "b8" + suffix + "stage"
	dbHostname := "b8d" + suffix

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		s.callTool("zerops_workflow", map[string]any{"action": "reset"})
		cleanupServices(ctx, h.client, h.projectID, devHostname, stageHostname, dbHostname)
	})

	// --- Phase 1: Simple mode bootstrap (dev + db, no stage) ---
	logStep(t, 1, "Phase 1: simple mode bootstrap")
	phase1Plan := []any{
		map[string]any{
			"runtime": map[string]any{
				"devHostname":   devHostname,
				"type":          "php-nginx@8.4",
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

	phase1Import := buildImportYAML([]importService{
		{Hostname: dbHostname, Type: "postgresql@16", Mode: "NON_HA", Priority: 10},
		{Hostname: devHostname, Type: "php-nginx@8.4", StartWithoutCode: true, MinContainers: 1, EnableSubdomain: true},
	})

	resp1 := bootstrapAndProvision(t, s, phase1Plan, phase1Import, []string{devHostname, dbHostname})
	assertProvisionPassed(t, resp1)
	assertEnvVarCheck(t, resp1, dbHostname)
	assertNoStageCheck(t, resp1)
	t.Log("  Phase 1 complete: simple mode (dev + db)")

	// --- Phase 2: Standard mode expansion (add stage, db as EXISTS) ---
	logStep(t, 2, "Phase 2: expand to standard mode")
	phase2Plan := []any{
		map[string]any{
			"runtime": map[string]any{
				"devHostname":   devHostname,
				"type":          "php-nginx@8.4",
				"bootstrapMode": "standard",
			},
			"dependencies": []any{
				map[string]any{
					"hostname":   dbHostname,
					"type":       "postgresql@16",
					"resolution": "EXISTS",
				},
			},
		},
	}

	// Only import the new stage service.
	phase2Import := buildImportYAML([]importService{
		{Hostname: stageHostname, Type: "php-nginx@8.4", EnableSubdomain: true},
	})

	resp2 := bootstrapAndProvision(t, s, phase2Plan, phase2Import, []string{stageHostname})
	assertProvisionPassed(t, resp2)
	assertEnvVarCheck(t, resp2, dbHostname)
	assertHasStageCheck(t, resp2, stageHostname)
	t.Log("  Phase 2 complete: standard mode expansion (dev ACTIVE + new stage + db EXISTS)")
}

// TestE2E_Bootstrap_StandardMultiTarget tests two runtime targets sharing a postgresql.
// Target A: nodejs@22, Target B: go@1, both depend on the same postgresql@16.
// Target A uses CREATE, Target B uses SHARED resolution for the db.
func TestE2E_Bootstrap_StandardMultiTarget(t *testing.T) {
	h := newHarness(t)
	s := newSession(t, h.srv)

	suffix := randomSuffix()[:4]
	aDevHostname := "ba" + suffix + "dev"
	aStageHostname := "ba" + suffix + "stage"
	bDevHostname := "bb" + suffix + "dev"
	bStageHostname := "bb" + suffix + "stage"
	dbHostname := "bad" + suffix

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
		defer cancel()
		s.callTool("zerops_workflow", map[string]any{"action": "reset"})
		cleanupServices(ctx, h.client, h.projectID,
			aDevHostname, aStageHostname, bDevHostname, bStageHostname, dbHostname)
	})

	plan := []any{
		map[string]any{
			"runtime": map[string]any{
				"devHostname":   aDevHostname,
				"type":          "nodejs@22",
				"bootstrapMode": "standard",
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
		map[string]any{
			"runtime": map[string]any{
				"devHostname":   bDevHostname,
				"type":          "go@1",
				"bootstrapMode": "standard",
			},
			"dependencies": []any{
				map[string]any{
					"hostname":   dbHostname,
					"type":       "postgresql@16",
					"resolution": "SHARED",
				},
			},
		},
	}

	importYAML := buildImportYAML([]importService{
		{Hostname: dbHostname, Type: "postgresql@16", Mode: "NON_HA", Priority: 10},
		{Hostname: aDevHostname, Type: "nodejs@22", StartWithoutCode: true, MinContainers: 1, EnableSubdomain: true},
		{Hostname: aStageHostname, Type: "nodejs@22", EnableSubdomain: true},
		{Hostname: bDevHostname, Type: "go@1", StartWithoutCode: true, MinContainers: 1, EnableSubdomain: true},
		{Hostname: bStageHostname, Type: "go@1", EnableSubdomain: true},
	})

	resp := bootstrapAndProvision(t, s, plan, importYAML,
		[]string{aDevHostname, aStageHostname, bDevHostname, bStageHostname, dbHostname})
	assertProvisionPassed(t, resp)
	assertEnvVarCheck(t, resp, dbHostname)
	assertHasStageCheck(t, resp, aStageHostname)
	assertHasStageCheck(t, resp, bStageHostname)

	t.Logf("  Provision checks (%d total):", len(resp.CheckResult.Checks))
	for _, c := range resp.CheckResult.Checks {
		t.Logf("    %s: %s %s", c.Name, c.Status, c.Detail)
	}
	t.Log("  Standard mode multi-target (nodejs + go sharing postgresql) provision passed")
}

