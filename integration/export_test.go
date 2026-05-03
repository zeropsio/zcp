// Tests for: integration — export-buildFromGit multi-call flow via full MCP server with mock backend + routed SSH stub.
//
// Phase 7 of the export-buildFromGit plan: confirm the three-call
// narrowing (probe → classify → publish) flows end-to-end through the
// MCP transport, mock platform.Client, and a substring-routed SSH
// deployer. The unit tests under `internal/tools/` already cover each
// branch in isolation; this integration test pins the JSON wire shape
// + serialization round-trip + tool dispatch for the canonical
// happy-path sequence.

package integration_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/server"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// exportSSHStub is the integration-layer SSH router: substring match on
// command → stdout. Mirrors the unit-test routedSSH but lives in the
// integration package so the export test doesn't pull tools-internal
// types.
type exportSSHStub struct {
	responses map[string]string
}

func (s *exportSSHStub) ExecSSH(_ context.Context, _ string, command string) ([]byte, error) {
	for k, v := range s.responses {
		if strings.Contains(command, k) {
			return []byte(v), nil
		}
	}
	return nil, nil
}

func (s *exportSSHStub) ExecSSHBackground(_ context.Context, _, _ string, _ time.Duration) ([]byte, error) {
	return nil, nil
}

const exportIntegrationZeropsYAML = `zerops:
  - setup: appdev
    build:
      base: nodejs@22
      buildCommands:
        - npm install
        - npm run build
      deployFiles: ["./"]
    run:
      base: nodejs@22
      ports:
        - port: 3000
          httpSupport: true
      envVariables:
        LOG_LEVEL: ${LOG_LEVEL}
`

// TestExportFlow_MultiCallThroughServer exercises Phase 1+2+3+4+5+6
// land-in-line: scope-prompt → variant-prompt → classify-prompt →
// publish-ready, all through the full MCP transport.
func TestExportFlow_MultiCallThroughServer(t *testing.T) {
	// Cannot t.Parallel — t.Chdir is needed to anchor stateDir at the
	// server's cwd-resolved .zcp path, and t.Chdir is incompatible
	// with t.Parallel.
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "demo", Status: "ACTIVE"}).
		WithServices([]platform.ServiceStack{
			{
				ID:   "svc-app",
				Name: "appdev",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{
					ServiceStackTypeVersionName:  "nodejs@22",
					ServiceStackTypeCategoryName: "USER",
				},
				Status: "ACTIVE",
				Mode:   "NON_HA",
			},
			// Phase 8 eval finding: managed services MUST be in the bundle
			// so `${db_*}` references in zerops.yaml resolve at re-import.
			// Pre-fix the handler filtered Discover to a single hostname,
			// leaving collectManagedServices empty. This fixture pins the
			// fixed behavior — db appears in the bundle's services list.
			{
				ID:   "svc-db",
				Name: "db",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{
					ServiceStackTypeVersionName:  "postgresql@16",
					ServiceStackTypeCategoryName: "DB",
				},
				Status: "ACTIVE",
				Mode:   "NON_HA",
			},
		}).
		WithProjectEnv([]platform.EnvVar{{Key: "LOG_LEVEL", Content: "info"}})

	// Seed ServiceMeta so the handler's bootstrap-meta gate passes.
	stateDir := writeIntegrationMeta(t, "appdev", topology.ModeStandard, topology.GitPushConfigured)

	const liveRemote = "https://github.com/example/demo.git"
	ssh := &exportSSHStub{responses: map[string]string{
		"cat /var/www/zerops.yaml": exportIntegrationZeropsYAML,
		"git remote get-url":       liveRemote,
	}}

	authInfo := &auth.Info{
		ProjectID:   "proj-1",
		Token:       "test-token",
		APIHost:     "localhost",
		Region:      "prg1",
		ClientID:    "client-1",
		ProjectName: "demo",
	}
	store, err := knowledge.GetEmbeddedStore()
	if err != nil {
		t.Fatalf("knowledge store: %v", err)
	}

	srv := server.New(context.Background(), mock, authInfo, store, nil, ssh, nil, runtime.Info{InContainer: true, ServiceName: "zcp"})
	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	ss, err := srv.MCPServer().Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { _ = ss.Close() })

	client := mcp.NewClient(&mcp.Implementation{Name: "export-integration", Version: "0.1"}, nil)
	session, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	// Call 1: workflow="export" with no targetService → scope-prompt.
	body := callExport(t, session, map[string]any{"workflow": "export"})
	if body["status"] != "scope-prompt" {
		t.Errorf("call 1: expected status=scope-prompt, got %v", body["status"])
	}

	// Call 2: targetService set, ModeStandard → variant-prompt.
	body = callExport(t, session, map[string]any{
		"workflow":      "export",
		"targetService": "appdev",
	})
	if body["status"] != "variant-prompt" {
		t.Errorf("call 2: expected status=variant-prompt, got %v", body["status"])
	}

	// Call 3: variant=dev, no envClassifications → classify-prompt with
	// LOG_LEVEL row (no value leaked per Phase 3 redaction).
	body = callExport(t, session, map[string]any{
		"workflow":      "export",
		"targetService": "appdev",
		"variant":       "dev",
	})
	if body["status"] != "classify-prompt" {
		t.Errorf("call 3: expected status=classify-prompt, got %v", body["status"])
	}
	rows, _ := body["envClassificationTable"].([]any)
	if len(rows) != 1 {
		t.Fatalf("call 3: expected 1 env row, got %d", len(rows))
	}
	row, _ := rows[0].(map[string]any)
	if row["key"] != "LOG_LEVEL" {
		t.Errorf("call 3: expected key=LOG_LEVEL, got %v", row["key"])
	}
	if _, hasValue := row["value"]; hasValue {
		t.Error("call 3: classify-prompt rows must NOT include raw env values (Phase 3 redaction)")
	}

	// Call 4: classifications populated → publish-ready (bundle has
	// no validation errors against a real-shaped fixture).
	body = callExport(t, session, map[string]any{
		"workflow":      "export",
		"targetService": "appdev",
		"variant":       "dev",
		"envClassifications": map[string]any{
			"LOG_LEVEL": "plain-config",
		},
	})
	if body["status"] != "publish-ready" {
		t.Errorf("call 4: expected status=publish-ready, got %v body=%v", body["status"], body)
	}
	bundle, _ := body["bundle"].(map[string]any)
	if bundle == nil {
		t.Fatal("call 4: expected bundle in publish-ready response")
	}
	importYaml, _ := bundle["importYaml"].(string)
	if !strings.Contains(importYaml, "buildFromGit") {
		t.Errorf("call 4: importYaml missing buildFromGit, got %q", importYaml)
	}
	if !strings.Contains(importYaml, liveRemote) {
		t.Errorf("call 4: importYaml missing live remote URL %q, got %q", liveRemote, importYaml)
	}
	// Managed-deps inclusion (Phase 8 eval finding fix): bundle MUST
	// carry `db` (managed postgresql) with `priority: 10` alongside the
	// runtime so `${db_*}` references in zerops.yaml resolve at
	// re-import. Without this the destination project boots with
	// unresolved managed-service refs.
	if !strings.Contains(importYaml, "hostname: db") {
		t.Errorf("call 4: importYaml missing managed db service entry, got %q", importYaml)
	}
	if !strings.Contains(importYaml, "priority: 10") {
		t.Errorf("call 4: importYaml missing managed-service priority: 10, got %q", importYaml)
	}
	steps, _ := body["nextSteps"].([]any)
	hasDeploy := false
	for _, s := range steps {
		if str, ok := s.(string); ok && strings.Contains(str, "zerops_deploy") {
			hasDeploy = true
		}
	}
	if !hasDeploy {
		t.Error("call 4: nextSteps should include zerops_deploy strategy=git-push")
	}

	// Verify the cache was refreshed to the live remote.
	refreshed, err := workflow.FindServiceMeta(stateDir, "appdev")
	if err != nil {
		t.Fatalf("re-read meta: %v", err)
	}
	if refreshed == nil || refreshed.RemoteURL != liveRemote {
		t.Errorf("meta.RemoteURL after publish-ready = %q, want %q", refreshedURL(refreshed), liveRemote)
	}
}

// callExport calls zerops_workflow with the given args and returns the
// decoded JSON body as a generic map. Fails the test on transport or
// JSON decode error.
func callExport(t *testing.T, session *mcp.ClientSession, args map[string]any) map[string]any {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "zerops_workflow",
		Arguments: args,
	})
	if err != nil {
		t.Fatalf("call zerops_workflow: %v", err)
	}
	if len(result.Content) == 0 {
		t.Fatal("no content in zerops_workflow result")
	}
	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(tc.Text), &body); err != nil {
		t.Fatalf("decode zerops_workflow body: %v\nbody=%s", err, tc.Text)
	}
	return body
}

// writeIntegrationMeta seeds ServiceMeta in a TempDir under the test's
// working directory so server.New (which resolves stateDir from cwd)
// finds the meta. Returns the resolved stateDir for downstream
// assertions.
func writeIntegrationMeta(t *testing.T, hostname string, mode topology.Mode, gitPushState topology.GitPushState) string {
	t.Helper()
	// server.New resolves stateDir = filepath.Join(cwd, ".zcp", "state").
	// The integration package's TestMain (main_test.go) clears .zcp at
	// suite boundaries, so writing under cwd is safe within a single
	// test's lifetime.
	t.Chdir(t.TempDir())
	stateDir := ".zcp/state"
	meta := &workflow.ServiceMeta{
		Hostname:                 hostname,
		Mode:                     mode,
		BootstrapSession:         "test-session",
		BootstrappedAt:           time.Now().UTC().Format(time.RFC3339),
		FirstDeployedAt:          time.Now().UTC().Format(time.RFC3339),
		CloseDeployMode:          topology.CloseModeManual,
		CloseDeployModeConfirmed: true,
		GitPushState:             gitPushState,
	}
	if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
	return stateDir
}

func refreshedURL(m *workflow.ServiceMeta) string {
	if m == nil {
		return "<nil>"
	}
	return m.RemoteURL
}
