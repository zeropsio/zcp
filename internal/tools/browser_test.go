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
	"path/filepath"
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

// TestBrowserToolResult_LongOutputIsCutAndTheWholeResultIsInAFile: a 73k-char
// snapshot and hundreds of failed requests reach the model as at most 20k
// chars — the head of each output, still JSON — and the reply names the file
// that holds the whole result.
func TestBrowserToolResult_LongOutputIsCutAndTheWholeResultIsInAFile(t *testing.T) {
	// Not parallel: points the temp dir at this test's own.
	t.Setenv("TMPDIR", t.TempDir())
	snapshot, err := json.Marshal(map[string]string{"snapshot": "- heading \"Orders\" [ref=e1]\n" + strings.Repeat("- row \"order\" [ref=e2]\n", 3000)})
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	network := make([]ops.NetworkRequest, 0, 400)
	for i := range 400 {
		network = append(network, ops.NetworkRequest{URL: fmt.Sprintf("https://appdev.example.invalid/api/item/%d", i), Method: "GET", Status: 404})
	}
	result := &ops.BrowserBatchResult{
		URL:           "https://appdev.example.invalid",
		Steps:         []ops.BrowserStepResult{{Command: []string{"snapshot", "-i", "-c"}, Success: true, Result: snapshot}},
		NetworkOutput: network,
		DurationMs:    1234,
	}
	whole, err := json.Marshal(result)
	if err != nil || len(whole) <= 20000 {
		t.Fatalf("the fixture must be longer than the cap: %d chars, %v", len(whole), err)
	}

	text := resultText(t, browserToolResult(result))

	if len(text) > 20000 {
		t.Errorf("the model got %d chars, want at most 20000", len(text))
	}
	var shown struct {
		Steps []struct {
			Result string `json:"result"`
		} `json:"steps"`
		NetworkOutput []ops.NetworkRequest `json:"networkOutput"`
		DurationMs    int64                `json:"durationMs"`
		FullResult    string               `json:"fullResult"`
	}
	if err := json.Unmarshal([]byte(text), &shown); err != nil {
		t.Fatalf("the cut reply must stay JSON: %v\n%.300s", err, text)
	}
	if shown.DurationMs != 1234 || len(shown.Steps) != 1 || !strings.HasPrefix(shown.Steps[0].Result, `{"snapshot":"- heading \"Orders\"`) {
		t.Errorf("the reply must keep the facts and the head of the snapshot: %.300s", text)
	}
	if len(shown.NetworkOutput) == 0 || len(shown.NetworkOutput) >= 400 || shown.NetworkOutput[0].URL != "https://appdev.example.invalid/api/item/0" {
		t.Errorf("the reply must keep the first failed requests, not all 400: got %d", len(shown.NetworkOutput))
	}
	saved, err := os.ReadFile(shown.FullResult)
	if err != nil || !bytes.Equal(saved, whole) {
		t.Errorf("fullResult %q must hold the whole result (%d chars): got %d chars, %v", shown.FullResult, len(whole), len(saved), err)
	}
}

// longBrowserResult is a result past the cap: a snapshot of n rows.
func longBrowserResult(t *testing.T, rows int) *ops.BrowserBatchResult {
	t.Helper()
	snapshot, err := json.Marshal(map[string]string{"snapshot": strings.Repeat("- row \"order\" [ref=e2]\n", rows)})
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	return &ops.BrowserBatchResult{URL: "https://appdev.example.invalid", Steps: []ops.BrowserStepResult{{Command: []string{"snapshot"}, Success: true, Result: snapshot}}}
}

// TestBrowserToolResult_LongOutputsInARowKeepOneFileWithTheLatestResult: a
// long-lived Mate cuts many results; each one replaces the file the previous
// one wrote, so they never pile up, and fullResult holds the latest.
func TestBrowserToolResult_LongOutputsInARowKeepOneFileWithTheLatestResult(t *testing.T) {
	// Not parallel: points the temp dir at this test's own.
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	var shown struct {
		FullResult string `json:"fullResult"`
	}
	for _, rows := range []int{3000, 4000} {
		if err := json.Unmarshal([]byte(resultText(t, browserToolResult(longBrowserResult(t, rows)))), &shown); err != nil {
			t.Fatalf("the cut reply must stay JSON: %v", err)
		}
	}
	files, err := filepath.Glob(filepath.Join(tmp, "zerops-browser-*"))
	if err != nil || len(files) != 1 {
		t.Errorf("files after two cut results = %v (%v), want one", files, err)
	}
	whole, err := json.Marshal(longBrowserResult(t, 4000))
	if err != nil {
		t.Fatal(err)
	}
	if saved, err := os.ReadFile(shown.FullResult); err != nil || !bytes.Equal(saved, whole) {
		t.Errorf("fullResult %q must hold the latest result (%d chars): got %d chars, %v", shown.FullResult, len(whole), len(saved), err)
	}
}

// TestBrowserToolResult_ACutResultWhoseFileCannotBeWrittenNamesNoFile: when
// the whole result cannot be saved, the cut output says it was cut and points
// at no fullResult.
func TestBrowserToolResult_ACutResultWhoseFileCannotBeWrittenNamesNoFile(t *testing.T) {
	// Not parallel: points the temp dir at a folder that does not exist.
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "gone"))
	text := resultText(t, browserToolResult(longBrowserResult(t, 3000)))
	if strings.Contains(text, "fullResult") {
		t.Errorf("a result no file holds must not name fullResult: %.300s…%s", text, text[max(0, len(text)-200):])
	}
	if !strings.Contains(text, "… (cut)") {
		t.Errorf("the cut output must still say it was cut: …%s", text[max(0, len(text)-200):])
	}
}

// TestBrowserToolResult_ShortOutputStaysWhole: a result within the cap is the
// whole result, and names no file.
func TestBrowserToolResult_ShortOutputStaysWhole(t *testing.T) {
	result := &ops.BrowserBatchResult{URL: "https://example.com", ConsoleOutput: json.RawMessage(`{"messages":["ready"]}`), DurationMs: 9}
	whole, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	if text := resultText(t, browserToolResult(result)); text != string(whole) {
		t.Errorf("a short result must reach the model whole:\n got %s\nwant %s", text, whole)
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
// timing facts are the ones to report; find's --name is a substring match
// unless --exact; a refused command comes back as a failed step.
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
		"click, fill, check, find, get and is look once and fail at once on an element that is not there yet",
		`["is","checked","<sel>"]`,
		`["find","label","<text>","check"]`,
		"find's action follows its value and is only click, fill, check, hover or text; a role's name goes in --name after the action",
		"An @eN ref exists only after a snapshot earlier in the SAME call and dies with the call's tab",
		"invalid-command",
		"stale-ref",
		"script-timeout (your eval's promise never settled, e.g. awaiting a looping animation's finished; the page is alive)",
		`--name matches a substring ("Sign in" hits "Sign in with passkey"); --exact matches the whole name.`,
		`["eval","<js>"] (value as JSON text; only for what get/is/find cannot read, e.g. localStorage).`,
		"open, close, tab and window never run; each returns a failed invalid-command step.",
	} {
		if !strings.Contains(desc, want) {
			t.Errorf("description lacks %q", want)
		}
	}
	for _, gone := range []string{"for a first visit", `["click","@e1"]`} {
		if strings.Contains(desc, gone) {
			t.Errorf("description still says %q", gone)
		}
	}
	if prop := tl.Tools[0].InputSchema.(map[string]any)["properties"].(map[string]any)["fullPage"]; prop == nil {
		t.Error("the input schema has no fullPage option")
	}
}

// TestBrowserTool_SchemaSaysWhatTheCaptureAndTheViewportDo: a fullPage
// capture of a page whose map loads when scrolled into view came back blank
// below the fold, and an agent cannot know a leading set viewport is applied
// before the page loads, or that a call setting none loads at the default
// size and not at an earlier call's, unless the schema says so.
func TestBrowserTool_SchemaSaysWhatTheCaptureAndTheViewportDo(t *testing.T) {
	tl, err := browserSession(t).ListTools(t.Context(), &mcp.ListToolsParams{})
	if err != nil || len(tl.Tools) != 1 {
		t.Fatalf("list tools: %v %+v", err, tl)
	}
	props := tl.Tools[0].InputSchema.(map[string]any)["properties"].(map[string]any)
	for field, want := range map[string]string{
		"fullPage": "The capture does not scroll: content that loads when scrolled into view stays blank unless you scroll to it or wait for it first.",
		"commands": `A leading ["set","viewport","<w>","<h>"] runs before the open, so the page loads at that size; a call without one loads at 1280×577, the size a fresh Chrome opens at, whatever an earlier call set.`,
	} {
		prop, _ := props[field].(map[string]any)
		if desc, _ := prop["description"].(string); !strings.Contains(desc, want) {
			t.Errorf("%s description %q lacks %q", field, desc, want)
		}
	}
}
