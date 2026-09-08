package capture

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// ReadManifestFile is the one read-only accessor a downstream consumer (the
// behavioral report, docs/spec-capture-inspector.md §8.5) uses to read one
// canonical raw file out of a finalized capture window. It resolves relPath
// with the same path resolver InspectSession uses (inside the window, no
// symlink component, regular file), requires the path be listed in
// manifest.Files, then re-verifies size and SHA-256 against the manifest
// entry immediately before returning the bytes — the same re-verify-before-
// read rule §8 states for detail files.
func ReadManifestFile(sessionDir string, manifest *SessionManifestDocument, relPath string) ([]byte, error) {
	if manifest == nil {
		return nil, errors.New("read manifest file: capture window has no manifest.json (legacy window)")
	}
	path, err := resolveInspectionPath(sessionDir, relPath)
	if err != nil {
		return nil, fmt.Errorf("read manifest file %q: %w", relPath, err)
	}
	normalized := filepath.ToSlash(relPath)
	var expected *ManifestFile
	for index := range manifest.Files {
		if filepath.ToSlash(manifest.Files[index].Path) == normalized {
			expected = &manifest.Files[index]
			break
		}
	}
	if expected == nil {
		return nil, fmt.Errorf("read manifest file %q: not listed in capture manifest", relPath)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("stat manifest file %q: %w", relPath, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("read manifest file %q: not a regular file", relPath)
	}
	if info.Size() != expected.SizeBytes {
		return nil, fmt.Errorf("read manifest file %q: size mismatch: got %d, want %d", relPath, info.Size(), expected.SizeBytes)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest file %q: %w", relPath, err)
	}
	actual := sha256File(data)
	if actual != expected.SHA256 {
		return nil, fmt.Errorf("read manifest file %q: hash mismatch: got %s, want %s", relPath, actual, expected.SHA256)
	}
	return data, nil
}

func sha256File(data []byte) string {
	hash := sha256.New()
	_, _ = io.Writer(hash).Write(data)
	return hex.EncodeToString(hash.Sum(nil))
}
