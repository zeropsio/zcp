package sync

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// PushRecipes pushes local recipe knowledge to GitHub app repos as PRs.
func PushRecipes(cfg *Config, root, filter string, dryRun bool) ([]PushResult, error) {
	recipesDir := filepath.Join(root, cfg.Paths.Output, "recipes")
	recipes, err := findLocalRecipes(recipesDir, filter)
	if err != nil {
		return nil, err
	}

	var results []PushResult
	for _, slug := range recipes {
		result := pushOneRecipe(cfg, root, slug, dryRun)
		results = append(results, result)
	}
	return results, nil
}

func findLocalRecipes(dir, filter string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read recipes dir %s: %w", dir, err)
	}

	var slugs []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		slug := strings.TrimSuffix(entry.Name(), ".md")
		if filter != "" && slug != filter {
			continue
		}
		slugs = append(slugs, slug)
	}
	return slugs, nil
}

// recipeFragments holds all extracted fragments from a recipe .md file.
// ZeropsYAML is always derived from IntegrationGuide — they share the same
// YAML code block. Editing the YAML in the integration-guide section
// automatically updates both the README markers and the zerops.yaml file.
type recipeFragments struct {
	KnowledgeBase    string
	IntegrationGuide string
	Intro            string
	ZeropsYAML       string // derived from IntegrationGuide, never independent
}

func extractFragments(content string) recipeFragments {
	ig := ExtractIntegrationGuide(content)

	// ZeropsYAML is the YAML code block WITHIN the integration-guide.
	// Single source of truth: edit the YAML in the ## zerops.yaml section,
	// and both the README integration-guide markers AND zerops.yaml file update.
	var yaml string
	if ig != "" {
		yaml = extractYAMLFromFragment(ig)
	}

	return recipeFragments{
		KnowledgeBase:    ExtractKnowledgeBase(content),
		IntegrationGuide: ig,
		Intro:            ExtractIntro(content),
		ZeropsYAML:       yaml,
	}
}

func (f recipeFragments) hasContent() bool {
	return f.KnowledgeBase != "" || f.IntegrationGuide != "" || f.Intro != ""
}

// extractYAMLFromFragment extracts the first ```yaml block from a fragment.
func extractYAMLFromFragment(fragment string) string {
	lines := strings.Split(fragment, "\n")
	inYAML := false
	var out []string

	for _, line := range lines {
		if strings.HasPrefix(line, "```yaml") {
			inYAML = true
			continue
		}
		if inYAML && strings.HasPrefix(line, "```") {
			break
		}
		if inYAML {
			out = append(out, line)
		}
	}

	if len(out) == 0 {
		return ""
	}
	return strings.Join(out, "\n")
}

func pushOneRecipe(cfg *Config, root, slug string, dryRun bool) PushResult {
	// 1. Read local recipe
	recipeFile := filepath.Join(root, cfg.Paths.Output, "recipes", slug+".md")
	content, err := os.ReadFile(recipeFile)
	if err != nil {
		return PushResult{Slug: slug, Status: Error, Err: fmt.Errorf("read recipe: %w", err)}
	}

	// 2. Extract all fragments
	frags := extractFragments(string(content))
	if !frags.hasContent() {
		return PushResult{Slug: slug, Status: Skipped, Reason: "no pushable content"}
	}

	// 3. Resolve GitHub repo from frontmatter (written during pull from API's gitRepo field)
	repo := resolveRepo(string(content), cfg, slug)
	if repo == "" {
		return PushResult{Slug: slug, Status: Skipped, Reason: "no repo in frontmatter and pattern resolution failed"}
	}

	gh := &GH{Repo: repo}

	if dryRun {
		return pushRecipeDryRun(gh, slug, frags)
	}

	return pushRecipeCreate(cfg, gh, slug, frags)
}

// resolveRepo extracts the app repo from frontmatter (authoritative, written by pull).
// Falls back to config pattern matching if frontmatter has no repo field.
func resolveRepo(content string, cfg *Config, slug string) string {
	repoURL := ExtractRepo(content)
	if repoURL != "" {
		// Convert "https://github.com/org/repo" → "org/repo"
		repoURL = strings.TrimPrefix(repoURL, "https://github.com/")
		repoURL = strings.TrimPrefix(repoURL, "http://github.com/")
		repoURL = strings.TrimSuffix(repoURL, ".git")
		return repoURL
	}

	// Fallback: pattern matching (for recipes without frontmatter repo)
	return cfg.ResolveRecipeRepo(slug, &GH{})
}

func pushRecipeDryRun(gh *GH, slug string, frags recipeFragments) PushResult {
	readme, _, err := gh.ReadFile("README.md")
	if err != nil {
		return PushResult{Slug: slug, Status: DryRun, Diff: "new file with fragments"}
	}

	updated := injectAllFragments(readme, frags)
	if updated == readme {
		return PushResult{Slug: slug, Status: Skipped, Reason: "no changes"}
	}

	var parts []string
	if frags.IntegrationGuide != "" {
		parts = append(parts, "integration-guide")
	}
	if frags.KnowledgeBase != "" {
		parts = append(parts, "knowledge-base")
	}
	if frags.ZeropsYAML != "" {
		parts = append(parts, "zerops.yaml")
	}

	return PushResult{Slug: slug, Status: DryRun, Diff: fmt.Sprintf("would update: %s", strings.Join(parts, ", "))}
}

// injectAllFragments injects non-empty fragments into the README.
// Intro is NOT injected — the pull-side strips markdown links and collapses
// whitespace, making the frontmatter description lossy. Pushing it back would
// overwrite the richer original in the README. The intro marker is read-only.
func injectAllFragments(readme string, frags recipeFragments) string {
	if frags.IntegrationGuide != "" {
		readme = InjectFragment(readme, "integration-guide", frags.IntegrationGuide)
	}
	if frags.KnowledgeBase != "" {
		readme = InjectFragment(readme, "knowledge-base", frags.KnowledgeBase)
	}
	return readme
}

func pushRecipeCreate(cfg *Config, gh *GH, slug string, frags recipeFragments) PushResult {
	// 4. Read current README.md
	readme, readmeSHA, err := gh.ReadFile("README.md")
	if err != nil {
		return PushResult{Slug: slug, Status: Error, Err: fmt.Errorf("read README: %w", err)}
	}

	// 5. Inject all fragments into README
	updated := injectAllFragments(readme, frags)

	// 6. Create branch — date + short random suffix so a same-day second
	// push of the same recipe doesn't hit "reference already exists".
	branch := fmt.Sprintf("%s/%s-%s-%s", cfg.Push.Recipes.BranchPrefix, slug, today(), shortRand())
	if err := gh.CreateBranch(branch); err != nil {
		return PushResult{Slug: slug, Status: Error, Err: fmt.Errorf("create branch: %w", err)}
	}

	// 7. Commit README with all fragment updates
	commitMsg := fmt.Sprintf("%s: update %s", cfg.Push.Recipes.CommitPrefix, slug)
	if err := gh.UpdateFile("README.md", branch, commitMsg, updated, readmeSHA); err != nil {
		return PushResult{Slug: slug, Status: Error, Err: fmt.Errorf("update README: %w", err)}
	}

	// 8. Commit zerops.yaml if applicable.
	// Skip if the existing file is longer — the API's integration-guide YAML
	// may be a subset (e.g. missing healthCheck) and we don't want to regress.
	if frags.ZeropsYAML != "" {
		existing, existingSHA, readErr := gh.ReadFile("zerops.yaml")
		if readErr != nil {
			// File doesn't exist yet — create it
			_ = gh.UpdateFile("zerops.yaml", branch, commitMsg, frags.ZeropsYAML+"\n", "")
		} else if len(strings.TrimSpace(frags.ZeropsYAML)) >= len(strings.TrimSpace(existing)) {
			// New content is same size or larger — safe to update
			_ = gh.UpdateFile("zerops.yaml", branch, commitMsg, frags.ZeropsYAML+"\n", existingSHA)
		}
		// Otherwise: existing file has more content, skip to avoid regression
	}

	// 9. Create PR
	title := fmt.Sprintf("%s: update %s knowledge", cfg.Push.Recipes.CommitPrefix, slug)
	body := "Automated knowledge sync from ZCP.\n\nUpdates README.md fragments (intro, integration-guide, knowledge-base) and zerops.yaml."
	prURL, err := gh.CreatePR(branch, title, body)
	if err != nil {
		return PushResult{Slug: slug, Status: Error, Err: fmt.Errorf("create PR: %w", err)}
	}

	return PushResult{Slug: slug, Status: Created, PRURL: prURL}
}

func today() string {
	return time.Now().Format("20060102")
}

// shortRand returns a 4-character hex string for branch name uniqueness.
func shortRand() string {
	b := make([]byte, 2)
	if _, err := rand.Read(b); err != nil {
		return "0000"
	}
	return hex.EncodeToString(b)
}
