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
			var parsed map[string]string
			if err := json.Unmarshal([]byte(text), &parsed); err != nil {
				t.Fatalf("failed to parse result JSON: %v", err)
			}
			if parsed["code"] != tt.wantCode {
				t.Errorf("code = %q, want %q", parsed["code"], tt.wantCode)
			}
			if parsed["error"] != tt.wantMsg {
				t.Errorf("error = %q, want %q", parsed["error"], tt.wantMsg)
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
	var parsed map[string]string
	if jsonErr := json.Unmarshal([]byte(text), &parsed); jsonErr != nil {
		t.Fatalf("failed to parse result JSON: %v", jsonErr)
	}
	if parsed["suggestion"] != "Swap the values" {
		t.Errorf("suggestion = %q, want %q", parsed["suggestion"], "Swap the values")
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
