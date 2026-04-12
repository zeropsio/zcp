package tools

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/workflow"
)

// testGuidanceServer creates an MCP server with zerops_guidance registered.
// The engine has no active session, so the tool will use a nil plan.
func testGuidanceServer(t *testing.T) *mcp.Server {
	t.Helper()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test-guidance", Version: "0.1"}, nil)
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	RegisterGuidance(srv, engine)
	return srv
}

func TestGuidance_KnownTopic_ReturnsContent(t *testing.T) {
	t.Parallel()
	srv := testGuidanceServer(t)
	// container-state has no predicate — works even without a plan.
	result := callTool(t, srv, "zerops_guidance", map[string]any{
		"topic": "container-state",
	})
	text := getTextContent(t, result)
	if len(text) < 50 {
		t.Errorf("container-state content too short: %d bytes", len(text))
	}
}

func TestGuidance_UnknownTopic_ReturnsError(t *testing.T) {
	t.Parallel()
	srv := testGuidanceServer(t)
	result := callTool(t, srv, "zerops_guidance", map[string]any{
		"topic": "nonexistent-topic",
	})
	text := getTextContent(t, result)
	if !strings.Contains(text, "unknown") {
		t.Errorf("expected error mentioning 'unknown', got: %s", text)
	}
}

func TestGuidance_PredicateGated_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	srv := testGuidanceServer(t)
	// dual-runtime-urls requires isDualRuntime, nil plan → false.
	result := callTool(t, srv, "zerops_guidance", map[string]any{
		"topic": "dual-runtime-urls",
	})
	text := getTextContent(t, result)
	if !strings.Contains(text, "does not apply") {
		t.Errorf("expected 'does not apply' message, got: %s", text)
	}
}

func TestGuidance_DeployFlow_ReturnsContent(t *testing.T) {
	t.Parallel()
	srv := testGuidanceServer(t)
	result := callTool(t, srv, "zerops_guidance", map[string]any{
		"topic": "deploy-flow",
	})
	text := getTextContent(t, result)
	if !strings.Contains(text, "Dev deployment flow") {
		t.Error("deploy-flow missing expected heading")
	}
}

func TestGuidance_CommentStyle_ReturnsContent(t *testing.T) {
	t.Parallel()
	srv := testGuidanceServer(t)
	result := callTool(t, srv, "zerops_guidance", map[string]any{
		"topic": "comment-style",
	})
	text := getTextContent(t, result)
	if !strings.Contains(text, "Comment style") {
		t.Error("comment-style missing expected heading")
	}
}
