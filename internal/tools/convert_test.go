// Tests for: convert.go — error conversion and result helper functions.

package tools

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/platform"
)

func TestConvertError_PlatformError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		err       error
		wantCode  string
		wantMsg   string
		wantIsErr bool
	}{
		{
			name:      "service not found",
			err:       platform.NewPlatformError(platform.ErrServiceNotFound, "Service 'api' not found", ""),
			wantCode:  platform.ErrServiceNotFound,
			wantMsg:   "Service 'api' not found",
			wantIsErr: true,
		},
		{
			name:      "auth required",
			err:       platform.NewPlatformError(platform.ErrAuthRequired, "Authentication required", ""),
			wantCode:  platform.ErrAuthRequired,
			wantMsg:   "Authentication required",
			wantIsErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := convertError(tt.err)

			if result.IsError != tt.wantIsErr {
				t.Errorf("IsError = %v, want %v", result.IsError, tt.wantIsErr)
			}

			text := getResultText(t, result)
			var parsed map[string]any
			if err := json.Unmarshal([]byte(text), &parsed); err != nil {
				t.Fatalf("failed to parse result JSON: %v", err)
			}
			if parsed["code"] != tt.wantCode {
				t.Errorf("code = %v, want %q", parsed["code"], tt.wantCode)
			}
			if parsed["error"] != tt.wantMsg {
				t.Errorf("error = %v, want %q", parsed["error"], tt.wantMsg)
			}
		})
	}
}

func TestConvertError_WithSuggestion(t *testing.T) {
	t.Parallel()
	err := platform.NewPlatformError(platform.ErrInvalidScaling, "minCpu must be <= maxCpu", "Swap the values")
	result := convertError(err)

	if !result.IsError {
		t.Error("IsError = false, want true")
	}

	text := getResultText(t, result)
	var parsed map[string]any
	if jsonErr := json.Unmarshal([]byte(text), &parsed); jsonErr != nil {
		t.Fatalf("failed to parse result JSON: %v", jsonErr)
	}
	if parsed["suggestion"] != "Swap the values" {
		t.Errorf("suggestion = %v, want %q", parsed["suggestion"], "Swap the values")
	}
}

func TestConvertError_GenericError(t *testing.T) {
	t.Parallel()
	err := errors.New("something went wrong")
	result := convertError(err)

	if !result.IsError {
		t.Error("IsError = false, want true")
	}

	text := getResultText(t, result)
	if text != "something went wrong" {
		t.Errorf("text = %q, want %q", text, "something went wrong")
	}
}

func TestJsonResult(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    any
		wantJSON string
		wantErr  bool
	}{
		{
			name:     "map",
			input:    map[string]string{"key": "value"},
			wantJSON: `{"key":"value"}`,
		},
		{
			name:     "struct",
			input:    struct{ Name string }{Name: "test"},
			wantJSON: `{"Name":"test"}`,
		},
		{
			name:    "unmarshalable",
			input:   make(chan int),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := jsonResult(tt.input)

			if tt.wantErr {
				if !result.IsError {
					t.Error("expected IsError for unmarshalable input")
				}
				return
			}

			if result.IsError {
				t.Error("unexpected IsError")
			}
			text := getResultText(t, result)
			if text != tt.wantJSON {
				t.Errorf("text = %q, want %q", text, tt.wantJSON)
			}
		})
	}
}

func TestTextResult(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		text string
	}{
		{name: "simple", text: "hello"},
		{name: "empty", text: ""},
		{name: "multiline", text: "line1\nline2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := textResult(tt.text)

			if result.IsError {
				t.Error("unexpected IsError")
			}
			text := getResultText(t, result)
			if text != tt.text {
				t.Errorf("text = %q, want %q", text, tt.text)
			}
		})
	}
}

func TestConvertError_WithAPICode(t *testing.T) {
	t.Parallel()
	pe := platform.NewPlatformError(platform.ErrAPIError, "invalid yaml", "Check input")
	pe.APICode = "projectImportInvalidYaml"
	result := convertError(pe)

	if !result.IsError {
		t.Error("IsError = false, want true")
	}

	text := getResultText(t, result)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("failed to parse result JSON: %v", err)
	}
	if parsed["apiCode"] != "projectImportInvalidYaml" {
		t.Errorf("apiCode = %v, want %q", parsed["apiCode"], "projectImportInvalidYaml")
	}
	if parsed["code"] != platform.ErrAPIError {
		t.Errorf("code = %v, want %q", parsed["code"], platform.ErrAPIError)
	}
}

func TestConvertError_WithoutAPICode(t *testing.T) {
	t.Parallel()
	pe := platform.NewPlatformError(platform.ErrServiceNotFound, "not found", "Check hostname")
	result := convertError(pe)

	text := getResultText(t, result)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("failed to parse result JSON: %v", err)
	}
	if _, hasAPICode := parsed["apiCode"]; hasAPICode {
		t.Errorf("apiCode should not be present when empty, got %v", parsed["apiCode"])
	}
	if _, hasAPIMeta := parsed["apiMeta"]; hasAPIMeta {
		t.Errorf("apiMeta should not be present when empty, got %v", parsed["apiMeta"])
	}
}

func TestConvertError_WithAPIMeta(t *testing.T) {
	t.Parallel()
	// When PlatformError.APIMeta carries field-level detail, convertError
	// surfaces it into an "apiMeta" JSON array on the MCP response so the
	// LLM can read failing fields without trial-and-error.
	pe := platform.NewPlatformError(
		platform.ErrAPIError,
		"Invalid parameter provided.",
		"The platform flagged specific fields — see apiMeta for each field's failure reason.",
	)
	pe.APICode = "projectImportInvalidParameter"
	pe.APIMeta = []platform.APIMetaItem{
		{
			Code:  "projectImportInvalidParameter",
			Error: "Invalid parameter provided.",
			Metadata: map[string][]string{
				"storage.mode": {"mode not supported"},
			},
		},
	}

	result := convertError(pe)
	if !result.IsError {
		t.Fatal("IsError = false, want true")
	}

	text := getResultText(t, result)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("failed to parse result JSON: %v", err)
	}

	metaRaw, ok := parsed["apiMeta"]
	if !ok {
		t.Fatalf("apiMeta absent from JSON %q", text)
	}
	metaArr, ok := metaRaw.([]any)
	if !ok {
		t.Fatalf("apiMeta = %T, want []any", metaRaw)
	}
	if len(metaArr) != 1 {
		t.Fatalf("apiMeta has %d items, want 1", len(metaArr))
	}
	item, ok := metaArr[0].(map[string]any)
	if !ok {
		t.Fatalf("apiMeta[0] = %T, want map[string]any", metaArr[0])
	}
	md, ok := item["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("apiMeta[0].metadata = %T, want map", item["metadata"])
	}
	reasons, ok := md["storage.mode"].([]any)
	if !ok || len(reasons) != 1 || reasons[0] != "mode not supported" {
		t.Errorf("apiMeta[0].metadata[storage.mode] = %v, want [\"mode not supported\"]", md["storage.mode"])
	}
}

func TestConvertError_APIMetaEmptyOmitted(t *testing.T) {
	t.Parallel()
	// APIMeta = nil and APIMeta = [] are both absence — neither produces
	// a JSON key. Regression guard: previously adding APIMeta unconditionally
	// would have emitted "apiMeta":null for every error. That's worse UX
	// than omitting the key (consumers would have to special-case null vs
	// empty-array vs present-and-populated).
	for _, meta := range [][]platform.APIMetaItem{nil, {}} {
		pe := platform.NewPlatformError(platform.ErrAPIError, "boom", "")
		pe.APIMeta = meta
		text := getResultText(t, convertError(pe))
		var parsed map[string]any
		if err := json.Unmarshal([]byte(text), &parsed); err != nil {
			t.Fatalf("json unmarshal: %v", err)
		}
		if _, ok := parsed["apiMeta"]; ok {
			t.Errorf("apiMeta should be omitted when empty (got %v)", parsed["apiMeta"])
		}
	}
}

// getResultText extracts the text content from a CallToolResult.
func getResultText(t *testing.T, result *mcp.CallToolResult) string {
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
