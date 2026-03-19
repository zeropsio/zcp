package tools

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/workflow"
)

// callTool connects to a test server and calls a named tool with the given arguments.
func callTool(t *testing.T, srv *mcp.Server, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()

	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.1"}, nil)
	session, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// callToolMayError connects to a test server and calls a named tool.
// Returns nil on success, or err when the call itself fails
// (e.g. schema validation rejects missing required fields).
func callToolMayError(t *testing.T, srv *mcp.Server, name string, args map[string]any) error {
	t.Helper()
	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()

	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.1"}, nil)
	session, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	_, err = session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	return err
}

// testEngine creates a workflow engine with an active session for tests.
func testEngine(t *testing.T) *workflow.Engine {
	t.Helper()
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	if _, err := engine.Start("proj-1", "deploy", "test"); err != nil {
		t.Fatalf("start test session: %v", err)
	}
	return engine
}

// getTextContent extracts the text string from the first content item of a CallToolResult.
func getTextContent(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if len(result.Content) == 0 {
		t.Fatal("no content in result")
	}
	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected *mcp.TextContent, got %T", result.Content[0])
	}
	return tc.Text
}
