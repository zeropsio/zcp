package sync

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PullGuides reads .mdx files from the docs clone and converts them to .md guides.
func PullGuides(cfg *Config, root, filter string, dryRun bool) ([]PullResult, error) {
	docsDir := cfg.Paths.DocsLocal
	if docsDir == "" {
		return nil, fmt.Errorf("no docs_local path configured (set paths.docs_local in .sync.yaml or DOCS_GUIDES env)")
	}

	entries, err := os.ReadDir(docsDir)
	if err != nil {
		return nil, fmt.Errorf("read docs dir %s: %w", docsDir, err)
	}

	guidesDir := filepath.Join(root, cfg.Paths.Output, "guides")
	decisionsDir := filepath.Join(root, cfg.Paths.Output, "decisions")

	if !dryRun {
		if err := os.MkdirAll(guidesDir, 0755); err != nil {
			return nil, fmt.Errorf("create guides dir: %w", err)
		}
		if err := os.MkdirAll(decisionsDir, 0755); err != nil {
			return nil, fmt.Errorf("create decisions dir: %w", err)
		}
	}

	var results []PullResult
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".mdx") {
			continue
		}

		slug := strings.TrimSuffix(entry.Name(), ".mdx")
		if filter != "" && slug != filter {
			continue
		}

		result := pullOneGuide(slug, filepath.Join(docsDir, entry.Name()), guidesDir, decisionsDir, dryRun)
		results = append(results, result)
	}

	return results, nil
}

func pullOneGuide(slug, srcPath, guidesDir, decisionsDir string, dryRun bool) PullResult {
	mdxContent, err := os.ReadFile(srcPath)
	if err != nil {
		return PullResult{Slug: slug, Status: Error, Reason: fmt.Sprintf("read: %v", err)}
	}

	md := ConvertMDXToGuide(string(mdxContent))

	// choose-* files go to decisions/
	targetDir := guidesDir
	if strings.HasPrefix(slug, "choose-") {
		targetDir = decisionsDir
	}
	target := filepath.Join(targetDir, slug+".md")

	if dryRun {
		return PullResult{Slug: slug, Status: DryRun, Diff: md}
	}

	if err := os.WriteFile(target, []byte(md), 0644); err != nil {
		return PullResult{Slug: slug, Status: Error, Reason: fmt.Sprintf("write: %v", err)}
	}

	return PullResult{Slug: slug, Status: Created}
}
