package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

func TestDeployPreFlight_ValidConfig_Passes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write zerops.yaml at project root.
	yaml := `zerops:
  - setup: dev
    build:
      base: nodejs@22
    run:
      start: node index.js
      envVariables:
        NODE_ENV: development
`
	if err := os.WriteFile(filepath.Join(dir, "zerops.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}

	// Write ServiceMeta.
	meta := &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             "dev",
		BootstrapSession: "s1",
		BootstrappedAt:   "2026-04-01T00:00:00Z",
	}
	if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
		t.Fatal(err)
	}

	mock := platform.NewMock()
	_, result, err := deployPreFlight(context.Background(), mock, "proj-1", stateDir, "appdev", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if !result.Passed {
		t.Errorf("expected passed, got failed: %s", result.Summary)
		for _, c := range result.Checks {
			if c.Status == statusFail {
				t.Errorf("  %s: %s", c.Name, c.Detail)
			}
		}
	}
}

func TestDeployPreFlight_MissingZeropsYaml_Fails(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}

	meta := &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             "dev",
		BootstrapSession: "s1",
		BootstrappedAt:   "2026-04-01T00:00:00Z",
	}
	if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
		t.Fatal(err)
	}

	// No zerops.yaml written.
	mock := platform.NewMock()
	_, result, err := deployPreFlight(context.Background(), mock, "proj-1", stateDir, "appdev", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Passed {
		t.Error("expected fail when zerops.yaml missing")
	}
	hasYmlCheck := false
	for _, c := range result.Checks {
		if c.Name == "zerops_yml_exists" && c.Status == statusFail {
			hasYmlCheck = true
		}
	}
	if !hasYmlCheck {
		t.Error("expected zerops_yml_exists fail check")
	}
}

// TestDeployPreFlight_MissingZeropsYaml_NamesPerServicePath pins G16:
// when zerops.yaml is missing in container env (per-service mount exists
// but is empty, and project root has no fallback), the failure detail
// must name BOTH paths searched AND point the agent at the per-service
// mount as the canonical scaffold location. Pre-fix the detail named
// only the project root and the agent had to infer the right directory
// from CLAUDE.md prose after the error.
func TestDeployPreFlight_MissingZeropsYaml_NamesPerServicePath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Container-env shape: per-service mount directory exists at the
	// SSHFS path, but it is empty (no zerops.yaml scaffolded yet).
	if err := os.MkdirAll(filepath.Join(dir, "probe"), 0o755); err != nil {
		t.Fatal(err)
	}

	meta := &workflow.ServiceMeta{
		Hostname:         "probe",
		Mode:             "simple",
		BootstrapSession: "s1",
		BootstrappedAt:   "2026-04-01T00:00:00Z",
	}
	if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
		t.Fatal(err)
	}

	mock := platform.NewMock()
	_, result, err := deployPreFlight(context.Background(), mock, "proj-1", stateDir, "probe", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil || result.Passed {
		t.Fatalf("expected fail result, got: %+v", result)
	}

	var detail string
	for _, c := range result.Checks {
		if c.Name == "zerops_yml_exists" && c.Status == statusFail {
			detail = c.Detail
			break
		}
	}
	if detail == "" {
		t.Fatal("expected zerops_yml_exists fail check with detail")
	}

	mountPath := filepath.Join(dir, "probe")
	for _, want := range []string{
		mountPath, // per-service mount path explicitly named
		dir,       // project root also named (fallback path)
		"probe",   // hostname surfaced for clarity
	} {
		if !strings.Contains(detail, want) {
			t.Errorf("detail should mention %q\ndetail: %q", want, detail)
		}
	}
}

// TestDeployPreFlight_InvalidMountYaml_DoesNotFallBackToRoot pins the
// Codex finding: when the per-service mount has an INVALID zerops.yaml
// (file present, parser rejects), the preflight must NOT silently fall
// back to a valid project-root zerops.yaml — the per-service file is
// canonical for the targetHostname; falling back would validate an
// unrelated config and surface the real failure later. Pre-fix the
// mount-path parse error was discarded and the root yaml short-circuited
// the path search.
func TestDeployPreFlight_InvalidMountYaml_DoesNotFallBackToRoot(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Per-service mount: zerops.yaml exists but is malformed YAML.
	mountDir := filepath.Join(dir, "probe")
	if err := os.MkdirAll(mountDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mountDir, "zerops.yaml"), []byte(":not valid: yaml: ["), 0o600); err != nil {
		t.Fatal(err)
	}

	// Project root: a perfectly valid zerops.yaml — would silently
	// shadow the broken per-service one if the fallback fired.
	rootYaml := `zerops:
  - setup: probe
    build:
      base: nodejs@22
    run:
      start: node index.js
`
	if err := os.WriteFile(filepath.Join(dir, "zerops.yaml"), []byte(rootYaml), 0o600); err != nil {
		t.Fatal(err)
	}

	meta := &workflow.ServiceMeta{
		Hostname:         "probe",
		Mode:             "simple",
		BootstrapSession: "s1",
		BootstrappedAt:   "2026-04-01T00:00:00Z",
	}
	if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
		t.Fatal(err)
	}

	mock := platform.NewMock()
	_, result, err := deployPreFlight(context.Background(), mock, "proj-1", stateDir, "probe", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil || result.Passed {
		t.Fatalf("preflight must fail when per-service yaml is invalid; got: %+v", result)
	}

	var detail string
	for _, c := range result.Checks {
		if c.Name == "zerops_yml_exists" && c.Status == statusFail {
			detail = c.Detail
			break
		}
	}
	if detail == "" {
		t.Fatal("expected zerops_yml_exists fail check with detail naming the invalid file")
	}
	// Detail must name the per-service mount path AND signal that the
	// failure is parse-not-missing (so the agent fixes the right file).
	if !strings.Contains(detail, mountDir) {
		t.Errorf("detail must name the per-service mount path %q\ndetail: %q", mountDir, detail)
	}
	if !strings.Contains(detail, "invalid YAML") && !strings.Contains(detail, "invalid") {
		t.Errorf("detail must signal that the per-service file is invalid (not just missing)\ndetail: %q", detail)
	}
}

// TestDeployPreFlight_MountProbeError_DoesNotFallBackToRoot pins the
// degraded-mount tri-state guard: when the per-service mount path
// probe fails for any reason OTHER than confirmed absence (permission
// denied, stale SSHFS, non-directory), the preflight must surface
// that error immediately rather than silently fall back to the
// project-root zerops.yaml. The fallback is only safe on confirmed
// absence — Codex re-review of the G16 follow-up.
func TestDeployPreFlight_MountProbeError_DoesNotFallBackToRoot(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Create the mount path as a FILE, not a directory — probing it
	// returns a non-directory error, which is a probe failure (not
	// confirmed absence). Same blast radius as a stale SSHFS mount or
	// a permission-denied stat.
	mountPath := filepath.Join(dir, "probe")
	if err := os.WriteFile(mountPath, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Project root with a perfectly valid zerops.yaml — would silently
	// shadow the broken mount probe if the fallback fired.
	rootYaml := `zerops:
  - setup: probe
    build:
      base: nodejs@22
    run:
      start: node index.js
`
	if err := os.WriteFile(filepath.Join(dir, "zerops.yaml"), []byte(rootYaml), 0o600); err != nil {
		t.Fatal(err)
	}

	meta := &workflow.ServiceMeta{
		Hostname:         "probe",
		Mode:             "simple",
		BootstrapSession: "s1",
		BootstrappedAt:   "2026-04-01T00:00:00Z",
	}
	if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
		t.Fatal(err)
	}

	mock := platform.NewMock()
	_, result, err := deployPreFlight(context.Background(), mock, "proj-1", stateDir, "probe", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil || result.Passed {
		t.Fatalf("preflight must fail when per-service mount probe errors; got: %+v", result)
	}

	var detail string
	for _, c := range result.Checks {
		if c.Name == "zerops_yml_exists" && c.Status == statusFail {
			detail = c.Detail
			break
		}
	}
	if detail == "" {
		t.Fatal("expected zerops_yml_exists fail check with detail naming the probe failure")
	}
	if !strings.Contains(detail, mountPath) {
		t.Errorf("detail must name the per-service mount path that failed to probe (%q)\ndetail: %q", mountPath, detail)
	}
	if !strings.Contains(detail, "probe failed") && !strings.Contains(detail, "not a directory") {
		t.Errorf("detail must signal probe failure / non-directory, not just 'not found'\ndetail: %q", detail)
	}
}

func TestDeployPreFlight_MissingSetupEntry_Fails(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// zerops.yaml with "prod" setup but no "dev" setup.
	yaml := `zerops:
  - setup: prod
    build:
      base: nodejs@22
    run:
      start: node index.js
`
	if err := os.WriteFile(filepath.Join(dir, "zerops.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}

	meta := &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             "dev",
		BootstrapSession: "s1",
		BootstrappedAt:   "2026-04-01T00:00:00Z",
	}
	if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
		t.Fatal(err)
	}

	mock := platform.NewMock()
	_, result, err := deployPreFlight(context.Background(), mock, "proj-1", stateDir, "appdev", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Passed {
		t.Error("expected fail when setup entry missing")
	}
	hasSetupCheck := false
	for _, c := range result.Checks {
		if c.Name == "appdev_setup" && c.Status == statusFail {
			hasSetupCheck = true
		}
	}
	if !hasSetupCheck {
		t.Error("expected appdev_setup fail check")
	}
}

func TestDeployPreFlight_ExplicitSetup_Passes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}

	yaml := `zerops:
  - setup: prod
    build:
      base: nodejs@22
    run:
      start: node index.js
`
	if err := os.WriteFile(filepath.Join(dir, "zerops.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}

	// Simple mode service — role "simple" maps to "prod" only via explicit param.
	meta := &workflow.ServiceMeta{
		Hostname:         "app",
		Mode:             "simple",
		BootstrapSession: "s1",
		BootstrappedAt:   "2026-04-01T00:00:00Z",
	}
	if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
		t.Fatal(err)
	}

	mock := platform.NewMock()
	_, result, err := deployPreFlight(context.Background(), mock, "proj-1", stateDir, "app", "prod")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if !result.Passed {
		t.Errorf("expected passed with explicit setup=prod, got: %s", result.Summary)
	}
}

func TestDeployPreFlight_EmptyStateDir_ReturnsNil(t *testing.T) {
	t.Parallel()

	_, result, err := deployPreFlight(context.Background(), nil, "", "", "appdev", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("expected nil result when stateDir is empty")
	}
}

func TestDeployPreFlight_NoMeta_ReturnsNil(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	_, result, err := deployPreFlight(context.Background(), nil, "", stateDir, "unknown", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("expected nil result when no ServiceMeta exists")
	}
}

// TestDeployPreFlight_DeployFilesNotCheckedInPreflight is a DM-4 regression
// gate (docs/spec-workflows.md §8 Deploy Modes). Deploy-class-aware
// deployFiles validation lives at the push site in ops.ValidateZeropsYml
// (DM-2 self-deploy enforcement) and at the Zerops builder (post-build
// filesystem existence). The tool-layer pre-flight MUST NOT duplicate
// either — that would violate DM-4's layered-authority invariant and
// re-introduce F3-style false positives. A cherry-pick deployFiles
// pointing at a path absent from the pre-build tree must NOT produce
// an `appdev_deploy_files` check of any kind.
func TestDeployPreFlight_DeployFilesNotCheckedInPreflight(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}

	yaml := `zerops:
  - setup: dev
    build:
      base: nodejs@22
      deployFiles: dist
    run:
      start: node index.js
`
	if err := os.WriteFile(filepath.Join(dir, "zerops.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}

	meta := &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             "dev",
		BootstrapSession: "s1",
		BootstrappedAt:   "2026-04-01T00:00:00Z",
	}
	if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
		t.Fatal(err)
	}

	mock := platform.NewMock()
	_, result, err := deployPreFlight(context.Background(), mock, "proj-1", stateDir, "appdev", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	for _, c := range result.Checks {
		if c.Name == "appdev_deploy_files" {
			t.Errorf("pre-flight must NOT emit deploy_files check (DM-4): found %+v", c)
		}
	}
}

// TestDeployPreFlight_ResolvedSetupEchoedBack — v8.85. When the caller
// passes empty `setup` and pre-flight resolves it via role fallback, the
// resolved setup name must be returned so the handler can pass it
// explicitly to zcli. Without this, zcli received an empty --setup flag
// and errored with "Cannot find corresponding setup in zerops.yaml" — the
// session-log-16 L145 failure that forced the agent to self-correct.
func TestDeployPreFlight_ResolvedSetupEchoedBack(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// zerops.yaml with BOTH dev and prod — like every recipe ships. An
	// empty setup param with hostname=apidev (role=dev) must resolve to
	// "dev", and the resolver must echo "dev" back.
	yaml := `zerops:
  - setup: dev
    build:
      base: nodejs@22
      deployFiles: [.]
    run:
      start: node dist/main.js
      ports:
        - port: 3000
      envVariables:
        NODE_ENV: development
  - setup: prod
    build:
      base: nodejs@22
      deployFiles: [.]
    run:
      start: node dist/main.js
      ports:
        - port: 3000
      envVariables:
        NODE_ENV: production
`
	if err := os.WriteFile(filepath.Join(dir, "zerops.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	meta := &workflow.ServiceMeta{
		Hostname:         "apidev",
		Mode:             "dev",
		BootstrapSession: "s1",
		BootstrappedAt:   "2026-04-01T00:00:00Z",
	}
	if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
		t.Fatal(err)
	}

	mock := platform.NewMock()
	resolved, result, err := deployPreFlight(context.Background(), mock, "proj-1", stateDir, "apidev", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if !result.Passed {
		t.Fatalf("expected pre-flight to pass; got failures: %+v", result.Checks)
	}
	if resolved != "dev" {
		t.Errorf("expected resolvedSetup=\"dev\" (role-based fallback for hostname=apidev); got %q", resolved)
	}
}

// TestDeployPreFlight_UnknownSetup_ListsAvailable — v8.85. When the caller
// passes an explicit setup that doesn't match any block, the error must
// list the actual setup names available so the agent can correct the call
// instead of guessing.
func TestDeployPreFlight_UnknownSetup_ListsAvailable(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}

	yaml := `zerops:
  - setup: dev
    build:
      base: nodejs@22
      deployFiles: [.]
    run:
      start: node dist/main.js
      ports:
        - port: 3000
      envVariables:
        NODE_ENV: development
  - setup: prod
    build:
      base: nodejs@22
      deployFiles: [.]
    run:
      start: node dist/main.js
      ports:
        - port: 3000
      envVariables:
        NODE_ENV: production
`
	if err := os.WriteFile(filepath.Join(dir, "zerops.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	meta := &workflow.ServiceMeta{
		Hostname:         "apidev",
		Mode:             "dev",
		BootstrapSession: "s1",
		BootstrappedAt:   "2026-04-01T00:00:00Z",
	}
	if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
		t.Fatal(err)
	}

	mock := platform.NewMock()
	_, result, err := deployPreFlight(context.Background(), mock, "proj-1", stateDir, "apidev", "apidev")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil || result.Passed {
		t.Fatal("expected pre-flight failure on unknown setup name")
	}
	var detail string
	for _, c := range result.Checks {
		if c.Name == "apidev_setup" && c.Status == statusFail {
			detail = c.Detail
			break
		}
	}
	if detail == "" {
		t.Fatal("expected apidev_setup fail check with detail")
	}
	// Must name each available setup so the agent can self-correct.
	for _, want := range []string{"dev", "prod", "available setups"} {
		if !containsString(detail, want) {
			t.Errorf("error detail missing %q; got: %q", want, detail)
		}
	}
}

func containsString(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return len(needle) == 0
}
