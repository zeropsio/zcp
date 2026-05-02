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

// scaffoldServiceYaml writes zerops.yaml on the per-service SSHFS mount
// (`<projectRoot>/<hostname>/zerops.yaml`) — the canonical container-env
// layout that mirrors what `ops.deploySSH` reads at deploy time.
func scaffoldServiceYaml(t *testing.T, projectRoot, hostname, body string) {
	t.Helper()
	mountDir := filepath.Join(projectRoot, hostname)
	if err := os.MkdirAll(mountDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", mountDir, err)
	}
	if err := os.WriteFile(filepath.Join(mountDir, "zerops.yaml"), []byte(body), 0o600); err != nil {
		t.Fatalf("write zerops.yaml: %v", err)
	}
}

func TestDeployPreFlight_ValidConfig_Passes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Container-env shape: yaml on the source service's mount.
	scaffoldServiceYaml(t, dir, "appdev", `zerops:
  - setup: dev
    build:
      base: nodejs@22
    run:
      start: node index.js
      envVariables:
        NODE_ENV: development
`)

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
	_, result, err := deployPreFlight(context.Background(), mock, "proj-1", stateDir, "appdev", "appdev", "")
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

	// No zerops.yaml written anywhere.
	mock := platform.NewMock()
	_, result, err := deployPreFlight(context.Background(), mock, "proj-1", stateDir, "appdev", "appdev", "")
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

// TestDeployPreFlight_MissingZeropsYaml_NamesSourceMount pins the failure
// detail when zerops.yaml is missing on the source service's mount: the
// agent must see the canonical mount path AND the hostname so they can
// scaffold at the right location without re-deriving the SSHFS layout
// from CLAUDE.md prose. (Replaces G16's "names per-service path" pin —
// the project-root fallback was removed in the e769c9f7 reverse, so the
// detail no longer mentions it.)
func TestDeployPreFlight_MissingZeropsYaml_NamesSourceMount(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Mount directory exists but is empty (no zerops.yaml scaffolded yet).
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
	_, result, err := deployPreFlight(context.Background(), mock, "proj-1", stateDir, "probe", "probe", "")
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
		mountPath, // source mount path explicitly named
		"probe",   // hostname surfaced for clarity
	} {
		if !strings.Contains(detail, want) {
			t.Errorf("detail should mention %q\ndetail: %q", want, detail)
		}
	}
}

// TestDeployPreFlight_InvalidMountYaml pins that a present-but-malformed
// yaml on the source mount produces a parse-failure diagnostic naming the
// invalid file. The Codex G16 review pinned "no silent fallback" — that
// constraint stayed; the project-root fallback path was removed entirely
// (no fallback anywhere when sourceHostname is set), so the test focuses
// on parse-failure surface clarity.
func TestDeployPreFlight_InvalidMountYaml(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}

	mountDir := filepath.Join(dir, "probe")
	if err := os.MkdirAll(mountDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mountDir, "zerops.yaml"), []byte(":not valid: yaml: ["), 0o600); err != nil {
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
	_, result, err := deployPreFlight(context.Background(), mock, "proj-1", stateDir, "probe", "probe", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil || result.Passed {
		t.Fatalf("preflight must fail when source-mount yaml is invalid; got: %+v", result)
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
	if !strings.Contains(detail, mountDir) {
		t.Errorf("detail must name the source mount path %q\ndetail: %q", mountDir, detail)
	}
	if !strings.Contains(detail, "invalid") {
		t.Errorf("detail must signal that the source-mount file is invalid (not just missing)\ndetail: %q", detail)
	}
}

// TestDeployPreFlight_MountProbeError pins the degraded-mount tri-state
// guard: when the source-mount probe fails for any reason OTHER than
// confirmed absence (permission denied, stale SSHFS, non-directory),
// preflight surfaces that error immediately rather than silently
// succeeding or falling through to a different lookup. The Codex G16
// re-review motivated the guard.
func TestDeployPreFlight_MountProbeError(t *testing.T) {
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
	_, result, err := deployPreFlight(context.Background(), mock, "proj-1", stateDir, "probe", "probe", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil || result.Passed {
		t.Fatalf("preflight must fail when source-mount probe errors; got: %+v", result)
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
		t.Errorf("detail must name the source mount path that failed to probe (%q)\ndetail: %q", mountPath, detail)
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
	scaffoldServiceYaml(t, dir, "appdev", `zerops:
  - setup: prod
    build:
      base: nodejs@22
    run:
      start: node index.js
`)

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
	_, result, err := deployPreFlight(context.Background(), mock, "proj-1", stateDir, "appdev", "appdev", "")
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

	scaffoldServiceYaml(t, dir, "app", `zerops:
  - setup: prod
    build:
      base: nodejs@22
    run:
      start: node index.js
`)

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
	_, result, err := deployPreFlight(context.Background(), mock, "proj-1", stateDir, "app", "app", "prod")
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

	_, result, err := deployPreFlight(context.Background(), nil, "", "", "appdev", "appdev", "")
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
	_, result, err := deployPreFlight(context.Background(), nil, "", stateDir, "unknown", "unknown", "")
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

	scaffoldServiceYaml(t, dir, "appdev", `zerops:
  - setup: dev
    build:
      base: nodejs@22
      deployFiles: dist
    run:
      start: node index.js
`)

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
	_, result, err := deployPreFlight(context.Background(), mock, "proj-1", stateDir, "appdev", "appdev", "")
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
	scaffoldServiceYaml(t, dir, "apidev", `zerops:
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
`)
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
	resolved, result, err := deployPreFlight(context.Background(), mock, "proj-1", stateDir, "apidev", "apidev", "")
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

	scaffoldServiceYaml(t, dir, "apidev", `zerops:
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
`)
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
	_, result, err := deployPreFlight(context.Background(), mock, "proj-1", stateDir, "apidev", "apidev", "apidev")
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

// TestDeployPreFlight_CrossDeploy_ReadsFromSourceMount pins the source-mount
// canonical layout for cross-deploy (docs/spec-workflows.md §1132 + §8 E8).
// `zerops_deploy sourceService=appdev targetService=appstage setup=prod` is
// the dev→stage promotion every standard pair runs at first deploy. Yaml
// lives on the source service's mount; preflight MUST resolve it from
// there. Pre-fix (commit c53e86b1, 2026-04-14) preflight searched
// /var/www/<targetHostname>/zerops.yaml and fell back to /var/www/zerops.yaml,
// neither of which the recipe layout populates — every cross-deploy bounced
// off PREFLIGHT_FAILED until the agent manually copied yaml to project root
// (commit e769c9f7 documented that workaround as canonical instead of
// fixing the lookup).
func TestDeployPreFlight_CrossDeploy_ReadsFromSourceMount(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Standard-pair recipe layout: yaml lives on the dev (source) mount.
	// Stage's mount stays empty until the cross-deploy lands code there.
	scaffoldServiceYaml(t, dir, "appdev", `zerops:
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
      deployFiles: [./dist]
    run:
      start: node dist/main.js
      ports:
        - port: 3000
      envVariables:
        NODE_ENV: production
`)

	// Pair-keyed meta: one file represents both dev and stage halves
	// (spec-workflows.md §8 E8).
	meta := &workflow.ServiceMeta{
		Hostname:         "appdev",
		StageHostname:    "appstage",
		Mode:             "standard",
		BootstrapSession: "s1",
		BootstrappedAt:   "2026-04-01T00:00:00Z",
	}
	if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
		t.Fatal(err)
	}

	mock := platform.NewMock()
	_, result, err := deployPreFlight(
		context.Background(), mock, "proj-1", stateDir,
		"appdev", "appstage", "prod",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if !result.Passed {
		t.Errorf("cross-deploy preflight must succeed with yaml on source mount; got: %s", result.Summary)
		for _, c := range result.Checks {
			if c.Status == statusFail {
				t.Errorf("  fail %s: %s", c.Name, c.Detail)
			}
		}
	}
}

// TestDeployPreFlight_PairAwareStageMetaLookup pins spec-workflows.md §8 E8.
// Stage hostname is a field on the dev meta (one file per pair, not two).
// Reading meta by `appstage` directly (the pre-fix `ReadServiceMeta` path)
// returned nil → preflight bailed out permissively. Switching to
// `FindServiceMeta` resolves the dev meta via StageHostname, and
// `RoleFor("appstage")` returns DeployRoleStage so role-based setup
// resolution maps to "prod" (not "dev").
func TestDeployPreFlight_PairAwareStageMetaLookup(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}

	scaffoldServiceYaml(t, dir, "appdev", `zerops:
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
      deployFiles: [./dist]
    run:
      start: node dist/main.js
      ports:
        - port: 3000
      envVariables:
        NODE_ENV: production
`)

	meta := &workflow.ServiceMeta{
		Hostname:         "appdev",
		StageHostname:    "appstage",
		Mode:             "standard",
		BootstrapSession: "s1",
		BootstrappedAt:   "2026-04-01T00:00:00Z",
	}
	if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
		t.Fatal(err)
	}

	mock := platform.NewMock()
	resolved, result, err := deployPreFlight(
		context.Background(), mock, "proj-1", stateDir,
		"appdev", "appstage", "",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result (pair-aware meta lookup must resolve dev meta via StageHostname)")
	}
	if !result.Passed {
		t.Fatalf("preflight must pass for stage cross-deploy with pair-keyed meta; got: %s", result.Summary)
	}
	// Empty input setup + stage role must resolve to "prod" — proves
	// RoleFor(target) is being used, not PrimaryRole() (which would have
	// returned dev role and resolved to "dev").
	if resolved != "prod" {
		t.Errorf("expected resolvedSetup=\"prod\" via DeployRoleStage; got %q (RoleFor likely not consulted)", resolved)
	}
}

// TestDeployPreFlight_LocalMode_ReadsFromProjectRoot pins the local-env
// lookup path: when sourceHostname is empty (no per-service SSHFS mount —
// the user's developer box), yaml lives at the project root. This is the
// path `ops.deployLocal` reads from, and the only path that makes sense
// when there are no per-service subdirectories.
func TestDeployPreFlight_LocalMode_ReadsFromProjectRoot(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Local layout: yaml at the user's working directory.
	if err := os.WriteFile(filepath.Join(dir, "zerops.yaml"), []byte(`zerops:
  - setup: dev
    build:
      base: nodejs@22
    run:
      start: node index.js
`), 0o600); err != nil {
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
	_, result, err := deployPreFlight(
		context.Background(), mock, "proj-1", stateDir,
		"", "appdev", "",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if !result.Passed {
		t.Errorf("local-mode preflight must read yaml from project root; got: %s", result.Summary)
	}
}

// TestDeployPreFlight_ContainerMode_NoProjectRootFallback pins the
// container-env contract: when sourceHostname is set, preflight MUST
// resolve yaml from the source mount only — never from the project root.
// A stray `<projectRoot>/zerops.yaml` describes nothing the platform
// understands (the recipe-route scaffold places it on the source mount;
// no other valid layout produces a project-root yaml in container env),
// and silently validating it masked the dev→stage cross-deploy failure
// the e769c9f7 atom documentation papered over.
func TestDeployPreFlight_ContainerMode_NoProjectRootFallback(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// A perfectly valid yaml at project root — pre-fix the fallback would
	// have silently used it.
	if err := os.WriteFile(filepath.Join(dir, "zerops.yaml"), []byte(`zerops:
  - setup: dev
    build:
      base: nodejs@22
    run:
      start: node index.js
`), 0o600); err != nil {
		t.Fatal(err)
	}

	// Source-mount directory does not exist (or, equivalently, has no
	// zerops.yaml). Either way the source-mount lookup misses; preflight
	// must NOT silently pick up the project-root yaml.
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
	_, result, err := deployPreFlight(
		context.Background(), mock, "proj-1", stateDir,
		"appdev", "appdev", "",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil || result.Passed {
		t.Fatalf("container-env preflight must NOT fall back to project-root yaml; got: %+v", result)
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
