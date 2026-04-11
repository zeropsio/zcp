package workflow

import (
	"reflect"
	"testing"
)

// TestExtractBlocks drives the <block name="..."> parser through the cases
// the recipe delivery refactor relies on: verbatim passthrough (no blocks),
// preamble + children, children with embedded code fences, and empty input.
func TestExtractBlocks(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want []Block
	}{
		{
			name: "empty body returns nil",
			in:   "",
			want: nil,
		},
		{
			name: "no blocks — whole body is a single preamble block",
			in:   "## Heading\n\nBody text.",
			want: []Block{{Name: "", Body: "## Heading\n\nBody text."}},
		},
		{
			name: "single block, no preamble",
			in: `<block name="only">
### Heading
body
</block>`,
			want: []Block{{Name: "only", Body: "### Heading\nbody"}},
		},
		{
			name: "preamble + two blocks",
			in: `## Heading

Preamble line.

<block name="a">
### Subheading A
body A
</block>

<block name="b">
### Subheading B
body B
</block>`,
			want: []Block{
				{Name: "", Body: "## Heading\n\nPreamble line."},
				{Name: "a", Body: "### Subheading A\nbody A"},
				{Name: "b", Body: "### Subheading B\nbody B"},
			},
		},
		{
			name: "block with embedded yaml code fence",
			in: "<block name=\"yaml-example\">\n" +
				"```yaml\n" +
				"foo: bar\n" +
				"```\n" +
				"</block>",
			want: []Block{{
				Name: "yaml-example",
				Body: "```yaml\nfoo: bar\n```",
			}},
		},
		{
			name: "missing close tag — body extends to EOF",
			in: `<block name="unclosed">
hello
world`,
			want: []Block{{Name: "unclosed", Body: "hello\nworld"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ExtractBlocks(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("\ngot:  %#v\nwant: %#v", got, tt.want)
			}
		})
	}
}
