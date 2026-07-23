package skillpacks

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"os"
	"path/filepath"
	"sort"
)

// digestPrefix marks the hash algorithm a stored digest string uses; every
// digest skillpacks ever writes or compares carries it.
const digestPrefix = "sha256:"

// digestRow is one entry contributed to a tree digest: its slash-relative
// path from the tree root, a one-byte kind tag, and (for a regular file or
// symlink only) its content/target bytes. size is redundant with
// len(data) for a regular file but is hashed explicitly so a digest is
// unambiguous about which representation a kind='f' row uses.
type digestRow struct {
	relPath string
	kind    byte // 'd' directory, 'f' regular file, 'l' symlink, 'o' other (device/socket/FIFO)
	size    int64
	data    []byte
}

// treeDigest computes the canonical content digest of the directory tree at
// root's rel path, excluding only markerFileName at the tree's own root. It
// hashes, in sorted slash-relative path order: entry kind, path, size, and
// content — matching spec-welcome-mode's digest contract exactly (see
// skillpacks package doc).
//
// This walks with Lstat at every entry rather than following symlinks
// (unlike a plain fs.WalkDir over root.FS()), so it stays well-defined for
// an EXISTING, possibly user-modified installed copy at pack-status time: a
// symlink or special file the user later added there changes the digest
// deterministically (a 'modified' verdict) rather than causing an error or a
// followed read outside the tree. A staged copy skillpacks itself builds is
// guaranteed by construction (see copy.go) to contain only 'd' and 'f' rows;
// 'l'/'o' rows only ever arise when hashing a directory this package did not
// itself just create.
func treeDigest(root *os.Root, rel string) (string, error) {
	var rows []digestRow
	if err := collectDigestRows(root, rel, ".", markerFileName, &rows); err != nil {
		return "", err
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].relPath < rows[j].relPath })

	h := sha256.New()
	for _, r := range rows {
		writeDigestRow(h, r)
	}
	return digestPrefix + hex.EncodeToString(h.Sum(nil)), nil
}

func writeDigestRow(h hash.Hash, r digestRow) {
	_, _ = fmt.Fprintf(h, "%c\x00%s\x00%d\x00", r.kind, r.relPath, r.size)
	h.Write(r.data)
	h.Write([]byte{0})
}

// collectDigestRows recurses depth-first from tree root treeRel, appending
// one digestRow per entry (relative to treeRel) into out. dirRel is "."
// for the tree root itself and grows with each recursion.
func collectDigestRows(root *os.Root, treeRel, dirRel, excludeName string, out *[]digestRow) error {
	listRel := treeRel
	if dirRel != "." {
		listRel = filepath.Join(treeRel, dirRel)
	}
	names, err := readDirNames(root, listRel)
	if err != nil {
		return fmt.Errorf("read %s: %w", listRel, err)
	}

	for _, name := range names {
		if dirRel == "." && name == excludeName {
			continue
		}
		entryRel := name
		if dirRel != "." {
			entryRel = filepath.Join(dirRel, name)
		}
		entryFull := filepath.Join(listRel, name)

		info, err := root.Lstat(entryFull)
		if err != nil {
			return fmt.Errorf("lstat %s: %w", entryFull, err)
		}
		slashPath := filepath.ToSlash(entryRel)

		switch {
		case info.Mode()&os.ModeSymlink != 0:
			target, err := root.Readlink(entryFull)
			if err != nil {
				return fmt.Errorf("readlink %s: %w", entryFull, err)
			}
			*out = append(*out, digestRow{relPath: slashPath, kind: 'l', data: []byte(target)})
		case info.IsDir():
			*out = append(*out, digestRow{relPath: slashPath, kind: 'd'})
			if err := collectDigestRows(root, treeRel, entryRel, excludeName, out); err != nil {
				return err
			}
		case info.Mode().IsRegular():
			data, err := root.ReadFile(entryFull)
			if err != nil {
				return fmt.Errorf("read %s: %w", entryFull, err)
			}
			*out = append(*out, digestRow{relPath: slashPath, kind: 'f', size: info.Size(), data: data})
		default:
			*out = append(*out, digestRow{relPath: slashPath, kind: 'o'})
		}
	}
	return nil
}

// readDirNames returns, in whatever order the directory yields them
// (collectDigestRows sorts the resulting rows, so order here is
// immaterial), every entry name directly inside root's dirRel.
func readDirNames(root *os.Root, dirRel string) ([]string, error) {
	f, err := root.Open(dirRel)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	entries, err := f.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	return names, nil
}
