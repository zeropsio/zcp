package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// APIResponse is the top-level response from the recipe API.
type APIResponse struct {
	Data []APIRecipe `json:"data"`
}

// APIRecipe represents a single recipe from the API.
type APIRecipe struct {
	Slug                     string                 `json:"slug"`
	Name                     string                 `json:"name"`
	Icon                     string                 `json:"icon"`
	Source                   string                 `json:"source"`
	RecipeLanguageFrameworks []apiLanguageFramework `json:"recipeLanguageFrameworks"`
	SourceData               json.RawMessage        `json:"sourceData"`
}

// apiLanguageFramework is one row of recipeLanguageFrameworks. We only need
// slug + type ("language" or "framework") to drive intent matching.
type apiLanguageFramework struct {
	Slug string `json:"slug"`
	Type string `json:"type"`
}

// sourceData is the parsed sourceData field.
type sourceData struct {
	Environments []environment `json:"environments"`
	Extracts     extracts      `json:"extracts"`
}

type environment struct {
	Name     string    `json:"name"`
	Import   string    `json:"import"`
	Services []service `json:"services"`
}

type service struct {
	IsUtility  bool     `json:"isUtility"`
	Category   string   `json:"category"`
	ZeropsYaml string   `json:"zeropsYaml"`
	GitRepo    string   `json:"gitRepo"`
	Extracts   extracts `json:"extracts"`
}

type extracts struct {
	Intro            string `json:"intro"`
	KnowledgeBase    string `json:"knowledge-base"`    //nolint:tagliatelle
	IntegrationGuide string `json:"integration-guide"` //nolint:tagliatelle
}

// PullRecipes fetches recipes from the API and writes them to the output directory.
func PullRecipes(cfg *Config, root, filter string, dryRun bool) ([]PullResult, error) {
	apiURL := cfg.APIURL + "?filters%5BrecipeCategories%5D%5Bslug%5D%5B%24ne%5D=service-utility&populate%5BrecipeCategories%5D=true&populate%5BrecipeLanguageFrameworks%5D%5Bpopulate%5D=*&pagination%5BpageSize%5D=100"

	var apiResp APIResponse
	err := fetchJSON(context.Background(), func(ctx context.Context) (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	}, &apiResp)
	if err != nil {
		return nil, fmt.Errorf("fetch recipes: %w", err)
	}

	if len(apiResp.Data) == 0 {
		return nil, fmt.Errorf("no recipes found in API response")
	}

	outDir := filepath.Join(root, cfg.Paths.Output, "recipes")
	if !dryRun {
		if err := os.MkdirAll(outDir, 0755); err != nil {
			return nil, fmt.Errorf("create recipes dir: %w", err)
		}
	}

	var results []PullResult
	for _, recipe := range apiResp.Data {
		slug := cfg.RemapSlug(recipe.Slug)

		if filter != "" && slug != filter {
			continue
		}

		result := pullOneRecipe(recipe, slug, outDir, dryRun)
		results = append(results, result)
	}

	return results, nil
}

func pullOneRecipe(recipe APIRecipe, slug, outDir string, dryRun bool) PullResult {
	var sd sourceData
	if err := json.Unmarshal(recipe.SourceData, &sd); err != nil {
		return PullResult{Slug: slug, Status: Error, Reason: fmt.Sprintf("parse sourceData: %v", err)}
	}

	md := buildRecipeMarkdown(recipe.Name, slug, &sd, recipe.RecipeLanguageFrameworks)
	if md == "" {
		return PullResult{Slug: slug, Status: Skipped, Reason: "no content in API"}
	}

	importYAML := findAgentImportYAML(&sd)

	target := filepath.Join(outDir, slug+".md")
	importTarget := filepath.Join(outDir, slug+".import.yml")

	if dryRun {
		return PullResult{Slug: slug, Status: DryRun, Diff: md}
	}

	if err := os.WriteFile(target, []byte(md), 0600); err != nil {
		return PullResult{Slug: slug, Status: Error, Reason: fmt.Sprintf("write: %v", err)}
	}
	if importYAML != "" {
		if err := os.WriteFile(importTarget, []byte(importYAML), 0600); err != nil {
			return PullResult{Slug: slug, Status: Error, Reason: fmt.Sprintf("write import: %v", err)}
		}
	}

	return PullResult{Slug: slug, Status: Created}
}

// markdownLinkPattern matches [text](url) markdown links.
var markdownLinkPattern = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)

func buildRecipeMarkdown(name, slug string, sd *sourceData, langs []apiLanguageFramework) string {
	// Find intro: prefer per-service intro over recipe-level
	intro := findServiceIntro(sd)
	if intro == "" {
		intro = sd.Extracts.Intro
	}
	if intro != "" {
		// Strip markdown links, collapse whitespace
		intro = markdownLinkPattern.ReplaceAllString(intro, " $1")
		intro = strings.Join(strings.Fields(intro), " ")
	}

	// Find knowledge-base from first service
	kb := findServiceExtract(sd, func(e extracts) string { return e.KnowledgeBase })

	// Find integration-guide from first service
	guide := findServiceExtract(sd, func(e extracts) string { return e.IntegrationGuide })

	// Fallback: raw zeropsYaml
	var yamlContent string
	if guide == "" {
		yamlContent = findServiceYAML(sd)
	}

	// Find app repo URL from first non-utility service with a gitRepo
	repo := findServiceGitRepo(sd)

	// Skip if no content
	if kb == "" && guide == "" && yamlContent == "" && intro == "" {
		return ""
	}

	languages, frameworks := partitionTaxonomy(langs)

	var sb strings.Builder

	// Always write frontmatter — repo is needed for push even if intro is empty
	sb.WriteString("---\n")
	if intro != "" {
		sb.WriteString(fmt.Sprintf("description: %q\n", intro))
	}
	if repo != "" {
		sb.WriteString(fmt.Sprintf("repo: %q\n", repo))
	}
	if len(languages) > 0 {
		sb.WriteString(fmt.Sprintf("languages: [%s]\n", strings.Join(languages, ", ")))
	}
	if len(frameworks) > 0 {
		sb.WriteString(fmt.Sprintf("frameworks: [%s]\n", strings.Join(frameworks, ", ")))
	}
	sb.WriteString("---\n\n")

	title := name
	if title == "" {
		title = slug
	}
	sb.WriteString("# " + title + " on Zerops\n\n")

	if kb != "" {
		// Promote H3→H2
		sb.WriteString(promoteHeadings(kb))
		sb.WriteString("\n\n")
	}

	if guide != "" {
		sb.WriteString(promoteHeadings(guide))
		sb.WriteString("\n\n")
	} else if yamlContent != "" {
		sb.WriteString("## zerops.yaml\n\n")
		sb.WriteString("> Reference implementation — learn the patterns, adapt to your project.\n\n")
		sb.WriteString("```yaml\n")
		sb.WriteString(yamlContent)
		sb.WriteString("\n```\n")
	}

	return sb.String()
}

func findServiceIntro(sd *sourceData) string {
	if len(sd.Environments) == 0 {
		return ""
	}
	for _, svc := range sd.Environments[0].Services {
		if svc.IsUtility || svc.Category == "CORE" || svc.Category == "STANDARD" {
			continue
		}
		if svc.Extracts.Intro != "" {
			return svc.Extracts.Intro
		}
	}
	return ""
}

func findServiceExtract(sd *sourceData, getter func(extracts) string) string {
	if len(sd.Environments) == 0 {
		return ""
	}
	for _, svc := range sd.Environments[0].Services {
		val := getter(svc.Extracts)
		if val != "" {
			return val
		}
	}
	return ""
}

func findServiceGitRepo(sd *sourceData) string {
	if len(sd.Environments) == 0 {
		return ""
	}
	for _, svc := range sd.Environments[0].Services {
		if svc.IsUtility || svc.Category == "CORE" || svc.Category == "STANDARD" {
			continue
		}
		if svc.GitRepo != "" {
			return svc.GitRepo
		}
	}
	return ""
}

func findServiceYAML(sd *sourceData) string {
	if len(sd.Environments) == 0 {
		return ""
	}
	for _, svc := range sd.Environments[0].Services {
		if svc.ZeropsYaml != "" {
			return svc.ZeropsYaml
		}
	}
	return ""
}

// findAgentImportYAML returns the project-import YAML for the AI Agent
// environment — the first environment whose name contains "AI Agent".
// Falls back to environments[0] when no agent-named environment exists,
// so recipes predating the 6-environment split still yield a YAML.
func findAgentImportYAML(sd *sourceData) string {
	if len(sd.Environments) == 0 {
		return ""
	}
	lower := strings.ToLower
	for _, env := range sd.Environments {
		if strings.Contains(lower(env.Name), "ai agent") {
			return env.Import
		}
	}
	return sd.Environments[0].Import
}

// partitionTaxonomy splits the Strapi language/framework tags into two slugs
// lists. Slugs are written into the .md frontmatter and consumed by the
// recipe matcher at runtime.
func partitionTaxonomy(langs []apiLanguageFramework) (languages, frameworks []string) {
	for _, lf := range langs {
		switch lf.Type {
		case "language":
			languages = append(languages, lf.Slug)
		case "framework":
			frameworks = append(frameworks, lf.Slug)
		}
	}
	return languages, frameworks
}

// promoteHeadings converts ### to ## in content.
func promoteHeadings(content string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if after, ok := strings.CutPrefix(line, "### "); ok {
			lines[i] = "## " + after
		}
	}
	return strings.Join(lines, "\n")
}
