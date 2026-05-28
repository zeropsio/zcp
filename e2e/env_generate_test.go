//go:build e2e

// Tests for: e2e — EnvGenerateDotenv recursive expansion against real Zerops API.
//
// Verifies the Phase 4 doctrine: when a yaml run.envVariables value references
// a managed-service template var (e.g. ${db_connectionString} where
// db.connectionString is itself postgresql://${user}:${password}@${hostname}:${port}/${dbName}),
// the .env generator must recursively resolve the inner sibling refs against
// db's own env vars, producing a fully-resolved URL with no ${...} placeholders.
//
// This test was missing from the e2e suite — the closest sibling
// (TestE2E_LocalDeploy_EnvVarBridge) only exercises FormatEnvFile (multi-service
// dump shape), not the EnvGenerateDotenv recursive path that local agents
// actually call via zerops_env action="generate-dotenv".
//
// Architecture: bypasses MCP + workflow lifecycle entirely. Provisions
// services via direct platform.Client.ImportServices, then calls
// ops.EnvGenerateDotenv. The test purpose is to verify the expander
// against real API responses, not the workflow gate.
//
// Prerequisites:
//   - ZCP_API_KEY set
//
// Run: go test ./e2e/ -tags e2e -run TestE2E_EnvGenerateDotenv -v -timeout 600s

package e2e_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// TestE2E_EnvGenerateDotenv_RecursiveConnectionString provisions an app + db
// pair via direct API, writes a yaml that references ${dbHost_connectionString},
// calls EnvGenerateDotenv against the real API, and asserts the resulting .env
// has a fully-resolved postgresql URL — no literal ${user}/${password}/etc.
func TestE2E_EnvGenerateDotenv_RecursiveConnectionString(t *testing.T) {
	h := newLocalHarness(t)

	suffix := randomSuffix()
	appHost := "zcpld" + suffix
	dbHost := "zcpdb" + suffix

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		cleanupServices(ctx, h.client, h.projectID, appHost, dbHost)
	})

	// Provision app + db via direct API import (bypasses MCP/workflow).
	// db has the connectionString template Zerops emits for managed Postgres;
	// we want to verify cross-service ref to it recursively resolves
	// against db's own siblings.
	importYAML := fmt.Sprintf(`services:
  - hostname: %s
    type: postgresql@16
    mode: NON_HA
    priority: 1
  - hostname: %s
    type: nodejs@22
    minContainers: 1
    startWithoutCode: true
`, dbHost, appHost)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()
	importResult, err := h.client.ImportServices(ctx, h.projectID, importYAML)
	if err != nil {
		t.Fatalf("ImportServices: %v", err)
	}
	t.Logf("  Imported %d services", len(importResult.ServiceStacks))
	for _, s := range importResult.ServiceStacks {
		if s.Error != nil {
			t.Fatalf("import error for %s: %s (%s)", s.Name, s.Error.Message, s.Error.Code)
		}
		t.Logf("    %s id=%s processes=%d", s.Name, s.ID, len(s.Processes))
	}

	// Wait for both services to reach RUNNING/ACTIVE state.
	if err := waitForServiceActive(ctx, h.client, h.projectID, dbHost, 180*time.Second); err != nil {
		t.Fatalf("db not active: %v", err)
	}
	if err := waitForServiceActive(ctx, h.client, h.projectID, appHost, 180*time.Second); err != nil {
		t.Fatalf("app not active: %v", err)
	}
	t.Logf("  Both services active")

	// Write a local working directory with zerops.yaml pointing at the app.
	// run.envVariables references the db service via three patterns:
	//   - DATABASE_URL: ${db_connectionString} — recursive (template inside)
	//   - DB_HOST: ${db_hostname} — flat literal lookup
	//   - DB_PORT: ${db_port} — flat literal lookup
	// All three should appear fully resolved in the generated .env.
	workdir := t.TempDir()
	yamlContent := fmt.Sprintf(`zerops:
  - setup: %s
    build:
      base: nodejs@22
      deployFiles: ./
    run:
      base: nodejs@22
      ports:
        - port: 3000
          httpSupport: true
      start: node server.js
      envVariables:
        DATABASE_URL: ${%s_connectionString}
        DB_HOST: ${%s_hostname}
        DB_PORT: ${%s_port}
`, appHost, dbHost, dbHost, dbHost)
	if err := os.WriteFile(filepath.Join(workdir, "zerops.yaml"), []byte(yamlContent), 0o644); err != nil {
		t.Fatalf("write zerops.yaml: %v", err)
	}

	// Call generate-dotenv via direct ops API.
	result, err := ops.EnvGenerateDotenv(ctx, h.client, h.projectID, appHost, workdir, ops.EnvGenerateDotenvOptions{})
	if err != nil {
		t.Fatalf("EnvGenerateDotenv: %v", err)
	}
	t.Logf("  Result: path=%s services=%d variables=%d vpnHint=%q",
		result.Path, result.Services, result.Variables, result.VPNHint)

	envBytes, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatalf("read .env: %v", err)
	}
	envContent := string(envBytes)
	t.Logf("  Generated .env content:\n%s", envContent)

	// Assertion 1: the cross-service connectionString must be fully resolved —
	// no leftover ${...} placeholders (sibling refs from db's template) in
	// the DATABASE_URL line.
	dbURLLine := extractEnvLine(envContent, "DATABASE_URL")
	if dbURLLine == "" {
		t.Fatalf("DATABASE_URL line missing from .env:\n%s", envContent)
	}
	if strings.Contains(dbURLLine, "${") {
		t.Errorf("DATABASE_URL contains unresolved ${...} placeholder — recursive expansion failed:\n  %s", dbURLLine)
	}

	// Assertion 2: shape — DATABASE_URL should look like a Postgres URL with
	// concrete user/password/host/port/dbName. We don't pin exact values
	// (Zerops generates them), just the structural pattern.
	postgresURLRe := regexp.MustCompile(`^DATABASE_URL=postgresql://[^:@$]+:[^@$]+@[^:@$]+:\d+(/[^/$]+)?$`)
	if !postgresURLRe.MatchString(dbURLLine) {
		t.Errorf("DATABASE_URL not in resolved postgresql shape: %q", dbURLLine)
	}

	// Assertion 3: flat lookups resolve correctly.
	if got := extractEnvLine(envContent, "DB_HOST"); got != fmt.Sprintf("DB_HOST=%s", dbHost) {
		t.Errorf("DB_HOST = %q, want DB_HOST=%s", got, dbHost)
	}
	dbPortLine := extractEnvLine(envContent, "DB_PORT")
	if !regexp.MustCompile(`^DB_PORT=\d+$`).MatchString(dbPortLine) {
		t.Errorf("DB_PORT = %q, want DB_PORT=<digits>", dbPortLine)
	}

	// Assertion 4: result.Services count matches the unique cross-service
	// hosts referenced (1: db).
	if result.Services != 1 {
		t.Errorf("result.Services = %d, want 1 (only db referenced)", result.Services)
	}
}

// TestE2E_EnvGenerateDotenv_FlatVsRecursiveTogether verifies that the
// connectionString recursive form and an individually-composed Postgres URL
// from flat refs both resolve to equivalent shapes. Different platform-side
// rendering of dbName / trailing path may make values differ, but both must
// have all ${...} resolved.
func TestE2E_EnvGenerateDotenv_FlatVsRecursiveTogether(t *testing.T) {
	h := newLocalHarness(t)

	suffix := randomSuffix()
	appHost := "zcpld" + suffix
	dbHost := "zcpdb" + suffix

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		cleanupServices(ctx, h.client, h.projectID, appHost, dbHost)
	})

	importYAML := fmt.Sprintf(`services:
  - hostname: %s
    type: postgresql@16
    mode: NON_HA
    priority: 1
  - hostname: %s
    type: nodejs@22
    minContainers: 1
    startWithoutCode: true
`, dbHost, appHost)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()
	importResult, err := h.client.ImportServices(ctx, h.projectID, importYAML)
	if err != nil {
		t.Fatalf("ImportServices: %v", err)
	}
	for _, s := range importResult.ServiceStacks {
		if s.Error != nil {
			t.Fatalf("import error for %s: %s (%s)", s.Name, s.Error.Message, s.Error.Code)
		}
	}
	if err := waitForServiceActive(ctx, h.client, h.projectID, dbHost, 180*time.Second); err != nil {
		t.Fatalf("db not active: %v", err)
	}
	if err := waitForServiceActive(ctx, h.client, h.projectID, appHost, 180*time.Second); err != nil {
		t.Fatalf("app not active: %v", err)
	}

	workdir := t.TempDir()
	yamlContent := fmt.Sprintf(`zerops:
  - setup: %s
    build:
      base: nodejs@22
      deployFiles: ./
    run:
      base: nodejs@22
      ports:
        - port: 3000
          httpSupport: true
      start: node server.js
      envVariables:
        DATABASE_URL_FROM_CS: ${%s_connectionString}
        DATABASE_URL_FROM_FIELDS: postgresql://${%s_user}:${%s_password}@${%s_hostname}:${%s_port}/${%s_dbName}
`, appHost, dbHost, dbHost, dbHost, dbHost, dbHost, dbHost)
	if err := os.WriteFile(filepath.Join(workdir, "zerops.yaml"), []byte(yamlContent), 0o644); err != nil {
		t.Fatalf("write zerops.yaml: %v", err)
	}

	result, err := ops.EnvGenerateDotenv(ctx, h.client, h.projectID, appHost, workdir, ops.EnvGenerateDotenvOptions{})
	if err != nil {
		t.Fatalf("EnvGenerateDotenv: %v", err)
	}

	envBytes, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatalf("read .env: %v", err)
	}
	envContent := string(envBytes)
	t.Logf("  Generated .env content:\n%s", envContent)

	csLine := extractEnvLine(envContent, "DATABASE_URL_FROM_CS")
	fieldsLine := extractEnvLine(envContent, "DATABASE_URL_FROM_FIELDS")

	for _, l := range []struct{ name, line string }{
		{"DATABASE_URL_FROM_CS", csLine},
		{"DATABASE_URL_FROM_FIELDS", fieldsLine},
	} {
		if l.line == "" {
			t.Errorf("%s missing from .env", l.name)
			continue
		}
		if strings.Contains(l.line, "${") {
			t.Errorf("%s has unresolved ${...}: %q", l.name, l.line)
		}
		if !strings.HasPrefix(l.line, l.name+"=postgresql://") {
			t.Errorf("%s not in postgresql:// shape: %q", l.name, l.line)
		}
	}
	t.Logf("  CS form:     %s", csLine)
	t.Logf("  Fields form: %s", fieldsLine)
}

// TestE2E_EnvGenerateDotenv_ProjectOnly validates the result-based empty
// guard against the real API: a setup whose run.envVariables is empty is a
// valid local bridge when project envs contribute. The old wrapper rejected
// it with "no run.envVariables"; now it renders the project layer. Provisions
// a code-less runtime (no deploy needed) + a throwaway project env, then
// asserts generate-dotenv succeeds and the project var lands in .env.
func TestE2E_EnvGenerateDotenv_ProjectOnly(t *testing.T) {
	h := newLocalHarness(t)

	suffix := randomSuffix()
	appHost := "zcpld" + suffix
	projKey := "ZCPE2E_PROJONLY_" + strings.ToUpper(suffix)

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		cleanupServices(ctx, h.client, h.projectID, appHost)
		if envs, err := h.client.GetProjectEnv(ctx, h.projectID); err == nil {
			for _, e := range envs {
				if e.Key == projKey {
					if proc, derr := h.client.DeleteProjectEnv(ctx, e.ID); derr == nil && proc != nil {
						waitForProcessDirect(ctx, h.client, proc.ID)
					}
				}
			}
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	// Code-less runtime — active without a deploy; enough for the planner to
	// list services + read project env. No db: the project-only case needs
	// no cross-service refs.
	importYAML := fmt.Sprintf(`services:
  - hostname: %s
    type: nodejs@22
    minContainers: 1
    startWithoutCode: true
`, appHost)
	if _, err := h.client.ImportServices(ctx, h.projectID, importYAML); err != nil {
		t.Fatalf("ImportServices: %v", err)
	}
	if err := waitForServiceActive(ctx, h.client, h.projectID, appHost, 180*time.Second); err != nil {
		t.Fatalf("app not active: %v", err)
	}

	// Throwaway project env — the only source the project-only plan renders.
	proc, err := h.client.CreateProjectEnv(ctx, h.projectID, projKey, "projonly-value", false)
	if err != nil {
		t.Fatalf("CreateProjectEnv: %v", err)
	}
	if proc != nil {
		waitForProcessDirect(ctx, h.client, proc.ID)
	}

	// Local zerops.yaml whose setup has NO run.envVariables — the case the
	// old input-based guard rejected outright.
	workdir := t.TempDir()
	yamlContent := fmt.Sprintf(`zerops:
  - setup: %s
    build:
      base: nodejs@22
      deployFiles: ./
    run:
      base: nodejs@22
      start: node server.js
`, appHost)
	if err := os.WriteFile(filepath.Join(workdir, "zerops.yaml"), []byte(yamlContent), 0o644); err != nil {
		t.Fatalf("write zerops.yaml: %v", err)
	}

	result, err := ops.EnvGenerateDotenv(ctx, h.client, h.projectID, appHost, workdir, ops.EnvGenerateDotenvOptions{})
	if err != nil {
		t.Fatalf("EnvGenerateDotenv (project-only) errored — the result-based guard should allow it: %v", err)
	}
	envBytes, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatalf("read .env: %v", err)
	}
	if got := extractEnvLine(string(envBytes), projKey); got != projKey+"=projonly-value" {
		t.Errorf("project-only .env missing the project var: got %q, full:\n%s", got, string(envBytes))
	}
	t.Logf("  project-only .env rendered %d var(s); %s present", result.Variables, projKey)
}

// extractEnvLine returns the first line from envContent matching ^KEY= ...,
// or "" if absent. Used for line-precise assertions on .env output.
func extractEnvLine(envContent, key string) string {
	for _, line := range strings.Split(envContent, "\n") {
		if strings.HasPrefix(line, key+"=") {
			return line
		}
	}
	return ""
}

// waitForServiceActive polls list-services until the named hostname has
// status RUNNING or ACTIVE. Used by env e2e tests that need provisioned
// services before calling EnvGenerateDotenv.
func waitForServiceActive(ctx context.Context, client platform.Client, projectID, hostname string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		services, err := client.ListServices(ctx, projectID)
		if err != nil {
			return fmt.Errorf("list services: %w", err)
		}
		for _, s := range services {
			if s.Name == hostname && (s.Status == "RUNNING" || s.Status == "ACTIVE") {
				return nil
			}
		}
		time.Sleep(3 * time.Second)
	}
	return fmt.Errorf("%s did not reach RUNNING/ACTIVE within %s", hostname, timeout)
}
