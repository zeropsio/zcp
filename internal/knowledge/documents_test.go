package knowledge

import (
	"reflect"
	"testing"
)

func TestParseDocument_ReadsCategories(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		want    []string
	}{
		{
			"populated_categories",
			"---\ncategories: [hello-world-examples, framework-oss-examples]\n---\n\n# Title\n",
			[]string{"hello-world-examples", "framework-oss-examples"},
		},
		{
			"no_categories_key",
			"---\ndescription: \"test\"\n---\n\n# Title\n",
			nil,
		},
		{
			"no_frontmatter",
			"# Title\n",
			nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc := parseDocument("recipes/test.md", tt.content)
			if !reflect.DeepEqual(doc.Categories, tt.want) {
				t.Errorf("Categories = %#v, want %#v", doc.Categories, tt.want)
			}
		})
	}
}
