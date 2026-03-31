package sync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindLocalRecipes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, name := range []string{"bun-hello-world.md", "go-hello-world.md", "not-md.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("# test"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0755); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		filter string
		want   int
	}{
		{"all_recipes", "", 2},
		{"filtered", "bun-hello-world", 1},
		{"no_match", "nonexistent", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			slugs, err := findLocalRecipes(dir, tt.filter)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(slugs) != tt.want {
				t.Errorf("got %d slugs, want %d", len(slugs), tt.want)
			}
		})
	}
}

func TestExtractFragments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		content    string
		wantKB     bool
		wantIG     bool
		wantIntro  bool
		wantYAML   bool
		wantPush   bool
	}{
		{
			"full_recipe",
			"---\ndescription: \"A Bun app\"\n---\n\n# Bun on Zerops\n\n## Base Image\n\nIncludes Bun.\n\n## zerops.yml\n\n```yaml\nzerops:\n  - setup: prod\n```\n\n## Service Definitions\n",
			true, true, true, true, true,
		},
		{
			"kb_only",
			"# Title\n\n## Gotchas\n\n- Watch out\n",
			true, false, false, false, true,
		},
		{
			"intro_and_yaml_only",
			"---\ndescription: \"test\"\n---\n\n# Title\n\n## zerops.yml\n\n```yaml\nzerops:\n  - setup: prod\n```\n",
			false, true, true, true, true,
		},
		{
			"empty_recipe",
			"# Title\n",
			false, false, false, false, false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			frags := extractFragments(tt.content)
			if (frags.KnowledgeBase != "") != tt.wantKB {
				t.Errorf("KnowledgeBase: got %q, wantPresent=%v", frags.KnowledgeBase, tt.wantKB)
			}
			if (frags.IntegrationGuide != "") != tt.wantIG {
				t.Errorf("IntegrationGuide: got %q, wantPresent=%v", frags.IntegrationGuide, tt.wantIG)
			}
			if (frags.Intro != "") != tt.wantIntro {
				t.Errorf("Intro: got %q, wantPresent=%v", frags.Intro, tt.wantIntro)
			}
			if (frags.ZeropsYAML != "") != tt.wantYAML {
				t.Errorf("ZeropsYAML: got %q, wantPresent=%v", frags.ZeropsYAML, tt.wantYAML)
			}
			if frags.hasContent() != tt.wantPush {
				t.Errorf("hasContent() = %v, want %v", frags.hasContent(), tt.wantPush)
			}
		})
	}
}

func TestInjectAllFragments(t *testing.T) {
	t.Parallel()

	readme := `# App

<!-- #ZEROPS_EXTRACT_START:intro# -->
old intro
<!-- #ZEROPS_EXTRACT_END:intro# -->

## Integration Guide

<!-- #ZEROPS_EXTRACT_START:integration-guide# -->
old guide
<!-- #ZEROPS_EXTRACT_END:integration-guide# -->

<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->
old kb
<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->`

	frags := recipeFragments{
		Intro:            "New intro text",
		IntegrationGuide: "### 1. New guide",
		KnowledgeBase:    "### Base Image\n\nNew KB",
	}

	got := injectAllFragments(readme, frags)

	tests := []struct {
		name string
		want string
		gone string
	}{
		// Intro is NOT pushed back (lossy round-trip) — old intro preserved
		{"intro_preserved", "old intro", ""},
		{"new_guide", "### 1. New guide", "old guide"},
		{"new_kb", "### Base Image", "old kb"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if !strings.Contains(got, tt.want) {
				t.Errorf("expected %q in output", tt.want)
			}
			if tt.gone != "" && strings.Contains(got, tt.gone) {
				t.Errorf("expected %q to be replaced", tt.gone)
			}
		})
	}
}

func TestZeropsYAML_DerivedFromIntegrationGuide(t *testing.T) {
	t.Parallel()

	// The YAML in zerops.yaml must always come from the integration-guide section.
	// If someone edits the YAML block in ## zerops.yml, both the README
	// integration-guide markers AND zerops.yaml file get the same content.
	content := "---\ndescription: \"test\"\n---\n\n# App\n\n## Base Image\n\nContent\n\n## zerops.yml\n\n> Ref\n\n```yaml\nzerops:\n  - setup: prod\n    build:\n      base: bun@1.2\n```\n\n## Service Definitions\n"

	frags := extractFragments(content)

	// Integration guide must contain the YAML
	if !strings.Contains(frags.IntegrationGuide, "base: bun@1.2") {
		t.Errorf("IntegrationGuide missing YAML content:\n%s", frags.IntegrationGuide)
	}

	// ZeropsYAML must be exactly the code block from within the integration guide
	if frags.ZeropsYAML != "zerops:\n  - setup: prod\n    build:\n      base: bun@1.2" {
		t.Errorf("ZeropsYAML not derived from integration guide:\ngot:  %q", frags.ZeropsYAML)
	}

	// Invariant: editing the YAML in the recipe changes both outputs
	editedContent := strings.Replace(content, "base: bun@1.2", "base: bun@1.3", 1)
	editedFrags := extractFragments(editedContent)

	if !strings.Contains(editedFrags.IntegrationGuide, "base: bun@1.3") {
		t.Error("editing YAML did not update IntegrationGuide")
	}
	if !strings.Contains(editedFrags.ZeropsYAML, "base: bun@1.3") {
		t.Error("editing YAML did not update ZeropsYAML — single-source-of-truth broken")
	}
}

func TestExtractYAMLFromFragment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		fragment string
		want     string
	}{
		{
			"extracts_yaml_block",
			"### zerops.yml\n\n> Ref\n\n```yaml\nzerops:\n  - setup: prod\n```\n\nMore prose.",
			"zerops:\n  - setup: prod",
		},
		{
			"no_yaml_block",
			"### Section\n\nJust prose, no code blocks.",
			"",
		},
		{
			"multiple_blocks_takes_first",
			"```yaml\nfirst: block\n```\n\n```yaml\nsecond: block\n```",
			"first: block",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := extractYAMLFromFragment(tt.fragment)
			if got != tt.want {
				t.Errorf("extractYAMLFromFragment():\ngot:  %q\nwant: %q", got, tt.want)
			}
		})
	}
}

func TestResolveRepo_FromFrontmatter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			"github_url",
			"---\nrepo: \"https://github.com/zerops-recipe-apps/bun-hello-world-app\"\n---\n",
			"zerops-recipe-apps/bun-hello-world-app",
		},
		{
			"strips_git_suffix",
			"---\nrepo: \"https://github.com/org/repo.git\"\n---\n",
			"org/repo",
		},
		{
			"no_repo_falls_back",
			"---\ndescription: \"test\"\n---\n",
			"", // pattern fallback will also fail (no gh CLI in tests)
		},
	}

	cfg := DefaultConfig()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := resolveRepo(tt.content, cfg, "test-slug")
			if got != tt.want {
				t.Errorf("resolveRepo() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPushOneRecipe_NoPushableContent(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	recipesDir := filepath.Join(root, "internal", "knowledge", "recipes")
	if err := os.MkdirAll(recipesDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Recipe with only a title — nothing pushable
	content := "# Title\n"
	if err := os.WriteFile(filepath.Join(recipesDir, "empty.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := DefaultConfig()
	result := pushOneRecipe(cfg, root, "empty", true)

	if result.Status != Skipped {
		t.Errorf("expected Skipped, got %v", result.Status)
	}
	if result.Reason != "no pushable content" {
		t.Errorf("unexpected reason: %s", result.Reason)
	}
}

func TestPushOneRecipe_WithContent(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	recipesDir := filepath.Join(root, "internal", "knowledge", "recipes")
	if err := os.MkdirAll(recipesDir, 0755); err != nil {
		t.Fatal(err)
	}

	content := "---\ndescription: \"test\"\n---\n\n# Bun on Zerops\n\n## Base Image\n\nIncludes Bun.\n\n## zerops.yml\n\n```yaml\nzerops:\n  - setup: prod\n```\n"
	if err := os.WriteFile(filepath.Join(recipesDir, "bun-test.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := DefaultConfig()
	// No repo in frontmatter and no gh CLI → Skipped
	result := pushOneRecipe(cfg, root, "bun-test", true)

	if result.Status != Skipped {
		t.Errorf("expected Skipped (no repo), got %v", result.Status)
	}
	if !strings.Contains(result.Reason, "no repo") {
		t.Errorf("unexpected reason: %s", result.Reason)
	}
}

func TestDryRun_NoSideEffects(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	recipesDir := filepath.Join(root, "internal", "knowledge", "recipes")
	if err := os.MkdirAll(recipesDir, 0755); err != nil {
		t.Fatal(err)
	}

	content := "# Title\n\n## Section\n\nContent\n"
	if err := os.WriteFile(filepath.Join(recipesDir, "test.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := DefaultConfig()
	results, err := PushRecipes(cfg, root, "", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, r := range results {
		if r.Status == Created || r.Status == Updated {
			t.Errorf("dry-run should not create/update, got %v for %s", r.Status, r.Slug)
		}
	}
}
