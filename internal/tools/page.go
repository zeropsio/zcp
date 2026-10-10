package tools

import (
	"context"
	"net/url"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
)

// PublishPageInput is what zerops_publish_page takes.
type PublishPageInput struct {
	Title string `json:"title"          jsonschema:"What the page shows, in a few words. Required."`
	HTML  string `json:"html,omitempty" jsonschema:"The whole page as one self-contained HTML document. Give this or path."`
	Path  string `json:"path,omitempty" jsonschema:"An .html file in the project, relative to it or absolute. Give this or html."`
}

// publishedPage is the tool's result: the page, where the Mate server takes
// it from (page.file, docs/spec-mate.md §5.9), and what the agent does next.
type publishedPage struct {
	Page    *ops.Page `json:"page"`
	Check   string    `json:"check,omitempty"`
	Message string    `json:"message"`
}

const publishedPageMessage = "Published: the person sees the page above your final reply. " +
	"Don't restate or announce it there; add only what it doesn't say."

// RegisterPublishPage registers zerops_publish_page. Mate-gated by the
// server: only a Mate's conversation draws a page. Pages are kept under
// stateDir; a relative path resolves in cwd. canCheck says zerops_browser is
// there to look at a page before the person does.
func RegisterPublishPage(srv *mcp.Server, stateDir, cwd string, canCheck bool) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "zerops_publish_page",
		Description: "Show the person a self-contained HTML page — a chart, comparison, plan or gallery — inline " +
			"in the conversation, above your final reply. Pass title and html, or path to an .html file. Local " +
			"pictures are inlined; the page cannot load anything from the network, so inline scripts and styles. " +
			"Call it before your final reply.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Publish a page to the conversation",
			IdempotentHint:  true,
			DestructiveHint: boolPtr(false),
			OpenWorldHint:   boolPtr(false),
		},
	}, func(_ context.Context, _ *mcp.CallToolRequest, in PublishPageInput) (*mcp.CallToolResult, any, error) {
		page, err := ops.PublishPage(stateDir, cwd, ops.PageInput{Title: in.Title, HTML: in.HTML, Path: in.Path})
		if err != nil {
			return convertError(err), nil, nil
		}
		return jsonResult(publishPageResult(page, canCheck)), nil, nil
	})
}

// publishPageResult is the result for page; with canCheck it says how to
// look at the page as the person will.
func publishPageResult(page *ops.Page, canCheck bool) publishedPage {
	result := publishedPage{Page: page, Message: publishedPageMessage}
	if canCheck {
		file := url.URL{Scheme: "file", Path: page.File}
		result.Check = `To look at it first: zerops_browser url="` + file.String() + `" screenshot=true.`
	}
	return result
}
