package sync

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PullGuides fetches .mdx files from the docs repo via GitHub API and converts them to .md.
// Falls back to local filesystem if docs_local is configured.
func PullGuides(cfg *Config, root, filter string, dryRun bool) ([]PullResult, error) {
	// Prefer GitHub API (no local clone needed)
	if cfg.Push.Guides.Repo != "" && cfg.Push.Guides.Path != "" {
		return pullGuidesFromGitHub(cfg, root, filter, dryRun)
	}

	// Fallback: local docs clone
	if cfg.Paths.DocsLocal != "" {
		return pullGuidesFromLocal(cfg, root, filter, dryRun)
	}

	return nil, fmt.Errorf("no guides source configured (set push.guides.repo in .sync.yaml or docs_local path)")
}

func pullGuidesFromGitHub(cfg *Config, root, filter string, dryRun bool) ([]PullResult, error) {
	gh := &GH{Repo: cfg.Push.Guides.Repo}

	// List files in the guides directory
	entries, err := gh.ListDirectory(cfg.Push.Guides.Path)
	if err != nil {
		return nil, fmt.Errorf("list guides from %s: %w", cfg.Push.Guides.Repo, err)
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
		if !strings.HasSuffix(entry, ".mdx") {
			continue
		}

		slug := strings.TrimSuffix(entry, ".mdx")
		if filter != "" && slug != filter {
			continue
		}

		result := pullOneGuideFromGitHub(gh, cfg.Push.Guides.Path, slug, guidesDir, decisionsDir, dryRun)
		results = append(results, result)
	}

	return results, nil
}

func pullOneGuideFromGitHub(gh *GH, basePath, slug, guidesDir, decisionsDir string, dryRun bool) PullResult {
	filePath := basePath + "/" + slug + ".mdx"
	content, _, err := gh.ReadFile(filePath)
	if err != nil {
		return PullResult{Slug: slug, Status: Error, Reason: fmt.Sprintf("read from GitHub: %v", err)}
	}

	md := ConvertMDXToGuide(content)

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

func pullGuidesFromLocal(cfg *Config, root, filter string, dryRun bool) ([]PullResult, error) {
	docsDir := cfg.Paths.DocsLocal

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

		result := pullOneGuideFromLocal(slug, filepath.Join(docsDir, entry.Name()), guidesDir, decisionsDir, dryRun)
		results = append(results, result)
	}

	return results, nil
}

func pullOneGuideFromLocal(slug, srcPath, guidesDir, decisionsDir string, dryRun bool) PullResult {
	mdxContent, err := os.ReadFile(srcPath)
	if err != nil {
		return PullResult{Slug: slug, Status: Error, Reason: fmt.Sprintf("read: %v", err)}
	}

	md := ConvertMDXToGuide(string(mdxContent))

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

// ListDirectory is defined on GH — adding here to keep pull_guides self-contained.
// It lists file names in a directory via the GitHub Contents API.
func (g *GH) ListDirectory(path string) ([]string, error) {
	out, err := g.api("repos/"+g.Repo+"/contents/"+path, "--jq", ".[].name")
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", path, err)
	}

	var names []string
	if err := json.Unmarshal([]byte("["+quotedLines(out)+"]"), &names); err != nil {
		// Fallback: plain newline-separated names
		for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
			if line != "" {
				names = append(names, line)
			}
		}
	}

	return names, nil
}

// quotedLines converts newline-separated values to JSON array elements.
func quotedLines(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	var quoted []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			quoted = append(quoted, `"`+l+`"`)
		}
	}
	return strings.Join(quoted, ",")
}
