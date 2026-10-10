package tools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
)

func TestPublishPageResult_NamesTheFileTheMateTakes(t *testing.T) {
	t.Parallel()
	page := &ops.Page{ID: "page-0123456789abcdef", Title: "Plan", File: "/var/www/.zcp/state/pages/page-0123456789abcdef.html", Bytes: 42}
	tests := []struct {
		name      string
		canCheck  bool
		wantCheck string
	}{
		{name: "with a browser to look at it", canCheck: true, wantCheck: `zerops_browser url="file:///var/www/.zcp/state/pages/page-0123456789abcdef.html" screenshot=true`},
		{name: "without one", canCheck: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tr := jsonResult(publishPageResult(page, tt.canCheck))
			if tr.IsError || tr.StructuredContent != nil || len(tr.Content) != 1 {
				t.Fatalf("result = %+v, want one text block", tr)
			}
			var got struct {
				Page    ops.Page `json:"page"`
				Check   string   `json:"check"`
				Message string   `json:"message"`
			}
			if err := json.Unmarshal([]byte(tr.Content[0].(*mcp.TextContent).Text), &got); err != nil {
				t.Fatalf("result text is not JSON: %v", err)
			}
			if got.Page.File != page.File || got.Page.ID != page.ID || got.Page.Title != "Plan" || got.Page.Bytes != 42 {
				t.Errorf("page = %+v, want %+v", got.Page, page)
			}
			if !strings.Contains(got.Check, tt.wantCheck) || (tt.wantCheck == "") != (got.Check == "") {
				t.Errorf("check = %q, want %q", got.Check, tt.wantCheck)
			}
			if !strings.Contains(got.Message, "above your final reply") {
				t.Errorf("message = %q", got.Message)
			}
		})
	}
}
