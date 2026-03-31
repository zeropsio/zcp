package sync

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config holds all sync configuration loaded from .sync.yaml.
type Config struct {
	APIURL            string            `yaml:"api_url"`
	SlugRemap         map[string]string `yaml:"slug_remap"`
	Environments      EnvConfig         `yaml:"environments"`
	ExcludeCategories []string          `yaml:"exclude_categories"`
	Push              PushConfig        `yaml:"push"`
	Paths             PathsConfig       `yaml:"paths"`
}

// EnvConfig holds environment name patterns for matching API environments.
type EnvConfig struct {
	DevStage  string `yaml:"dev_stage"`
	SmallProd string `yaml:"small_prod"`
}

// PushConfig holds push target configuration.
type PushConfig struct {
	Recipes RecipePushConfig `yaml:"recipes"`
	Guides  GuidePushConfig  `yaml:"guides"`
}

// RecipePushConfig holds recipe push target configuration.
type RecipePushConfig struct {
	Org          string   `yaml:"org"`
	RepoPatterns []string `yaml:"repo_patterns"`
	BranchPrefix string   `yaml:"branch_prefix"`
	CommitPrefix string   `yaml:"commit_prefix"`
}

// GuidePushConfig holds guide push target configuration.
type GuidePushConfig struct {
	Repo         string `yaml:"repo"`
	Path         string `yaml:"path"`
	BranchPrefix string `yaml:"branch_prefix"`
	CommitPrefix string `yaml:"commit_prefix"`
}

// PathsConfig holds filesystem path configuration.
type PathsConfig struct {
	Output    string `yaml:"output"`
	DocsLocal string `yaml:"docs_local"`
}

// DefaultConfig returns the default configuration matching current bash behavior.
func DefaultConfig() *Config {
	return &Config{
		APIURL: "https://api.zerops.io/api/recipes",
		SlugRemap: map[string]string{
			"recipe": "nodejs-hello-world",
		},
		Environments: EnvConfig{
			DevStage:  "AI Agent",
			SmallProd: "Small Production",
		},
		ExcludeCategories: []string{"service-utility"},
		Push: PushConfig{
			Recipes: RecipePushConfig{
				Org:          "zerops-recipe-apps",
				RepoPatterns: []string{"{slug}-app", "{slug}"},
				BranchPrefix: "zcp",
				CommitPrefix: "chore(knowledge)",
			},
			Guides: GuidePushConfig{
				Repo:         "zeropsio/docs",
				Path:         "apps/docs/content/guides",
				BranchPrefix: "zcp",
				CommitPrefix: "chore(guides)",
			},
		},
		Paths: PathsConfig{
			Output: "internal/knowledge",
		},
	}
}

// LoadConfig reads .sync.yaml from root, falling back to DefaultConfig.
func LoadConfig(root string) (*Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(filepath.Join(root, ".sync.yaml"))
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("read .sync.yaml: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse .sync.yaml: %w", err)
	}

	cfg.Paths.Output = expandEnv(cfg.Paths.Output)
	cfg.Paths.DocsLocal = expandEnv(cfg.Paths.DocsLocal)

	// Fall back to DOCS_GUIDES env var if docs_local not set in config
	if cfg.Paths.DocsLocal == "" {
		cfg.Paths.DocsLocal = os.Getenv("DOCS_GUIDES")
	}

	return cfg, nil
}

// RemapSlug returns the remapped slug if one exists, otherwise the original.
func (c *Config) RemapSlug(slug string) string {
	if mapped, ok := c.SlugRemap[slug]; ok {
		return mapped
	}
	return slug
}

// envPattern matches ${VAR} or ${VAR:-default}.
var envPattern = regexp.MustCompile(`\$\{([^}:]+)(?::-([^}]*))?\}`)

// expandEnv expands ${VAR} and ${VAR:-default} patterns in a string.
func expandEnv(s string) string {
	if s == "" {
		return s
	}
	return envPattern.ReplaceAllStringFunc(s, func(match string) string {
		parts := envPattern.FindStringSubmatch(match)
		if parts == nil {
			return match
		}
		name := parts[1]
		defaultVal := parts[2]
		if val := os.Getenv(name); val != "" {
			return val
		}
		return defaultVal
	})
}

// ResolveRecipeRepo tries each repo pattern and returns the first that matches.
// Returns "" if no pattern resolves to an existing repo.
func (c *Config) ResolveRecipeRepo(slug string, checker RepoChecker) string {
	for _, pattern := range c.Push.Recipes.RepoPatterns {
		repo := c.Push.Recipes.Org + "/" + strings.ReplaceAll(pattern, "{slug}", slug)
		if checker.RepoExists(repo) {
			return repo
		}
	}
	return ""
}

// RepoChecker checks whether a GitHub repo exists.
type RepoChecker interface {
	RepoExists(repo string) bool
}
