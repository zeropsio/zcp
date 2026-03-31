package sync

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEnvMatchByName(t *testing.T) {
	t.Parallel()

	envs := []environment{
		{Name: "AI Agent", Import: "import-dev"},
		{Name: "Development", Import: "import-plain"},
		{Name: "Small Production", Import: "import-prod"},
	}

	tests := []struct {
		name    string
		pattern string
		want    string
		wantNil bool
	}{
		{"finds_ai_agent", "AI Agent", "import-dev", false},
		{"finds_small_prod", "Small Production", "import-prod", false},
		{"partial_match", "Agent", "import-dev", false},
		{"no_match", "Nonexistent", "", true},
		{"empty_pattern", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := findEnvByName(envs, tt.pattern)
			if tt.wantNil {
				if got != nil {
					t.Errorf("expected nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("expected non-nil result")
			}
			if got.Import != tt.want {
				t.Errorf("got import %q, want %q", got.Import, tt.want)
			}
		})
	}
}

func TestPullRecipeMarkdown(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()

	sd := &sourceData{
		Environments: []environment{
			{
				Name:   "AI Agent",
				Import: "project:\n  name: test\nservices:\n  - hostname: app",
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
			{
				Name:   "Small Production",
				Import: "project:\n  name: test-prod",
			},
		},
	}

	md := buildRecipeMarkdown(cfg, "Bun Hello World", "bun-hello-world", sd)

	tests := []struct {
		name string
		want string
	}{
		{"has_frontmatter_desc", `description: "A great Bun app."`},
		{"has_frontmatter_repo", `repo: "https://github.com/zerops-recipe-apps/bun-hello-world-app"`},
		{"has_title", "# Bun Hello World on Zerops"},
		{"has_kb_promoted", "## Base Image"},
		{"has_gotchas_promoted", "## Gotchas"},
		{"has_service_defs", "## Service Definitions"},
		{"has_dev_import", "### Dev/Stage (from AI Agent environment)"},
		{"has_prod_import", "### Small Production"},
		{"has_dev_yaml", "hostname: app"},
		{"has_prod_yaml", "name: test-prod"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if !strings.Contains(md, tt.want) {
				t.Errorf("expected output to contain %q\ngot:\n%s", tt.want, md)
			}
		})
	}
}

func TestPullRecipeMarkdown_WithIntegrationGuide(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()

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

	md := buildRecipeMarkdown(cfg, "Test", "test", sd)

	if !strings.Contains(md, "## zerops.yml") {
		t.Error("expected promoted ## zerops.yml heading")
	}
	if !strings.Contains(md, "## Integration Steps") {
		t.Error("expected promoted ## Integration Steps heading")
	}
}

func TestPullRecipeMarkdown_FallbackYAML(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()

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

	md := buildRecipeMarkdown(cfg, "Fallback", "fallback", sd)

	if !strings.Contains(md, "## zerops.yml") {
		t.Error("expected ## zerops.yml section")
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

	cfg := DefaultConfig()
	sd := &sourceData{
		Environments: []environment{
			{Name: "AI Agent", Services: []service{}},
		},
	}

	md := buildRecipeMarkdown(cfg, "Empty", "empty", sd)
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
}
