package sync

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// CacheClear invalidates the Strapi cache for the given recipe slugs.
// If slugs is empty, clears cache for all recipes.
// Requires STRAPI_API_TOKEN environment variable.
func CacheClear(cfg *Config, slugs []string) ([]CacheClearResult, error) {
	token := os.Getenv("STRAPI_API_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("STRAPI_API_TOKEN not set (add to .env or export)")
	}

	// If no slugs specified, fetch all from API
	if len(slugs) == 0 {
		all, err := fetchAllSlugs(cfg)
		if err != nil {
			return nil, err
		}
		slugs = all
	}

	baseURL := strings.TrimSuffix(cfg.APIURL, "/recipes")

	var results []CacheClearResult
	for _, slug := range slugs {
		result := clearOne(baseURL, token, slug)
		results = append(results, result)
	}
	return results, nil
}

// CacheClearResult holds the outcome of clearing cache for one recipe.
type CacheClearResult struct {
	Slug   string
	Status int
	Err    error
}

func clearOne(baseURL, token, slug string) CacheClearResult {
	url := baseURL + "/recipes/" + slug + "/cache/clear"

	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return CacheClearResult{Slug: slug, Err: fmt.Errorf("build request: %w", err)}
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return CacheClearResult{Slug: slug, Err: fmt.Errorf("request: %w", err)}
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return CacheClearResult{Slug: slug, Status: resp.StatusCode, Err: fmt.Errorf("HTTP %d", resp.StatusCode)}
	}

	return CacheClearResult{Slug: slug, Status: resp.StatusCode}
}

func fetchAllSlugs(cfg *Config) ([]string, error) {
	apiURL := cfg.APIURL + "?filters%5BrecipeCategories%5D%5Bslug%5D%5B%24ne%5D=service-utility&pagination%5BpageSize%5D=100&fields%5B0%5D=slug"

	resp, err := http.Get(apiURL)
	if err != nil {
		return nil, fmt.Errorf("fetch recipe slugs: %w", err)
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

	var slugs []string
	for _, r := range apiResp.Data {
		slugs = append(slugs, r.Slug)
	}
	return slugs, nil
}
