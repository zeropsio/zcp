//go:build e2e

// Tests for: e2e — Laravel recipe claims verification against real Zerops API.
//
// Verifies: service-level env vars for cross-service ${hostname_varName} refs,
// project-level env vars (APP_KEY), managed service env var patterns.
//
// KEY FINDING: ${hostname_varName} in zerops.yml resolves at container level
// from service-level env vars, NOT from project env vars.
//
// Run: go test ./e2e/ -tags e2e -run TestLaravelRecipe -count=1 -v -timeout 600s

package e2e_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

type discoverEnv struct {
	Key         string `json:"key"`
	Value       string `json:"value"`
	IsReference bool   `json:"isReference,omitempty"`
}

type discoverService struct {
	Hostname string        `json:"hostname"`
	Type     string        `json:"type"`
	Status   string        `json:"status"`
	Envs     []discoverEnv `json:"envs,omitempty"`
}

type discoverResult struct {
	Project struct {
		ID   string        `json:"id"`
		Name string        `json:"name"`
		Envs []discoverEnv `json:"envs,omitempty"`
	} `json:"project"`
	Services []discoverService `json:"services"`
}

func parseDiscover(t *testing.T, text string) discoverResult {
	t.Helper()
	var r discoverResult
	if err := json.Unmarshal([]byte(text), &r); err != nil {
		t.Fatalf("parse discover: %v", err)
	}
	return r
}

func findServiceEnvs(r discoverResult, hostname string) []discoverEnv {
	for _, svc := range r.Services {
		if svc.Hostname == hostname {
			return svc.Envs
		}
	}
	return nil
}

func findProjectEnv(r discoverResult, key string) (string, bool) {
	for _, e := range r.Project.Envs {
		if e.Key == key {
			return e.Value, true
		}
	}
	return "", false
}

func findEnv(envs []discoverEnv, key string) (string, bool) {
	for _, e := range envs {
		if e.Key == key {
			return e.Value, true
		}
	}
	return "", false
}

func assertSvcEnv(t *testing.T, envs []discoverEnv, key, hostname string) string {
	t.Helper()
	val, found := findEnv(envs, key)
	if !found {
		t.Errorf("service %s: env %s not found — ${%s_%s} would fail", hostname, key, hostname, key)
		return ""
	}
	if val == "" {
		t.Errorf("service %s: env %s is empty", hostname, key)
	}
	return val
}

func truncateValue(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// TestLaravelRecipe_FullStack creates all Laravel recipe services in one import
// (php-nginx + PostgreSQL + Valkey + ObjectStorage + MariaDB) and verifies
// all env var claims from the recipe.
func TestLaravelRecipe_FullStack(t *testing.T) {
	h := newHarness(t)
	s := newSession(t, h.srv)

	suffix := randomSuffix()
	phpHost := "lrvph" + suffix
	pgHost := "lrvpg" + suffix
	vkHost := "lrvvk" + suffix
	s3Host := "lrvs3" + suffix
	myHost := "lrvmy" + suffix

	allHosts := []string{phpHost, pgHost, vkHost, s3Host, myHost}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
		defer cancel()
		cleanupServices(ctx, h.client, h.projectID, allHosts...)
	})

	// Start workflow and import all services at once.
	s.callTool("zerops_workflow", map[string]any{"action": "reset"})
	s.mustCallSuccess("zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "bootstrap",
		"intent":   "Laravel recipe E2E: full stack verification",
	})

	importYAML := buildImportYAML([]importService{
		{Hostname: phpHost, Type: "php-nginx@8.4", StartWithoutCode: true, EnableSubdomain: true, Priority: 5},
		{Hostname: pgHost, Type: "postgresql@16", Mode: "NON_HA", Priority: 10},
		{Hostname: vkHost, Type: "valkey@7.2", Mode: "NON_HA", Priority: 10},
		{Hostname: s3Host, Type: "object-storage", ObjStorageSize: 1, Priority: 10},
		{Hostname: myHost, Type: "mariadb@10.6", Mode: "NON_HA", Priority: 10},
	})
	s.mustCallSuccess("zerops_import", map[string]any{"content": importYAML})

	// Wait for all services.
	for _, h := range allHosts {
		waitForServiceStatus(s, h, "RUNNING", "ACTIVE")
	}

	// Discover with env vars.
	text := s.mustCallSuccess("zerops_discover", map[string]any{"includeEnvs": true})
	r := parseDiscover(t, text)

	// --- PostgreSQL env vars ---
	t.Run("PostgreSQL", func(t *testing.T) {
		envs := findServiceEnvs(r, pgHost)
		if envs == nil {
			t.Fatalf("PostgreSQL service %s not found", pgHost)
		}

		for _, key := range []string{"hostname", "port", "dbName", "user", "password", "connectionString"} {
			val := assertSvcEnv(t, envs, key, pgHost)
			t.Logf("  %s = %q → ref: ${%s_%s}", key, truncateValue(val, 30), pgHost, key)
		}

		// Log all env vars.
		t.Log("  Complete PostgreSQL env vars:")
		for _, e := range envs {
			t.Logf("    %s = %q (isRef=%v)", e.Key, truncateValue(e.Value, 40), e.IsReference)
		}
	})

	// --- Valkey env vars ---
	t.Run("Valkey", func(t *testing.T) {
		envs := findServiceEnvs(r, vkHost)
		if envs == nil {
			t.Fatalf("Valkey service %s not found", vkHost)
		}

		// Check what host-related keys exist.
		_, hasHost := findEnv(envs, "host")
		_, hasHostname := findEnv(envs, "hostname")
		t.Logf("  'host' exists: %v, 'hostname' exists: %v", hasHost, hasHostname)

		if !hasHost && hasHostname {
			t.Log("  CONFIRMED: Valkey uses 'hostname' NOT 'host' — recipe must use ${redis_hostname}")
		}

		assertSvcEnv(t, envs, "port", vkHost)
		assertSvcEnv(t, envs, "hostname", vkHost)

		t.Log("  Complete Valkey env vars:")
		for _, e := range envs {
			t.Logf("    %s = %q (isRef=%v)", e.Key, truncateValue(e.Value, 40), e.IsReference)
		}
	})

	// --- Object Storage env vars ---
	t.Run("ObjectStorage", func(t *testing.T) {
		envs := findServiceEnvs(r, s3Host)
		if envs == nil {
			t.Fatalf("Object Storage service %s not found", s3Host)
		}

		for _, key := range []string{"accessKeyId", "secretAccessKey", "apiUrl", "bucketName", "apiHost"} {
			val := assertSvcEnv(t, envs, key, s3Host)
			t.Logf("  %s = %q → ref: ${%s_%s}", key, truncateValue(val, 40), s3Host, key)
		}

		t.Log("  Complete Object Storage env vars:")
		for _, e := range envs {
			t.Logf("    %s = %q", e.Key, truncateValue(e.Value, 40))
		}
	})

	// --- MariaDB env vars ---
	t.Run("MariaDB", func(t *testing.T) {
		envs := findServiceEnvs(r, myHost)
		if envs == nil {
			t.Fatalf("MariaDB service %s not found", myHost)
		}

		// Check same pattern as PostgreSQL.
		for _, key := range []string{"hostname", "port", "user", "password", "connectionString"} {
			val, found := findEnv(envs, key)
			if !found {
				t.Errorf("MariaDB env %s not found — may differ from PostgreSQL", key)
			} else {
				t.Logf("  %s = %q", key, truncateValue(val, 40))
			}
		}

		// Check dbName.
		_, hasDbName := findEnv(envs, "dbName")
		t.Logf("  dbName present: %v", hasDbName)

		t.Log("  Complete MariaDB env vars:")
		for _, e := range envs {
			t.Logf("    %s = %q (isRef=%v)", e.Key, truncateValue(e.Value, 40), e.IsReference)
		}
	})

	// --- PHP-Nginx env vars ---
	t.Run("PHPNginx", func(t *testing.T) {
		envs := findServiceEnvs(r, phpHost)
		if envs == nil {
			t.Fatalf("PHP service %s not found", phpHost)
		}

		// zeropsSubdomain.
		subdomain, found := findEnv(envs, "zeropsSubdomain")
		if !found {
			t.Error("zeropsSubdomain not found — recipe uses ${zeropsSubdomain}")
		} else {
			t.Logf("  zeropsSubdomain = %q", subdomain)
			if !strings.Contains(subdomain, phpHost) {
				t.Errorf("zeropsSubdomain should contain hostname %s", phpHost)
			}
		}

		// appVersionId.
		_, found = findEnv(envs, "appVersionId")
		if !found {
			t.Error("appVersionId not found — recipe uses ${appVersionId} in initCommands")
		}

		t.Log("  Complete PHP-Nginx env vars:")
		for _, e := range envs {
			t.Logf("    %s = %q", e.Key, truncateValue(e.Value, 40))
		}
	})
}

// TestLaravelRecipe_ProjectEnvInheritance verifies project-level env vars
// set via zerops_env are visible in discover (APP_KEY use case).
func TestLaravelRecipe_ProjectEnvInheritance(t *testing.T) {
	h := newHarness(t)
	s := newSession(t, h.srv)

	testKey := "LRVTEST_" + randomSuffix()
	testValue := "base64:TestValue12345678901234567890abc"

	s.mustCallSuccess("zerops_env", map[string]any{
		"action":    "set",
		"project":   true,
		"variables": []string{testKey + "=" + testValue},
	})

	time.Sleep(3 * time.Second)

	text := s.mustCallSuccess("zerops_discover", map[string]any{"includeEnvs": true})
	r := parseDiscover(t, text)

	val, found := findProjectEnv(r, testKey)
	if !found {
		t.Fatalf("project env %s not found after setting", testKey)
	}
	if val != testValue {
		t.Errorf("project env %s = %q, want %q", testKey, val, testValue)
	}
	t.Logf("  Project env %s = %q (confirmed)", testKey, truncateValue(val, 40))

	// Cleanup.
	s.mustCallSuccess("zerops_env", map[string]any{
		"action":    "delete",
		"project":   true,
		"variables": []string{testKey},
	})
}
