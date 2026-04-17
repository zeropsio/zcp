package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
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
		t.Error("expected fail for missing zerops.yaml")
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
  - setup: dev
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
  - setup: prod
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
  - setup: prod
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
  - setup: prod
    envVariables:
      DATABASE_URL: ${db_connectionString}
    build:
      deployFiles: [.]
    run:
      start: node index.js
      ports:
        - port: 8080
      healthCheck:
        httpGet:
          path: /health
          port: 8080
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
  - setup: prod
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
  - setup: prod
    envVariables:
      DB_URL: ${db_connectionString}
      CACHE_URL: ${cache_connectionString}
    build:
      deployFiles: [.]
    run:
      start: node index.js
      ports:
        - port: 3000
      healthCheck:
        httpGet:
          path: /health
          port: 3000
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

	// zerops.yaml with only appdev entry — no appstage.
	writeZeropsYml(t, dir, `zerops:
  - setup: dev
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
		t.Errorf("expected pass with only dev hostname in zerops.yaml, got fail: %s", result.Summary)
		for _, c := range result.Checks {
			t.Logf("  %s: %s %s", c.Name, c.Status, c.Detail)
		}
	}
}

func TestCheckGenerate_StandardMode_NoStageChecks(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")

	// zerops.yaml with only appdev entry — no appstage.
	writeZeropsYml(t, dir, `zerops:
  - setup: dev
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
  - setup: prod
    envVariables:
      DATABASE_URL: ${db_connectionString}
      CACHE_PORT: ${cache_port}
    build:
      deployFiles: [.]
    run:
      start: node index.js
      ports:
        - port: 3000
      healthCheck:
        httpGet:
          path: /health
          port: 3000
`)

	plan := &workflow.ServicePlan{
		Targets: []workflow.BootstrapTarget{{
			Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", BootstrapMode: "simple"},
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

	// Write zerops.yaml to mount path /var/www/{hostname}/ (simulated as dir/{hostname}/).
	mountDir := filepath.Join(dir, "appdev")
	if err := os.MkdirAll(mountDir, 0755); err != nil {
		t.Fatal(err)
	}
	writeZeropsYml(t, mountDir, `zerops:
  - setup: prod
    build:
      deployFiles: [.]
    run:
      start: node index.js
      ports:
        - port: 8080
      healthCheck:
        httpGet:
          path: /health
          port: 8080
`)
	// No zerops.yaml at project root — only in mount path.

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
		t.Errorf("expected pass when zerops.yaml is in mount path, got fail: %s", result.Summary)
		for _, c := range result.Checks {
			t.Logf("  %s: %s %s", c.Name, c.Status, c.Detail)
		}
	}
}

func TestCheckGenerate_HealthCheck(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		yml           string
		bootstrapMode string
		wantPassed    bool
		wantCheckName string
		wantStatus    string
	}{
		{
			name: "simple mode with healthCheck passes",
			yml: `zerops:
  - setup: prod
    build:
      deployFiles: [.]
    run:
      start: node index.js
      ports:
        - port: 8080
      healthCheck:
        httpGet:
          path: /health
          port: 8080
`,
			bootstrapMode: "simple",
			wantPassed:    true,
			wantCheckName: "appdev_health_check",
			wantStatus:    "pass",
		},
		{
			name: "simple mode without healthCheck fails",
			yml: `zerops:
  - setup: prod
    build:
      deployFiles: [.]
    run:
      start: node index.js
      ports:
        - port: 8080
`,
			bootstrapMode: "simple",
			wantPassed:    false,
			wantCheckName: "appdev_health_check",
			wantStatus:    "fail",
		},
		{
			name: "standard mode without healthCheck passes (not required)",
			yml: `zerops:
  - setup: dev
    build:
      deployFiles: [.]
    run:
      start: node index.js
      ports:
        - port: 8080
`,
			bootstrapMode: "",
			wantPassed:    true,
			wantCheckName: "",
		},
		{
			name: "dev mode without healthCheck passes (not required)",
			yml: `zerops:
  - setup: dev
    build:
      deployFiles: [.]
    run:
      start: node index.js
      ports:
        - port: 8080
`,
			bootstrapMode: "dev",
			wantPassed:    true,
			wantCheckName: "",
		},
		{
			name: "simple mode with implicit web server and no healthCheck passes (exempt)",
			yml: `zerops:
  - setup: prod
    build:
      deployFiles: [.]
    run:
      base: php-nginx@8.4
`,
			bootstrapMode: "simple",
			wantPassed:    true,
			wantCheckName: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			stateDir := filepath.Join(dir, ".zcp", "state")
			writeZeropsYml(t, dir, tt.yml)

			plan := &workflow.ServicePlan{
				Targets: []workflow.BootstrapTarget{{
					Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", BootstrapMode: tt.bootstrapMode},
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
			if result.Passed != tt.wantPassed {
				t.Errorf("Passed = %v, want %v; summary: %s", result.Passed, tt.wantPassed, result.Summary)
				for _, c := range result.Checks {
					t.Logf("  %s: %s %s", c.Name, c.Status, c.Detail)
				}
			}
			if tt.wantCheckName != "" {
				found := false
				for _, c := range result.Checks {
					if c.Name == tt.wantCheckName && c.Status == tt.wantStatus {
						found = true
					}
				}
				if !found {
					t.Errorf("expected check %q with status %q", tt.wantCheckName, tt.wantStatus)
					for _, c := range result.Checks {
						t.Logf("  %s: %s %s", c.Name, c.Status, c.Detail)
					}
				}
			}
		})
	}
}

func TestCheckGenerate_MissingRunStart_DynamicRuntime_Fail(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")

	writeZeropsYml(t, dir, `zerops:
  - setup: dev
    build:
      deployFiles: [.]
    run:
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
		t.Error("expected fail for missing run.start on dynamic runtime")
	}
	hasRunStartFail := false
	for _, c := range result.Checks {
		if c.Name == "appdev_run_start" && c.Status == "fail" {
			hasRunStartFail = true
		}
	}
	if !hasRunStartFail {
		t.Error("expected appdev_run_start fail check")
	}
}

func TestCheckGenerate_MissingRunStart_ImplicitWebServer_Pass(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")

	writeZeropsYml(t, dir, `zerops:
  - setup: prod
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
	for _, c := range result.Checks {
		if c.Name == "appdev_run_start" && c.Status == "fail" {
			t.Error("implicit web server should not fail run_start check")
		}
	}
}

func TestCheckGenerate_DevDeployFilesNotDot_Fail(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")

	writeZeropsYml(t, dir, `zerops:
  - setup: dev
    build:
      deployFiles: [app]
    run:
      start: node index.js
      ports:
        - port: 8080
`)

	plan := &workflow.ServicePlan{
		Targets: []workflow.BootstrapTarget{{
			Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", BootstrapMode: "dev"},
		}},
	}

	checker := checkGenerate(stateDir)
	result, err := checker(context.Background(), plan, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected fail for dev service without deployFiles: [.]")
	}
	hasDevDeployFail := false
	for _, c := range result.Checks {
		if c.Name == "appdev_dev_deploy_files" && c.Status == "fail" {
			hasDevDeployFail = true
		}
	}
	if !hasDevDeployFail {
		t.Error("expected appdev_dev_deploy_files fail check")
	}
}

func TestCheckGenerate_DevDeployFilesDot_Pass(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")

	writeZeropsYml(t, dir, `zerops:
  - setup: dev
    build:
      deployFiles: [.]
    run:
      start: node index.js
      ports:
        - port: 8080
`)

	plan := &workflow.ServicePlan{
		Targets: []workflow.BootstrapTarget{{
			Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", BootstrapMode: "dev"},
		}},
	}

	checker := checkGenerate(stateDir)
	result, err := checker(context.Background(), plan, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, c := range result.Checks {
		if c.Name == "appdev_dev_deploy_files" && c.Status == "fail" {
			t.Error("dev service with deployFiles: [.] should not fail dev_deploy_files check")
		}
	}
}

func TestCheckGenerate_RunStartBuildCommand_Fail(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		runStart string
	}{
		{"npm_install", "npm install"},
		{"pip_install", "pip install -r requirements.txt"},
		{"go_build", "go build -o app ."},
		{"cargo_build", "cargo build --release"},
		{"mvn_package", "mvn package"},
		{"gradle_build", "gradle build"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			stateDir := filepath.Join(dir, ".zcp", "state")

			writeZeropsYml(t, dir, `zerops:
  - setup: dev
    build:
      deployFiles: [.]
    run:
      start: `+tt.runStart+`
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
			hasBuildCmdFail := false
			for _, c := range result.Checks {
				if c.Name == "appdev_run_start_build_cmd" && c.Status == "fail" {
					hasBuildCmdFail = true
				}
			}
			if !hasBuildCmdFail {
				t.Errorf("expected appdev_run_start_build_cmd fail for run.start=%q", tt.runStart)
			}
		})
	}
}

func TestCheckGenerate_RunStartValidCommand_NoBuildCmdFail(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")

	writeZeropsYml(t, dir, `zerops:
  - setup: dev
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
	for _, c := range result.Checks {
		if c.Name == "appdev_run_start_build_cmd" && c.Status == "fail" {
			t.Error("valid run.start should not trigger build command check")
		}
	}
}

func TestCheckGenerate_IsExisting_SkipsValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		isExisting bool
		hasYml     bool
		wantPass   bool
	}{
		{"existing_no_yml_passes", true, false, true},
		{"existing_with_yml_passes", true, true, true},
		{"new_no_yml_fails", false, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			stateDir := filepath.Join(dir, ".zcp", "state")

			if tt.hasYml {
				writeZeropsYml(t, dir, `zerops:
  - setup: prod
    build:
      deployFiles: [.]
    run:
      start: node index.js
      ports:
        - port: 3000
`)
			}

			plan := &workflow.ServicePlan{
				Targets: []workflow.BootstrapTarget{{
					Runtime: workflow.RuntimeTarget{
						DevHostname:   "appdev",
						Type:          "nodejs@22",
						IsExisting:    tt.isExisting,
						BootstrapMode: "simple",
					},
				}},
			}

			checker := checkGenerate(stateDir)
			result, err := checker(context.Background(), plan, nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result == nil {
				if tt.wantPass {
					return // nil result = no checks = pass for existing
				}
				t.Fatal("expected non-nil result")
			}
			if result.Passed != tt.wantPass {
				t.Errorf("Passed = %v, want %v; summary: %s", result.Passed, tt.wantPass, result.Summary)
				for _, c := range result.Checks {
					t.Logf("  %s: %s %s", c.Name, c.Status, c.Detail)
				}
			}
		})
	}
}

func TestCheckGenerate_MixedExistingAndNew(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")

	// Write zerops.yaml only for the NEW target — existing target has no yml.
	writeZeropsYml(t, dir, `zerops:
  - setup: dev
    build:
      deployFiles: [.]
    run:
      start: node index.js
      ports:
        - port: 3000
`)

	plan := &workflow.ServicePlan{
		Targets: []workflow.BootstrapTarget{
			{
				Runtime: workflow.RuntimeTarget{
					DevHostname:   "api",
					Type:          "go@1",
					IsExisting:    true,
					BootstrapMode: "simple",
				},
			},
			{
				Runtime: workflow.RuntimeTarget{
					DevHostname:   "webdev",
					Type:          "nodejs@22",
					IsExisting:    false,
					BootstrapMode: "dev",
				},
			},
		},
	}

	checker := checkGenerate(stateDir)
	result, err := checker(context.Background(), plan, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result for mixed plan")
	}
	if !result.Passed {
		t.Errorf("expected pass for mixed plan (existing skipped, new has yml): %s", result.Summary)
		for _, c := range result.Checks {
			t.Logf("  %s: %s %s", c.Name, c.Status, c.Detail)
		}
	}
	// Verify no checks emitted for the existing target.
	for _, c := range result.Checks {
		if strings.HasPrefix(c.Name, "api_") {
			t.Errorf("unexpected check for existing target api: %s %s", c.Name, c.Status)
		}
	}
}

func TestCheckGenerate_ServiceTypeImplicitWebServer_NoBuildBaseMatch(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")

	// build.base is "php@8.4" (just the compiler), no run.base set.
	// Service type is php-nginx@8.4 → implicit web server.
	// Checker must recognize this and skip ports/start/healthCheck checks.
	writeZeropsYml(t, dir, `zerops:
  - setup: prod
    build:
      base: php@8.4
      buildCommands:
        - composer install --no-dev
      deployFiles: [.]
    run:
      envVariables:
        DB_HOST: db
`)

	plan := &workflow.ServicePlan{
		Targets: []workflow.BootstrapTarget{{
			Runtime: workflow.RuntimeTarget{DevHostname: "kanboard", Type: "php-nginx@8.4", BootstrapMode: "simple"},
		}},
	}

	checker := checkGenerate(stateDir)
	result, err := checker(context.Background(), plan, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Errorf("expected pass — php-nginx service type has implicit web server, got fail: %s", result.Summary)
		for _, c := range result.Checks {
			t.Logf("  %s: %s %s", c.Name, c.Status, c.Detail)
		}
	}
	// Specifically: no ports, start, or health_check failures.
	for _, c := range result.Checks {
		if c.Status == "fail" && (strings.Contains(c.Name, "ports") || strings.Contains(c.Name, "run_start") || strings.Contains(c.Name, "health_check")) {
			t.Errorf("implicit web server runtime should not fail %s: %s", c.Name, c.Detail)
		}
	}
}

func TestHasPkgInstallWithoutSudo(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		commands any
		want     bool
	}{
		{"nil_commands", nil, false},
		{"empty_string", "", false},
		{"sudo_apk_add", "sudo apk add --no-cache php84-ctype", false},
		{"apk_add_no_sudo", "apk add --no-cache php84-ctype", true},
		{"sudo_apt_get", "sudo apt-get install -y php8.4-ctype", false},
		{"apt_get_no_sudo", "apt-get install -y php8.4-ctype", true},
		{"list_with_sudo", []any{"sudo apk add --no-cache php84-ctype"}, false},
		{"list_without_sudo", []any{"apk add --no-cache php84-ctype"}, true},
		{"mixed_list_one_bad", []any{"sudo apk add --no-cache php84-gd", "apk add php84-ctype"}, true},
		{"unrelated_command", []any{"echo hello"}, false},
		{"chained_with_sudo", "sudo apt-get update && sudo apt-get install -y imagemagick", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ops.HasPkgInstallWithoutSudo(tt.commands)
			if got != tt.want {
				t.Errorf("HasPkgInstallWithoutSudo(%v) = %v, want %v", tt.commands, got, tt.want)
			}
		})
	}
}

func TestCheckGenerate_PrepareCommandsMissingSudo_Fails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")

	writeZeropsYml(t, dir, `zerops:
  - setup: dev
    build:
      base: php@8.4
      buildCommands:
        - composer install --no-dev
      deployFiles: [.]
    run:
      base: php-nginx@8.4
      prepareCommands:
        - apk add --no-cache php84-ctype
`)

	plan := &workflow.ServicePlan{
		Targets: []workflow.BootstrapTarget{{
			Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "php-nginx@8.4"},
		}},
	}

	checker := checkGenerate(stateDir)
	result, err := checker(context.Background(), plan, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected fail for missing sudo in prepareCommands")
	}
	hasSudoFail := false
	for _, c := range result.Checks {
		if c.Name == "appdev_prepare_missing_sudo" && c.Status == "fail" {
			hasSudoFail = true
			if !strings.Contains(c.Detail, "sudo") {
				t.Errorf("detail should mention sudo, got: %s", c.Detail)
			}
		}
	}
	if !hasSudoFail {
		t.Error("expected appdev_prepare_missing_sudo fail check")
	}
}

func TestCheckGenerate_PrepareCommandsWithSudo_Passes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")

	writeZeropsYml(t, dir, `zerops:
  - setup: dev
    build:
      base: php@8.4
      buildCommands:
        - composer install --no-dev
      deployFiles: [.]
    run:
      base: php-nginx@8.4
      prepareCommands:
        - sudo apk add --no-cache php84-ctype
`)

	plan := &workflow.ServicePlan{
		Targets: []workflow.BootstrapTarget{{
			Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "php-nginx@8.4"},
		}},
	}

	checker := checkGenerate(stateDir)
	result, err := checker(context.Background(), plan, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, c := range result.Checks {
		if c.Name == "appdev_prepare_missing_sudo" {
			t.Errorf("should not flag sudo when sudo is present, got check: %+v", c)
		}
	}
}

// writeZeropsYml is a test helper that writes zerops.yaml to the given directory.
func writeZeropsYml(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "zerops.yaml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
