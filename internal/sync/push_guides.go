package sync

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
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

	// Build converted content map: path → MDX content. Only ACTUALLY-changed
	// guides are staged — a byte-identical guide is skipped so the tree, the
	// commit count, and the PR body reflect the real diff (not every processed
	// guide: the "update 27 guides" headline on a 4-file change).
	changes := make(map[string]string)
	var changedSlugs []string
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
		existingFound := false
		existing, _, readErr := gh.ReadFile(targetPath)
		if readErr == nil {
			existingMDX = existing
			existingFound = true
		}

		mdx := ConvertGuideToMDX(string(guideContent), existingMDX)

		// Compare the POST-transform bytes that would actually be committed
		// against the existing file — a byte-identical guide is NOT a change
		// (staging it would inflate the commit count and re-upload an identical
		// blob into the tree).
		action := classifyGuidePush(mdx, existingMDX, existingFound)

		if dryRun {
			switch action {
			case guideWouldCreate:
				results = append(results, PushResult{Slug: gf.slug, Status: DryRun, Diff: "would create " + targetPath})
			case guideNoChange:
				results = append(results, PushResult{Slug: gf.slug, Status: Skipped, Reason: "no changes"})
			case guideWouldUpdate:
				results = append(results, PushResult{Slug: gf.slug, Status: DryRun, Diff: "would update " + targetPath})
			}
			continue
		}

		if action == guideNoChange {
			continue
		}
		changes[targetPath] = mdx
		changedSlugs = append(changedSlugs, gf.slug)
	}

	if dryRun || len(changes) == 0 {
		return results, nil
	}

	// Create single PR with all guide changes using Git Trees API
	headSHA, err := gh.DefaultBranchSHA()
	if err != nil {
		return nil, fmt.Errorf("get HEAD SHA: %w", err)
	}

	// Branch name carries date + short random suffix so multiple pushes on
	// the same day (e.g. landing two independent guide fixes) don't collide
	// on GitHub's "reference already exists" rejection.
	branch := fmt.Sprintf("%s/guides-%s-%s", cfg.Push.Guides.BranchPrefix, today(), shortRand())
	if err := gh.CreateBranch(branch); err != nil {
		return nil, fmt.Errorf("create branch: %w", err)
	}

	treeSHA, err := gh.CreateTree(headSHA, changes)
	if err != nil {
		return nil, fmt.Errorf("create tree: %w", err)
	}

	sort.Strings(changedSlugs)
	slugList := strings.Join(changedSlugs, ", ")
	commitMsg := fmt.Sprintf("%s: update %d guide(s) — %s", cfg.Push.Guides.CommitPrefix, len(changedSlugs), slugList)
	commitSHA, err := gh.CreateCommit(treeSHA, headSHA, commitMsg)
	if err != nil {
		return nil, fmt.Errorf("create commit: %w", err)
	}

	if err := gh.UpdateRef(branch, commitSHA); err != nil {
		return nil, fmt.Errorf("update ref: %w", err)
	}

	title := fmt.Sprintf("%s: update %d guide(s)", cfg.Push.Guides.CommitPrefix, len(changedSlugs))
	body := fmt.Sprintf("Automated guide sync from ZCP.\n\nUpdates %d guide file(s): %s.", len(changedSlugs), slugList)
	prURL, err := gh.CreatePR(branch, title, body)
	if err != nil {
		return nil, fmt.Errorf("create PR: %w", err)
	}

	for _, gf := range files {
		results = append(results, PushResult{Slug: gf.slug, Status: Created, PRURL: prURL})
	}

	return results, nil
}

// guideDryRunVerdict classifies a guide dry-run by comparing the post-transform
// MDX that would actually be committed against the existing file.
type guideDryRunVerdict int

const (
	guideWouldCreate guideDryRunVerdict = iota
	guideWouldUpdate
	guideNoChange
)

// classifyGuidePush is the pure core of the guides dry-run. The push commits
// the converted MDX, so comparing that exact MDX against the existing MDX is an
// accurate preview — semantically-identical content that differs only in the
// .md/.mdx representation is already collapsed by ConvertGuideToMDX, so it
// reports guideNoChange instead of the old unconditional false positive.
func classifyGuidePush(mdx, existingMDX string, existingFound bool) guideDryRunVerdict {
	switch {
	case !existingFound:
		return guideWouldCreate
	case mdx == existingMDX:
		return guideNoChange
	default:
		return guideWouldUpdate
	}
}

type guideFile struct {
	slug string
	path string
}

func collectGuideFiles(guidesDir, decisionsDir, filter string) ([]guideFile, error) {
	// Filter may be empty (collect all), a single slug, or a comma-separated
	// list of slugs. Comma-separated support lets a single push command
	// bundle multiple guides into one PR — which is required when two
	// guides need to land on the same date-stamped branch (GitHub rejects
	// duplicate branch creation).
	var allowed map[string]struct{}
	if filter != "" {
		allowed = make(map[string]struct{})
		for slug := range strings.SplitSeq(filter, ",") {
			slug = strings.TrimSpace(slug)
			if slug != "" {
				allowed[slug] = struct{}{}
			}
		}
	}

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
			if allowed != nil {
				if _, ok := allowed[slug]; !ok {
					continue
				}
			}
			files = append(files, guideFile{
				slug: slug,
				path: filepath.Join(dir, entry.Name()),
			})
		}
	}
	return files, nil
}
