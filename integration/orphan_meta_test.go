// Tests for: integration — orphan-meta visibility (G4 plan §4.6 Phase 7).
//
// Exercises the end-to-end path: write a complete ServiceMeta to disk for
// a hostname that the mock platform does NOT report as live → call
// zerops_workflow action=status through MCP → assert the response
// includes orphanMetas + IdleOrphan + the reset recovery primary action.

package integration_test

import (
	"context"
	"encoding/json"
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

func TestIntegration_OrphanMeta_StatusSurfacesResetHint(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()

	// Seed: complete meta for a hostname the mock won't return as live.
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:       "ghostdev",
		StageHostname:  "ghoststage",
		Mode:           topology.PlanModeStandard,
		BootstrappedAt: "2026-04-25",
	}); err != nil {
		t.Fatalf("seed meta: %v", err)
	}

	// Mock: no live services (project exists, just empty).
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "demo"}).
		WithServices(nil)

	mcpSrv := mcp.NewServer(
		&mcp.Implementation{Name: "zcp-test", Version: "0.1"},
		nil,
	)
	engine := workflow.NewEngine(stateDir, workflow.EnvLocal, nil)
	tools.RegisterWorkflow(mcpSrv, mock, "proj-1", nil, nil, engine, nil, stateDir, "", nil, nil, runtime.Info{})

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

	// Call zerops_workflow action=status — canonical recovery primitive.
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

	// Status response is markdown-rendered. Verify orphan visibility +
	// reset primary action both appear.
	if !strings.Contains(body, "ghostdev") {
		t.Errorf("status response missing orphan hostname `ghostdev`:\n%s", body)
	}
	if !strings.Contains(body, "reset") {
		t.Errorf("status response missing `reset` primary action:\n%s", body)
	}
	if !strings.Contains(body, "orphan") {
		t.Errorf("status response missing `orphan` framing:\n%s", body)
	}
}

// contentText extracts the concatenated text from a tool response. Used
// by integration tests that don't care about the structured shape.
func contentText(resp *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range resp.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

// TestIntegration_OrphanMeta_ResetActuallyClears pins the Codex pass-2
// finding: when status routes IdleOrphan to action=reset, executing that
// action MUST actually remove the orphan meta. Without this, status keeps
// reporting IdleOrphan and the agent loops without remediation.
func TestIntegration_OrphanMeta_ResetActuallyClears(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()

	// Seed: complete meta for a hostname not in the mock's live list.
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:       "ghostdev",
		Mode:           topology.PlanModeDev,
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
	tools.RegisterWorkflow(mcpSrv, mock, "proj-1", nil, nil, engine, nil, stateDir, "", nil, nil, runtime.Info{})

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

	// Step 1: status confirms orphan visible.
	status1, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "zerops_workflow",
		Arguments: map[string]any{"action": "status"},
	})
	if err != nil {
		t.Fatalf("status before reset: %v", err)
	}
	if !strings.Contains(contentText(status1), "ghostdev") {
		t.Fatal("orphan ghostdev not visible in status before reset")
	}

	// Step 2: execute the recommended primary action (reset workflow=bootstrap).
	resetResp, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "zerops_workflow",
		Arguments: map[string]any{
			"action":   "reset",
			"workflow": "bootstrap",
		},
	})
	if err != nil {
		t.Fatalf("reset call: %v", err)
	}
	if resetResp.IsError {
		t.Fatalf("reset returned IsError: %s", contentText(resetResp))
	}
	if !strings.Contains(contentText(resetResp), "ghostdev") {
		t.Errorf("reset report should name cleared orphan ghostdev:\n%s", contentText(resetResp))
	}

	// Step 3: status after reset must NOT route to IdleOrphan + must NOT
	// list ghostdev. This pins the Codex pass-2 fix.
	status2, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "zerops_workflow",
		Arguments: map[string]any{"action": "status"},
	})
	if err != nil {
		t.Fatalf("status after reset: %v", err)
	}
	if strings.Contains(contentText(status2), "ghostdev") {
		t.Errorf("orphan ghostdev still visible after reset:\n%s", contentText(status2))
	}
	if strings.Contains(contentText(status2), "OrphanMetas:") {
		t.Errorf("OrphanMetas section still present after reset:\n%s", contentText(status2))
	}
}

// TestIntegration_OrphanMeta_EnvelopeFieldExists pins the JSON envelope
// shape. Status renders markdown to the wire but the underlying envelope
// can also be extracted via the structured Content path.
func TestIntegration_OrphanMeta_EnvelopeFieldExists(t *testing.T) {
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

	if len(env.OrphanMetas) != 1 {
		t.Fatalf("env.OrphanMetas = %d, want 1", len(env.OrphanMetas))
	}
	if env.OrphanMetas[0].Hostname != "phantomdev" {
		t.Errorf("hostname = %q, want phantomdev", env.OrphanMetas[0].Hostname)
	}
	if env.OrphanMetas[0].Reason != workflow.OrphanReasonLiveDeleted {
		t.Errorf("reason = %q, want %q", env.OrphanMetas[0].Reason, workflow.OrphanReasonLiveDeleted)
	}
	if env.IdleScenario != workflow.IdleOrphan {
		t.Errorf("idleScenario = %q, want %q", env.IdleScenario, workflow.IdleOrphan)
	}

	// Round-trip through JSON to confirm wire shape carries OrphanMetas.
	encoded, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("Marshal envelope: %v", err)
	}
	if !strings.Contains(string(encoded), `"orphanMetas"`) {
		t.Errorf("encoded envelope missing orphanMetas key: %s", encoded)
	}
}
