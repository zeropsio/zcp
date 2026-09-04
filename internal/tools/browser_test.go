// Tests for: browser.go's tool-result assembly — the S7 image-content
// promotion. White-box (package tools) so browserToolResult, which is
// unexported, is directly testable without spinning up a full MCP
// session (TestAnnotations_BrowserTool in annotations_test.go already
// covers the registered-tool/session path end to end).
package tools

import (
	"slices"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
)

// TestBrowserToolResult_AppendsImageContentWhenScreenshotPresent pins
// the S7 result shape: the result stays text (JSON, as before) plus,
// when ops.BrowserBatch captured a screenshot, exactly one appended
// mcp.ImageContent{MIMEType: "image/png"} — never inside
// structuredContent (spec-mate.md §1.6: a tool handler's typed output
// stays nil, or Claude Code strips the guidance text).
func TestBrowserToolResult_AppendsImageContentWhenScreenshotPresent(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	result := &ops.BrowserBatchResult{
		URL: "https://example.com",
		Screenshot: &ops.BrowserScreenshotResult{
			PNG:    png,
			Width:  10,
			Height: 6,
		},
	}

	tr := browserToolResult(result)

	if len(tr.Content) != 2 {
		t.Fatalf("expected 2 content blocks (text + image), got %d: %+v", len(tr.Content), tr.Content)
	}
	if _, ok := tr.Content[0].(*mcp.TextContent); !ok {
		t.Errorf("Content[0] must be TextContent, got %T", tr.Content[0])
	}
	img, ok := tr.Content[1].(*mcp.ImageContent)
	if !ok {
		t.Fatalf("Content[1] must be ImageContent, got %T", tr.Content[1])
	}
	if img.MIMEType != "image/png" {
		t.Errorf("MIMEType = %q, want %q", img.MIMEType, "image/png")
	}
	if !slices.Equal(img.Data, png) {
		t.Error("ImageContent.Data does not match Screenshot.PNG bytes")
	}
}

// TestBrowserToolResult_NoImageContentWhenNoScreenshot guards the
// default path: no screenshot was captured (or none requested), so the
// result carries only the text block — no empty/placeholder image
// content.
func TestBrowserToolResult_NoImageContentWhenNoScreenshot(t *testing.T) {
	result := &ops.BrowserBatchResult{URL: "https://example.com"}

	tr := browserToolResult(result)

	if len(tr.Content) != 1 {
		t.Fatalf("expected 1 content block (text only), got %d: %+v", len(tr.Content), tr.Content)
	}
	if _, ok := tr.Content[0].(*mcp.TextContent); !ok {
		t.Errorf("Content[0] must be TextContent, got %T", tr.Content[0])
	}
}

// TestBrowserToolResult_EmptyScreenshotBytesNoImageContent guards
// against a Screenshot struct present but empty (e.g. the file read
// failed after a requested screenshot) — must not append a useless
// zero-byte image block.
func TestBrowserToolResult_EmptyScreenshotBytesNoImageContent(t *testing.T) {
	result := &ops.BrowserBatchResult{
		URL:        "https://example.com",
		Screenshot: &ops.BrowserScreenshotResult{Width: 10, Height: 6}, // PNG left nil
	}

	tr := browserToolResult(result)

	if len(tr.Content) != 1 {
		t.Fatalf("expected 1 content block (text only) when PNG bytes are empty, got %d: %+v", len(tr.Content), tr.Content)
	}
}
