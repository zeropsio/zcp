package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

var errExpected = errors.New("simulated SSH failure")

// routedSSH dispatches commands by substring match. Each entry's value
// is returned as stdout when its key is found in the command. Use
// `errs` for failure injection. Order matters — the first matching
// substring wins, so register more-specific keys first.
type routedSSH struct {
	responses map[string]string
	errs      map[string]error
}

func (s *routedSSH) ExecSSH(_ context.Context, _ string, command string) ([]byte, error) {
	for k, e := range s.errs {
		if strings.Contains(command, k) {
			return nil, e
		}
	}
	for k, v := range s.responses {
		if strings.Contains(command, k) {
			return []byte(v), nil
		}
	}
	return nil, nil
}

func (s *routedSSH) ExecSSHBackground(_ context.Context, _, _ string, _ time.Duration) ([]byte, error) {
	return nil, nil
}

const exportTestZeropsYAML = `zerops:
  - setup: appdev
    build:
      base: php@8.4
      buildCommands:
        - composer install
      deployFiles: ["./"]
    run:
      base: php-apache@8.4
      envVariables:
        APP_ENV: dev
        DB_HOST: ${db_hostname}
`

func newExportMock(svcs []platform.ServiceStack, projectEnvs []platform.EnvVar) *platform.Mock {
	m := platform.NewMock().
		WithProject(&platform.Project{ID: "proj1", Name: "demo", Status: "ACTIVE"}).
		WithServices(svcs).
		WithProjectEnv(projectEnvs)
	return m
}

func runtimeService(hostname, typeVersion string, subdomain bool) platform.ServiceStack {
	return platform.ServiceStack{
		ID:   "svc-" + hostname,
		Name: hostname,
		ServiceStackTypeInfo: platform.ServiceTypeInfo{
			ServiceStackTypeVersionName:  typeVersion,
			ServiceStackTypeCategoryName: "USER",
		},
		Status:          "ACTIVE",
		Mode:            "NON_HA",
		SubdomainAccess: subdomain,
	}
}

func managedService(hostname, typeVersion string) platform.ServiceStack {
	return platform.ServiceStack{
		ID:   "svc-" + hostname,
		Name: hostname,
		ServiceStackTypeInfo: platform.ServiceTypeInfo{
			ServiceStackTypeVersionName:  typeVersion,
			ServiceStackTypeCategoryName: "DB", // any non-USER category surfaces as infrastructure
		},
		Status: "ACTIVE",
		Mode:   "NON_HA",
	}
}

// writeBootstrappedMeta seeds a ServiceMeta into stateDir for tests
// that exercise the export handler's meta-required code paths. The
// hostname is fixed to "appdev" — the export handler always reads the
// per-hostname meta keyed by the inbound TargetService, and the
// fixture suite only ever exercises that single hostname. Callers
// vary the topology.Mode + GitPushState dimensions instead.
func writeBootstrappedMeta(t *testing.T, dir string, mode topology.Mode, gitPushState topology.GitPushState) {
	t.Helper()
	meta := &workflow.ServiceMeta{
		Hostname:                 "appdev",
		Mode:                     mode,
		BootstrapSession:         "test-session",
		BootstrappedAt:           time.Now().UTC().Format(time.RFC3339),
		FirstDeployedAt:          time.Now().UTC().Format(time.RFC3339),
		CloseDeployMode:          topology.CloseModeManual,
		CloseDeployModeConfirmed: true,
		GitPushState:             gitPushState,
	}
	if err := workflow.WriteServiceMeta(dir, meta); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
}

func decodeExportJSON(t *testing.T, result *mcp.CallToolResult) map[string]any {
	t.Helper()
	text := getTextContent(t, result)
	var doc map[string]any
	if err := json.Unmarshal([]byte(text), &doc); err != nil {
		t.Fatalf("decode export response: %v\nbody=%s", err, text)
	}
	return doc
}

// TestHandleExport_NoTargetService_ReturnsScopePrompt covers Phase A.1:
// agent calls with no targetService and receives the runtime list.
func TestHandleExport_NoTargetService_ReturnsScopePrompt(t *testing.T) {
	t.Parallel()
	mock := newExportMock([]platform.ServiceStack{
		runtimeService("appdev", "php-apache@8.4", false),
		runtimeService("workerdev", "nodejs@22", false),
		managedService("db", "postgresql@16"),
	}, nil)

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, mock, nil, "proj1", nil, nil, nil, nil, t.TempDir(), "", nil, nil, runtime.Info{InContainer: true})

	result := callTool(t, srv, "zerops_workflow", map[string]any{"workflow": "export"})
	if result.IsError {
		t.Fatalf("scope prompt should not error, got: %s", getTextContent(t, result))
	}

	body := decodeExportJSON(t, result)
	if body["status"] != "scope-prompt" {
		t.Errorf("expected status=scope-prompt, got %v", body["status"])
	}
	runtimes, _ := body["runtimes"].([]any)
	if len(runtimes) != 2 {
		t.Errorf("expected 2 runtime hostnames, got %v", runtimes)
	}
	for _, want := range []string{"appdev", "workerdev"} {
		found := false
		for _, r := range runtimes {
			if r == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("scope prompt missing runtime %q in %v", want, runtimes)
		}
	}
}

// TestHandleExport_PairMode_VariantUnset_ReturnsVariantPrompt covers
// Phase A.2: ModeStandard pair without Variant returns the dev/stage
// prompt.
func TestHandleExport_PairMode_VariantUnset_ReturnsVariantPrompt(t *testing.T) {
	t.Parallel()
	mock := newExportMock([]platform.ServiceStack{runtimeService("appdev", "php-apache@8.4", false)}, nil)

	dir := t.TempDir()
	writeBootstrappedMeta(t, dir, topology.ModeStandard, topology.GitPushUnconfigured)

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, mock, nil, "proj1", nil, nil, nil, nil, dir, "", nil, nil, runtime.Info{InContainer: true})

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"workflow":      "export",
		"targetService": "appdev",
	})
	if result.IsError {
		t.Fatalf("variant prompt should not error, got: %s", getTextContent(t, result))
	}

	body := decodeExportJSON(t, result)
	if body["status"] != "variant-prompt" {
		t.Errorf("expected status=variant-prompt, got %v", body["status"])
	}
	if body["targetService"] != "appdev" {
		t.Errorf("expected targetService=appdev, got %v", body["targetService"])
	}
	options, _ := body["options"].([]any)
	if len(options) != 2 {
		t.Errorf("expected 2 variant options (dev/stage), got %v", options)
	}
}

// TestHandleExport_SimpleMode_SkipsVariantPrompt verifies the
// single-half source-mode path: ModeSimple does not solicit a variant
// since there is no pair half to choose between. The handler proceeds
// straight to Phase A's SSH probe.
func TestHandleExport_SimpleMode_SkipsVariantPrompt(t *testing.T) {
	t.Parallel()
	mock := newExportMock(
		[]platform.ServiceStack{runtimeService("appdev", "nodejs@22", false)},
		[]platform.EnvVar{{Key: "LOG_LEVEL", Content: "info"}},
	)

	dir := t.TempDir()
	writeBootstrappedMeta(t, dir, topology.ModeSimple, topology.GitPushConfigured)

	ssh := &routedSSH{responses: map[string]string{
		"cat /var/www/zerops.yaml": exportTestZeropsYAML,
		"git remote get-url":       "https://github.com/example/simple.git",
	}}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, mock, nil, "proj1", nil, nil, nil, nil, dir, "", nil, ssh, runtime.Info{InContainer: true})

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"workflow":      "export",
		"targetService": "appdev",
	})
	if result.IsError {
		t.Fatalf("simple-mode export should not error, got: %s", getTextContent(t, result))
	}

	body := decodeExportJSON(t, result)
	if body["status"] == "variant-prompt" {
		t.Errorf("ModeSimple should skip variant prompt, got status=%v", body["status"])
	}
	if body["status"] != "classify-prompt" {
		t.Errorf("expected classify-prompt for ModeSimple with envs present, got %v", body["status"])
	}
}

// TestHandleExport_VariantStageOnDevHostname_Errors confirms the dev/
// stage hostname/variant mismatch is caught early.
func TestHandleExport_VariantStageOnDevHostname_Errors(t *testing.T) {
	t.Parallel()
	mock := newExportMock([]platform.ServiceStack{runtimeService("appdev", "php-apache@8.4", false)}, nil)

	dir := t.TempDir()
	writeBootstrappedMeta(t, dir, topology.ModeStandard, topology.GitPushUnconfigured)

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, mock, nil, "proj1", nil, nil, nil, nil, dir, "", nil, nil, runtime.Info{InContainer: true})

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"workflow":      "export",
		"targetService": "appdev",
		"variant":       "stage",
	})
	if !result.IsError {
		t.Fatalf("expected mismatch error, got: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "dev half") {
		t.Errorf("expected dev/stage mismatch hint, got: %s", text)
	}
}

// TestHandleExport_MissingZeropsYaml_ChainsToScaffold pins Q5:
// empty /var/www/zerops.yaml routes the agent to scaffold-zerops-yaml.
func TestHandleExport_MissingZeropsYaml_ChainsToScaffold(t *testing.T) {
	t.Parallel()
	mock := newExportMock([]platform.ServiceStack{runtimeService("appdev", "php-apache@8.4", false)}, nil)

	dir := t.TempDir()
	writeBootstrappedMeta(t, dir, topology.ModeStandard, topology.GitPushUnconfigured)

	ssh := &routedSSH{responses: map[string]string{
		"cat /var/www/zerops.yaml": "", // empty body
	}}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, mock, nil, "proj1", nil, nil, nil, nil, dir, "", nil, ssh, runtime.Info{InContainer: true})

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"workflow":      "export",
		"targetService": "appdev",
		"variant":       "dev",
	})
	if result.IsError {
		t.Fatalf("scaffold chain should not error, got: %s", getTextContent(t, result))
	}

	body := decodeExportJSON(t, result)
	if body["status"] != "scaffold-required" {
		t.Errorf("expected status=scaffold-required, got %v", body["status"])
	}
}

// TestHandleExport_MissingGitRemote_ChainsToGitPushSetup pins the
// no-remote branch — handler chains to git-push-setup before BuildBundle
// runs (avoids a pointless empty-remote composition error).
func TestHandleExport_MissingGitRemote_ChainsToGitPushSetup(t *testing.T) {
	t.Parallel()
	mock := newExportMock([]platform.ServiceStack{runtimeService("appdev", "php-apache@8.4", false)}, nil)

	dir := t.TempDir()
	writeBootstrappedMeta(t, dir, topology.ModeStandard, topology.GitPushUnconfigured)

	ssh := &routedSSH{responses: map[string]string{
		"cat /var/www/zerops.yaml": exportTestZeropsYAML,
		"git remote get-url":       "", // no remote
	}}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, mock, nil, "proj1", nil, nil, nil, nil, dir, "", nil, ssh, runtime.Info{InContainer: true})

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"workflow":      "export",
		"targetService": "appdev",
		"variant":       "dev",
	})
	if result.IsError {
		t.Fatalf("missing-remote chain should not error, got: %s", getTextContent(t, result))
	}

	body := decodeExportJSON(t, result)
	if body["status"] != "git-push-setup-required" {
		t.Errorf("expected status=git-push-setup-required, got %v", body["status"])
	}
	steps, _ := body["nextSteps"].([]any)
	if len(steps) == 0 {
		t.Error("expected nextSteps with git-push-setup pointer")
	}
}

// TestHandleExport_ClassifyPrompt covers Phase B.2: project envs
// present + EnvClassifications empty → preview + classify-prompt.
func TestHandleExport_ClassifyPrompt(t *testing.T) {
	t.Parallel()
	mock := newExportMock(
		[]platform.ServiceStack{runtimeService("appdev", "php-apache@8.4", false)},
		[]platform.EnvVar{
			{Key: "APP_KEY", Content: "old-key"},
			{Key: "DB_HOST", Content: "${db_hostname}"},
		},
	)

	dir := t.TempDir()
	writeBootstrappedMeta(t, dir, topology.ModeStandard, topology.GitPushConfigured)

	ssh := &routedSSH{responses: map[string]string{
		"cat /var/www/zerops.yaml": exportTestZeropsYAML,
		"git remote get-url":       "https://github.com/example/demo.git",
	}}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, mock, nil, "proj1", nil, nil, nil, nil, dir, "", nil, ssh, runtime.Info{InContainer: true})

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"workflow":      "export",
		"targetService": "appdev",
		"variant":       "dev",
	})
	if result.IsError {
		t.Fatalf("classify prompt should not error, got: %s", getTextContent(t, result))
	}

	body := decodeExportJSON(t, result)
	if body["status"] != "classify-prompt" {
		t.Errorf("expected status=classify-prompt, got %v", body["status"])
	}
	rows, _ := body["envClassificationTable"].([]any)
	if len(rows) != 2 {
		t.Fatalf("expected 2 env classification rows, got %d", len(rows))
	}
	preview, _ := body["preview"].(map[string]any)
	if preview["importYaml"] == "" {
		t.Error("preview.importYaml should be populated")
	}
}

// TestHandleExport_GitPushUnconfigured_ChainsAfterClassify confirms the
// chain fires AFTER classifications are accepted (so the agent has
// already seen the preview / chosen buckets) when meta.GitPushState
// has not been provisioned.
func TestHandleExport_GitPushUnconfigured_ChainsAfterClassify(t *testing.T) {
	t.Parallel()
	mock := newExportMock(
		[]platform.ServiceStack{runtimeService("appdev", "php-apache@8.4", false)},
		[]platform.EnvVar{{Key: "LOG_LEVEL", Content: "info"}},
	)

	dir := t.TempDir()
	writeBootstrappedMeta(t, dir, topology.ModeStandard, topology.GitPushUnconfigured)

	ssh := &routedSSH{responses: map[string]string{
		"cat /var/www/zerops.yaml": exportTestZeropsYAML,
		"git remote get-url":       "https://github.com/example/demo.git",
	}}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, mock, nil, "proj1", nil, nil, nil, nil, dir, "", nil, ssh, runtime.Info{InContainer: true})

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"workflow":           "export",
		"targetService":      "appdev",
		"variant":            "dev",
		"envClassifications": map[string]any{"LOG_LEVEL": "plain-config"},
	})
	if result.IsError {
		t.Fatalf("git-push-setup chain should not error, got: %s", getTextContent(t, result))
	}

	body := decodeExportJSON(t, result)
	if body["status"] != "git-push-setup-required" {
		t.Errorf("expected status=git-push-setup-required, got %v", body["status"])
	}
	preview, _ := body["preview"].(map[string]any)
	if preview == nil {
		t.Error("preview should be present so the agent can review while resolving prereq")
	}
}

// TestHandleExport_PublishReady covers the happy path: variant resolved,
// classifications accepted, GitPushState=configured → publish-ready
// response with bundle + write/commit/push instructions.
func TestHandleExport_PublishReady(t *testing.T) {
	t.Parallel()
	mock := newExportMock(
		[]platform.ServiceStack{
			runtimeService("appdev", "php-apache@8.4", true), // subdomain enabled
			managedService("db", "postgresql@16"),
		},
		[]platform.EnvVar{
			{Key: "APP_KEY", Content: "old-key"},
			{Key: "DB_HOST", Content: "${db_hostname}"},
			{Key: "LOG_LEVEL", Content: "info"},
		},
	)

	dir := t.TempDir()
	writeBootstrappedMeta(t, dir, topology.ModeStandard, topology.GitPushConfigured)

	ssh := &routedSSH{responses: map[string]string{
		"cat /var/www/zerops.yaml": exportTestZeropsYAML,
		"git remote get-url":       "https://github.com/example/demo.git",
	}}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, mock, nil, "proj1", nil, nil, nil, nil, dir, "", nil, ssh, runtime.Info{InContainer: true})

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"workflow":      "export",
		"targetService": "appdev",
		"variant":       "dev",
		"envClassifications": map[string]any{
			"APP_KEY":   "auto-secret",
			"DB_HOST":   "infrastructure",
			"LOG_LEVEL": "plain-config",
		},
	})
	if result.IsError {
		t.Fatalf("publish-ready should not error, got: %s", getTextContent(t, result))
	}

	body := decodeExportJSON(t, result)
	if body["status"] != "publish-ready" {
		t.Errorf("expected status=publish-ready, got %v", body["status"])
	}
	if body["targetService"] != "appdev" {
		t.Errorf("targetService = %v, want appdev", body["targetService"])
	}
	bundle, _ := body["bundle"].(map[string]any)
	if bundle == nil {
		t.Fatal("bundle should be present")
	}
	importYaml, _ := bundle["importYaml"].(string)
	if !strings.Contains(importYaml, "buildFromGit") {
		t.Errorf("importYaml missing buildFromGit, got: %s", importYaml)
	}
	if !strings.Contains(importYaml, "<@generateRandomString") {
		t.Errorf("importYaml missing auto-secret directive, got: %s", importYaml)
	}
	steps, _ := body["nextSteps"].([]any)
	if len(steps) < 3 {
		t.Errorf("expected at least 3 next steps (write yaml, commit, deploy), got %d", len(steps))
	}
	// Ensure the deploy step is included.
	hasDeploy := false
	for _, s := range steps {
		if str, ok := s.(string); ok && strings.Contains(str, "zerops_deploy") {
			hasDeploy = true
			break
		}
	}
	if !hasDeploy {
		t.Error("publish-ready nextSteps should include zerops_deploy strategy=git-push")
	}
}

// TestHandleExport_NilSSH_Errors verifies handler rejects when SSH is
// unavailable (CLI mode without container access).
func TestHandleExport_NilSSH_Errors(t *testing.T) {
	t.Parallel()
	mock := newExportMock([]platform.ServiceStack{runtimeService("appdev", "php-apache@8.4", false)}, nil)

	dir := t.TempDir()
	writeBootstrappedMeta(t, dir, topology.ModeStandard, topology.GitPushUnconfigured)

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, mock, nil, "proj1", nil, nil, nil, nil, dir, "", nil, nil, runtime.Info{InContainer: true})

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"workflow":      "export",
		"targetService": "appdev",
		"variant":       "dev",
	})
	if !result.IsError {
		t.Fatalf("nil SSH should error, got: %s", getTextContent(t, result))
	}
	if !strings.Contains(getTextContent(t, result), "SSH access unavailable") {
		t.Errorf("expected SSH-unavailable hint, got: %s", getTextContent(t, result))
	}
}

// TestHandleExport_UnbootstrappedService_Errors verifies the meta gate.
func TestHandleExport_UnbootstrappedService_Errors(t *testing.T) {
	t.Parallel()
	mock := newExportMock([]platform.ServiceStack{runtimeService("appdev", "php-apache@8.4", false)}, nil)

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	// stateDir is empty → no ServiceMeta exists for appdev
	RegisterWorkflow(srv, mock, nil, "proj1", nil, nil, nil, nil, t.TempDir(), "", nil, nil, runtime.Info{InContainer: true})

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"workflow":      "export",
		"targetService": "appdev",
	})
	if !result.IsError {
		t.Fatalf("unbootstrapped service should error, got: %s", getTextContent(t, result))
	}
	if !strings.Contains(getTextContent(t, result), "no bootstrapped meta") {
		t.Errorf("expected no-meta hint, got: %s", getTextContent(t, result))
	}
}

// TestHandleExport_ManagedServiceTarget_Errors verifies the runtime-only
// gate: managed services cannot be export targets.
func TestHandleExport_ManagedServiceTarget_Errors(t *testing.T) {
	t.Parallel()
	mock := newExportMock([]platform.ServiceStack{managedService("db", "postgresql@16")}, nil)

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, mock, nil, "proj1", nil, nil, nil, nil, t.TempDir(), "", nil, nil, runtime.Info{InContainer: true})

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"workflow":      "export",
		"targetService": "db",
	})
	if !result.IsError {
		t.Fatalf("managed-service target should error, got: %s", getTextContent(t, result))
	}
	if !strings.Contains(getTextContent(t, result), "managed service") {
		t.Errorf("expected managed-service hint, got: %s", getTextContent(t, result))
	}
}

// TestHandleExport_ModeStage_VariantUnset_ReturnsVariantPrompt covers
// the stage-half pair branch (`ModeStage`). Per Codex Phase 3 POST-WORK
// Amendment 4: the dev-half ModeStandard branch was tested but the
// stage-half branch was uncovered.
func TestHandleExport_ModeStage_VariantUnset_ReturnsVariantPrompt(t *testing.T) {
	t.Parallel()
	mock := newExportMock([]platform.ServiceStack{runtimeService("appdev", "php-apache@8.4", false)}, nil)

	dir := t.TempDir()
	writeBootstrappedMeta(t, dir, topology.ModeStage, topology.GitPushUnconfigured)

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, mock, nil, "proj1", nil, nil, nil, nil, dir, "", nil, nil, runtime.Info{InContainer: true})

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"workflow":      "export",
		"targetService": "appdev",
	})
	if result.IsError {
		t.Fatalf("ModeStage variant prompt should not error, got: %s", getTextContent(t, result))
	}

	body := decodeExportJSON(t, result)
	if body["status"] != "variant-prompt" {
		t.Errorf("expected status=variant-prompt for ModeStage, got %v", body["status"])
	}
}

// TestHandleExport_ModeLocalStage_VariantUnset_ReturnsVariantPrompt
// covers the ModeLocalStage pair branch — local CWD paired with a
// Zerops stage half. Per Codex Phase 3 POST-WORK Amendment 4.
func TestHandleExport_ModeLocalStage_VariantUnset_ReturnsVariantPrompt(t *testing.T) {
	t.Parallel()
	mock := newExportMock([]platform.ServiceStack{runtimeService("appdev", "nodejs@22", false)}, nil)

	dir := t.TempDir()
	writeBootstrappedMeta(t, dir, topology.ModeLocalStage, topology.GitPushUnconfigured)

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, mock, nil, "proj1", nil, nil, nil, nil, dir, "", nil, nil, runtime.Info{InContainer: true})

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"workflow":      "export",
		"targetService": "appdev",
	})
	if result.IsError {
		t.Fatalf("ModeLocalStage variant prompt should not error, got: %s", getTextContent(t, result))
	}

	body := decodeExportJSON(t, result)
	if body["status"] != "variant-prompt" {
		t.Errorf("expected status=variant-prompt for ModeLocalStage, got %v", body["status"])
	}
}

// TestHandleExport_PartialClassifications_RePromptsClassify covers the
// Codex Phase 3 POST-WORK Amendment 3 fix: when EnvClassifications is
// non-empty but missing some project envs, the handler MUST re-prompt
// (it previously skipped the prompt for any non-empty map).
func TestHandleExport_PartialClassifications_RePromptsClassify(t *testing.T) {
	t.Parallel()
	mock := newExportMock(
		[]platform.ServiceStack{runtimeService("appdev", "php-apache@8.4", false)},
		[]platform.EnvVar{
			{Key: "APP_KEY", Content: "old"},
			{Key: "DB_HOST", Content: "${db_hostname}"},
			{Key: "LOG_LEVEL", Content: "info"},
		},
	)

	dir := t.TempDir()
	writeBootstrappedMeta(t, dir, topology.ModeStandard, topology.GitPushConfigured)

	ssh := &routedSSH{responses: map[string]string{
		"cat /var/www/zerops.yaml": exportTestZeropsYAML,
		"git remote get-url":       "https://github.com/example/demo.git",
	}}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, mock, nil, "proj1", nil, nil, nil, nil, dir, "", nil, ssh, runtime.Info{InContainer: true})

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"workflow":      "export",
		"targetService": "appdev",
		"variant":       "dev",
		"envClassifications": map[string]any{
			"APP_KEY": "auto-secret",
			// DB_HOST and LOG_LEVEL intentionally omitted
		},
	})
	if result.IsError {
		t.Fatalf("partial-classifications path should not error, got: %s", getTextContent(t, result))
	}

	body := decodeExportJSON(t, result)
	if body["status"] != "classify-prompt" {
		t.Errorf("expected status=classify-prompt for partial classifications, got %v", body["status"])
	}
}

// TestHandleExport_ExtraClassificationKeys_NoSuppress pins the policy:
// classifications keys that don't map to any project env are
// informational — they don't affect prompt suppression. Per Codex
// Phase 3 POST-WORK Amendment 6.
func TestHandleExport_ExtraClassificationKeys_NoSuppress(t *testing.T) {
	t.Parallel()
	mock := newExportMock(
		[]platform.ServiceStack{runtimeService("appdev", "php-apache@8.4", false)},
		[]platform.EnvVar{{Key: "LOG_LEVEL", Content: "info"}},
	)

	dir := t.TempDir()
	writeBootstrappedMeta(t, dir, topology.ModeStandard, topology.GitPushConfigured)

	ssh := &routedSSH{responses: map[string]string{
		"cat /var/www/zerops.yaml": exportTestZeropsYAML,
		"git remote get-url":       "https://github.com/example/demo.git",
	}}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, mock, nil, "proj1", nil, nil, nil, nil, dir, "", nil, ssh, runtime.Info{InContainer: true})

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"workflow":      "export",
		"targetService": "appdev",
		"variant":       "dev",
		"envClassifications": map[string]any{
			"LOG_LEVEL":    "plain-config",
			"GHOST_VAR":    "plain-config", // not in projectEnvs — informational
			"ANOTHER_GONE": "auto-secret",  // not in projectEnvs — informational
		},
	})
	if result.IsError {
		t.Fatalf("extra-keys map should not error, got: %s", getTextContent(t, result))
	}

	body := decodeExportJSON(t, result)
	if body["status"] != "publish-ready" {
		t.Errorf("expected publish-ready (LOG_LEVEL covered, extras ignored), got %v", body["status"])
	}
}

// TestHandleExport_SSHReadError_Propagates pins SSH error propagation.
// Per Codex Phase 3 POST-WORK Amendment 5: prior tests exercised only
// happy-path SSH responses; ExecSSH failures (network drop, container
// gone) need explicit coverage.
func TestHandleExport_SSHReadError_Propagates(t *testing.T) {
	t.Parallel()
	mock := newExportMock([]platform.ServiceStack{runtimeService("appdev", "php-apache@8.4", false)}, nil)

	dir := t.TempDir()
	writeBootstrappedMeta(t, dir, topology.ModeStandard, topology.GitPushUnconfigured)

	ssh := &routedSSH{errs: map[string]error{
		"cat /var/www/zerops.yaml": errExpected,
	}}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, mock, nil, "proj1", nil, nil, nil, nil, dir, "", nil, ssh, runtime.Info{InContainer: true})

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"workflow":      "export",
		"targetService": "appdev",
		"variant":       "dev",
	})
	if !result.IsError {
		t.Fatalf("SSH read error must propagate, got: %s", getTextContent(t, result))
	}
	if !strings.Contains(getTextContent(t, result), "Read zerops.yaml") {
		t.Errorf("expected SSH error remediation hint, got: %s", getTextContent(t, result))
	}
}

// TestHandleExport_ClassifyPromptDoesNotLeakValues pins the redaction
// invariant per plan §14.2 #2 + Codex Phase 3 POST-WORK Blocker 1.
// The classify-prompt response MUST NOT contain raw env values
// inline — neither in the rendered import.yaml nor in the per-env
// table. Live zerops.yaml is allowed (no rendered secrets).
func TestHandleExport_ClassifyPromptDoesNotLeakValues(t *testing.T) {
	t.Parallel()
	const sentinelValue = "S3CRET_SENTINEL_VALUE_DO_NOT_LEAK"
	mock := newExportMock(
		[]platform.ServiceStack{runtimeService("appdev", "php-apache@8.4", false)},
		[]platform.EnvVar{{Key: "APP_KEY", Content: sentinelValue}},
	)

	dir := t.TempDir()
	writeBootstrappedMeta(t, dir, topology.ModeStandard, topology.GitPushConfigured)

	ssh := &routedSSH{responses: map[string]string{
		"cat /var/www/zerops.yaml": exportTestZeropsYAML,
		"git remote get-url":       "https://github.com/example/demo.git",
	}}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, mock, nil, "proj1", nil, nil, nil, nil, dir, "", nil, ssh, runtime.Info{InContainer: true})

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"workflow":      "export",
		"targetService": "appdev",
		"variant":       "dev",
	})
	if result.IsError {
		t.Fatalf("classify-prompt should not error, got: %s", getTextContent(t, result))
	}

	text := getTextContent(t, result)
	if strings.Contains(text, sentinelValue) {
		t.Errorf("classify-prompt response leaked raw env value %q in body: %s", sentinelValue, text)
	}

	body := decodeExportJSON(t, result)
	if body["status"] != "classify-prompt" {
		t.Fatalf("expected status=classify-prompt, got %v", body["status"])
	}
	rows, _ := body["envClassificationTable"].([]any)
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	row, _ := rows[0].(map[string]any)
	if _, hasValue := row["value"]; hasValue {
		t.Error("classify-prompt rows must NOT include the raw env value field")
	}
}

// TestNeedsClassifyPrompt covers the partial-classification logic
// directly. Per Codex Phase 3 POST-WORK Amendment 3: the original
// implementation treated any non-empty map as fully classified.
func TestNeedsClassifyPrompt(t *testing.T) {
	t.Parallel()
	envs := []ops.ProjectEnvVar{
		{Key: "APP_KEY", Value: "x"},
		{Key: "DB_HOST", Value: "y"},
	}
	tests := []struct {
		name string
		in   map[string]string
		envs []ops.ProjectEnvVar
		want bool
	}{
		{"empty input + envs → prompt", nil, envs, true},
		{"all classified → no prompt", map[string]string{"APP_KEY": "auto-secret", "DB_HOST": "infrastructure"}, envs, false},
		{"partial classified → prompt", map[string]string{"APP_KEY": "auto-secret"}, envs, true},
		{"empty envs → no prompt regardless", map[string]string{"APP_KEY": "auto-secret"}, nil, false},
		{"extra unmapped keys + all envs covered → no prompt", map[string]string{"APP_KEY": "auto-secret", "DB_HOST": "infrastructure", "GHOST": "plain-config"}, envs, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := needsClassifyPrompt(tt.in, tt.envs)
			if got != tt.want {
				t.Errorf("needsClassifyPrompt = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestResolveExportVariant covers the variant-resolution truth table
// directly. Per Codex Phase 3 POST-WORK test-layer recommendation: a
// pure-helper unit test catches mode/variant combinations the MCP
// round-trip suite would only exercise indirectly.
func TestResolveExportVariant(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		mode        topology.Mode
		variant     string
		wantVariant topology.ExportVariant
		wantPrompt  bool
		wantError   bool
	}{
		{"ModeStandard + variant=dev → pass", topology.ModeStandard, "dev", topology.ExportVariantDev, false, false},
		{"ModeStandard + variant=stage → mismatch error", topology.ModeStandard, "stage", topology.ExportVariantUnset, false, true},
		{"ModeStandard + variant unset → prompt", topology.ModeStandard, "", topology.ExportVariantUnset, true, false},
		{"ModeStage + variant=stage → pass", topology.ModeStage, "stage", topology.ExportVariantStage, false, false},
		{"ModeStage + variant=dev → mismatch error", topology.ModeStage, "dev", topology.ExportVariantUnset, false, true},
		{"ModeStage + variant unset → prompt", topology.ModeStage, "", topology.ExportVariantUnset, true, false},
		{"ModeLocalStage + variant=dev → pass", topology.ModeLocalStage, "dev", topology.ExportVariantDev, false, false},
		{"ModeLocalStage + variant unset → prompt", topology.ModeLocalStage, "", topology.ExportVariantUnset, true, false},
		{"ModeDev → forced unset (no prompt)", topology.ModeDev, "", topology.ExportVariantUnset, false, false},
		{"ModeSimple → forced unset (no prompt)", topology.ModeSimple, "", topology.ExportVariantUnset, false, false},
		{"ModeLocalOnly → forced unset (no prompt)", topology.ModeLocalOnly, "", topology.ExportVariantUnset, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, prompt := resolveExportVariant(WorkflowInput{TargetService: "appdev", Variant: tt.variant}, tt.mode)
			if got != tt.wantVariant {
				t.Errorf("variant = %q, want %q", got, tt.wantVariant)
			}
			gotPrompt := prompt != nil && !prompt.IsError
			gotError := prompt != nil && prompt.IsError
			if gotPrompt != tt.wantPrompt {
				t.Errorf("prompt = %v, want %v", gotPrompt, tt.wantPrompt)
			}
			if gotError != tt.wantError {
				t.Errorf("error = %v, want %v", gotError, tt.wantError)
			}
		})
	}
}

// TestHandleExport_ValidationFailed covers the Phase 5 validation gate:
// when bundle.Errors is non-empty the handler MUST return
// status="validation-failed" instead of publish-ready, and the response
// MUST surface the schema errors via the response payload's `errors`
// field. Per Codex Phase 5 POST-WORK amendment 3.
//
// Trigger: bundled `zerops.yaml` is missing the `setup:` key in one of
// its zerops list entries — schema enforces `setup` as required, so the
// validator surfaces a leaf error pointing at the missing field. The
// setup name `appdev` is still present elsewhere so the handler's pick
// logic resolves; the schema-validate step catches the structural gap.
func TestHandleExport_ValidationFailed(t *testing.T) {
	t.Parallel()
	const invalidZeropsYAML = `zerops:
  - setup: appdev
    build:
      base: php@8.4
    run:
      base: php-apache@8.4
  - run:
      base: nodejs@22
`
	mock := newExportMock(
		[]platform.ServiceStack{runtimeService("appdev", "php-apache@8.4", false)},
		[]platform.EnvVar{{Key: "LOG_LEVEL", Content: "info"}},
	)

	dir := t.TempDir()
	writeBootstrappedMeta(t, dir, topology.ModeStandard, topology.GitPushConfigured)

	ssh := &routedSSH{responses: map[string]string{
		"cat /var/www/zerops.yaml": invalidZeropsYAML,
		"git remote get-url":       "https://github.com/example/demo.git",
	}}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, mock, nil, "proj1", nil, nil, nil, nil, dir, "", nil, ssh, runtime.Info{InContainer: true})

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"workflow":      "export",
		"targetService": "appdev",
		"variant":       "dev",
		"envClassifications": map[string]any{
			"LOG_LEVEL": "plain-config",
		},
	})
	if result.IsError {
		t.Fatalf("validation-failed response should not error, got: %s", getTextContent(t, result))
	}

	body := decodeExportJSON(t, result)
	if body["status"] != "validation-failed" {
		t.Errorf("expected status=validation-failed, got %v", body["status"])
	}

	errs, _ := body["errors"].([]any)
	if len(errs) == 0 {
		t.Fatalf("expected errors slice populated, got %v", body["errors"])
	}
	preview, _ := body["preview"].(map[string]any)
	if preview == nil {
		t.Error("preview should be present alongside errors so the agent can review")
	}
	previewErrors, _ := preview["errors"].([]any)
	if len(previewErrors) == 0 {
		t.Error("preview.errors should mirror the top-level errors field")
	}
	steps, _ := body["nextSteps"].([]any)
	if len(steps) == 0 {
		t.Error("validation-failed should carry actionable nextSteps")
	}
}

// TestHandleExport_ValidationOutranksGitPushSetup covers the Phase 5
// branch-order amendment: when both validation errors AND missing
// GitPushState fire, validation-failed wins. The git-push-setup chain
// would otherwise mask a structural bundle problem the agent must fix
// before publish becomes meaningful.
func TestHandleExport_ValidationOutranksGitPushSetup(t *testing.T) {
	t.Parallel()
	const invalidZeropsYAML = `zerops:
  - setup: appdev
    build:
      base: php@8.4
    run:
      base: php-apache@8.4
  - run:
      base: nodejs@22
`
	mock := newExportMock(
		[]platform.ServiceStack{runtimeService("appdev", "php-apache@8.4", false)},
		[]platform.EnvVar{{Key: "LOG_LEVEL", Content: "info"}},
	)

	dir := t.TempDir()
	// GitPushUnconfigured AND validation errors present → validation
	// must still win.
	writeBootstrappedMeta(t, dir, topology.ModeStandard, topology.GitPushUnconfigured)

	ssh := &routedSSH{responses: map[string]string{
		"cat /var/www/zerops.yaml": invalidZeropsYAML,
		"git remote get-url":       "https://github.com/example/demo.git",
	}}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, mock, nil, "proj1", nil, nil, nil, nil, dir, "", nil, ssh, runtime.Info{InContainer: true})

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"workflow":      "export",
		"targetService": "appdev",
		"variant":       "dev",
		"envClassifications": map[string]any{
			"LOG_LEVEL": "plain-config",
		},
	})
	if result.IsError {
		t.Fatalf("validation-outranks should not error, got: %s", getTextContent(t, result))
	}

	body := decodeExportJSON(t, result)
	if body["status"] != "validation-failed" {
		t.Errorf("expected validation-failed (outranks git-push-setup-required), got %v", body["status"])
	}
}

// TestPickSetupName covers the setup-resolution heuristic across hostname/
// mode combinations and edge cases (missing zerops, malformed yaml, no
// match with multiple options).
func TestPickSetupName(t *testing.T) {
	t.Parallel()
	const twoSetupYAML = `zerops:
  - setup: appdev
    run: { base: php-apache@8.4 }
  - setup: appprod
    run: { base: php-apache@8.4 }
`
	tests := []struct {
		name     string
		body     string
		hostname string
		mode     topology.Mode
		want     string
		errSub   string
	}{
		{
			name:     "exact hostname match",
			body:     twoSetupYAML,
			hostname: "appdev",
			mode:     topology.ModeStandard,
			want:     "appdev",
		},
		{
			name:     "stage hostname → appprod via prefix-strip then suffix",
			body:     twoSetupYAML,
			hostname: "appstage",
			mode:     topology.ModeStage,
			want:     "appprod",
		},
		{
			name:     "single setup → fallback regardless of hostname",
			body:     `zerops:` + "\n  - setup: only\n    run: { base: nodejs@22 }\n",
			hostname: "anyhost",
			mode:     topology.ModeSimple,
			want:     "only",
		},
		{
			name:     "no match, multiple setups → error listing options",
			body:     twoSetupYAML,
			hostname: "ghost",
			mode:     topology.ModeSimple,
			errSub:   "Cannot resolve",
		},
		{
			name:     "malformed yaml errors with parse hint",
			body:     "zerops:\n  - setup: x\n  bad indent",
			hostname: "x",
			mode:     topology.ModeSimple,
			errSub:   "Parse zerops.yaml",
		},
		{
			name:     "no zerops list → no-setup-blocks error",
			body:     "name: nope\n",
			hostname: "x",
			mode:     topology.ModeSimple,
			errSub:   "no setup blocks",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := pickSetupName(tt.body, tt.hostname, tt.mode)
			if tt.errSub != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got %q", tt.errSub, got)
				}
				if !strings.Contains(err.Error(), tt.errSub) {
					t.Errorf("err %q missing %q", err.Error(), tt.errSub)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got != tt.want {
				t.Errorf("pickSetupName = %q, want %q", got, tt.want)
			}
		})
	}
}
