package tools_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/server"
)

func TestPreprocess_SingleExpansion(t *testing.T) {
	t.Parallel()

	session := newPreprocessTestSession(t)
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "zerops_preprocess",
		Arguments: map[string]any{
			"input": "<@generateRandomString(<32>)>",
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %+v", result.Content)
	}

	payload := decodeResult[map[string]any](t, result)
	expanded, _ := payload["expanded"].(string)
	if len(expanded) != 32 {
		t.Errorf("expanded length = %d, want 32 (value: %q)", len(expanded), expanded)
	}
	if strings.Contains(expanded, "<@") {
		t.Errorf("expansion still contains syntax: %q", expanded)
	}
}

func TestPreprocess_PlainValuePassthrough(t *testing.T) {
	t.Parallel()

	session := newPreprocessTestSession(t)
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "zerops_preprocess",
		Arguments: map[string]any{
			"input": "literal-secret-value",
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %+v", result.Content)
	}

	payload := decodeResult[map[string]any](t, result)
	if expanded, _ := payload["expanded"].(string); expanded != "literal-secret-value" {
		t.Errorf("expanded = %q, want literal-secret-value", expanded)
	}
}

func TestPreprocess_BatchWithVariableSharing(t *testing.T) {
	t.Parallel()

	// setVar in the first key, getVar + modifier in the second — the batch
	// must share a variable store so the second key resolves.
	session := newPreprocessTestSession(t)
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "zerops_preprocess",
		Arguments: map[string]any{
			"inputs": map[string]string{
				"RAW":   "<@generateRandomStringVar(<myKey>, <16>)>",
				"HEX":   "<@getVar(myKey) | toHex>",
				"UPPER": "<@getVar(myKey) | upper>",
			},
			"order": []string{"RAW", "HEX", "UPPER"},
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %+v", result.Content)
	}

	payload := decodeResult[map[string]any](t, result)
	expansion, _ := payload["expansion"].(map[string]any)
	raw, _ := expansion["RAW"].(string)
	hex, _ := expansion["HEX"].(string)
	upper, _ := expansion["UPPER"].(string)

	if len(raw) != 16 {
		t.Errorf("RAW length = %d, want 16", len(raw))
	}
	if len(hex) != 32 { // 16 bytes → 32 hex chars
		t.Errorf("HEX length = %d, want 32", len(hex))
	}
	if upper != strings.ToUpper(raw) {
		t.Errorf("UPPER = %q, want uppercase of RAW %q", upper, raw)
	}
}

func TestPreprocess_NeitherInputNorInputs(t *testing.T) {
	t.Parallel()

	session := newPreprocessTestSession(t)
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "zerops_preprocess",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !result.IsError {
		t.Error("expected error when neither input nor inputs is provided")
	}
}

func TestPreprocess_BothInputAndInputs(t *testing.T) {
	t.Parallel()

	session := newPreprocessTestSession(t)
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "zerops_preprocess",
		Arguments: map[string]any{
			"input":  "<@generateRandomString(<8>)>",
			"inputs": map[string]string{"A": "<@generateRandomString(<8>)>"},
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !result.IsError {
		t.Error("expected error when both input and inputs are provided")
	}
}

func TestPreprocess_SyntaxError(t *testing.T) {
	t.Parallel()

	session := newPreprocessTestSession(t)
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "zerops_preprocess",
		Arguments: map[string]any{
			"input": "<@thisFunctionDoesNotExist(<32>)>",
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for unknown preprocessor function")
	}
}

// newPreprocessTestSession spins up an in-memory MCP server/client pair and
// returns the connected client session. Mirrors the pattern used by
// listAllTools in annotations_test.go.
func newPreprocessTestSession(t *testing.T) *mcp.ClientSession {
	t.Helper()

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "test"}).
		WithServices(nil)
	authInfo := &auth.Info{ProjectID: "p1", Token: "test", APIHost: "localhost"}
	store, err := knowledge.GetEmbeddedStore()
	if err != nil {
		t.Fatalf("knowledge store: %v", err)
	}
	logFetcher := platform.NewMockLogFetcher()

	srv := server.New(context.Background(), mock, authInfo, store, logFetcher, &nopSSH{}, &nopMounter{}, runtime.Info{})

	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	if _, err := srv.MCPServer().Connect(ctx, st, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	session, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}

// decodeResult extracts the JSON payload from a tool result's first text
// content block and unmarshals it into T. Fails the test on any problem.
func decodeResult[T any](t *testing.T, result *mcp.CallToolResult) T {
	t.Helper()
	if len(result.Content) == 0 {
		t.Fatal("tool result has no content")
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("first content block is not text: %T", result.Content[0])
	}
	var out T
	if err := json.Unmarshal([]byte(text.Text), &out); err != nil {
		t.Fatalf("unmarshal result: %v\n%s", err, text.Text)
	}
	return out
}
