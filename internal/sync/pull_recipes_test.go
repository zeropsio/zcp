package sync

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPullRecipeMarkdown(t *testing.T) {
	t.Parallel()

	sd := &sourceData{
		Environments: []environment{
			{
				Name: "AI Agent",
				Services: []service{
					{
						GitRepo: "https://github.com/zerops-recipe-apps/bun-hello-world-app",
						Extracts: extracts{
							Intro:         "A [great](http://example.com) Bun app.",
							KnowledgeBase: "### Base Image\n\nIncludes: Bun.\n\n### Gotchas\n\n- Watch out",
						},
						ZeropsYaml: "zerops:\n  - setup: prod",
					},
				},
			},
			{Name: "Development"},
			{Name: "Stage"},
			{Name: "Production"},
			{Name: "Small Production"},
		},
	}

	md := buildRecipeMarkdown("Bun Hello World", "bun-hello-world", "bun-hello-world", sd, nil, nil)

	tests := []struct {
		name string
		want string
	}{
		{"has_frontmatter_desc", `description: "A great Bun app."`},
		{"has_frontmatter_repo", `repo: "https://github.com/zerops-recipe-apps/bun-hello-world-app"`},
		{"has_title", "# Bun Hello World on Zerops"},
		{"has_kb_promoted", "## Base Image"},
		{"has_gotchas_promoted", "## Gotchas"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if !strings.Contains(md, tt.want) {
				t.Errorf("expected output to contain %q\ngot:\n%s", tt.want, md)
			}
		})
	}

	t.Run("no_service_definitions", func(t *testing.T) {
		t.Parallel()
		if strings.Contains(md, "## Service Definitions") {
			t.Error("output should not contain service definitions")
		}
	})
}

func TestPullRecipeMarkdown_WithIntegrationGuide(t *testing.T) {
	t.Parallel()

	sd := &sourceData{
		Environments: []environment{
			{
				Name: "AI Agent",
				Services: []service{
					{
						Extracts: extracts{
							IntegrationGuide: "### zerops.yml\n\n```yaml\nzerops:\n  - setup: prod\n```\n\n### Integration Steps\n\n1. Do this",
						},
					},
				},
			},
		},
	}

	md := buildRecipeMarkdown("Test", "test", "test", sd, nil, nil)

	if !strings.Contains(md, "## zerops.yml") && !strings.Contains(md, "## zerops.yaml") {
		t.Error("expected promoted ## zerops.yaml or ## zerops.yml heading")
	}
	if !strings.Contains(md, "## Integration Steps") {
		t.Error("expected promoted ## Integration Steps heading")
	}
}

func TestPullRecipeMarkdown_FallbackYAML(t *testing.T) {
	t.Parallel()

	sd := &sourceData{
		Environments: []environment{
			{
				Name: "AI Agent",
				Services: []service{
					{
						ZeropsYaml: "zerops:\n  - setup: prod\n    build:\n      base: bun@1",
					},
				},
			},
		},
	}

	md := buildRecipeMarkdown("Fallback", "fallback", "fallback", sd, nil, nil)

	if !strings.Contains(md, "## zerops.yaml") {
		t.Error("expected ## zerops.yaml section")
	}
	if !strings.Contains(md, "```yaml") {
		t.Error("expected yaml code block")
	}
	if !strings.Contains(md, "base: bun@1") {
		t.Error("expected yaml content")
	}
}

func TestPullRecipeMarkdown_EmptySkipped(t *testing.T) {
	t.Parallel()

	sd := &sourceData{
		Environments: []environment{
			{Name: "AI Agent", Services: []service{}},
		},
	}

	md := buildRecipeMarkdown("Empty", "empty", "empty", sd, nil, nil)
	if md != "" {
		t.Errorf("expected empty markdown for recipe with no content, got: %q", md)
	}
}

func TestBuildRecipeMarkdown_SourceDataJSON(t *testing.T) {
	t.Parallel()

	// Verify JSON unmarshaling works for the sourceData structure
	raw := `{
		"environments": [
			{
				"name": "AI Agent",
				"import": "test-import",
				"services": [
					{
						"isUtility": false,
						"category": "APP",
						"zeropsYaml": "test-yaml",
						"extracts": {
							"intro": "intro text",
							"knowledge-base": "kb content",
							"integration-guide": ""
						}
					}
				]
			}
		],
		"extracts": {"intro": "fallback intro"}
	}`

	var sd sourceData
	if err := json.Unmarshal([]byte(raw), &sd); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(sd.Environments) != 1 {
		t.Fatalf("expected 1 environment, got %d", len(sd.Environments))
	}
	if sd.Environments[0].Services[0].Extracts.KnowledgeBase != "kb content" {
		t.Error("expected knowledge-base extract")
	}
	if sd.Environments[0].Import != "test-import" {
		t.Errorf("expected Import=%q, got %q", "test-import", sd.Environments[0].Import)
	}
}

func TestFindAgentImportYAML(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		envs []environment
		want string
	}{
		{
			name: "picks_first_env_with_agent_name",
			envs: []environment{
				{Name: "0 — AI Agent", Import: "agent-yaml"},
				{Name: "1 — Remote", Import: "remote-yaml"},
			},
			want: "agent-yaml",
		},
		{
			name: "fallback_to_first_env_when_no_agent_name",
			envs: []environment{
				{Name: "Development", Import: "dev-yaml"},
			},
			want: "dev-yaml",
		},
		{
			name: "empty_when_no_envs",
			envs: nil,
			want: "",
		},
		{
			name: "empty_when_no_import",
			envs: []environment{{Name: "AI Agent"}},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := findAgentImportYAML(&sourceData{Environments: tt.envs})
			if got != tt.want {
				t.Errorf("findAgentImportYAML: want %q, got %q", tt.want, got)
			}
		})
	}
}

func TestBuildRecipeMarkdown_FrontmatterTaxonomy(t *testing.T) {
	t.Parallel()

	recipe := APIRecipe{
		Name: "Laravel Minimal",
		Slug: "laravel-minimal",
		RecipeLanguageFrameworks: []apiLanguageFramework{
			{Slug: "php", Type: "language"},
			{Slug: "laravel", Type: "framework"},
		},
	}
	sd := &sourceData{
		Environments: []environment{
			{
				Name: "0 — AI Agent",
				Services: []service{
					{
						GitRepo:  "https://github.com/zerops-recipe-apps/laravel-minimal-app",
						Extracts: extracts{Intro: "Laravel minimal."},
					},
				},
			},
		},
	}

	md := buildRecipeMarkdown(recipe.Name, recipe.Slug, recipe.Slug, sd, recipe.RecipeLanguageFrameworks, nil)
	if !strings.Contains(md, "languages: [php]") {
		t.Errorf("expected languages frontmatter, got:\n%s", md)
	}
	if !strings.Contains(md, "frameworks: [laravel]") {
		t.Errorf("expected frameworks frontmatter, got:\n%s", md)
	}
}

func TestBuildRecipeMarkdown_EmitsCategories(t *testing.T) {
	t.Parallel()

	sd := &sourceData{
		Environments: []environment{
			{
				Name: "0 — AI Agent",
				Services: []service{
					{Extracts: extracts{Intro: "A recipe."}},
				},
			},
		},
	}

	// Literal category slugs from the live payload shape captured 2026-08-03.
	categories := []apiCategory{
		{Slug: "hello-world-examples"},
		{Slug: "framework-oss-examples"},
	}

	md := buildRecipeMarkdown("Bun Hello World", "bun-hello-world", "bun-hello-world", sd, nil, categories)
	if !strings.Contains(md, "categories: [hello-world-examples, framework-oss-examples]") {
		t.Errorf("expected categories frontmatter, got:\n%s", md)
	}
}

func TestBuildRecipeMarkdown_TakeoverGuide_EmittedOnlyWhenNonEmpty(t *testing.T) {
	t.Parallel()

	// Sample from the wordpress recipe's takeover-guide extract, captured
	// 2026-08-03 — independent oracle, not read back from the implementation.
	takeoverBody := "Update WordPress core, plugins, and themes regularly. Back up the\n" +
		"database and `wp-content/uploads` before major upgrades."

	tests := []struct {
		name          string
		takeoverGuide string
		wantSection   bool
	}{
		{"populated_extract", takeoverBody, true},
		{"empty_extract", "", false},
		{"whitespace_only_extract", "   \n\t\n  ", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sd := &sourceData{
				Environments: []environment{
					{
						Name:     "0 — AI Agent",
						Services: []service{{Extracts: extracts{Intro: "A recipe."}}},
					},
				},
				Extracts: extracts{TakeoverGuide: tt.takeoverGuide},
			}

			md := buildRecipeMarkdown("WordPress", "wordpress", "wordpress", sd, nil, nil)

			hasSection := strings.Contains(md, "## Take ownership")
			if hasSection != tt.wantSection {
				t.Errorf("## Take ownership present = %v, want %v\ngot:\n%s", hasSection, tt.wantSection, md)
			}
			if tt.wantSection && !strings.Contains(md, takeoverBody) {
				t.Errorf("expected takeover body verbatim in output, got:\n%s", md)
			}
		})
	}
}

func TestBuildRecipeMarkdown_NoCategories_OmitsFrontmatterKey(t *testing.T) {
	t.Parallel()

	sd := &sourceData{
		Environments: []environment{
			{
				Name:     "0 — AI Agent",
				Services: []service{{Extracts: extracts{Intro: "A recipe."}}},
			},
		},
	}

	md := buildRecipeMarkdown("Test", "test", "test", sd, nil, nil)
	if strings.Contains(md, "categories:") {
		t.Errorf("expected no categories key when the recipe has none, got:\n%s", md)
	}
}

// TestBuildRecipeMarkdown_EmitsGUISlug pins the fix for the dead-link defect
// (owner-reported 2026-08-04): the GUI recipe detail route matches Strapi's
// ORIGINAL slug, while `.sync.yaml`'s slug_remap can rename the corpus slug
// the file is written under (node-js-hello-world -> nodejs-hello-world).
// `guiSlug:` frontmatter persists the original Strapi slug independent of
// any remap, so downstream renderers never need to re-derive it.
//
// Independent oracle: "node-js-hello-world" is the live Strapi slug from
// .sync.yaml's slug_remap table + the owner's reported working URL — not
// recomputed via cfg.RemapSlug/StrapiSlugFor.
func TestBuildRecipeMarkdown_EmitsGUISlug(t *testing.T) {
	t.Parallel()

	sd := &sourceData{
		Environments: []environment{
			{
				Name:     "0 — AI Agent",
				Services: []service{{Extracts: extracts{Intro: "A recipe."}}},
			},
		},
	}

	tests := []struct {
		name     string
		slug     string
		guiSlug  string
		wantLine string
	}{
		{
			name:     "remapped_slug_persists_original_strapi_slug",
			slug:     "nodejs-hello-world",
			guiSlug:  "node-js-hello-world",
			wantLine: `guiSlug: "node-js-hello-world"`,
		},
		{
			name:     "non_remapped_slug_equals_corpus_slug",
			slug:     "bun-hello-world",
			guiSlug:  "bun-hello-world",
			wantLine: `guiSlug: "bun-hello-world"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			md := buildRecipeMarkdown("Test", tt.slug, tt.guiSlug, sd, nil, nil)
			if !strings.Contains(md, tt.wantLine) {
				t.Errorf("expected frontmatter %q, got:\n%s", tt.wantLine, md)
			}
		})
	}
}

func TestBuildRecipeMarkdown_ByteIdempotent(t *testing.T) {
	t.Parallel()

	recipe := APIRecipe{
		Name: "Bun Hello World",
		Slug: "bun-hello-world",
		RecipeLanguageFrameworks: []apiLanguageFramework{
			{Slug: "bun", Type: "language"},
		},
		RecipeCategories: []apiCategory{
			{Slug: "hello-world-examples"},
			{Slug: "framework-oss-examples"},
		},
	}
	sd := &sourceData{
		Environments: []environment{
			{
				Name: "0 — AI Agent",
				Services: []service{
					{
						GitRepo: "https://github.com/zerops-recipe-apps/bun-hello-world-app",
						Extracts: extracts{
							Intro:            "A [great](http://example.com) Bun app.",
							KnowledgeBase:    "### Base Image\n\nIncludes: Bun.",
							IntegrationGuide: "### zerops.yml\n\n```yaml\nzerops:\n  - setup: prod\n```",
						},
					},
				},
			},
		},
		Extracts: extracts{TakeoverGuide: "Keep dependencies patched."},
	}

	first := buildRecipeMarkdown(recipe.Name, recipe.Slug, recipe.Slug, sd, recipe.RecipeLanguageFrameworks, recipe.RecipeCategories)
	second := buildRecipeMarkdown(recipe.Name, recipe.Slug, recipe.Slug, sd, recipe.RecipeLanguageFrameworks, recipe.RecipeCategories)

	if first != second {
		t.Errorf("two pulls of the same payload must be byte-identical:\nfirst:\n%q\nsecond:\n%q", first, second)
	}
}
