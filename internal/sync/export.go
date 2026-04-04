package sync

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// skipDirs are directories to exclude from the export archive.
var skipDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
}

// ExportRecipe creates a .tar.gz archive of the recipe output directory.
// Returns the path to the created archive.
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

	baseName := filepath.Base(sourceDir)
	archivePrefix := baseName + "-zcprecipator"
	outPath := archivePrefix + ".tar.gz"

	f, err := os.Create(outPath)
	if err != nil {
		return "", fmt.Errorf("create archive: %w", err)
	}
	defer f.Close()

	gw := gzip.NewWriter(f)
	defer gw.Close()

	tw := tar.NewWriter(gw)
	defer tw.Close()

	err = filepath.Walk(sourceDir, func(path string, fi os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		// Skip excluded directories.
		if fi.IsDir() && skipDirs[fi.Name()] {
			return filepath.SkipDir
		}

		// Skip directories themselves — tar entries are files only.
		if fi.IsDir() {
			return nil
		}

		// Skip OS junk files.
		if fi.Name() == ".DS_Store" || fi.Name() == "Thumbs.db" {
			return nil
		}

		// Build archive path: {prefix}/{relative-path}
		rel, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return fmt.Errorf("rel path: %w", err)
		}
		archivePath := filepath.Join(archivePrefix, rel)

		header, err := tar.FileInfoHeader(fi, "")
		if err != nil {
			return fmt.Errorf("file header %s: %w", rel, err)
		}
		header.Name = archivePath

		if err := tw.WriteHeader(header); err != nil {
			return fmt.Errorf("write header %s: %w", rel, err)
		}

		file, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("open %s: %w", rel, err)
		}
		defer file.Close()

		if _, err := io.Copy(tw, file); err != nil {
			return fmt.Errorf("copy %s: %w", rel, err)
		}

		return nil
	})
	if err != nil {
		return "", fmt.Errorf("walk source dir: %w", err)
	}

	return outPath, nil
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
		if fi.Name() == ".DS_Store" || fi.Name() == "Thumbs.db" {
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
