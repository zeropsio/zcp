package tools

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/schema"
	"github.com/zeropsio/zcp/internal/workflow"
)

func decodePortJSON(t *testing.T, result *mcp.CallToolResult) map[string]any {
	t.Helper()
	text := getTextContent(t, result)
	var doc map[string]any
	if err := json.Unmarshal([]byte(text), &doc); err != nil {
		t.Fatalf("decode port response: %v\nbody=%s", err, text)
	}
	return doc
}

// TestHandlePort_Start_ReturnsPopulatedPortPlan covers Phase 0: the agent
// supplies a target descriptor, recon classifies it, the handler returns the
// PortPlan + band as the response with ZERO deploy.
func TestHandlePort_Start_ReturnsPopulatedPortPlan(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cache := schema.NewCache(15*time.Minute, "", nil)
	engine := workflow.NewEngine(dir, workflow.EnvContainer, nil)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, nil, "proj1", cache, engine, nil, dir, "", nil, nil, runtime.Info{InContainer: true})

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "port",
		"portTarget": map[string]any{
			"name":            "strapi",
			"acquisitionHint": "source-repo",
			"dependencies":    []any{"postgresql", "object-storage"},
			"runtimes":        []any{"nodejs@22"},
		},
	})
	if result.IsError {
		t.Fatalf("port start should not error, got: %s", getTextContent(t, result))
	}

	body := decodePortJSON(t, result)
	if body["status"] != "recon" {
		t.Errorf("expected status=recon, got %v", body["status"])
	}
	plan, ok := body["portPlan"].(map[string]any)
	if !ok {
		t.Fatalf("expected portPlan object, got %T: %v", body["portPlan"], body["portPlan"])
	}
	if plan["target"] != "strapi" {
		t.Errorf("portPlan.target: got %v", plan["target"])
	}
	if plan["acquisition"] != string(workflow.AcquireSourceBuild) {
		t.Errorf("portPlan.acquisition: got %v", plan["acquisition"])
	}
	if plan["band"] != string(workflow.BandEasy) {
		t.Errorf("portPlan.band: got %v want %v", plan["band"], workflow.BandEasy)
	}
}

func TestHandlePort_Start_MissingDescriptor_Errors(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cache := schema.NewCache(15*time.Minute, "", nil)
	engine := workflow.NewEngine(dir, workflow.EnvContainer, nil)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, nil, "proj1", cache, engine, nil, dir, "", nil, nil, runtime.Info{InContainer: true})

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "port",
	})
	if !result.IsError {
		t.Fatalf("port start without a descriptor must error")
	}
}
