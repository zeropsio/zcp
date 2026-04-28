// Tests for: integration — orphan-meta auto-cleanup at bootstrap-start (E3).
//
// Engine plan 2026-04-27 ticket E3 made orphan ServiceMeta cleanup a
// transparent side-effect of `zerops_workflow action="start"
// workflow="bootstrap"` commit. The agent never sees a dedicated reset
// recommendation; the response surfaces the cleaned hostnames via
// `cleanedUpOrphanMetas`.

package integration_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/tools"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// fixedTimeForTest returns a deterministic time used in envelope assertions.
func fixedTimeForTest() time.Time {
	return time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)
}

// contentText concatenates the text from a tool response. Local helper so
// the orphan-meta tests don't share assertion code with the wider
// `multi_tool_test.go` `getTextContent`, which fatal-fails on empty bodies.
func contentText(resp *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range resp.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

// TestIntegration_OrphanMeta_StatusFallsThroughToBootstrap pins the post-E3
// routing: with stale ServiceMetas on disk and no live services, the idle
// scenario collapses to `empty` and status recommends starting a bootstrap.
// The dedicated `IdleOrphan` reset path is gone — cleanup happens
// transparently inside bootstrap-start.
func TestIntegration_OrphanMeta_StatusFallsThroughToBootstrap(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()

	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:       "ghostdev",
		StageHostname:  "ghoststage",
		Mode:           topology.PlanModeStandard,
		BootstrappedAt: "2026-04-25",
	}); err != nil {
		t.Fatalf("seed meta: %v", err)
	}

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "demo"}).
		WithServices(nil)

	mcpSrv := mcp.NewServer(
		&mcp.Implementation{Name: "zcp-test", Version: "0.1"},
		nil,
	)
	engine := workflow.NewEngine(stateDir, workflow.EnvLocal, nil)
	tools.RegisterWorkflow(mcpSrv, mock, nil, "proj-1", nil, nil, engine, nil, stateDir, "", nil, nil, runtime.Info{})

	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	ss, err := mcpSrv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	session, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer session.Close()

	resp, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "zerops_workflow",
		Arguments: map[string]any{
			"action": "status",
		},
	})
	if err != nil {
		t.Fatalf("CallTool status: %v", err)
	}
	if resp.IsError {
		t.Fatalf("status returned IsError: %+v", resp.Content)
	}

	body := contentText(resp)

	// Status no longer surfaces the orphan hostname or routes to reset —
	// cleanup happens at bootstrap-start time.
	if strings.Contains(body, "ghostdev") {
		t.Errorf("status response leaks orphan hostname `ghostdev`:\n%s", body)
	}
	if strings.Contains(body, `action="reset"`) {
		t.Errorf("status should not recommend reset for orphan-only state:\n%s", body)
	}
	if !strings.Contains(body, `action="start" workflow="bootstrap"`) {
		t.Errorf("status missing recommendation to start a bootstrap:\n%s", body)
	}
}

// TestIntegration_OrphanMeta_BootstrapStartCleansAndReports pins E3's
// transparent-cleanup contract: a fresh `start workflow=bootstrap` with a
// concrete route prunes orphan metas before creating the new session and
// names the cleaned hostnames in the response.
func TestIntegration_OrphanMeta_BootstrapStartCleansAndReports(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()

	for _, host := range []string{"ghostdev", "phantomdev"} {
		if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
			Hostname:       host,
			Mode:           topology.PlanModeDev,
			BootstrappedAt: "2026-04-25",
		}); err != nil {
			t.Fatalf("seed meta %s: %v", host, err)
		}
	}

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "demo"}).
		WithServices(nil)

	mcpSrv := mcp.NewServer(
		&mcp.Implementation{Name: "zcp-test", Version: "0.1"},
		nil,
	)
	engine := workflow.NewEngine(stateDir, workflow.EnvLocal, nil)
	tools.RegisterWorkflow(mcpSrv, mock, nil, "proj-1", nil, nil, engine, nil, stateDir, "", nil, nil, runtime.Info{})

	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	ss, err := mcpSrv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	session, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer session.Close()

	resp, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "zerops_workflow",
		Arguments: map[string]any{
			"action":   "start",
			"workflow": "bootstrap",
			"route":    "classic",
			"intent":   "fresh project after orphans",
		},
	})
	if err != nil {
		t.Fatalf("CallTool bootstrap-start: %v", err)
	}
	if resp.IsError {
		t.Fatalf("bootstrap-start returned IsError: %s", contentText(resp))
	}

	body := contentText(resp)
	if !strings.Contains(body, `"cleanedUpOrphanMetas"`) {
		t.Errorf("response missing cleanedUpOrphanMetas field:\n%s", body)
	}
	for _, host := range []string{"ghostdev", "phantomdev"} {
		if !strings.Contains(body, host) {
			t.Errorf("response missing cleaned hostname %q:\n%s", host, body)
		}
	}

	// The meta files must be gone from disk after the cleanup.
	for _, host := range []string{"ghostdev", "phantomdev"} {
		path := filepath.Join(stateDir, "services", host+".json")
		meta, err := workflow.ReadServiceMeta(stateDir, host)
		if err != nil {
			t.Fatalf("ReadServiceMeta(%s): %v", host, err)
		}
		if meta != nil {
			t.Errorf("meta file %s still exists post-cleanup: %+v", path, meta)
		}
	}
}

// TestIntegration_OrphanMeta_EnvelopeOmitsOrphanField pins the post-E3
// envelope shape: orphan metas are no longer surfaced as a first-class
// field. The cleanup is invisible to the agent until bootstrap-start
// reports it via `cleanedUpOrphanMetas`.
func TestIntegration_OrphanMeta_EnvelopeOmitsOrphanField(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname: "phantomdev", Mode: topology.PlanModeDev, BootstrappedAt: "2026-04-25",
	}); err != nil {
		t.Fatalf("seed meta: %v", err)
	}

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "demo"}).
		WithServices(nil)

	env, err := workflow.ComputeEnvelope(context.Background(), mock, stateDir, "proj-1", runtime.Info{}, fixedTimeForTest())
	if err != nil {
		t.Fatalf("ComputeEnvelope: %v", err)
	}

	if env.IdleScenario != workflow.IdleEmpty {
		t.Errorf("idleScenario = %q, want %q (orphan-only collapses to empty post-E3)", env.IdleScenario, workflow.IdleEmpty)
	}

	encoded, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("Marshal envelope: %v", err)
	}
	if strings.Contains(string(encoded), `"orphanMetas"`) {
		t.Errorf("encoded envelope still carries orphanMetas key: %s", encoded)
	}
}
