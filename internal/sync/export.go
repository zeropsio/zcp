package sync

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// skipDirs are directories to exclude from the export archive.
var skipDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
}

// skipFiles are file names to exclude from the export archive.
var skipFiles = map[string]bool{
	".DS_Store": true,
	"Thumbs.db": true,
}

// skipPathPatterns are path segments that indicate generated/cached files.
// Used only in the walk fallback (non-git directories).
var skipPathPatterns = []string{
	"storage/framework/views/",    // Laravel compiled Blade cache
	"storage/framework/cache/",    // Laravel file cache
	"storage/framework/sessions/", // Laravel file sessions
	"storage/logs/",               // Laravel log files
	"bootstrap/cache/packages.php",
	"bootstrap/cache/services.php",
	"bootstrap/cache/events.php",
	".phpunit.cache/",
	"__pycache__/",
	".next/cache/",
}

// knownEnvFolders are the expected environment tier folder prefixes.
var knownEnvFolders = []string{
	"0 \u2014", "1 \u2014", "2 \u2014", "3 \u2014", "4 \u2014", "5 \u2014",
}

// ExportRecipe creates a .tar.gz archive of the recipe output directory.
//
// The source directory typically has no .git at root but contains subdirectories
// (like appdev/) that are SSHFS mounts with their own .git. The export handles
// this by using git ls-files for any subdirectory that has .git (respecting its
// .gitignore), and a filtered walk for the rest.
//
// The archive is written to os.TempDir first, then moved to CWD to avoid
// packing the output file into its own archive.
//
// If includeTimeline is true and TIMELINE.md doesn't exist, returns a
// TimelinePrompt in the result instead of an error.
func ExportRecipe(sourceDir string, includeTimeline bool) (*ExportResult, error) {
	sourceDir, err := filepath.Abs(sourceDir)
	if err != nil {
		return nil, fmt.Errorf("resolve source dir: %w", err)
	}

	info, err := os.Stat(sourceDir)
	if err != nil {
		return nil, fmt.Errorf("stat source dir: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", sourceDir)
	}

	// Warn about structure issues.
	warnings := validateRecipeLayout(sourceDir)
	for _, w := range warnings {
		fmt.Fprintf(os.Stderr, "  warning: %s\n", w)
	}

	// Check TIMELINE.md when requested.
	if includeTimeline {
		timelinePath := filepath.Join(sourceDir, "TIMELINE.md")
		if _, err := os.Stat(timelinePath); os.IsNotExist(err) {
			return &ExportResult{
				NeedsTimeline:  true,
				TimelinePrompt: buildTimelinePrompt(sourceDir),
				TimelinePath:   timelinePath,
			}, nil
		}
	}

	baseName := filepath.Base(sourceDir)
	archivePrefix := baseName + "-zcprecipator"
	finalName := archivePrefix + ".tar.gz"

	// Write to temp dir first to avoid self-inclusion.
	tmpFile, err := os.CreateTemp("", archivePrefix+"-*.tar.gz")
	if err != nil {
		return nil, fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	success := false
	defer func() {
		if !success {
			os.Remove(tmpPath)
		}
	}()

	gw := gzip.NewWriter(tmpFile)
	tw := tar.NewWriter(gw)

	err = exportHybrid(tw, sourceDir, archivePrefix)

	tw.Close()
	gw.Close()
	tmpFile.Close()

	if err != nil {
		return nil, err
	}

	// Move to CWD.
	if err := os.Rename(tmpPath, finalName); err != nil {
		if cpErr := copyFile(tmpPath, finalName); cpErr != nil {
			return nil, fmt.Errorf("move archive: %w", cpErr)
		}
		os.Remove(tmpPath)
	}

	success = true
	return &ExportResult{ArchivePath: finalName}, nil
}

// ExportResult holds the outcome of an export operation.
type ExportResult struct {
	ArchivePath    string // path to created archive (empty if NeedsTimeline)
	NeedsTimeline  bool   // true if TIMELINE.md is missing and was requested
	TimelinePrompt string // prompt for the AI to generate TIMELINE.md
	TimelinePath   string // where to write TIMELINE.md
}

// exportHybrid walks the source directory. For subdirectories that have
// their own .git (e.g. SSHFS-mounted app dirs), it uses git ls-files to
// respect .gitignore. For everything else, it uses a filtered walk.
func exportHybrid(tw *tar.Writer, sourceDir, prefix string) error {
	// First pass: find subdirectories with .git (git subtrees).
	gitSubtrees := make(map[string]bool)
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		return fmt.Errorf("read source dir: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		subdir := filepath.Join(sourceDir, e.Name())
		if hasGitDir(subdir) {
			gitSubtrees[subdir] = true
		}
	}

	// If root itself has .git, use git for everything.
	if hasGitDir(sourceDir) {
		if err := exportGitSubtree(tw, sourceDir, prefix, ""); err != nil {
			return err
		}
		return includeUntrackedTimeline(tw, sourceDir, prefix, nil)
	}

	// Walk the root, switching to git for subtrees.
	return filepath.Walk(sourceDir, func(path string, fi os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		// If this directory is a git subtree, export via git and skip walk.
		if fi.IsDir() && gitSubtrees[path] {
			rel, _ := filepath.Rel(sourceDir, path)
			if err := exportGitSubtree(tw, path, prefix, rel); err != nil {
				return err
			}
			return filepath.SkipDir
		}

		if fi.IsDir() && skipDirs[fi.Name()] {
			return filepath.SkipDir
		}
		if fi.IsDir() {
			return nil
		}
		if skipFiles[fi.Name()] {
			return nil
		}

		rel, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return fmt.Errorf("rel path: %w", err)
		}

		relSlash := strings.ReplaceAll(rel, string(filepath.Separator), "/")
		if matchesSkipPattern(relSlash) {
			return nil
		}

		return addFileToTar(tw, path, filepath.Join(prefix, rel), fi)
	})
}

// exportGitSubtree exports a directory's git-tracked files to the tar.
// relPrefix is the relative path from source root to this subtree (empty for root).
func exportGitSubtree(tw *tar.Writer, dir, archivePrefix, relPrefix string) error {
	cmd := exec.Command("git", "ls-files", "-z") //nolint:noctx // controlled path
	cmd.Dir = dir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Fallback: if git fails (e.g. bare init), walk instead.
		fmt.Fprintf(os.Stderr, "  warning: git ls-files failed in %s, falling back to walk\n", dir)
		return exportSubdirWalk(tw, dir, archivePrefix, relPrefix)
	}

	for f := range strings.SplitSeq(stdout.String(), "\x00") {
		if f == "" {
			continue
		}
		fullPath := filepath.Join(dir, f)
		fi, err := os.Stat(fullPath)
		if err != nil {
			continue // deleted but tracked
		}
		if fi.IsDir() || skipFiles[fi.Name()] {
			continue
		}

		archivePath := filepath.Join(archivePrefix, relPrefix, f)
		if err := addFileToTar(tw, fullPath, archivePath, fi); err != nil {
			return err
		}
	}

	return nil
}

// exportSubdirWalk walks a subdirectory with skip lists (fallback when git fails).
func exportSubdirWalk(tw *tar.Writer, dir, archivePrefix, relPrefix string) error {
	return filepath.Walk(dir, func(path string, fi os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if fi.IsDir() && skipDirs[fi.Name()] {
			return filepath.SkipDir
		}
		if fi.IsDir() {
			return nil
		}
		if skipFiles[fi.Name()] {
			return nil
		}

		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return fmt.Errorf("rel path: %w", err)
		}

		relSlash := strings.ReplaceAll(rel, string(filepath.Separator), "/")
		if matchesSkipPattern(relSlash) {
			return nil
		}

		archivePath := filepath.Join(archivePrefix, relPrefix, rel)
		return addFileToTar(tw, path, archivePath, fi)
	})
}

// includeUntrackedTimeline adds TIMELINE.md if it exists but isn't tracked by git.
func includeUntrackedTimeline(tw *tar.Writer, sourceDir, prefix string, trackedFiles []string) error {
	timelinePath := filepath.Join(sourceDir, "TIMELINE.md")
	ti, statErr := os.Stat(timelinePath)
	if statErr != nil {
		return nil //nolint:nilerr // not-found is expected, not an error
	}

	if slices.Contains(trackedFiles, "TIMELINE.md") {
		return nil
	}

	return addFileToTar(tw, timelinePath, filepath.Join(prefix, "TIMELINE.md"), ti)
}

// matchesSkipPattern checks if a relative path matches any skip pattern.
func matchesSkipPattern(rel string) bool {
	for _, pattern := range skipPathPatterns {
		if strings.Contains(rel, pattern) {
			return true
		}
	}
	return false
}

// addFileToTar adds a single file to the tar writer.
func addFileToTar(tw *tar.Writer, fullPath, archivePath string, fi os.FileInfo) error {
	header, err := tar.FileInfoHeader(fi, "")
	if err != nil {
		return fmt.Errorf("file header %s: %w", archivePath, err)
	}
	header.Name = archivePath

	if err := tw.WriteHeader(header); err != nil {
		return fmt.Errorf("write header %s: %w", archivePath, err)
	}

	f, err := os.Open(fullPath)
	if err != nil {
		return fmt.Errorf("open %s: %w", archivePath, err)
	}
	defer f.Close()

	if _, err := io.Copy(tw, f); err != nil {
		return fmt.Errorf("copy %s: %w", archivePath, err)
	}
	return nil
}

// validateRecipeLayout checks for common structure issues and returns warnings.
func validateRecipeLayout(sourceDir string) []string {
	var warnings []string

	envsDir := filepath.Join(sourceDir, "environments")
	if _, err := os.Stat(envsDir); os.IsNotExist(err) {
		entries, readErr := os.ReadDir(sourceDir)
		if readErr == nil {
			for _, e := range entries {
				if e.IsDir() && isEnvFolder(e.Name()) {
					warnings = append(warnings,
						"environment folders found at root level — expected inside environments/ subdirectory")
					break
				}
			}
		}
	}

	hasApp := false
	entries, err := os.ReadDir(sourceDir)
	if err == nil {
		for _, e := range entries {
			if e.IsDir() && (strings.HasSuffix(e.Name(), "dev") || e.Name() == "app") {
				hasApp = true
				break
			}
		}
	}
	if !hasApp {
		warnings = append(warnings, "no app directory found (expected appdev/ or similar)")
	}

	return warnings
}

// isEnvFolder checks if a directory name matches an environment tier folder.
func isEnvFolder(name string) bool {
	for _, prefix := range knownEnvFolders {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// hasGitDir checks if a directory contains a .git directory.
func hasGitDir(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil && info.IsDir()
}

// copyFile copies src to dst via read/write (for cross-device moves).
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

// buildTimelinePrompt returns instructions for the AI to create TIMELINE.md.
func buildTimelinePrompt(sourceDir string) string {
	return fmt.Sprintf(`TIMELINE.md is missing from %s.

Write a TIMELINE.md documenting the recipe creation session. Include:

## Format
- Step-by-step build history with timestamps or durations
- Each step: what was done, decisions made, issues encountered
- Issues table: severity, description, resolution
- Final output section: URLs, file locations, archive path

## Steps to document
1. **Research** — framework analysis, recipe plan decisions
2. **Provision** — services created, env vars discovered
3. **Generate** — app scaffolded, code changes made, zerops.yaml written
4. **Deploy** — build durations, failures and fixes, verification results
5. **Finalize** — environment tiers generated, check results
6. **Close** — verification sub-agent findings, final fixes

## Rules
- Be factual — document what happened, not what should happen
- Include error messages and fix descriptions for any failures
- Note build durations where available
- List all URLs (dev, stage subdomains)

Write TIMELINE.md to: %s/TIMELINE.md
Then call export again.`, sourceDir, sourceDir)
}

// CollectRecipeFiles reads all files from a recipe environments directory
// and returns them as a map of relative paths to content.
// Paths are prefixed with slug/ and the environments/ prefix is stripped.
func CollectRecipeFiles(sourceDir, slug string) (map[string]string, error) {
	envsDir := filepath.Join(sourceDir, "environments")
	info, err := os.Stat(envsDir)
	if err != nil {
		return nil, fmt.Errorf("stat environments dir: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s/environments is not a directory", sourceDir)
	}

	files := make(map[string]string)

	err = filepath.Walk(envsDir, func(path string, fi os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if fi.IsDir() {
			return nil
		}
		if skipFiles[fi.Name()] {
			return nil
		}

		rel, err := filepath.Rel(envsDir, path)
		if err != nil {
			return fmt.Errorf("rel path: %w", err)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", rel, err)
		}

		targetPath := slug + "/" + strings.ReplaceAll(rel, string(filepath.Separator), "/")
		files[targetPath] = string(data)

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk environments: %w", err)
	}

	return files, nil
}
