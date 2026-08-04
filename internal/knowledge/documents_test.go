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

// TestParseDocument_ReadsGUISlug_DefaultsToSlug pins the fix for the
// dead-link defect (owner-reported 2026-08-04): `guiSlug:` frontmatter
// persists the recipe's original Strapi slug (which can differ from the
// corpus/file slug via `.sync.yaml`'s slug_remap). A doc with no `guiSlug:`
// frontmatter — e.g. the force-tracked mailpit recipe, which is never
// synced through the pull pipeline that writes it — falls back to the doc's
// own slug (the last path segment) so callers always get a usable value.
func TestParseDocument_ReadsGUISlug_DefaultsToSlug(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		path    string
		content string
		want    string
	}{
		{
			name:    "guiSlug_frontmatter_present_differs_from_file_slug",
			path:    "recipes/nodejs-hello-world.md",
			content: "---\nguiSlug: \"node-js-hello-world\"\n---\n\n# Title\n",
			want:    "node-js-hello-world",
		},
		{
			name:    "no_guiSlug_frontmatter_defaults_to_doc_slug",
			path:    "recipes/mailpit.md",
			content: "---\ndescription: \"Mailpit.\"\n---\n\n# Title\n",
			want:    "mailpit",
		},
		{
			name:    "no_frontmatter_at_all_defaults_to_doc_slug",
			path:    "recipes/mailpit.md",
			content: "# Title\n",
			want:    "mailpit",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc := parseDocument(tt.path, tt.content)
			if doc.GUISlug != tt.want {
				t.Errorf("GUISlug = %q, want %q", doc.GUISlug, tt.want)
			}
		})
	}
}
