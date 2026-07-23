package skillpacks

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// copyTreeIntoRoot copies the directory tree rooted at src (a plain
// filesystem path — a freshly cloned repo's validated skill subtree;
// untrusted content, trusted path) into destRel, a staging path relative to
// root's workspace. Every destination write goes through root, so a
// symlinked ancestor is refused rather than followed.
//
// discoverSkills has already hard-validated that a selected skill tree
// contains no symlinks or special files; this copy hard-errors on one too
// (defense in depth against a caller that reaches here without going
// through discovery first) rather than silently skipping it.
func copyTreeIntoRoot(root *os.Root, src, destRel string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return fmt.Errorf("resolve relative path for %s: %w", path, err)
		}
		target := destRel
		if rel != "." {
			target = filepath.Join(destRel, rel)
		}

		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("stat %s: %w", path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return codedErrorf(CodeInvalidSource, "%s is a symlink; symlinks inside a selected skill tree are not allowed", path)
		}
		if d.IsDir() {
			return root.MkdirAll(target, 0o755)
		}
		if !info.Mode().IsRegular() {
			return codedErrorf(CodeInvalidSource, "%s is not a regular file or directory", path)
		}
		return copyRegularFileIntoRoot(root, path, target, info.Mode())
	})
}

func copyRegularFileIntoRoot(root *os.Root, src, destRel string, mode fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer func() { _ = in.Close() }()

	out, err := root.OpenFile(destRel, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode.Perm())
	if err != nil {
		return wrapCoded(CodeFilesystem, err, "create %s", destRel)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return fmt.Errorf("copy %s to %s: %w", src, destRel, err)
	}
	return out.Close()
}

// renameNoReplace publishes a fully-built staged directory at oldRel to
// newRel, refusing if newRel already exists.
//
// This is a best-effort no-replace guard, not a kernel-level atomic
// primitive: unlike a file, a directory has no portable O_EXCL-equivalent
// rename in the Go standard library (Linux's renameat2(RENAME_NOREPLACE)
// and Darwin's renamex_np(RENAME_EXCL) are platform-specific syscalls this
// package does not shell out to). The residual TOCTOU window between the
// existence check and the rename is narrow, and every zcp-driven mutation
// of this workspace is already serialized by the exclusive cross-process
// skill-packs lock (see lock.go) — the only remaining race is an external
// actor (not another zcp process) creating newRel in that instant, which
// this guard still catches as a collision on this call's own re-check
// rather than a silently-clobbered destination.
func renameNoReplace(root *os.Root, oldRel, newRel string) error {
	exists, err := rootExists(root, newRel)
	if err != nil {
		return err
	}
	if exists {
		return codedErrorf(CodeCollision, "%s already exists; nothing was changed", newRel)
	}
	if err := root.Rename(oldRel, newRel); err != nil {
		return wrapCoded(CodeFilesystem, err, "publish %s", newRel)
	}
	return nil
}
