package sync

import (
	"strings"
	"testing"
)

func TestExtractKnowledgeBase(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			"full_recipe",
			"---\ndescription: \"A Bun app\"\n---\n\n# Bun Hello World on Zerops\n\n## Base Image\n\nIncludes: Bun.\n\n## Gotchas\n\n- Watch out\n\n## zerops.yml\n\n```yaml\nzerops:\n  - setup: prod\n```\n",
			"### Base Image\n\nIncludes: Bun.\n\n### Gotchas\n\n- Watch out",
		},
		{
			"no_frontmatter",
			"# Title\n\n## Section\n\nContent here\n\n## zerops.yml\n\n```yaml\nfoo: bar\n```\n",
			"### Section\n\nContent here",
		},
		{
			"empty_content",
			"---\ndescription: \"test\"\n---\n\n# Title\n\n## zerops.yml\n\n```yaml\nfoo: bar\n```\n",
			"",
		},
		{
			"stops_at_ig_boundary",
			"# Title\n\n## Base\n\nContent\n\n## zerops.yml\n\n```yaml\nfoo: bar\n```\n",
			"### Base\n\nContent",
		},
		{
			"no_zerops_yml",
			"---\ndescription: \"test\"\n---\n\n# Title\n\n## Section A\n\nFoo\n\n## Section B\n\nBar\n",
			"### Section A\n\nFoo\n\n### Section B\n\nBar",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ExtractKnowledgeBase(tt.input)
			if got != tt.want {
				t.Errorf("ExtractKnowledgeBase():\ngot:  %q\nwant: %q", got, tt.want)
			}
		})
	}
}

func TestInjectFragment_Existing(t *testing.T) {
	t.Parallel()

	readme := `# My App

Some intro

<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->
old content here
more old content
<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->

## Footer`

	got := InjectFragment(readme, "knowledge-base", "new fragment content")

	if !strings.Contains(got, "new fragment content") {
		t.Error("expected new fragment content in output")
	}
	if strings.Contains(got, "old content here") {
		t.Error("expected old content to be replaced")
	}
	if !strings.Contains(got, "## Footer") {
		t.Error("expected footer preserved")
	}
	if !strings.Contains(got, "ZEROPS_EXTRACT_START:knowledge-base") {
		t.Error("expected start marker preserved")
	}
}

func TestInjectFragment_New(t *testing.T) {
	t.Parallel()

	readme := "# My App\n\nSome content"
	got := InjectFragment(readme, "knowledge-base", "injected fragment")

	if !strings.Contains(got, "ZEROPS_EXTRACT_START:knowledge-base") {
		t.Error("expected start marker appended")
	}
	if !strings.Contains(got, "injected fragment") {
		t.Error("expected fragment in output")
	}
	if !strings.Contains(got, "ZEROPS_EXTRACT_END:knowledge-base") {
		t.Error("expected end marker appended")
	}
}

func TestExtractZeropsYAML(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			"extracts_yaml_block",
			"## Base\n\nContent\n\n## zerops.yml\n\n> Reference\n\n```yaml\nzerops:\n  - setup: prod\n```\n\n## Other\n",
			"zerops:\n  - setup: prod",
		},
		{
			"no_zerops_section",
			"## Base\n\nContent\n",
			"",
		},
		{
			"zerops_yaml_variant",
			"## zerops.yaml\n\n```yaml\nzerops:\n  - setup: dev\n```\n",
			"zerops:\n  - setup: dev",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ExtractZeropsYAML(tt.input)
			if got != tt.want {
				t.Errorf("ExtractZeropsYAML():\ngot:  %q\nwant: %q", got, tt.want)
			}
		})
	}
}

func TestExtractIntegrationGuide(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			"full_recipe_with_ig",
			"---\ndescription: \"test\"\n---\n\n# Title\n\n## Base Image\n\nContent\n\n## zerops.yml\n\n> Reference\n\n```yaml\nzerops:\n  - setup: prod\n```\n",
			"### zerops.yml\n\n> Reference\n\n```yaml\nzerops:\n  - setup: prod\n```",
		},
		{
			"no_zerops_section",
			"# Title\n\n## Base Image\n\nContent\n",
			"",
		},
		{
			"ig_with_prose_sections",
			"# Title\n\n## zerops.yml\n\n### 1. Adding zerops.yaml\n\nContent\n\n```yaml\nzerops:\n  - setup: prod\n```\n\n## Extra Steps\n\nDo this too\n",
			"### zerops.yml\n\n### 1. Adding zerops.yaml\n\nContent\n\n```yaml\nzerops:\n  - setup: prod\n```\n\n### Extra Steps\n\nDo this too",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ExtractIntegrationGuide(tt.input)
			if got != tt.want {
				t.Errorf("ExtractIntegrationGuide():\ngot:  %q\nwant: %q", got, tt.want)
			}
		})
	}
}

func TestExtractIntro(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			"with_frontmatter",
			"---\ndescription: \"A Bun app on Zerops\"\n---\n\n# Title\n",
			"A Bun app on Zerops",
		},
		{
			"no_frontmatter",
			"# Title\n\n## Section\n",
			"",
		},
		{
			"single_quoted",
			"---\ndescription: 'Single quoted'\n---\n",
			"Single quoted",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ExtractIntro(tt.input)
			if got != tt.want {
				t.Errorf("ExtractIntro(%q) = %q, want %q", tt.input[:min(len(tt.input), 40)], got, tt.want)
			}
		})
	}
}

func TestExtractRepo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			"with_repo",
			"---\ndescription: \"test\"\nrepo: \"https://github.com/zerops-recipe-apps/bun-hello-world-app\"\n---\n",
			"https://github.com/zerops-recipe-apps/bun-hello-world-app",
		},
		{
			"no_repo",
			"---\ndescription: \"test\"\n---\n",
			"",
		},
		{
			"no_frontmatter",
			"# Title\n",
			"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ExtractRepo(tt.input)
			if got != tt.want {
				t.Errorf("ExtractRepo() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestConvertMDXToGuide(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			"strips_frontmatter_and_imports",
			"---\ntitle: My Guide\ndescription: \"A guide\"\n---\n\nimport Foo from './Foo'\n\n## Section\n\nContent with `zerops://recipes/bun{version}` link\n",
			"# My Guide\n\n## Section\n\nContent with zerops://recipes/bun{version} link\n",
		},
		{
			"no_frontmatter",
			"## Section\n\nPlain content\n",
			"## Section\n\nPlain content\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ConvertMDXToGuide(tt.input)
			if got != tt.want {
				t.Errorf("ConvertMDXToGuide():\ngot:  %q\nwant: %q", got, tt.want)
			}
		})
	}
}

func TestConvertGuideToMDX(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		guide       string
		existingMDX string
		wantParts   []string
	}{
		{
			"new_mdx_generates_frontmatter",
			"# My Guide\n\n## TL;DR\n\nShort summary\n\n## Section\n\nContent with zerops://recipes/bun{version} link\n",
			"",
			[]string{
				"---\ntitle: My Guide",
				`description: "Short summary"`,
				"---",
				"`zerops://recipes/bun{version}`",
			},
		},
		{
			"preserves_existing_frontmatter",
			"# My Guide\n\n## Section\n\nContent\n",
			"---\ntitle: Existing Title\ncustom: field\n---\n\nOld body\n",
			[]string{
				"---\ntitle: Existing Title\ncustom: field\n---",
				"## Section",
				"Content",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ConvertGuideToMDX(tt.guide, tt.existingMDX)
			for _, part := range tt.wantParts {
				if !strings.Contains(got, part) {
					t.Errorf("ConvertGuideToMDX() missing %q\ngot: %q", part, got)
				}
			}
		})
	}
}
