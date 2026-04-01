package sync

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PushGuides pushes local guide knowledge to the docs repo as a single PR.
func PushGuides(cfg *Config, root, filter string, dryRun bool) ([]PushResult, error) {
	guidesDir := filepath.Join(root, cfg.Paths.Output, "guides")
	decisionsDir := filepath.Join(root, cfg.Paths.Output, "decisions")

	files, err := collectGuideFiles(guidesDir, decisionsDir, filter)
	if err != nil {
		return nil, err
	}

	if len(files) == 0 {
		return nil, nil
	}

	gh := &GH{Repo: cfg.Push.Guides.Repo}

	// Build converted content map: path → MDX content
	changes := make(map[string]string)
	var results []PushResult

	for _, gf := range files {
		guideContent, readErr := os.ReadFile(gf.path)
		if readErr != nil {
			results = append(results, PushResult{Slug: gf.slug, Status: Error, Err: fmt.Errorf("read: %w", readErr)})
			continue
		}

		// Try to read existing MDX from GitHub for frontmatter preservation
		targetPath := cfg.Push.Guides.Path + "/" + gf.slug + ".mdx"
		var existingMDX string
		existing, _, readErr := gh.ReadFile(targetPath)
		if readErr == nil {
			existingMDX = existing
		}

		mdx := ConvertGuideToMDX(string(guideContent), existingMDX)
		changes[targetPath] = mdx

		if dryRun {
			results = append(results, PushResult{Slug: gf.slug, Status: DryRun, Diff: "would update " + targetPath})
		}
	}

	if dryRun || len(changes) == 0 {
		return results, nil
	}

	// Create single PR with all guide changes using Git Trees API
	headSHA, err := gh.DefaultBranchSHA()
	if err != nil {
		return nil, fmt.Errorf("get HEAD SHA: %w", err)
	}

	branch := fmt.Sprintf("%s/guides-%s", cfg.Push.Guides.BranchPrefix, today())
	if err := gh.CreateBranch(branch); err != nil {
		return nil, fmt.Errorf("create branch: %w", err)
	}

	treeSHA, err := gh.CreateTree(headSHA, changes)
	if err != nil {
		return nil, fmt.Errorf("create tree: %w", err)
	}

	commitMsg := fmt.Sprintf("%s: update %d guides", cfg.Push.Guides.CommitPrefix, len(changes))
	commitSHA, err := gh.CreateCommit(treeSHA, headSHA, commitMsg)
	if err != nil {
		return nil, fmt.Errorf("create commit: %w", err)
	}

	if err := gh.UpdateRef(branch, commitSHA); err != nil {
		return nil, fmt.Errorf("update ref: %w", err)
	}

	title := fmt.Sprintf("%s: update guides", cfg.Push.Guides.CommitPrefix)
	body := fmt.Sprintf("Automated guide sync from ZCP.\n\nUpdates %d guide files.", len(changes))
	prURL, err := gh.CreatePR(branch, title, body)
	if err != nil {
		return nil, fmt.Errorf("create PR: %w", err)
	}

	for _, gf := range files {
		results = append(results, PushResult{Slug: gf.slug, Status: Created, PRURL: prURL})
	}

	return results, nil
}

type guideFile struct {
	slug string
	path string
}

func collectGuideFiles(guidesDir, decisionsDir, filter string) ([]guideFile, error) {
	var files []guideFile

	for _, dir := range []string{guidesDir, decisionsDir} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("read dir %s: %w", dir, err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			slug := strings.TrimSuffix(entry.Name(), ".md")
			if filter != "" && slug != filter {
				continue
			}
			files = append(files, guideFile{
				slug: slug,
				path: filepath.Join(dir, entry.Name()),
			})
		}
	}
	return files, nil
}
