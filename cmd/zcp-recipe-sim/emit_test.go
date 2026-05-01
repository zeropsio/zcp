// Tests for the `emit` subcommand. S3 covers staging code artifacts
// (src/**, package.json, composer.json, app/**) so the replayed
// codebase-content + claudemd-author sub-agents can read what they
// reference. S4 covers Parent + MountRoot threading. S5 covers the
// refinement-prompt emit branch.
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestStageCodebaseArtifacts_CopiesSrcAndManifests asserts the union
// of files the codebase-content + claudemd-author briefs reference
// land in the simulation dir: `src/**`, `package.json`, `composer.json`,
// `app/**` (Laravel). Pinned by run-20 prep S3.
func TestStageCodebaseArtifacts_CopiesSrcAndManifests(t *testing.T) {
	runDir, simDir := t.TempDir(), t.TempDir()
	host := "api"
	runHostDir := filepath.Join(runDir, host+"dev")

	mustWrite(t, filepath.Join(runHostDir, "zerops.yaml"), "#bare\nzerops:\n")
	mustWrite(t, filepath.Join(runHostDir, "package.json"), `{"name":"api"}`)
	mustWrite(t, filepath.Join(runHostDir, "composer.json"), `{"name":"api/php"}`)
	mustWrite(t, filepath.Join(runHostDir, "src", "main.ts"), "console.log('hi');\n")
	mustWrite(t, filepath.Join(runHostDir, "src", "broker", "consumer.ts"), "// consumer\n")
	mustWrite(t, filepath.Join(runHostDir, "app", "Http", "Controllers", "Foo.php"), "<?php\n")

	simHostDir := filepath.Join(simDir, host+"dev")
	if err := os.MkdirAll(simHostDir, 0o755); err != nil {
		t.Fatalf("mkdir simHostDir: %v", err)
	}
	if err := stageCodebaseArtifacts(runHostDir, simHostDir); err != nil {
		t.Fatalf("stageCodebaseArtifacts: %v", err)
	}

	for _, rel := range []string{
		"package.json",
		"composer.json",
		"src/main.ts",
		"src/broker/consumer.ts",
		"app/Http/Controllers/Foo.php",
	} {
		if _, err := os.Stat(filepath.Join(simHostDir, rel)); err != nil {
			t.Errorf("expected %s staged in simHostDir; stat error: %v", rel, err)
		}
	}
}

// TestStageCodebaseArtifacts_SkipsBulkDirs asserts node_modules/,
// vendor/, .git/ are NOT copied. Pinned by run-20 prep S3.
func TestStageCodebaseArtifacts_SkipsBulkDirs(t *testing.T) {
	runDir, simDir := t.TempDir(), t.TempDir()
	host := "api"
	runHostDir := filepath.Join(runDir, host+"dev")

	mustWrite(t, filepath.Join(runHostDir, "package.json"), `{"name":"api"}`)
	mustWrite(t, filepath.Join(runHostDir, "node_modules", "foo", "index.js"), "// nm\n")
	mustWrite(t, filepath.Join(runHostDir, "vendor", "bar", "index.php"), "<?php\n")
	mustWrite(t, filepath.Join(runHostDir, ".git", "HEAD"), "ref: refs/heads/main\n")
	mustWrite(t, filepath.Join(runHostDir, "src", "node_modules", "x.js"), "// nested nm\n")

	simHostDir := filepath.Join(simDir, host+"dev")
	if err := os.MkdirAll(simHostDir, 0o755); err != nil {
		t.Fatalf("mkdir simHostDir: %v", err)
	}
	if err := stageCodebaseArtifacts(runHostDir, simHostDir); err != nil {
		t.Fatalf("stageCodebaseArtifacts: %v", err)
	}

	for _, rel := range []string{
		"node_modules/foo/index.js",
		"vendor/bar/index.php",
		".git/HEAD",
		"src/node_modules/x.js",
	} {
		if _, err := os.Stat(filepath.Join(simHostDir, rel)); err == nil {
			t.Errorf("expected %s NOT staged (bulk dir); but it was copied", rel)
		}
	}
	if _, err := os.Stat(filepath.Join(simHostDir, "package.json")); err != nil {
		t.Errorf("expected package.json staged; %v", err)
	}
}

// TestStageCodebaseArtifacts_SkipsUnknownFiles asserts files outside
// the documented union (e.g. dist/, build/, random top-level files
// not mentioned by either brief) are NOT staged — match what's
// referenced rather than over-staging "framework manifests" broadly.
// Pinned by run-20 prep S3.
func TestStageCodebaseArtifacts_SkipsUnknownFiles(t *testing.T) {
	runDir, simDir := t.TempDir(), t.TempDir()
	host := "api"
	runHostDir := filepath.Join(runDir, host+"dev")

	mustWrite(t, filepath.Join(runHostDir, "package.json"), `{"name":"api"}`)
	mustWrite(t, filepath.Join(runHostDir, "tsconfig.json"), `{}`)
	mustWrite(t, filepath.Join(runHostDir, "Dockerfile"), "FROM nodejs\n")
	mustWrite(t, filepath.Join(runHostDir, "dist", "main.js"), "// built\n")
	mustWrite(t, filepath.Join(runHostDir, "README.md"), "# api\n")

	simHostDir := filepath.Join(simDir, host+"dev")
	if err := os.MkdirAll(simHostDir, 0o755); err != nil {
		t.Fatalf("mkdir simHostDir: %v", err)
	}
	if err := stageCodebaseArtifacts(runHostDir, simHostDir); err != nil {
		t.Fatalf("stageCodebaseArtifacts: %v", err)
	}

	for _, rel := range []string{
		"tsconfig.json",
		"Dockerfile",
		"dist/main.js",
		"README.md",
	} {
		if _, err := os.Stat(filepath.Join(simHostDir, rel)); err == nil {
			t.Errorf("expected %s NOT staged (outside documented union); but it was copied", rel)
		}
	}
	if _, err := os.Stat(filepath.Join(simHostDir, "package.json")); err != nil {
		t.Errorf("expected package.json staged; %v", err)
	}
}

// TestRunEmit_StagesCodebaseArtifacts_EndToEnd exercises the full emit
// path with a fixture run dir; asserts src/** + manifests reach the
// simulation per-codebase dir. Pinned by run-20 prep S3.
func TestRunEmit_StagesCodebaseArtifacts_EndToEnd(t *testing.T) {
	runDir := t.TempDir()
	simDir := t.TempDir()
	if err := writeMinimalRunDir(t, runDir); err != nil {
		t.Fatalf("writeMinimalRunDir: %v", err)
	}

	if err := runEmit([]string{"-run", runDir, "-out", simDir}); err != nil {
		t.Fatalf("runEmit: %v", err)
	}

	simHost := filepath.Join(simDir, "apidev")
	if _, err := os.Stat(filepath.Join(simHost, "zerops.yaml")); err != nil {
		t.Errorf("zerops.yaml not staged: %v", err)
	}
	if _, err := os.Stat(filepath.Join(simHost, "package.json")); err != nil {
		t.Errorf("package.json not staged: %v", err)
	}
	if _, err := os.Stat(filepath.Join(simHost, "src", "main.ts")); err != nil {
		t.Errorf("src/main.ts not staged: %v", err)
	}
	// node_modules from the run dir must not leak into the simulation.
	if _, err := os.Stat(filepath.Join(simHost, "node_modules")); err == nil {
		t.Errorf("node_modules/ leaked into sim staging")
	}
}

// mustWrite is a test helper that writes data, creating parent dirs.
func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// writeMinimalRunDir lays out a run-19-shape run dir compatible with
// runEmit: environments/{plan.json,facts.jsonl}, one codebase
// (`apidev/`) with bare zerops.yaml + package.json + src/.
func writeMinimalRunDir(t *testing.T, runDir string) error {
	t.Helper()
	apiDir := filepath.Join(runDir, "apidev")
	envDir := filepath.Join(runDir, "environments")
	if err := os.MkdirAll(apiDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(envDir, 0o755); err != nil {
		return err
	}
	mustWrite(t, filepath.Join(apiDir, "zerops.yaml"), minimalYAML)
	mustWrite(t, filepath.Join(apiDir, "package.json"), `{"name":"api","version":"1.0.0"}`)
	mustWrite(t, filepath.Join(apiDir, "src", "main.ts"), "console.log('hi');\n")
	mustWrite(t, filepath.Join(apiDir, "node_modules", "foo", "index.js"), "// nm\n")

	plan := minimalPlan(filepath.Join(runDir, "apidev"))
	body, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	if err := os.WriteFile(filepath.Join(envDir, "plan.json"), body, 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(envDir, "facts.jsonl"), nil, 0o600); err != nil {
		return err
	}
	return nil
}
