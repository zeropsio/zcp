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
// Matched against the relative path within the source directory.
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
	"dist/",
}

// knownEnvFolders are the expected environment tier folder prefixes.
var knownEnvFolders = []string{
	"0 \u2014", "1 \u2014", "2 \u2014", "3 \u2014", "4 \u2014", "5 \u2014",
}

// ExportRecipe creates a .tar.gz archive of the recipe output directory.
// When the source directory contains a .git directory, uses git archive
// to export only tracked files (respects .gitignore). Otherwise falls
// back to a filesystem walk with skip lists.
// The archive is written to os.TempDir first, then moved to CWD to
// avoid packing the output file into its own archive.
// Returns the final path to the created archive.
func ExportRecipe(sourceDir string) (string, error) {
	sourceDir, err := filepath.Abs(sourceDir)
	if err != nil {
		return "", fmt.Errorf("resolve source dir: %w", err)
	}

	info, err := os.Stat(sourceDir)
	if err != nil {
		return "", fmt.Errorf("stat source dir: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", sourceDir)
	}

	// Warn about structure issues.
	warnings := validateRecipeLayout(sourceDir)
	for _, w := range warnings {
		fmt.Fprintf(os.Stderr, "  warning: %s\n", w)
	}

	baseName := filepath.Base(sourceDir)
	archivePrefix := baseName + "-zcprecipator"
	finalName := archivePrefix + ".tar.gz"

	// Write to temp dir first to avoid self-inclusion.
	tmpFile, err := os.CreateTemp("", archivePrefix+"-*.tar.gz")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	// Clean up temp file on error.
	success := false
	defer func() {
		if !success {
			os.Remove(tmpPath)
		}
	}()

	gw := gzip.NewWriter(tmpFile)
	tw := tar.NewWriter(gw)

	// Choose export strategy: git archive (respects .gitignore) or walk.
	hasGit := hasGitDir(sourceDir)
	if hasGit {
		err = exportViaGit(tw, sourceDir, archivePrefix)
	} else {
		err = exportViaWalk(tw, sourceDir, archivePrefix)
	}

	// Close writers in order (tar → gzip → file) before moving.
	tw.Close()
	gw.Close()
	tmpFile.Close()

	if err != nil {
		return "", err
	}

	// Move to CWD.
	if err := os.Rename(tmpPath, finalName); err != nil {
		// Cross-device: copy instead.
		if cpErr := copyFile(tmpPath, finalName); cpErr != nil {
			return "", fmt.Errorf("move archive: %w", cpErr)
		}
		os.Remove(tmpPath)
	}

	success = true
	return finalName, nil
}

// exportViaGit uses git ls-files to get tracked files and writes them to tar.
func exportViaGit(tw *tar.Writer, sourceDir, prefix string) error {
	cmd := exec.Command("git", "ls-files", "-z") //nolint:gosec,noctx // controlled internal path
	cmd.Dir = sourceDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git ls-files: %w\nstderr: %s", err, stderr.String())
	}

	files := strings.Split(stdout.String(), "\x00")
	for _, rel := range files {
		if rel == "" {
			continue
		}
		fullPath := filepath.Join(sourceDir, rel)
		fi, err := os.Stat(fullPath)
		if err != nil {
			continue // deleted but tracked — skip
		}
		if fi.IsDir() {
			continue
		}
		if skipFiles[fi.Name()] {
			continue
		}

		if err := addFileToTar(tw, fullPath, filepath.Join(prefix, rel), fi); err != nil {
			return err
		}
	}

	// Also include untracked TIMELINE.md if it exists (may not be committed yet).
	timelinePath := filepath.Join(sourceDir, "TIMELINE.md")
	if ti, err := os.Stat(timelinePath); err == nil {
		// Check if already included.
		found := false
		for _, f := range files {
			if f == "TIMELINE.md" {
				found = true
				break
			}
		}
		if !found {
			if err := addFileToTar(tw, timelinePath, filepath.Join(prefix, "TIMELINE.md"), ti); err != nil {
				return err
			}
		}
	}

	return nil
}

// exportViaWalk walks the filesystem with skip lists (no .git available).
func exportViaWalk(tw *tar.Writer, sourceDir, prefix string) error {
	return filepath.Walk(sourceDir, func(path string, fi os.FileInfo, walkErr error) error {
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

		rel, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return fmt.Errorf("rel path: %w", err)
		}

		// Skip known generated/cached file patterns.
		relSlash := strings.ReplaceAll(rel, string(filepath.Separator), "/")
		if matchesSkipPattern(relSlash) {
			return nil
		}

		return addFileToTar(tw, path, filepath.Join(prefix, rel), fi)
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

// validateRecipeLayout checks for common structure issues and returns warnings.
func validateRecipeLayout(sourceDir string) []string {
	var warnings []string

	// Check for environments/ directory.
	envsDir := filepath.Join(sourceDir, "environments")
	if _, err := os.Stat(envsDir); os.IsNotExist(err) {
		// Check if env folders are at root level (not nested).
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

	// Check for appdev/ or similar app directory.
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

// hasGitDir checks if sourceDir contains a .git directory.
func hasGitDir(sourceDir string) bool {
	info, err := os.Stat(filepath.Join(sourceDir, ".git"))
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

		// Target path in zeropsio/recipes: {slug}/{relative}
		targetPath := slug + "/" + strings.ReplaceAll(rel, string(filepath.Separator), "/")
		files[targetPath] = string(data)

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk environments: %w", err)
	}

	return files, nil
}
