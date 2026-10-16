// Tests for: browser.go's tool-result assembly — the S7 image-content
// promotion. White-box (package tools) so browserToolResult, which is
// unexported, is directly testable without spinning up a full MCP
// session (TestAnnotations_BrowserTool in annotations_test.go already
// covers the registered-tool/session path end to end).
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/workflow"
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

// TestKeepBrowserPicture: a screenshot zerops_browser takes is kept, and its
// result names it — the only way the Mate can put it in a description.
func TestKeepBrowserPicture(t *testing.T) {
	stateDir := t.TempDir()
	result := &ops.BrowserBatchResult{
		URL:        "https://appstage.example.invalid",
		Screenshot: &ops.BrowserScreenshotResult{PNG: []byte("\x89PNG-browser"), Width: 1280, Height: 720},
	}
	keepBrowserPicture(stateDir, result)
	if result.Screenshot.Picture != "shot-1" {
		t.Fatalf("picture = %q, want shot-1", result.Screenshot.Picture)
	}
	var shown struct {
		Screenshot struct {
			Picture string `json:"picture"`
			KeptFor string `json:"keptFor"`
		} `json:"screenshot"`
	}
	if err := json.Unmarshal([]byte(resultText(t, browserToolResult(result))), &shown); err != nil || shown.Screenshot.Picture != "shot-1" {
		t.Errorf("the result must name the picture: %+v (%v)", shown, err)
	}
	if shown.Screenshot.KeptFor != "up to 7 days" {
		t.Errorf("keptFor = %q, want the result to say how long the picture is kept (up to 7 days)", shown.Screenshot.KeptFor)
	}
	if _, png, err := workflow.KeptPicture(stateDir, "shot-1"); err != nil || !bytes.Equal(png, []byte("\x89PNG-browser")) {
		t.Errorf("the picture is not kept: %v", err)
	}

	for _, none := range []*ops.BrowserBatchResult{
		{URL: "https://appstage.example.invalid"},
		{URL: "https://appstage.example.invalid", Screenshot: &ops.BrowserScreenshotResult{}},
	} {
		keepBrowserPicture(stateDir, none)
	}
	keepBrowserPicture("", result)
	if _, _, err := workflow.KeptPicture(stateDir, "shot-2"); err == nil {
		t.Error("a result with no picture kept one")
	}
}

// fullPageRunner answers every step of a walk and writes png wherever the
// walk asks for a screenshot, the way agent-browser does.
type fullPageRunner struct {
	png       []byte
	lastStdin string
}

func (fullPageRunner) LookPath() (string, error) { return "/fake/agent-browser", nil }
func (fullPageRunner) Acquire(context.Context, string) (func(bool) error, bool, error) {
	return func(bool) error { return nil }, false, nil
}

func (f *fullPageRunner) Run(_ context.Context, stdin string, _ time.Duration) (string, string, bool, error) {
	f.lastStdin = stdin
	var batch [][]string
	if err := json.Unmarshal([]byte(stdin), &batch); err != nil {
		return "", "", false, fmt.Errorf("fullPageRunner: parse stdin: %w", err)
	}
	out := make([]map[string]any, 0, len(batch))
	for _, cmd := range batch {
		if cmd[0] == "screenshot" {
			if err := os.WriteFile(cmd[len(cmd)-1], f.png, 0o600); err != nil {
				return "", "", false, fmt.Errorf("fullPageRunner: write PNG: %w", err)
			}
		}
		out = append(out, map[string]any{"command": cmd, "success": true, "result": map[string]any{}})
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "", "", false, fmt.Errorf("fullPageRunner: marshal: %w", err)
	}
	return string(b), "", false, nil
}

// browserSession serves zerops_browser to an in-memory client.
func browserSession(t *testing.T) *mcp.ClientSession {
	t.Helper()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterBrowser(srv, "")
	st, ct := mcp.NewInMemoryTransports()
	if _, err := srv.Connect(t.Context(), st, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "client", Version: "0.1"}, nil).Connect(t.Context(), ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}

// TestBrowserTool_FullPageReturnsOneImageOfTheWholePage: fullPage asks for
// the whole page and the call answers with that one image — an agent that
// could not ask for it scrolled and took eleven viewport screenshots.
func TestBrowserTool_FullPageReturnsOneImageOfTheWholePage(t *testing.T) {
	// Not parallel: overrides the ops browser runner.
	img := image.NewRGBA(image.Rect(0, 0, 390, 3200))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode test PNG: %v", err)
	}
	runner := &fullPageRunner{png: buf.Bytes()}
	defer ops.OverrideBrowserRunnerForTest(runner)()

	res, err := browserSession(t).CallTool(t.Context(), &mcp.CallToolParams{
		Name:      "zerops_browser",
		Arguments: map[string]any{"url": "https://appdev.example.invalid", "fullPage": true},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	var images []*mcp.ImageContent
	for _, c := range res.Content {
		if ic, ok := c.(*mcp.ImageContent); ok {
			images = append(images, ic)
		}
	}
	if len(images) != 1 || !bytes.Equal(images[0].Data, buf.Bytes()) {
		t.Fatalf("got %d images, want the one full-page image", len(images))
	}
	if !strings.Contains(runner.lastStdin, `["screenshot","--full",`) {
		t.Errorf("the walk did not ask for the whole page: %s", runner.lastStdin)
	}
	var shown struct {
		Screenshot struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"screenshot"`
	}
	text, _ := res.Content[0].(*mcp.TextContent)
	if text == nil || json.Unmarshal([]byte(text.Text), &shown) != nil || shown.Screenshot.Width != 390 || shown.Screenshot.Height != 3200 {
		t.Errorf("the result must say the image is 390x3200: %+v", res.Content[0])
	}
}

// TestBrowserTool_DescriptionTeachesTheCall pins what the description tells
// an agent — which may call the tool from a script, not directly: the paint
// wait is done, so wait for something, not for a time; fresh-context costs a
// cold Chrome and pays only on the first check after a deploy; the result's
// timing facts are the ones to report.
func TestBrowserTool_DescriptionTeachesTheCall(t *testing.T) {
	tl, err := browserSession(t).ListTools(t.Context(), &mcp.ListToolsParams{})
	if err != nil || len(tl.Tools) != 1 {
		t.Fatalf("list tools: %v %+v", err, tl)
	}
	desc := tl.Tools[0].Description
	for _, want := range []string{
		"The call already waits for the page's first paint",
		`wait for what should appear — ["wait","<sel>"] or ["wait","--text","<text>"] — never for a fixed time`,
		`"fresh-context" starts with none of them — only for the first check after a deploy that changed sign-in, storage, a service worker or long-cached assets, or for a signed-out view`,
		"it stops Chrome as the call ends, so the next call starts a cold one",
		`Every other call stays on "tab"`,
		"fullPage=true for the whole page as one image",
		"browserRecycled: true means the call ended unclean and the browser was stopped",
		"To say how a call ran, report the result's durationMs, freshBrowser and browserRecycled; a script that summarises results keeps them.",
	} {
		if !strings.Contains(desc, want) {
			t.Errorf("description lacks %q", want)
		}
	}
	for _, gone := range []string{"for a first visit"} {
		if strings.Contains(desc, gone) {
			t.Errorf("description still says %q", gone)
		}
	}
	if prop := tl.Tools[0].InputSchema.(map[string]any)["properties"].(map[string]any)["fullPage"]; prop == nil {
		t.Error("the input schema has no fullPage option")
	}
}
