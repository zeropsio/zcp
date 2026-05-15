package ops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// writeYaml is a test helper that writes a zerops.yaml into dir.
func writeYaml(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "zerops.yaml"), []byte(content), 0644); err != nil {
		t.Fatalf("write zerops.yaml: %v", err)
	}
}

// writeEnvLocal is a test helper that writes a .env.local into dir.
func writeEnvLocal(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, ".env.local"), []byte(content), 0600); err != nil {
		t.Fatalf("write .env.local: %v", err)
	}
}

// findKey returns the EnvKey with the given key name from the plan,
// or nil if absent.
func findKey(plan *EnvPlan, key string) *EnvKey {
	for i := range plan.Keys {
		if plan.Keys[i].Key == key {
			return &plan.Keys[i]
		}
	}
	return nil
}

// TestBuildEnvPlan_OnlyProjectEnv pins that project envVariables flow
// into the plan with SourceProject when no zerops.yaml run.envVariables
// or .env.local overlay contributes.
func TestBuildEnvPlan_OnlyProjectEnv(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	writeYaml(t, tmpDir, `zerops:
  - setup: app
    run:
      base: nodejs@22
`)

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "test", Status: statusActive}).
		WithProjectEnv([]platform.ProjectEnvVar{
			{ID: "pe1", Key: "APP_KEY", Content: "base64:secret"},
			{ID: "pe2", Key: "JWT_SECRET", Content: "shared-jwt"},
		})

	plan, err := BuildEnvPlan(context.Background(), mock, "p1", "app", tmpDir)
	if err != nil {
		t.Fatalf("BuildEnvPlan: %v", err)
	}

	if got, want := len(plan.Keys), 2; got != want {
		t.Fatalf("plan keys = %d, want %d", got, want)
	}
	for _, k := range plan.Keys {
		if k.Source != SourceProject {
			t.Errorf("key %s source = %s, want %s", k.Key, k.Source, SourceProject)
		}
		if k.Conflict != StatusClean {
			t.Errorf("key %s conflict = %s, want %s", k.Key, k.Conflict, StatusClean)
		}
	}
}

// TestBuildEnvPlan_PrecedenceYAMLOverProject pins that yaml-setup
// values shadow project values for the same key, with StatusShadowed.
func TestBuildEnvPlan_PrecedenceYAMLOverProject(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	writeYaml(t, tmpDir, `zerops:
  - setup: app
    run:
      envVariables:
        APP_NAME: from-yaml-stage
`)

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "test", Status: statusActive}).
		WithServices([]platform.ServiceStack{
			{ID: "svc-app", Name: "app", ProjectID: "p1", Status: "RUNNING"},
		}).
		WithProjectEnv([]platform.ProjectEnvVar{
			{ID: "pe1", Key: "APP_NAME", Content: "from-project"},
		})

	plan, err := BuildEnvPlan(context.Background(), mock, "p1", "app", tmpDir)
	if err != nil {
		t.Fatalf("BuildEnvPlan: %v", err)
	}

	got := findKey(plan, "APP_NAME")
	if got == nil {
		t.Fatal("APP_NAME missing from plan")
	}
	if got.Value != "from-yaml-stage" {
		t.Errorf("APP_NAME value = %q, want %q (yaml shadows project)", got.Value, "from-yaml-stage")
	}
	if got.Source != SourceYAMLSetup {
		t.Errorf("APP_NAME source = %s, want %s", got.Source, SourceYAMLSetup)
	}
	if got.Conflict != StatusShadowed {
		t.Errorf("APP_NAME conflict = %s, want %s", got.Conflict, StatusShadowed)
	}
}

// TestBuildEnvPlan_OverlayWinsOverYAML pins that .env.local always
// wins over yaml-setup, marking conflict as StatusOverridden.
func TestBuildEnvPlan_OverlayWinsOverYAML(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	writeYaml(t, tmpDir, `zerops:
  - setup: app
    run:
      envVariables:
        APP_ENV: production
        DB_HOST: ${db_hostname}
`)
	writeEnvLocal(t, tmpDir, `# user override
APP_ENV=local
LOG_LEVEL=debug
`)

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "test", Status: statusActive}).
		WithServices([]platform.ServiceStack{
			{ID: "svc-app", Name: "app", ProjectID: "p1", Status: "RUNNING"},
			{ID: "svc-db", Name: "db", ProjectID: "p1", Status: "RUNNING"},
		}).
		WithServiceEnv("svc-db", []platform.ServiceEnvVar{
			{ID: "e1", Key: "hostname", Content: "db"},
		})

	plan, err := BuildEnvPlan(context.Background(), mock, "p1", "app", tmpDir)
	if err != nil {
		t.Fatalf("BuildEnvPlan: %v", err)
	}

	appEnv := findKey(plan, "APP_ENV")
	if appEnv == nil {
		t.Fatal("APP_ENV missing")
	}
	if appEnv.Value != "local" {
		t.Errorf("APP_ENV value = %q, want %q (overlay wins)", appEnv.Value, "local")
	}
	if appEnv.Source != SourceLocalOverlay {
		t.Errorf("APP_ENV source = %s, want %s", appEnv.Source, SourceLocalOverlay)
	}
	if appEnv.Conflict != StatusOverridden {
		t.Errorf("APP_ENV conflict = %s, want %s", appEnv.Conflict, StatusOverridden)
	}

	logLevel := findKey(plan, "LOG_LEVEL")
	if logLevel == nil {
		t.Fatal("LOG_LEVEL missing (should come from overlay)")
	}
	if logLevel.Source != SourceLocalOverlay {
		t.Errorf("LOG_LEVEL source = %s, want %s", logLevel.Source, SourceLocalOverlay)
	}
	if logLevel.Conflict != StatusClean {
		t.Errorf("LOG_LEVEL conflict = %s, want %s (no base value to override)", logLevel.Conflict, StatusClean)
	}
	if logLevel.Scope != ScopeLocalOverride {
		t.Errorf("LOG_LEVEL scope = %s, want %s", logLevel.Scope, ScopeLocalOverride)
	}

	dbHost := findKey(plan, "DB_HOST")
	if dbHost == nil {
		t.Fatal("DB_HOST missing")
	}
	if dbHost.Value != "db" {
		t.Errorf("DB_HOST value = %q, want %q (yaml ref resolved, no overlay)", dbHost.Value, "db")
	}
	if dbHost.Scope != ScopeManagedRef {
		t.Errorf("DB_HOST scope = %s, want %s", dbHost.Scope, ScopeManagedRef)
	}
}

// TestBuildEnvPlan_OverlayWinsOverProject pins that .env.local also
// overrides project envVariables (not just yaml-setup).
func TestBuildEnvPlan_OverlayWinsOverProject(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	writeYaml(t, tmpDir, `zerops:
  - setup: app
    run:
      base: nodejs@22
`)
	writeEnvLocal(t, tmpDir, `APP_KEY=local-dev-key
`)

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "test", Status: statusActive}).
		WithProjectEnv([]platform.ProjectEnvVar{
			{ID: "pe1", Key: "APP_KEY", Content: "base64:project-secret"},
		})

	plan, err := BuildEnvPlan(context.Background(), mock, "p1", "app", tmpDir)
	if err != nil {
		t.Fatalf("BuildEnvPlan: %v", err)
	}

	appKey := findKey(plan, "APP_KEY")
	if appKey == nil {
		t.Fatal("APP_KEY missing")
	}
	if appKey.Value != "local-dev-key" {
		t.Errorf("APP_KEY value = %q, want overlay value", appKey.Value)
	}
	if appKey.Conflict != StatusOverridden {
		t.Errorf("APP_KEY conflict = %s, want %s", appKey.Conflict, StatusOverridden)
	}
	if appKey.Scope != ScopeShared {
		t.Errorf("APP_KEY scope = %s, want %s (preserves base scope when overridden)", appKey.Scope, ScopeShared)
	}
}

// TestBuildEnvPlan_StableKeyOrdering pins alphabetical ordering of
// keys regardless of source. Determinism across runs is required for
// readable .env diffs in git.
func TestBuildEnvPlan_StableKeyOrdering(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	writeYaml(t, tmpDir, `zerops:
  - setup: app
    run:
      envVariables:
        ZEBRA_VAR: z
        ALPHA_VAR: a
`)
	writeEnvLocal(t, tmpDir, `MIDDLE_VAR=m
BETA_VAR=b
`)

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "test", Status: statusActive}).
		WithServices([]platform.ServiceStack{
			{ID: "svc-app", Name: "app", ProjectID: "p1", Status: "RUNNING"},
		}).
		WithProjectEnv([]platform.ProjectEnvVar{
			{ID: "pe1", Key: "PROJECT_VAR", Content: "p"},
		})

	plan, err := BuildEnvPlan(context.Background(), mock, "p1", "app", tmpDir)
	if err != nil {
		t.Fatalf("BuildEnvPlan: %v", err)
	}

	want := []string{"ALPHA_VAR", "BETA_VAR", "MIDDLE_VAR", "PROJECT_VAR", "ZEBRA_VAR"}
	got := make([]string, len(plan.Keys))
	for i, k := range plan.Keys {
		got[i] = k.Key
	}
	if len(got) != len(want) {
		t.Fatalf("got %d keys, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("position %d: got %q, want %q (full order: %v)", i, got[i], want[i], got)
		}
	}
}

// TestBuildEnvPlan_MultipleSetups_RequiresSelection pins that bare
// invocation against multi-setup zerops.yaml returns ErrSetupRequired
// with all available setup names.
func TestBuildEnvPlan_MultipleSetups_RequiresSelection(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	writeYaml(t, tmpDir, `zerops:
  - setup: app
    run:
      base: nodejs@22
  - setup: worker
    run:
      base: nodejs@22
`)

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "test", Status: statusActive})

	_, err := BuildEnvPlan(context.Background(), mock, "p1", "", tmpDir)
	if err == nil {
		t.Fatal("expected ErrSetupRequired, got nil")
	}
	var setupErr *SetupRequiredError
	if !errors.As(err, &setupErr) {
		t.Fatalf("error type = %T, want *SetupRequiredError: %v", err, err)
	}
	wantAvailable := []string{"app", "worker"}
	if len(setupErr.Available) != len(wantAvailable) {
		t.Fatalf("available = %v, want %v", setupErr.Available, wantAvailable)
	}
	for i, name := range wantAvailable {
		if setupErr.Available[i] != name {
			t.Errorf("available[%d] = %q, want %q", i, setupErr.Available[i], name)
		}
	}
}

// TestBuildEnvPlan_MultipleSetups_ExplicitPicks pins that explicit
// setup name selects the right block.
func TestBuildEnvPlan_MultipleSetups_ExplicitPicks(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	writeYaml(t, tmpDir, `zerops:
  - setup: app
    run:
      envVariables:
        SERVICE_NAME: app-runtime
  - setup: worker
    run:
      envVariables:
        SERVICE_NAME: worker-runtime
`)

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "test", Status: statusActive}).
		WithServices([]platform.ServiceStack{
			{ID: "svc-app", Name: "app", ProjectID: "p1", Status: "RUNNING"},
		})

	plan, err := BuildEnvPlan(context.Background(), mock, "p1", "worker", tmpDir)
	if err != nil {
		t.Fatalf("BuildEnvPlan: %v", err)
	}

	if plan.Setup != "worker" {
		t.Errorf("plan.Setup = %q, want %q", plan.Setup, "worker")
	}
	got := findKey(plan, "SERVICE_NAME")
	if got == nil || got.Value != "worker-runtime" {
		t.Errorf("SERVICE_NAME = %v, want value %q", got, "worker-runtime")
	}
}

// TestBuildEnvPlan_SetupNotFound_ReturnsError pins that an explicit
// setup name not in zerops.yaml errors out cleanly with available
// names.
func TestBuildEnvPlan_SetupNotFound_ReturnsError(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	writeYaml(t, tmpDir, `zerops:
  - setup: app
    run:
      base: nodejs@22
`)

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "test", Status: statusActive})

	_, err := BuildEnvPlan(context.Background(), mock, "p1", "nonexistent", tmpDir)
	if err == nil {
		t.Fatal("expected error for missing setup, got nil")
	}
	if !strings.Contains(err.Error(), "nonexistent") || !strings.Contains(err.Error(), "app") {
		t.Errorf("error should mention attempted and available setups, got: %v", err)
	}
}

// TestBuildEnvPlan_PlatformInternalsFiltered pins that the platform-
// internals denylist removes ZCP_API_KEY etc. from project envs in
// the plan. These keys would leak to the local .env if shipped.
func TestBuildEnvPlan_PlatformInternalsFiltered(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	writeYaml(t, tmpDir, `zerops:
  - setup: app
    run:
      base: nodejs@22
`)

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "test", Status: statusActive}).
		WithProjectEnv([]platform.ProjectEnvVar{
			{ID: "p1", Key: "APP_KEY", Content: "base64:secret"},
			{ID: "p2", Key: "ZCP_API_KEY", Content: "deploy-token-leak"},
			{ID: "p3", Key: "envIsolation", Content: "service"},
			{ID: "p4", Key: "apiCdnUrl", Content: "https://api.cdn.zerops.io"},
		})

	plan, err := BuildEnvPlan(context.Background(), mock, "p1", "app", tmpDir)
	if err != nil {
		t.Fatalf("BuildEnvPlan: %v", err)
	}

	for _, key := range []string{"ZCP_API_KEY", "envIsolation", "apiCdnUrl"} {
		if findKey(plan, key) != nil {
			t.Errorf("plan contains platform-internal key %q (denylist not applied)", key)
		}
	}
	if findKey(plan, "APP_KEY") == nil {
		t.Error("plan should still contain APP_KEY (not on denylist)")
	}

	wantOmitted := []string{"ZCP_API_KEY", "apiCdnUrl", "envIsolation"}
	if len(plan.OmittedPlatformKeys) != len(wantOmitted) {
		t.Fatalf("OmittedPlatformKeys = %v, want %v", plan.OmittedPlatformKeys, wantOmitted)
	}
	for i, want := range wantOmitted {
		if plan.OmittedPlatformKeys[i] != want {
			t.Errorf("OmittedPlatformKeys[%d] = %q, want %q (alphabetical)", i, plan.OmittedPlatformKeys[i], want)
		}
	}
}

// TestBuildEnvPlan_TouchedServiceHostnames pins that the plan reports
// every managed-service hostname the resolver fetched env from during
// ${svc_var} expansion. EnvGenerateDotenv uses this for VPN-probe
// hints and telemetry on referenced services.
func TestBuildEnvPlan_TouchedServiceHostnames(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	writeYaml(t, tmpDir, `zerops:
  - setup: app
    run:
      envVariables:
        DB_HOST: ${db_hostname}
        REDIS_URL: ${cache_url}
`)

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "test", Status: statusActive}).
		WithServices([]platform.ServiceStack{
			{ID: "svc-app", Name: "app", ProjectID: "p1", Status: "RUNNING"},
			{ID: "svc-db", Name: "db", ProjectID: "p1", Status: "RUNNING"},
			{ID: "svc-cache", Name: "cache", ProjectID: "p1", Status: "RUNNING"},
		}).
		WithServiceEnv("svc-db", []platform.ServiceEnvVar{
			{ID: "e1", Key: "hostname", Content: "db"},
		}).
		WithServiceEnv("svc-cache", []platform.ServiceEnvVar{
			{ID: "e2", Key: "url", Content: "redis://cache:6379"},
		})

	plan, err := BuildEnvPlan(context.Background(), mock, "p1", "app", tmpDir)
	if err != nil {
		t.Fatalf("BuildEnvPlan: %v", err)
	}

	want := []string{"cache", "db"}
	if len(plan.TouchedServiceHostnames) != len(want) {
		t.Fatalf("TouchedServiceHostnames = %v, want %v", plan.TouchedServiceHostnames, want)
	}
	for i, hostname := range want {
		if plan.TouchedServiceHostnames[i] != hostname {
			t.Errorf("TouchedServiceHostnames[%d] = %q, want %q (alphabetical)",
				i, plan.TouchedServiceHostnames[i], hostname)
		}
	}
}

// TestBuildEnvPlan_NoTouchedServices_WhenNoRefs pins that
// TouchedServiceHostnames is empty when run.envVariables has no
// cross-service refs (typical for project-only or static-value
// scenarios). VPN probe should be skipped in this case.
func TestBuildEnvPlan_NoTouchedServices_WhenNoRefs(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	writeYaml(t, tmpDir, `zerops:
  - setup: app
    run:
      envVariables:
        APP_ENV: production
`)

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "test", Status: statusActive}).
		WithServices([]platform.ServiceStack{
			{ID: "svc-app", Name: "app", ProjectID: "p1", Status: "RUNNING"},
		})

	plan, err := BuildEnvPlan(context.Background(), mock, "p1", "app", tmpDir)
	if err != nil {
		t.Fatalf("BuildEnvPlan: %v", err)
	}

	if len(plan.TouchedServiceHostnames) != 0 {
		t.Errorf("TouchedServiceHostnames = %v, want empty", plan.TouchedServiceHostnames)
	}
}

// TestBuildEnvPlan_BrownfieldImport_MergesAtCorrectPrecedence pins
// the Theme 3 reservation: when SourceBrownfieldImport is later wired
// in, it must merge between project and yaml-setup. The enum value
// being defined and a placeholder test ensures the contract is
// claimed before brownfield-adopt implementation lands.
func TestBuildEnvPlan_BrownfieldImport_MergesAtCorrectPrecedence(t *testing.T) {
	t.Parallel()
	// Until Theme 3 lands, this test just ensures the EnvSource enum
	// includes SourceBrownfieldImport and its String() works. The
	// precedence rule (project < brownfield-import < yaml-setup <
	// local-overlay) is documented in spec-env-handling.md §4.
	if SourceBrownfieldImport.String() != "brownfield-import" {
		t.Errorf("SourceBrownfieldImport.String() = %q, want %q",
			SourceBrownfieldImport.String(), "brownfield-import")
	}
	// Ordering of enum values must be: project < yaml-setup <
	// local-overlay < brownfield-import (current declaration order
	// in env_plan.go places brownfield-import last as a reserved
	// extension; precedence at merge time is implemented via the
	// merge sequence in BuildEnvPlan, not via enum-int comparison).
	if SourceProject >= SourceYAMLSetup ||
		SourceYAMLSetup >= SourceLocalOverlay ||
		SourceLocalOverlay >= SourceBrownfieldImport {
		t.Error("EnvSource enum ordering changed; review impact on String() and any callers comparing values")
	}
}

// TestEnvPlan_RenderDotenv_FormatStability pins the .env render
// format. Header + KEY=VALUE lines + trailing newline. Stable across
// runs (alphabetical key order from BuildEnvPlan).
func TestEnvPlan_RenderDotenv_FormatStability(t *testing.T) {
	t.Parallel()

	plan := &EnvPlan{
		Setup: "prod",
		CWD:   "/tmp/test",
		Keys: []EnvKey{
			{Key: "ALPHA", Value: "one", Source: SourceProject, Scope: ScopeShared, Conflict: StatusClean},
			{Key: "BETA", Value: "two", Source: SourceYAMLSetup, Scope: ScopeManagedRef, Conflict: StatusClean},
			{Key: "GAMMA", Value: "three", Source: SourceLocalOverlay, Scope: ScopeLocalOverride, Conflict: StatusClean},
		},
	}

	out, err := plan.Render(SinkDotenv)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	got := string(out)

	for _, want := range []string{
		"# Generated by ZCP",
		"setup prod",
		"For local-only overrides, edit .env.local",
		"\nALPHA=one\n",
		"\nBETA=two\n",
		"\nGAMMA=three\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf(".env render missing %q; got:\n%s", want, got)
		}
	}
}

// TestEnvPlan_RenderShellExport_QuotesValues pins that shell-export
// renders values with POSIX single-quote escaping (via shellQuote).
// Values containing apostrophes or shell metacharacters must not
// break the export line.
func TestEnvPlan_RenderShellExport_QuotesValues(t *testing.T) {
	t.Parallel()

	plan := &EnvPlan{
		Setup: "prod",
		Keys: []EnvKey{
			{Key: "PLAIN", Value: "simple"},
			{Key: "WITH_SPACES", Value: "hello world"},
			{Key: "WITH_QUOTE", Value: "it's mine"},
			{Key: "WITH_DOLLAR", Value: "$HOME"},
		},
	}

	out, err := plan.Render(SinkShellExport)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	got := string(out)

	for _, want := range []string{
		"export PLAIN=",
		"export WITH_SPACES=",
		"export WITH_QUOTE=",
		"export WITH_DOLLAR=",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("shell-export missing %q; got:\n%s", want, got)
		}
	}
	// Dollar-sign and apostrophe MUST be quoted, not literal — otherwise
	// `eval` would expand $HOME and break on apostrophe.
	if strings.Contains(got, "export WITH_DOLLAR=$HOME") {
		t.Errorf("shell-export rendered raw $HOME (would expand on eval); got:\n%s", got)
	}
	if strings.Contains(got, "export WITH_QUOTE=it's mine") {
		t.Errorf("shell-export rendered raw apostrophe (would break parsing); got:\n%s", got)
	}
}

// TestEnvPlan_Render_DryRunDiff_RequiresDiffInput pins that
// Render(SinkDryRunDiff) errors because the sink needs a diff (which
// the plan alone cannot produce). Callers must compute the diff via
// DiffAgainstExisting and pass it to RenderDiff. The split keeps
// EnvPlan a pure data type — Render is "format the plan", RenderDiff
// is "format the comparison".
func TestEnvPlan_Render_DryRunDiff_RequiresDiffInput(t *testing.T) {
	t.Parallel()
	plan := &EnvPlan{Setup: "prod"}
	_, err := plan.Render(SinkDryRunDiff)
	if err == nil {
		t.Fatal("Render(SinkDryRunDiff) should error; diff is computed by DiffAgainstExisting")
	}
	if !strings.Contains(err.Error(), "DiffAgainstExisting") {
		t.Errorf("error should steer caller to DiffAgainstExisting, got: %v", err)
	}
}

// TestReadEnvLocal_AbsentFileReturnsEmpty pins that absence of
// .env.local is not an error — overlay simply contributes nothing.
func TestReadEnvLocal_AbsentFileReturnsEmpty(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	got, err := readEnvLocal(tmpDir)
	if err != nil {
		t.Fatalf("readEnvLocal: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("absent file should give empty map, got %d entries", len(got))
	}
}

// TestReadEnvLocal_ParsesFormat pins the permissive KEY=VALUE parse:
// comments and blank lines skipped, raw value preserved (no quote
// stripping), malformed lines silently dropped.
func TestReadEnvLocal_ParsesFormat(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	writeEnvLocal(t, tmpDir, `# comment
APP_ENV=local

LOG_LEVEL=debug
   PADDED_KEY = with-spaces
QUOTED="raw quoted value"
malformed-line-no-equals
=value-with-no-key
`)
	got, err := readEnvLocal(tmpDir)
	if err != nil {
		t.Fatalf("readEnvLocal: %v", err)
	}
	if got["APP_ENV"] != "local" {
		t.Errorf("APP_ENV = %q, want %q", got["APP_ENV"], "local")
	}
	if got["LOG_LEVEL"] != "debug" {
		t.Errorf("LOG_LEVEL = %q, want %q", got["LOG_LEVEL"], "debug")
	}
	if got["PADDED_KEY"] != " with-spaces" {
		t.Errorf("PADDED_KEY = %q, want %q (raw value, leading space preserved)", got["PADDED_KEY"], " with-spaces")
	}
	if got["QUOTED"] != `"raw quoted value"` {
		t.Errorf("QUOTED = %q, want raw with quotes (no stripping)", got["QUOTED"])
	}
	if _, hasMalformed := got["malformed-line-no-equals"]; hasMalformed {
		t.Error("malformed line without = should be skipped")
	}
	if _, hasNoKey := got[""]; hasNoKey {
		t.Error("line with empty key (=value) should be skipped")
	}
}

// TestBuildEnvPlan_BrownfieldOverrides_MergeBetweenProjectAndYAML pins
// the Theme 3 reservation: brownfield-import values land between
// project (low) and yaml-setup (high) precedence, since they
// represent "user's previous truth" — more authoritative than
// project envVariables but shadowed by anything explicitly named in
// the new zerops.yaml.
func TestBuildEnvPlan_BrownfieldOverrides_MergeBetweenProjectAndYAML(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	writeYaml(t, tmpDir, `zerops:
  - setup: app
    run:
      envVariables:
        YAML_WINS: from-yaml
`)

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "test", Status: statusActive}).
		WithServices([]platform.ServiceStack{
			{ID: "svc-app", Name: "app", ProjectID: "p1", Status: "RUNNING"},
		}).
		WithProjectEnv([]platform.ProjectEnvVar{
			{ID: "pe1", Key: "PROJECT_ONLY", Content: "from-project"},
			{ID: "pe2", Key: "BROWNFIELD_OVERRIDES_PROJECT", Content: "project-loses"},
		})

	brownfield := map[string]string{
		"BROWNFIELD_OVERRIDES_PROJECT": "brownfield-wins-over-project",
		"BROWNFIELD_ONLY":              "from-brownfield",
		"YAML_WINS":                    "brownfield-loses-to-yaml",
	}

	plan, err := buildEnvPlanWith(context.Background(), mock, "p1", "app", tmpDir, nil, brownfield)
	if err != nil {
		t.Fatalf("buildEnvPlanWith: %v", err)
	}

	cases := []struct {
		key       string
		wantValue string
		wantSrc   EnvSource
	}{
		{"PROJECT_ONLY", "from-project", SourceProject},
		{"BROWNFIELD_OVERRIDES_PROJECT", "brownfield-wins-over-project", SourceBrownfieldImport},
		{"BROWNFIELD_ONLY", "from-brownfield", SourceBrownfieldImport},
		{"YAML_WINS", "from-yaml", SourceYAMLSetup},
	}
	for _, c := range cases {
		got := findKey(plan, c.key)
		if got == nil {
			t.Errorf("%s missing", c.key)
			continue
		}
		if got.Value != c.wantValue {
			t.Errorf("%s value = %q, want %q", c.key, got.Value, c.wantValue)
		}
		if got.Source != c.wantSrc {
			t.Errorf("%s source = %s, want %s", c.key, got.Source, c.wantSrc)
		}
	}
	// YAML_WINS shadows brownfield (which would have shadowed project) —
	// final conflict status should be Shadowed.
	if got := findKey(plan, "YAML_WINS"); got != nil && got.Conflict != StatusShadowed {
		t.Errorf("YAML_WINS conflict = %s, want %s", got.Conflict, StatusShadowed)
	}
}

// TestRefResolveTransientError_DetectableViaErrorsAs pins that callers
// can detect transient ref-resolution failures via errors.As. This
// is the migration path to "vpn-down" handling at higher layers
// (status check, atom recovery hints).
func TestRefResolveTransientError_DetectableViaErrorsAs(t *testing.T) {
	t.Parallel()
	wrapped := &RefResolveTransientError{
		Service: "db",
		Cause:   errors.New("connection refused"),
	}
	var asTransient *RefResolveTransientError
	if !errors.As(wrapped, &asTransient) {
		t.Fatal("errors.As should detect *RefResolveTransientError")
	}
	if asTransient.Service != "db" {
		t.Errorf("Service = %q, want %q", asTransient.Service, "db")
	}
	if !errors.Is(wrapped, asTransient.Cause) {
		t.Errorf("errors.Is should chain through Unwrap to the wrapped cause")
	}
}
