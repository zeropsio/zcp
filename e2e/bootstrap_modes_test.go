//go:build e2e

// Tests for: e2e — bootstrap modes (simple, dev) and diverse runtimes.
//
// Tests 2, 3, 4, 5, 9 from the bootstrap test matrix. Each is a single-phase
// bootstrap through provision completion with different mode/runtime/dep combos.
//
// Run: go test ./e2e/ -tags e2e -count=1 -v -run TestE2E_Bootstrap_ -timeout 600s

package e2e_test

import (
	"context"
	"testing"
	"time"
)

// TestE2E_Bootstrap_SimplePhpNginxMariadb tests simple mode with php-nginx + mariadb.
// Simple mode: no stage service, only dev + managed dependency.
func TestE2E_Bootstrap_SimplePhpNginxMariadb(t *testing.T) {
	h := newHarness(t)
	s := newSession(t, h.srv)

	suffix := randomSuffix()[:4]
	devHostname := "b2" + suffix + "dev"
	dbHostname := "b2m" + suffix

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		s.callTool("zerops_workflow", map[string]any{"action": "reset"})
		cleanupServices(ctx, h.client, h.projectID, devHostname, dbHostname)
	})

	plan := []any{
		map[string]any{
			"runtime": map[string]any{
				"devHostname":   devHostname,
				"type":          "php-nginx@8.4",
				"bootstrapMode": "simple",
			},
			"dependencies": []any{
				map[string]any{
					"hostname":   dbHostname,
					"type":       "mariadb@10.6",
					"mode":       "NON_HA",
					"resolution": "CREATE",
				},
			},
		},
	}

	importYAML := buildImportYAML([]importService{
		{Hostname: dbHostname, Type: "mariadb@10.6", Mode: "NON_HA", Priority: 10},
		{Hostname: devHostname, Type: "php-nginx@8.4", StartWithoutCode: true, MinContainers: 1, EnableSubdomain: true},
	})

	resp := bootstrapAndProvision(t, s, plan, importYAML, []string{devHostname, dbHostname})
	assertProvisionPassed(t, resp)
	assertEnvVarCheck(t, resp, dbHostname)
	assertNoStageCheck(t, resp)
	t.Log("  Simple mode php-nginx + mariadb provision passed")
}

// TestE2E_Bootstrap_DevGoValkey tests dev mode with go + valkey.
// Dev mode: no stage service, only dev + managed dependency.
func TestE2E_Bootstrap_DevGoValkey(t *testing.T) {
	h := newHarness(t)
	s := newSession(t, h.srv)

	suffix := randomSuffix()[:4]
	devHostname := "b3" + suffix + "dev"
	cacheHostname := "b3c" + suffix

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		s.callTool("zerops_workflow", map[string]any{"action": "reset"})
		cleanupServices(ctx, h.client, h.projectID, devHostname, cacheHostname)
	})

	plan := []any{
		map[string]any{
			"runtime": map[string]any{
				"devHostname":   devHostname,
				"type":          "go@1",
				"bootstrapMode": "dev",
			},
			"dependencies": []any{
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
		{Hostname: cacheHostname, Type: "valkey@7.2", Mode: "NON_HA", Priority: 10},
		{Hostname: devHostname, Type: "go@1", StartWithoutCode: true, MinContainers: 1, EnableSubdomain: true},
	})

	resp := bootstrapAndProvision(t, s, plan, importYAML, []string{devHostname, cacheHostname})
	assertProvisionPassed(t, resp)
	assertEnvVarCheck(t, resp, cacheHostname)
	assertNoStageCheck(t, resp)
	t.Log("  Dev mode go + valkey provision passed")
}

// TestE2E_Bootstrap_StandardPythonNats tests standard mode with python + nats.
// Standard mode: dev + stage + managed dependency.
func TestE2E_Bootstrap_StandardPythonNats(t *testing.T) {
	h := newHarness(t)
	s := newSession(t, h.srv)

	suffix := randomSuffix()[:4]
	devHostname := "b4" + suffix + "dev"
	stageHostname := "b4" + suffix + "stage"
	mqHostname := "b4q" + suffix

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		s.callTool("zerops_workflow", map[string]any{"action": "reset"})
		cleanupServices(ctx, h.client, h.projectID, devHostname, stageHostname, mqHostname)
	})

	plan := []any{
		map[string]any{
			"runtime": map[string]any{
				"devHostname": devHostname,
				"type":        "python@3.12",
			},
			"dependencies": []any{
				map[string]any{
					"hostname":   mqHostname,
					"type":       "nats@2.10",
					"mode":       "NON_HA",
					"resolution": "CREATE",
				},
			},
		},
	}

	importYAML := buildImportYAML([]importService{
		{Hostname: mqHostname, Type: "nats@2.10", Mode: "NON_HA", Priority: 10},
		{Hostname: devHostname, Type: "python@3.12", StartWithoutCode: true, MinContainers: 1, EnableSubdomain: true},
		{Hostname: stageHostname, Type: "python@3.12", EnableSubdomain: true},
	})

	resp := bootstrapAndProvision(t, s, plan, importYAML, []string{devHostname, stageHostname, mqHostname})
	assertProvisionPassed(t, resp)
	assertEnvVarCheck(t, resp, mqHostname)
	assertHasStageCheck(t, resp, stageHostname)
	t.Log("  Standard mode python + nats provision passed")
}

// TestE2E_Bootstrap_SimpleStaticObjStorage tests simple mode with static + object-storage.
// Object storage has no env vars to check.
func TestE2E_Bootstrap_SimpleStaticObjStorage(t *testing.T) {
	h := newHarness(t)
	s := newSession(t, h.srv)

	suffix := randomSuffix()[:4]
	devHostname := "b5" + suffix + "dev"
	storageHostname := "b5s" + suffix

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		s.callTool("zerops_workflow", map[string]any{"action": "reset"})
		cleanupServices(ctx, h.client, h.projectID, devHostname, storageHostname)
	})

	plan := []any{
		map[string]any{
			"runtime": map[string]any{
				"devHostname":   devHostname,
				"type":          "static",
				"bootstrapMode": "simple",
			},
			"dependencies": []any{
				map[string]any{
					"hostname":   storageHostname,
					"type":       "object-storage",
					"resolution": "CREATE",
				},
			},
		},
	}

	importYAML := buildImportYAML([]importService{
		{Hostname: storageHostname, Type: "object-storage", ObjStorageSize: 1},
		{Hostname: devHostname, Type: "static", StartWithoutCode: true, MinContainers: 1, EnableSubdomain: true},
	})

	resp := bootstrapAndProvision(t, s, plan, importYAML, []string{devHostname, storageHostname})
	assertProvisionPassed(t, resp)
	assertNoEnvVarCheck(t, resp, storageHostname)
	assertNoStageCheck(t, resp)
	t.Log("  Simple mode static + object-storage provision passed (no env var check for storage)")
}

// TestE2E_Bootstrap_DevDotnetPostgres tests dev mode with dotnet + postgresql.
func TestE2E_Bootstrap_DevDotnetPostgres(t *testing.T) {
	h := newHarness(t)
	s := newSession(t, h.srv)

	suffix := randomSuffix()[:4]
	devHostname := "b9" + suffix + "dev"
	dbHostname := "b9d" + suffix

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		s.callTool("zerops_workflow", map[string]any{"action": "reset"})
		cleanupServices(ctx, h.client, h.projectID, devHostname, dbHostname)
	})

	plan := []any{
		map[string]any{
			"runtime": map[string]any{
				"devHostname":   devHostname,
				"type":          "dotnet@9",
				"bootstrapMode": "dev",
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

	importYAML := buildImportYAML([]importService{
		{Hostname: dbHostname, Type: "postgresql@16", Mode: "NON_HA", Priority: 10},
		{Hostname: devHostname, Type: "dotnet@9", StartWithoutCode: true, MinContainers: 1, EnableSubdomain: true},
	})

	resp := bootstrapAndProvision(t, s, plan, importYAML, []string{devHostname, dbHostname})
	assertProvisionPassed(t, resp)
	assertEnvVarCheck(t, resp, dbHostname)
	assertNoStageCheck(t, resp)
	t.Log("  Dev mode dotnet + postgresql provision passed")
}
