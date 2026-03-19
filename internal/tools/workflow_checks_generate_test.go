package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/workflow"
)

func TestCheckGenerate_NoPlan_ReturnsNil2(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	checker := checkGenerate(stateDir)
	if checker == nil {
		t.Fatal("checkGenerate should return a non-nil checker")
	}
	result, err := checker(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("expected nil result for nil plan")
	}
}

func TestCheckGenerate_NoZeropsYml_Fails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")

	plan := &workflow.ServicePlan{
		Targets: []workflow.BootstrapTarget{{
			Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"},
		}},
	}

	checker := checkGenerate(stateDir)
	result, err := checker(context.Background(), plan, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Passed {
		t.Error("expected fail for missing zerops.yml")
	}
	if result.Summary != "generate checks failed" {
		t.Errorf("Summary = %q, want 'generate checks failed'", result.Summary)
	}
	hasFailCheck := false
	for _, c := range result.Checks {
		if c.Name == "zerops_yml_exists" && c.Status == "fail" {
			hasFailCheck = true
		}
	}
	if !hasFailCheck {
		t.Error("expected zerops_yml_exists fail check")
	}
}

func TestCheckGenerate_MissingHostname_Fails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")

	writeZeropsYml(t, dir, `zerops:
  - setup: other
    build:
      deployFiles: [.]
    run:
      start: node index.js
      ports:
        - port: 8080
`)

	plan := &workflow.ServicePlan{
		Targets: []workflow.BootstrapTarget{{
			Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"},
		}},
	}

	checker := checkGenerate(stateDir)
	result, err := checker(context.Background(), plan, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected fail for missing hostname setup entry")
	}
	hasSetupFail := false
	for _, c := range result.Checks {
		if c.Name == "appdev_setup" && c.Status == "fail" {
			hasSetupFail = true
		}
	}
	if !hasSetupFail {
		t.Error("expected appdev_setup fail check")
	}
}

func TestCheckGenerate_InvalidEnvRef_Fails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")

	writeZeropsYml(t, dir, `zerops:
  - setup: appdev
    envVariables:
      DATABASE_URL: ${db_fakeVar}
    build:
      deployFiles: [.]
    run:
      start: node index.js
      ports:
        - port: 8080
`)

	plan := &workflow.ServicePlan{
		Targets: []workflow.BootstrapTarget{{
			Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"},
			Dependencies: []workflow.Dependency{
				{Hostname: "db", Type: "postgresql@16", Resolution: "CREATE"},
			},
		}},
	}

	state := &workflow.BootstrapState{
		Active: true,
		Plan:   plan,
		DiscoveredEnvVars: map[string][]string{
			"db": {"connectionString", "host", "port"},
		},
	}

	checker := checkGenerate(stateDir)
	result, err := checker(context.Background(), plan, state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected fail for invalid env ref")
	}
	hasEnvRefFail := false
	for _, c := range result.Checks {
		if c.Name == "appdev_env_refs" && c.Status == "fail" {
			hasEnvRefFail = true
		}
	}
	if !hasEnvRefFail {
		t.Error("expected appdev_env_refs fail check")
	}
}

func TestCheckGenerate_NoPorts_Fails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")

	writeZeropsYml(t, dir, `zerops:
  - setup: appdev
    build:
      deployFiles: [.]
    run:
      start: node index.js
`)

	plan := &workflow.ServicePlan{
		Targets: []workflow.BootstrapTarget{{
			Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", BootstrapMode: "simple"},
		}},
	}

	checker := checkGenerate(stateDir)
	result, err := checker(context.Background(), plan, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected fail for no ports")
	}
	hasPortsFail := false
	for _, c := range result.Checks {
		if c.Name == "appdev_ports" && c.Status == "fail" {
			hasPortsFail = true
		}
	}
	if !hasPortsFail {
		t.Error("expected appdev_ports fail check")
	}
}

func TestCheckGenerate_NoDeployFiles_Fails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")

	writeZeropsYml(t, dir, `zerops:
  - setup: appdev
    run:
      start: node index.js
      ports:
        - port: 8080
`)

	plan := &workflow.ServicePlan{
		Targets: []workflow.BootstrapTarget{{
			Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", BootstrapMode: "simple"},
		}},
	}

	checker := checkGenerate(stateDir)
	result, err := checker(context.Background(), plan, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected fail for no deployFiles")
	}
	hasDeployFilesFail := false
	for _, c := range result.Checks {
		if c.Name == "appdev_deploy_files" && c.Status == "fail" {
			hasDeployFilesFail = true
		}
	}
	if !hasDeployFilesFail {
		t.Error("expected appdev_deploy_files fail check")
	}
}

func TestCheckGenerate_AllPass(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")

	writeZeropsYml(t, dir, `zerops:
  - setup: appdev
    envVariables:
      DATABASE_URL: ${db_connectionString}
    build:
      deployFiles: [.]
    run:
      start: node index.js
      ports:
        - port: 8080
`)

	plan := &workflow.ServicePlan{
		Targets: []workflow.BootstrapTarget{{
			Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", BootstrapMode: "simple"},
			Dependencies: []workflow.Dependency{
				{Hostname: "db", Type: "postgresql@16", Resolution: "CREATE"},
			},
		}},
	}

	state := &workflow.BootstrapState{
		Active: true,
		Plan:   plan,
		DiscoveredEnvVars: map[string][]string{
			"db": {"connectionString", "host", "port"},
		},
	}

	checker := checkGenerate(stateDir)
	result, err := checker(context.Background(), plan, state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Errorf("expected pass, got fail: %s", result.Summary)
		for _, c := range result.Checks {
			t.Logf("  %s: %s %s", c.Name, c.Status, c.Detail)
		}
	}
	if result.Summary != "generate checks passed" {
		t.Errorf("Summary = %q, want 'generate checks passed'", result.Summary)
	}
}

func TestCheckGenerate_ImplicitWebServer_SkipsPorts(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")

	writeZeropsYml(t, dir, `zerops:
  - setup: appdev
    build:
      deployFiles: [.]
    run:
      base: php-nginx@8.4
`)

	plan := &workflow.ServicePlan{
		Targets: []workflow.BootstrapTarget{{
			Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "php-nginx@8.4", BootstrapMode: "simple"},
		}},
	}

	checker := checkGenerate(stateDir)
	result, err := checker(context.Background(), plan, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Errorf("expected pass for implicit web server, got fail: %s", result.Summary)
		for _, c := range result.Checks {
			t.Logf("  %s: %s %s", c.Name, c.Status, c.Detail)
		}
	}
}

func TestCheckGenerate_ValidEnvRef_Passes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")

	writeZeropsYml(t, dir, `zerops:
  - setup: appdev
    envVariables:
      DB_URL: ${db_connectionString}
      CACHE_URL: ${cache_connectionString}
    build:
      deployFiles: [.]
    run:
      start: node index.js
      ports:
        - port: 3000
`)

	plan := &workflow.ServicePlan{
		Targets: []workflow.BootstrapTarget{{
			Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", BootstrapMode: "simple"},
			Dependencies: []workflow.Dependency{
				{Hostname: "db", Type: "postgresql@16", Resolution: "CREATE"},
				{Hostname: "cache", Type: "valkey@7.2", Resolution: "CREATE"},
			},
		}},
	}

	state := &workflow.BootstrapState{
		Active: true,
		Plan:   plan,
		DiscoveredEnvVars: map[string][]string{
			"db":    {"connectionString", "host", "port"},
			"cache": {"connectionString", "port"},
		},
	}

	checker := checkGenerate(stateDir)
	result, err := checker(context.Background(), plan, state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Errorf("expected pass, got fail: %s", result.Summary)
		for _, c := range result.Checks {
			t.Logf("  %s: %s %s", c.Name, c.Status, c.Detail)
		}
	}

	// Verify env_refs check is present and passed.
	hasEnvRefPass := false
	for _, c := range result.Checks {
		if c.Name == "appdev_env_refs" && c.Status == "pass" {
			hasEnvRefPass = true
		}
	}
	if !hasEnvRefPass {
		t.Error("expected appdev_env_refs pass check")
	}
}

func TestCheckGenerate_StandardMode_OnlyChecksDev(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")

	// zerops.yml with only appdev entry — no appstage.
	writeZeropsYml(t, dir, `zerops:
  - setup: appdev
    envVariables:
      DATABASE_URL: ${db_connectionString}
    build:
      deployFiles: [.]
    run:
      start: node index.js
      ports:
        - port: 8080
`)

	// Standard mode (BootstrapMode="" defaults to standard via EffectiveMode).
	plan := &workflow.ServicePlan{
		Targets: []workflow.BootstrapTarget{{
			Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"},
			Dependencies: []workflow.Dependency{
				{Hostname: "db", Type: "postgresql@16", Resolution: "CREATE"},
			},
		}},
	}

	state := &workflow.BootstrapState{
		Active: true,
		Plan:   plan,
		DiscoveredEnvVars: map[string][]string{
			"db": {"connectionString", "host", "port"},
		},
	}

	checker := checkGenerate(stateDir)
	result, err := checker(context.Background(), plan, state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Errorf("expected pass with only dev hostname in zerops.yml, got fail: %s", result.Summary)
		for _, c := range result.Checks {
			t.Logf("  %s: %s %s", c.Name, c.Status, c.Detail)
		}
	}
}

func TestCheckGenerate_StandardMode_NoStageChecks(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")

	// zerops.yml with only appdev entry — no appstage.
	writeZeropsYml(t, dir, `zerops:
  - setup: appdev
    build:
      deployFiles: [.]
    run:
      start: node index.js
      ports:
        - port: 8080
`)

	// Standard mode (BootstrapMode="" defaults to standard via EffectiveMode).
	plan := &workflow.ServicePlan{
		Targets: []workflow.BootstrapTarget{{
			Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"},
		}},
	}

	checker := checkGenerate(stateDir)
	result, err := checker(context.Background(), plan, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// No check should reference "appstage".
	for _, c := range result.Checks {
		if strings.Contains(c.Name, "appstage") {
			t.Errorf("unexpected stage check: %s (status=%s)", c.Name, c.Status)
		}
	}
}

func TestCheckGenerate_ExistsAndCreateDeps_EnvRefs_Pass(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")

	writeZeropsYml(t, dir, `zerops:
  - setup: appdev
    envVariables:
      DATABASE_URL: ${db_connectionString}
      CACHE_PORT: ${cache_port}
    build:
      deployFiles: [.]
    run:
      start: node index.js
      ports:
        - port: 3000
`)

	plan := &workflow.ServicePlan{
		Targets: []workflow.BootstrapTarget{{
			Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", IsExisting: true, BootstrapMode: "simple"},
			Dependencies: []workflow.Dependency{
				{Hostname: "db", Type: "postgresql@16", Resolution: "EXISTS"},
				{Hostname: "cache", Type: "valkey@7.2", Mode: "NON_HA", Resolution: "CREATE"},
			},
		}},
	}

	state := &workflow.BootstrapState{
		Active: true,
		Plan:   plan,
		DiscoveredEnvVars: map[string][]string{
			"db":    {"connectionString", "host", "port"},
			"cache": {"connectionString", "port"},
		},
	}

	checker := checkGenerate(stateDir)
	result, err := checker(context.Background(), plan, state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Errorf("expected pass for mixed EXISTS+CREATE deps with valid env refs: %s", result.Summary)
		for _, c := range result.Checks {
			t.Logf("  %s: %s %s", c.Name, c.Status, c.Detail)
		}
	}
	hasEnvRefPass := false
	for _, c := range result.Checks {
		if c.Name == "appdev_env_refs" && c.Status == "pass" {
			hasEnvRefPass = true
		}
	}
	if !hasEnvRefPass {
		t.Error("expected appdev_env_refs pass check")
	}
}

func TestCheckGenerate_MountPath_FindsYml(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")

	// Write zerops.yml to mount path /var/www/{hostname}/ (simulated as dir/{hostname}/).
	mountDir := filepath.Join(dir, "appdev")
	if err := os.MkdirAll(mountDir, 0755); err != nil {
		t.Fatal(err)
	}
	writeZeropsYml(t, mountDir, `zerops:
  - setup: appdev
    build:
      deployFiles: [.]
    run:
      start: node index.js
      ports:
        - port: 8080
`)
	// No zerops.yml at project root — only in mount path.

	plan := &workflow.ServicePlan{
		Targets: []workflow.BootstrapTarget{{
			Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", BootstrapMode: "simple"},
		}},
	}

	checker := checkGenerate(stateDir)
	result, err := checker(context.Background(), plan, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Errorf("expected pass when zerops.yml is in mount path, got fail: %s", result.Summary)
		for _, c := range result.Checks {
			t.Logf("  %s: %s %s", c.Name, c.Status, c.Detail)
		}
	}
}

// writeZeropsYml is a test helper that writes zerops.yml to the given directory.
func writeZeropsYml(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "zerops.yml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
