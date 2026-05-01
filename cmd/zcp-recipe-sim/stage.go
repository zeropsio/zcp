// Per-codebase staging helpers — copy code artifacts (src/**,
// package.json, composer.json, app/**) from a frozen run dir into
// the simulation dir so the replayed codebase-content +
// claudemd-author sub-agents can read what they reference.
//
// Spec: docs/zcprecipator3/plans/run-20-prep.md §S3.
//
// Codebase-content brief reads `<SourceRoot>/zerops.yaml` (already
// staged) plus `Glob <SourceRoot>/src/**`
// (briefs_content_phase.go:124-128). Claudemd-author brief explicitly
// reads `package.json`, `composer.json`, `src/**`, `app/**`
// (briefs_content_phase.go:304). We stage the union; the replayed
// agent runs against the same file shape it would in production.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// stagedTopLevelFiles is the file allowlist at the root of a codebase
// dir. The two briefs reference these by name; we copy verbatim when
// present.
var stagedTopLevelFiles = []string{
	"package.json",
	"composer.json",
}

// stagedTopLevelDirs is the directory allowlist at the root of a
// codebase dir. Walked recursively under the skip rules (see
// shouldSkipDir).
var stagedTopLevelDirs = []string{
	"src",
	"app", // Laravel — claudemd-author reads app/** explicitly
}

// skipDirNames are the directory bases that are skipped at every
// depth — bulk artifact dirs nobody references and that bloat the
// staged tree. node_modules + vendor + .git are the canonical set
// per S3.
var skipDirNames = map[string]bool{
	"node_modules": true,
	"vendor":       true,
	".git":         true,
}

// stageCodebaseArtifacts copies the documented union of code
// artifacts from `runHostDir` into `simHostDir`. Both are absolute
// per-codebase directories ending in `dev/` per engine M-1.
//
// What's copied:
//   - top-level files in stagedTopLevelFiles (package.json,
//     composer.json) when present
//   - the recursive contents of every dir in stagedTopLevelDirs
//     (src/, app/), pruning skipDirNames at every depth
//
// What's NOT copied (matches the briefs' read patterns, no over-
// staging): tsconfig.json, vite.config.*, Dockerfile, README.md,
// dist/, build/, lock files, etc. The two briefs don't reference
// these; staging them just bloats the tree.
//
// Returns the count of files staged + any I/O error.
func stageCodebaseArtifacts(runHostDir, simHostDir string) error {
	if runHostDir == "" || simHostDir == "" {
		return fmt.Errorf("stageCodebaseArtifacts: runHostDir and simHostDir required")
	}

	for _, name := range stagedTopLevelFiles {
		src := filepath.Join(runHostDir, name)
		if _, err := os.Stat(src); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("stat %s: %w", src, err)
		}
		dst := filepath.Join(simHostDir, name)
		if err := copyFileMode(src, dst); err != nil {
			return fmt.Errorf("copy %s: %w", name, err)
		}
	}

	for _, dirName := range stagedTopLevelDirs {
		src := filepath.Join(runHostDir, dirName)
		if _, err := os.Stat(src); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("stat %s: %w", src, err)
		}
		dst := filepath.Join(simHostDir, dirName)
		if err := copyTree(src, dst); err != nil {
			return fmt.Errorf("copy tree %s: %w", dirName, err)
		}
	}
	return nil
}

// copyTree copies the tree rooted at src to dst, pruning directories
// whose base is in skipDirNames (at any depth). Mirrors mode bits via
// copyFileMode for executable-bit fidelity (some src/** entries are
// shell scripts).
func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		// Skip bulk dirs at any depth. SkipDir prunes the subtree.
		if info.IsDir() && rel != "." && skipDirNames[info.Name()] {
			return filepath.SkipDir
		}
		// Also skip if any ancestor segment is in skipDirNames (defensive
		// — filepath.Walk's SkipDir handles this for us, but a bad
		// caller could pass a path already inside a skipped tree).
		if info.IsDir() {
			if dirContainsSkippedSegment(rel) {
				return filepath.SkipDir
			}
			return nil
		}
		if dirContainsSkippedSegment(filepath.Dir(rel)) {
			return nil
		}
		dstPath := filepath.Join(dst, rel)
		return copyFileMode(path, dstPath)
	})
}

// dirContainsSkippedSegment reports whether any path segment of `rel`
// (with `.` interpreted as no segments) is a skip name.
func dirContainsSkippedSegment(rel string) bool {
	if rel == "." || rel == "" {
		return false
	}
	for seg := range strings.SplitSeq(rel, string(filepath.Separator)) {
		if skipDirNames[seg] {
			return true
		}
	}
	return false
}

// copyFileMode copies src → dst preserving the source mode bits and
// creating dst's parent directory if needed.
func copyFileMode(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	info, err := in.Stat()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}
