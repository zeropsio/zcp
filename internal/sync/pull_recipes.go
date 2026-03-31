package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	Slug       string          `json:"slug"`
	Name       string          `json:"name"`
	SourceData json.RawMessage `json:"sourceData"`
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

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch recipes: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var apiResp APIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
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

		result := pullOneRecipe(cfg, recipe, slug, outDir, dryRun)
		results = append(results, result)
	}

	return results, nil
}

func pullOneRecipe(cfg *Config, recipe APIRecipe, slug, outDir string, dryRun bool) PullResult {
	var sd sourceData
	if err := json.Unmarshal(recipe.SourceData, &sd); err != nil {
		return PullResult{Slug: slug, Status: Error, Reason: fmt.Sprintf("parse sourceData: %v", err)}
	}

	md := buildRecipeMarkdown(cfg, recipe.Name, slug, &sd)
	if md == "" {
		return PullResult{Slug: slug, Status: Skipped, Reason: "no content in API"}
	}

	target := filepath.Join(outDir, slug+".md")

	if dryRun {
		return PullResult{Slug: slug, Status: DryRun, Diff: md}
	}

	if err := os.WriteFile(target, []byte(md), 0600); err != nil {
		return PullResult{Slug: slug, Status: Error, Reason: fmt.Sprintf("write: %v", err)}
	}

	return PullResult{Slug: slug, Status: Created}
}

// markdownLinkPattern matches [text](url) markdown links.
var markdownLinkPattern = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)

func buildRecipeMarkdown(cfg *Config, name, slug string, sd *sourceData) string {
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

	var sb strings.Builder

	// Always write frontmatter — repo is needed for push even if intro is empty
	sb.WriteString("---\n")
	if intro != "" {
		sb.WriteString(fmt.Sprintf("description: %q\n", intro))
	}
	if repo != "" {
		sb.WriteString(fmt.Sprintf("repo: %q\n", repo))
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
		sb.WriteString("## zerops.yml\n\n")
		sb.WriteString("> Reference implementation — learn the patterns, adapt to your project.\n\n")
		sb.WriteString("```yaml\n")
		sb.WriteString(yamlContent)
		sb.WriteString("\n```\n")
	}

	// Service definitions from environment imports
	devEnv := findEnvByName(sd.Environments, cfg.Environments.DevStage)
	prodEnv := findEnvByName(sd.Environments, cfg.Environments.SmallProd)

	if devEnv != nil || prodEnv != nil {
		sb.WriteString("\n## Service Definitions\n\n")
		sb.WriteString("> Per-service blocks extracted from battle-tested recipe imports.\n")
		sb.WriteString("> Use these proven scaling values when composing import.yaml for new projects.\n")
	}

	if devEnv != nil && devEnv.Import != "" {
		sb.WriteString("\n### Dev/Stage (from AI Agent environment)\n\n")
		sb.WriteString("```yaml\n")
		sb.WriteString(devEnv.Import)
		sb.WriteString("\n```\n")
	}

	if prodEnv != nil && prodEnv.Import != "" {
		sb.WriteString("\n### Small Production\n\n")
		sb.WriteString("```yaml\n")
		sb.WriteString(prodEnv.Import)
		sb.WriteString("\n```\n")
	}

	return sb.String()
}

func findEnvByName(envs []environment, pattern string) *environment {
	if pattern == "" {
		return nil
	}
	for i := range envs {
		if strings.Contains(envs[i].Name, pattern) {
			return &envs[i]
		}
	}
	return nil
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
