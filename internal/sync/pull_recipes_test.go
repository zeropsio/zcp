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

	md := buildRecipeMarkdown("Bun Hello World", "bun-hello-world", sd, nil)

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

	md := buildRecipeMarkdown("Test", "test", sd, nil)

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

	md := buildRecipeMarkdown("Fallback", "fallback", sd, nil)

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

	md := buildRecipeMarkdown("Empty", "empty", sd, nil)
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

	md := buildRecipeMarkdown(recipe.Name, recipe.Slug, sd, recipe.RecipeLanguageFrameworks)
	if !strings.Contains(md, "languages: [php]") {
		t.Errorf("expected languages frontmatter, got:\n%s", md)
	}
	if !strings.Contains(md, "frameworks: [laravel]") {
		t.Errorf("expected frameworks frontmatter, got:\n%s", md)
	}
}
