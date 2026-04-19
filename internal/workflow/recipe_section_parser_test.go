package workflow

import (
	"strings"
	"testing"
)

func TestExtractSection_Basic(t *testing.T) {
	t.Parallel()
	md := `some preamble
<section name="foo">
Content of foo section.
</section>
<section name="bar">
Content of bar section.
</section>`

	tests := []struct {
		name      string
		section   string
		wantSub   string
		wantEmpty bool
	}{
		{"foo_found", "foo", "Content of foo section.", false},
		{"bar_found", "bar", "Content of bar section.", false},
		{"missing_returns_empty", "baz", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := extractSection(md, tt.section)
			if tt.wantEmpty && got != "" {
				t.Errorf("extractSection(%q) = %q, want empty", tt.section, got)
			}
			if !tt.wantEmpty && !strings.Contains(got, tt.wantSub) {
				t.Errorf("extractSection(%q) missing %q, got %q", tt.section, tt.wantSub, got)
			}
		})
	}
}

func TestExtractSection_WithHashesInCodeBlocks(t *testing.T) {
	t.Parallel()
	md := `preamble
<section name="test">
Some intro text.

` + "````" + `
## This is a heading inside a code block
### Another heading
` + "````" + `

More text after code block.
</section>
trailing`

	got := extractSection(md, "test")
	if !strings.Contains(got, "## This is a heading inside a code block") {
		t.Error("extractSection lost content with # inside code blocks")
	}
	if !strings.Contains(got, "More text after code block.") {
		t.Error("extractSection truncated content after code block")
	}
}
