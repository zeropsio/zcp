package sync

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig_Defaults(t *testing.T) {
	t.Parallel()

	cfg, err := LoadConfig("/nonexistent/path")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tests := []struct {
		name string
		got  string
		want string
	}{
		{"api_url", cfg.APIURL, "https://api.zerops.io/api/recipes"},
		{"output_path", cfg.Paths.Output, "internal/knowledge"},
		{"push_recipes_org", cfg.Push.Recipes.Org, "zerops-recipe-apps"},
		{"push_recipes_branch_prefix", cfg.Push.Recipes.BranchPrefix, "zcp"},
		{"push_recipes_commit_prefix", cfg.Push.Recipes.CommitPrefix, "chore(knowledge)"},
		{"push_guides_repo", cfg.Push.Guides.Repo, "zeropsio/docs"},
		{"push_guides_path", cfg.Push.Guides.Path, "apps/docs/content/guides"},
		{"env_dev_stage", cfg.Environments.DevStage, "AI Agent"},
		{"env_small_prod", cfg.Environments.SmallProd, "Small Production"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.got != tt.want {
				t.Errorf("got %q, want %q", tt.got, tt.want)
			}
		})
	}
}

func TestLoadConfig_FromYAML(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	yamlContent := `
api_url: https://custom.api/recipes
slug_remap:
  foo: bar
environments:
  dev_stage: "Custom Dev"
  small_prod: "Custom Prod"
exclude_categories:
  - test-cat
push:
  recipes:
    org: custom-org
    repo_patterns:
      - "{slug}-repo"
    branch_prefix: custom
    commit_prefix: "fix(kb)"
  guides:
    repo: custom/docs
    path: content/guides
    branch_prefix: docs
    commit_prefix: "fix(guides)"
paths:
  output: custom/output
`
	if err := os.WriteFile(filepath.Join(dir, ".sync.yaml"), []byte(yamlContent), 0644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}

	cfg, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tests := []struct {
		name string
		got  string
		want string
	}{
		{"api_url", cfg.APIURL, "https://custom.api/recipes"},
		{"slug_remap_foo", cfg.SlugRemap["foo"], "bar"},
		{"env_dev_stage", cfg.Environments.DevStage, "Custom Dev"},
		{"push_org", cfg.Push.Recipes.Org, "custom-org"},
		{"push_branch_prefix", cfg.Push.Recipes.BranchPrefix, "custom"},
		{"guides_repo", cfg.Push.Guides.Repo, "custom/docs"},
		{"output", cfg.Paths.Output, "custom/output"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.got != tt.want {
				t.Errorf("got %q, want %q", tt.got, tt.want)
			}
		})
	}
}

// TestLoadConfig_EnvExpansion uses t.Setenv — cannot be parallel.
func TestLoadConfig_EnvExpansion(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		envKey  string
		envVal  string
		want    string
	}{
		{"simple_var", "${HOME}", "HOME", "/users/test", "/users/test"},
		{"with_default_unset", "${MISSING_VAR_SYNC_TEST:-/fallback}", "", "", "/fallback"},
		{"with_default_set", "${MY_VAR_SYNC_TEST:-/fallback}", "MY_VAR_SYNC_TEST", "/override", "/override"},
		{"no_vars", "plain/path", "", "", "plain/path"},
		{"empty_string", "", "", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envKey != "" {
				t.Setenv(tt.envKey, tt.envVal)
			}
			got := expandEnv(tt.input)
			if got != tt.want {
				t.Errorf("expandEnv(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSlugRemap(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()

	tests := []struct {
		name string
		slug string
		want string
	}{
		{"remap_recipe", "recipe", "nodejs-hello-world"},
		{"passthrough", "bun-hello-world", "bun-hello-world"},
		{"unknown", "nonexistent", "nonexistent"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := cfg.RemapSlug(tt.slug)
			if got != tt.want {
				t.Errorf("RemapSlug(%q) = %q, want %q", tt.slug, got, tt.want)
			}
		})
	}
}

type mockRepoChecker struct {
	existing map[string]bool
}

func (m *mockRepoChecker) RepoExists(repo string) bool {
	return m.existing[repo]
}

func TestResolveRecipeRepo(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	checker := &mockRepoChecker{existing: map[string]bool{
		"zerops-recipe-apps/bun-hello-world-app": true,
		"zerops-recipe-apps/go-hello-world":      true,
	}}

	tests := []struct {
		name string
		slug string
		want string
	}{
		{"first_pattern_match", "bun-hello-world", "zerops-recipe-apps/bun-hello-world-app"},
		{"fallback_pattern", "go-hello-world", "zerops-recipe-apps/go-hello-world"},
		{"no_match", "nonexistent", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := cfg.ResolveRecipeRepo(tt.slug, checker)
			if got != tt.want {
				t.Errorf("ResolveRecipeRepo(%q) = %q, want %q", tt.slug, got, tt.want)
			}
		})
	}
}
