// Package farm implements `zcp eval farm`: the bucket sink client, content
// digests, and the MCP capture-stream reader that back parallel disposable
// eval runs (docs/spec-eval-farm.md).
package farm

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// FileDigest returns the lowercase-hex sha256 digest of one file's exact
// bytes.
func FileDigest(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("hash %s: %w", path, err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// TreeDigest returns the tree digest of a directory: this is the identity
// the controller and `farm report` compare against a bundle's own claims
// (docs/spec-eval-farm.md §1.1).
//
// Rule: sha256 over the sorted list of "<relative-path>\n<file-sha256>\n"
// lines, one per regular file under dir (walked recursively, symlinks not
// followed). Paths are relative to dir and always slash-separated
// (filepath.ToSlash), so the digest is stable across platforms. Sorting the
// paths before hashing makes the digest independent of filesystem creation
// or readdir order; a renamed file changes its path line and therefore the
// digest, even though its content is unchanged.
func TreeDigest(dir string) (string, error) {
	var relPaths []string
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("inspect %s: %w", path, err)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return fmt.Errorf("rel %s: %w", path, err)
		}
		relPaths = append(relPaths, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("walk %s: %w", dir, err)
	}
	sort.Strings(relPaths)

	hash := sha256.New()
	for _, rel := range relPaths {
		fileHash, err := FileDigest(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			return "", err
		}
		if _, err := fmt.Fprintf(hash, "%s\n%s\n", rel, fileHash); err != nil {
			return "", fmt.Errorf("hash tree entry %s: %w", rel, err)
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
