package tools

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/platform"
)

// convertError converts an error to a CallToolResult with IsError=true.
// PlatformErrors are serialized as structured JSON with code/error/suggestion.
// Generic errors are returned as plain text.
func convertError(err error) *mcp.CallToolResult {
	var pe *platform.PlatformError
	if !errors.As(err, &pe) {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
			IsError: true,
		}
	}
	result := map[string]string{"code": pe.Code, "error": pe.Message}
	if pe.Suggestion != "" {
		result["suggestion"] = pe.Suggestion
	}
	b, err := json.Marshal(result)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("marshal error: %v", err)}},
			IsError: true,
		}
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(b)}},
		IsError: true,
	}
}

// jsonResult marshals v to JSON and returns it as a CallToolResult.
func jsonResult(v any) *mcp.CallToolResult {
	b, err := json.Marshal(v)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("marshal error: %v", err)}},
			IsError: true,
		}
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}
}

// textResult returns a plain text CallToolResult.
func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}

// boolPtr returns a pointer to b. Used for optional bool fields in ToolAnnotations.
func boolPtr(b bool) *bool { return &b }
