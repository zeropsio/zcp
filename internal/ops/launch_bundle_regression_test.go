package ops_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/topology"
)

// regressionGoldensDir locates the per-test goldens under testdata/.
// Following Go testdata convention; relative to this test file.
const regressionGoldensDir = "testdata/goldens/regression"

// assertGolden compares body against the file at
// testdata/goldens/regression/<key>.yaml. On mismatch the test fails
// with a unified summary. When ZCP_GOLDEN_UPDATE=1, the body is
// written to the file instead — useful for capture + after intentional
// behavior changes.
//
// Files are created with 0o644; directories under testdata/ are
// already committed and writable.
func assertGolden(t *testing.T, key, body string) {
	t.Helper()
	path := filepath.Join(regressionGoldensDir, key+".yaml")

	if os.Getenv("ZCP_GOLDEN_UPDATE") == "1" {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("write golden %s: %v", path, err)
		}
		t.Logf("captured golden %s (%d bytes)", path, len(body))
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v\n\nRun with ZCP_GOLDEN_UPDATE=1 to capture.", path, err)
	}
	if string(want) != body {
		t.Errorf("regression golden drift for %s.\n"+
			"Expected (golden, %d bytes):\n%s\n"+
			"Actual (current composer output, %d bytes):\n%s\n"+
			"If this drift is intentional, run with ZCP_GOLDEN_UPDATE=1 + review the diff.",
			key, len(want), string(want), len(body), body)
	}
}

// classifyPlainAll returns a classifications map bucketing every key
// in envs as PlainConfig. Avoids preprocessor-header insertion so
// goldens stay byte-identical across runs.
func classifyPlainAll(envs []ops.ProjectEnvVar) map[string]topology.SecretClassification {
	out := make(map[string]topology.SecretClassification, len(envs))
	for _, e := range envs {
		out[e.Key] = topology.SecretClassPlainConfig
	}
	return out
}

// --- Launch fixtures ---

// launchStandardPairInputs models a launch from a standard dev/stage
// pair into a fresh prod project. Single runtime + postgres managed
// dep. Mirrors the most common launch source shape.
func launchStandardPairInputs() ops.LaunchBundleInputs {
	return ops.LaunchBundleInputs{
		SourceProjectID:   "source-standard-pair",
		TargetProjectName: "myapp-prod",
		TargetHostname:    "app",
		ServiceType:       "nodejs@22",
		SetupName:         "prod",
		RepoURL:           "https://github.com/example/myapp",
		ZeropsYAMLBody: `zerops:
  - setup: prod
    build:
      base: nodejs@22
      buildCommands:
        - npm ci
        - npm run build
    run:
      base: nodejs@22
      start: node dist/server.js
`,
		GitCommitSHA: "abc123def4567890",
		ProjectEnvs: []ops.ProjectEnvVar{
			{Key: "LOG_LEVEL", Value: "info"},
			{Key: "NODE_ENV", Value: "production"},
		},
		ManagedServices: []ops.ManagedServiceEntry{
			{Hostname: "db", Type: "postgresql@16", Mode: "NON_HA"},
		},
	}
}

// launchDevOnlyInputs models a launch from a single dev-mode runtime
// (no stage half). Common minimal-source shape.
func launchDevOnlyInputs() ops.LaunchBundleInputs {
	return ops.LaunchBundleInputs{
		SourceProjectID:   "source-dev-only",
		TargetProjectName: "api-prod",
		TargetHostname:    "api",
		ServiceType:       "nodejs@22",
		SetupName:         "prod",
		RepoURL:           "https://github.com/example/api",
		ZeropsYAMLBody: `zerops:
  - setup: prod
    build:
      base: nodejs@22
    run:
      base: nodejs@22
      start: node dist/index.js
`,
		GitCommitSHA: "0123456789abcdef",
		ProjectEnvs: []ops.ProjectEnvVar{
			{Key: "PORT", Value: "3000"},
		},
		ManagedServices: []ops.ManagedServiceEntry{
			{Hostname: "db", Type: "postgresql@16", Mode: "NON_HA"},
		},
	}
}

// launchRecipeNodejsInputs models a launch from a typical Node recipe
// shape: app + postgres + redis.
func launchRecipeNodejsInputs() ops.LaunchBundleInputs {
	return ops.LaunchBundleInputs{
		SourceProjectID:   "source-recipe-nodejs",
		TargetProjectName: "node-recipe-prod",
		TargetHostname:    "app",
		ServiceType:       "nodejs@22",
		SetupName:         "prod",
		RepoURL:           "https://github.com/example/node-recipe",
		ZeropsYAMLBody: `zerops:
  - setup: prod
    build:
      base: nodejs@22
      buildCommands:
        - npm ci
        - npm run build
    run:
      base: nodejs@22
      start: node dist/server.js
`,
		GitCommitSHA: "fedcba9876543210",
		ProjectEnvs: []ops.ProjectEnvVar{
			{Key: "LOG_LEVEL", Value: "info"},
			{Key: "NODE_ENV", Value: "production"},
		},
		ManagedServices: []ops.ManagedServiceEntry{
			{Hostname: "db", Type: "postgresql@16", Mode: "NON_HA"},
			{Hostname: "redis", Type: "valkey@7", Mode: "NON_HA"},
		},
	}
}

// launchRecipeLaravelInputs models a launch from a Laravel-style stack:
// php-nginx runtime + postgres + redis + object-storage. The
// object-storage entry exercises the F20/F21 surface. After Phase 2b
// landed bundle.ServiceTypeRules + ManagedServiceEntry.QuotaGBytes,
// the golden pins the corrected shape: object-storage entry has NO
// `mode:` field and DOES have `objectStorageSize:` (default 1 when
// caller doesn't probe source quotaGBytes).
func launchRecipeLaravelInputs() ops.LaunchBundleInputs {
	return ops.LaunchBundleInputs{
		SourceProjectID:   "source-recipe-laravel",
		TargetProjectName: "laravel-prod",
		TargetHostname:    "app",
		ServiceType:       "php-nginx@8.3+8.0",
		SetupName:         "prod",
		RepoURL:           "https://github.com/example/laravel-showcase",
		ZeropsYAMLBody: `zerops:
  - setup: prod
    build:
      base:
        - php@8.3
        - nginx@1.22
      buildCommands:
        - composer install --no-dev --optimize-autoloader
        - npm ci && npm run build
    run:
      base:
        - php@8.3
        - nginx@1.22
      start: php artisan octane:start
`,
		GitCommitSHA: "0badcafebadcafe1",
		ProjectEnvs: []ops.ProjectEnvVar{
			{Key: "APP_ENV", Value: "production"},
			{Key: "APP_DEBUG", Value: "false"},
		},
		ManagedServices: []ops.ManagedServiceEntry{
			{Hostname: "db", Type: "postgresql@16", Mode: "NON_HA"},
			{Hostname: "redis", Type: "valkey@7", Mode: "NON_HA"},
			{Hostname: "storage", Type: "object-storage", Mode: ""},
		},
	}
}

func TestLaunchGolden_StandardPair(t *testing.T) {
	inputs := launchStandardPairInputs()
	bundle, err := ops.BuildLaunchBundle(inputs, classifyPlainAll(inputs.ProjectEnvs))
	if err != nil {
		t.Fatalf("BuildLaunchBundle: %v", err)
	}
	if bundle == nil {
		t.Fatal("nil bundle")
	}
	assertGolden(t, "launch-standard-pair", bundle.ImportYAML)
}

func TestLaunchGolden_DevOnly(t *testing.T) {
	inputs := launchDevOnlyInputs()
	bundle, err := ops.BuildLaunchBundle(inputs, classifyPlainAll(inputs.ProjectEnvs))
	if err != nil {
		t.Fatalf("BuildLaunchBundle: %v", err)
	}
	assertGolden(t, "launch-dev-only", bundle.ImportYAML)
}

func TestLaunchGolden_RecipeNodejs(t *testing.T) {
	inputs := launchRecipeNodejsInputs()
	bundle, err := ops.BuildLaunchBundle(inputs, classifyPlainAll(inputs.ProjectEnvs))
	if err != nil {
		t.Fatalf("BuildLaunchBundle: %v", err)
	}
	assertGolden(t, "launch-recipe-nodejs", bundle.ImportYAML)
}

func TestLaunchGolden_RecipeLaravel(t *testing.T) {
	inputs := launchRecipeLaravelInputs()
	bundle, err := ops.BuildLaunchBundle(inputs, classifyPlainAll(inputs.ProjectEnvs))
	if err != nil {
		t.Fatalf("BuildLaunchBundle: %v", err)
	}
	// Golden captures the Phase 2b corrected shape: storage entry has
	// no `mode:` field (F20) and carries `objectStorageSize: 1` (F21
	// default; caller may probe source quotaGBytes for a higher value).
	assertGolden(t, "launch-recipe-laravel", bundle.ImportYAML)
}

// --- Export fixtures ---

// exportPairInputs models the source state behind both Dev + Stage
// export variants. Variant is supplied per-test below.
func exportPairInputs() ops.BundleInputs {
	return ops.BundleInputs{
		ProjectName:      "myapp",
		TargetHostname:   "appdev",
		SourceMode:       topology.ModeDev,
		ServiceType:      "nodejs@22",
		SubdomainEnabled: true,
		SetupName:        "dev",
		RepoURL:          "https://github.com/example/myapp",
		ZeropsYAMLBody: `zerops:
  - setup: dev
    build:
      base: nodejs@22
      buildCommands:
        - npm ci
        - npm run build
    run:
      base: nodejs@22
      start: node dist/server.js
  - setup: stage
    build:
      base: nodejs@22
      buildCommands:
        - npm ci
        - npm run build
    run:
      base: nodejs@22
      start: node dist/server.js
`,
		ProjectEnvs: []ops.ProjectEnvVar{
			{Key: "LOG_LEVEL", Value: "info"},
			{Key: "NODE_ENV", Value: "development"},
		},
		ManagedServices: []ops.ManagedServiceEntry{
			{Hostname: "db", Type: "postgresql@16", Mode: "NON_HA"},
		},
	}
}

func TestExportGolden_DevVariant(t *testing.T) {
	inputs := exportPairInputs()
	bundle, err := ops.BuildBundle(inputs, topology.ExportVariantDev, classifyPlainAll(inputs.ProjectEnvs))
	if err != nil {
		t.Fatalf("BuildBundle Dev: %v", err)
	}
	if bundle == nil {
		t.Fatal("nil bundle")
	}
	assertGolden(t, "export-dev", bundle.ImportYAML)
}

func TestExportGolden_StageVariant(t *testing.T) {
	inputs := exportPairInputs()
	inputs.TargetHostname = "appstage"
	inputs.SourceMode = topology.ModeStage
	inputs.SetupName = "stage"
	bundle, err := ops.BuildBundle(inputs, topology.ExportVariantStage, classifyPlainAll(inputs.ProjectEnvs))
	if err != nil {
		t.Fatalf("BuildBundle Stage: %v", err)
	}
	assertGolden(t, "export-stage", bundle.ImportYAML)
}
