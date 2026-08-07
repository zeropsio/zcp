package adapters

import (
	"errors"
	"fmt"
	"os"
)

// EnsureDir creates dir and its parents, replacing an opaque MkdirAll
// failure with what the offending path actually IS.
//
// os.MkdirAll reports a bare EEXIST/ENOTDIR naming the ANCESTOR it tripped
// over — `mkdir /root/.claude: file exists` — which is the same message for
// a dangling symlink, a symlink loop, and a symlink into a mount that has
// not come up yet. None of those are repairable from the message alone, and
// the entry is typically a deliberate operator arrangement (an agent's
// config directory symlinked into persistent storage so history survives a
// restart) that becomes valid moments later. So ZCP names it and leaves it
// alone; it never deletes or retargets an entry it did not create.
//
// It lives here rather than in the parent init package because both layers
// need it and the dependency runs init/ → init/adapters/.
func EnsureDir(dir string) error {
	err := os.MkdirAll(dir, 0o755)
	if err == nil {
		return nil
	}
	var pathErr *os.PathError
	if !errors.As(err, &pathErr) {
		return err
	}
	obstruction := describeObstruction(pathErr.Path)
	if obstruction == "" {
		return err
	}
	return fmt.Errorf("cannot create %s — %s", dir, obstruction)
}

// describeObstruction reports what the existing entry at path is, for the
// case where it is something other than a usable directory. It returns ""
// when path is not the explanation (absent, or a perfectly good directory —
// e.g. a plain permission failure), leaving the caller its original error.
func describeObstruction(path string) string {
	info, err := os.Lstat(path)
	if err != nil {
		return ""
	}
	if info.Mode()&os.ModeSymlink == 0 {
		if info.IsDir() {
			return ""
		}
		return fmt.Sprintf("%s exists as a %s, not a directory", path, entryKind(info.Mode()))
	}
	target, err := os.Readlink(path)
	if err != nil {
		return fmt.Sprintf("%s is a symlink that cannot be read: %v", path, err)
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Sprintf("%s is a symlink to %s that does not resolve (%v) — left untouched", path, target, err)
	}
	return fmt.Sprintf("%s is a symlink to %s, which is not a directory — left untouched", path, target)
}

// entryKind names a non-directory file mode in operator prose.
func entryKind(m os.FileMode) string {
	switch {
	case m.IsRegular():
		return "regular file"
	case m&os.ModeNamedPipe != 0:
		return "named pipe"
	case m&os.ModeSocket != 0:
		return "socket"
	default:
		return "non-directory entry"
	}
}
