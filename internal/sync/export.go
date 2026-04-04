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

// Environment folder layout constants.
const (
	layoutNested = "nested" // environments/ subdir
	layoutRoot   = "root"   // env folders at recipe dir root
)

// knownEnvFolders are the expected environment tier folder prefixes.
var knownEnvFolders = []string{
	"0 \u2014", "1 \u2014", "2 \u2014", "3 \u2014", "4 \u2014", "5 \u2014",
}

// ExportOpts configures the recipe export.
type ExportOpts struct {
	RecipeDir       string // recipe output dir (env folders + README)
	AppDir          string // app source dir (SSHFS mount or local subdir), optional
	IncludeTimeline bool   // prompt for TIMELINE.md if missing
}

// ExportResult holds the outcome of an export operation.
type ExportResult struct {
	ArchivePath    string // path to created archive (empty if NeedsTimeline)
	NeedsTimeline  bool   // true if TIMELINE.md is missing and was requested
	TimelinePrompt string // prompt for the AI to generate TIMELINE.md
	TimelinePath   string // where to write TIMELINE.md
}

// ExportRecipe creates a .tar.gz archive combining:
//   - Environment folders (from recipeDir, at root or in environments/ subdir)
//     → always placed under environments/ in the archive
//   - App source (from appDir, if provided) → placed under appdev/ (or dir basename)
//   - TIMELINE.md (from recipeDir root, if present)
//   - README.md (from recipeDir root, if present)
//
// The archive is written to os.TempDir first, then moved to CWD.
func ExportRecipe(opts ExportOpts) (*ExportResult, error) {
	recipeDir, err := filepath.Abs(opts.RecipeDir)
	if err != nil {
		return nil, fmt.Errorf("resolve recipe dir: %w", err)
	}
	if info, err := os.Stat(recipeDir); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", opts.RecipeDir)
	}

	// Find environment folders — either at root or inside environments/.
	envsDir, envLayout := findEnvFolders(recipeDir)
	if envsDir == "" {
		fmt.Fprintln(os.Stderr, "  warning: no environment folders found")
	}

	// Check TIMELINE.md when requested.
	timelinePath := filepath.Join(recipeDir, "TIMELINE.md")
	if opts.IncludeTimeline {
		if _, err := os.Stat(timelinePath); os.IsNotExist(err) {
			return &ExportResult{
				NeedsTimeline:  true,
				TimelinePrompt: buildTimelinePrompt(recipeDir),
				TimelinePath:   timelinePath,
			}, nil
		}
	}

	baseName := filepath.Base(recipeDir)
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

	// 1. Add root files (TIMELINE.md, README.md).
	for _, name := range []string{"TIMELINE.md", "README.md"} {
		p := filepath.Join(recipeDir, name)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			if addErr := addFileToTar(tw, p, filepath.Join(archivePrefix, name), fi); addErr != nil {
				tw.Close()
				gw.Close()
				tmpFile.Close()
				return nil, addErr
			}
		}
	}

	// 2. Add environment folders → always under environments/ in archive.
	if envsDir != "" {
		if err := exportEnvFolders(tw, envsDir, envLayout, archivePrefix); err != nil {
			tw.Close()
			gw.Close()
			tmpFile.Close()
			return nil, err
		}
	}

	// 3. Add app source dir if provided.
	if opts.AppDir != "" {
		appDir, absErr := filepath.Abs(opts.AppDir)
		if absErr != nil {
			tw.Close()
			gw.Close()
			tmpFile.Close()
			return nil, fmt.Errorf("resolve app dir: %w", absErr)
		}
		appName := filepath.Base(appDir)
		archiveAppDir := filepath.Join(archivePrefix, appName)
		if hasGitDir(appDir) {
			err = exportGitSubtree(tw, appDir, archiveAppDir, "")
		} else {
			err = exportSubdirWalk(tw, appDir, archiveAppDir, "")
		}
		if err != nil {
			tw.Close()
			gw.Close()
			tmpFile.Close()
			return nil, fmt.Errorf("export app dir: %w", err)
		}
	}

	tw.Close()
	gw.Close()
	tmpFile.Close()

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

// findEnvFolders locates environment tier folders.
// Returns (dir containing them, layout) where layout is:
//   - layoutNested if found in {recipeDir}/environments/
//   - layoutRoot if found at {recipeDir}/ root level
//   - "" if not found
func findEnvFolders(recipeDir string) (string, string) {
	// Check environments/ subdir first.
	envsDir := filepath.Join(recipeDir, "environments")
	if entries, err := os.ReadDir(envsDir); err == nil {
		for _, e := range entries {
			if e.IsDir() && isEnvFolder(e.Name()) {
				return envsDir, layoutNested
			}
		}
	}
	// Check root level.
	if entries, err := os.ReadDir(recipeDir); err == nil {
		for _, e := range entries {
			if e.IsDir() && isEnvFolder(e.Name()) {
				return recipeDir, layoutRoot
			}
		}
	}
	return "", ""
}

// exportEnvFolders adds environment folders to the archive under environments/.
func exportEnvFolders(tw *tar.Writer, envsDir, layout, archivePrefix string) error {
	envArchiveDir := filepath.Join(archivePrefix, "environments")

	return filepath.Walk(envsDir, func(path string, fi os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == envsDir && fi.IsDir() {
			return nil // skip the root dir itself
		}
		// For root layout, skip non-env directories (appdev/, etc.).
		if layout == layoutRoot && fi.IsDir() && !isEnvFolder(fi.Name()) {
			// Allow subdirs within env folders.
			rel, _ := filepath.Rel(envsDir, path)
			parts := strings.SplitN(rel, string(filepath.Separator), 2)
			if !isEnvFolder(parts[0]) {
				return filepath.SkipDir
			}
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

		// For root layout, only include files inside env folders (+ README.md at root).
		if layout == layoutRoot {
			parts := strings.SplitN(rel, string(filepath.Separator), 2)
			if !isEnvFolder(parts[0]) && fi.Name() != "README.md" {
				return nil
			}
		}

		archivePath := filepath.Join(envArchiveDir, rel)
		return addFileToTar(tw, path, archivePath, fi)
	})
}

// exportGitSubtree exports a directory's git-tracked files to the tar.
func exportGitSubtree(tw *tar.Writer, dir, archivePrefix, relPrefix string) error {
	cmd := exec.Command("git", "ls-files", "-z") //nolint:noctx // controlled path
	cmd.Dir = dir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
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
			continue
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
// Handles both layouts: environments/ subdir or env folders at root.
func CollectRecipeFiles(sourceDir, slug string) (map[string]string, error) {
	envsDir, layout := findEnvFolders(sourceDir)
	if envsDir == "" {
		return nil, fmt.Errorf("no environment folders found in %s", sourceDir)
	}

	files := make(map[string]string)

	err := filepath.Walk(envsDir, func(path string, fi os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == envsDir && fi.IsDir() {
			return nil
		}
		// For root layout, skip non-env directories.
		if layout == layoutRoot && fi.IsDir() && !isEnvFolder(fi.Name()) {
			rel, _ := filepath.Rel(envsDir, path)
			parts := strings.SplitN(rel, string(filepath.Separator), 2)
			if !isEnvFolder(parts[0]) {
				return filepath.SkipDir
			}
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

		// For root layout, only include files inside env folders + root README.
		if layout == layoutRoot {
			parts := strings.SplitN(rel, string(filepath.Separator), 2)
			if !isEnvFolder(parts[0]) && fi.Name() != "README.md" {
				return nil
			}
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
