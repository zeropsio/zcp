package mate

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// An update is a transaction over the server's state (§2.1d "The state
// transaction"). Before a version that last-good does not name first starts,
// the unit's start command copies the state aside, tagged with that version;
// a rollback to last-good puts the copy back by renames; the copy is dropped
// once last-good names its version. The copy is the swapped set: the state
// directory without its logs, and the uploads index beside it.
const (
	stateDirName     = "userdata"
	stateLogsName    = "logs"
	uploadsIndexName = "uploads-index"
	stateCopyTemp    = ".state-copy"
	stateDropPrefix  = ".state-drop-"
	stateCopyInfix   = ".pre-"
	stateFailedInfix = ".failed-"
)

// swappedItems are the swapped set's names under BaseDir(). The state
// directory comes last: its copy is the tag, made last by a copy, put back
// last by a restore and moved away first by a drop.
var swappedItems = []string{uploadsIndexName, stateDirName}

// PrepareStateForLaunch is the last step before every launch of the server,
// run by the unit's start command while the server is down and no state
// writer can run. It decides from the copy's tag and the version about to
// launch, never from the update record:
//
//   - a copy tagged with last-good: the update was committed — drop it;
//   - a copy tagged with the version about to launch: that version starts
//     again on its own state — keep it;
//   - a copy tagged otherwise while last-good is about to launch: a rollback —
//     restore it;
//   - a copy tagged otherwise while another version is about to launch: a
//     route outside the transaction (§2.1d) installed it — drop it;
//   - then, with no copy and a version about to launch that last-good does
//     not name: copy.
//
// Before it decides, it finishes what a start cut short left: a tagged copy
// beside no live state directory is a restore between its first and last
// rename, and is finished; a failed set beside no live state directory and
// no copy launches nothing; no live state directory with neither is a new
// Mate, launched without a copy. A failed copy or restore returns an error and the
// caller does not launch. The returned line says what was done, empty when
// nothing was.
func PrepareStateForLaunch() (string, error) {
	launching, err := InstalledVersion()
	if err != nil {
		return "", fmt.Errorf("state before launch: %w", err)
	}
	lastGood, err := LastGoodVersion()
	if errors.Is(err, fs.ErrNotExist) {
		lastGood = ""
	} else if err != nil {
		return "", fmt.Errorf("state before launch: %w", err)
	}
	return prepareState(BaseDir(), launching, lastGood)
}

func prepareState(base, launching, lastGood string) (string, error) {
	if err := clearStateLeftovers(base); err != nil {
		return "", err
	}
	tag, err := stateCopyTag(base)
	if err != nil {
		return "", err
	}
	var done []string
	liveMissing, err := missing(filepath.Join(base, stateDirName))
	if err != nil {
		return "", err
	}
	if tag != "" && liveMissing {
		if err := restoreState(base, tag); err != nil {
			return "", err
		}
		done = append(done, "finished the restore of the state from before "+tag)
		tag = ""
	} else if tag == "" && liveMissing {
		failed, err := filepath.Glob(filepath.Join(base, stateDirName+stateFailedInfix+"*"))
		if err != nil {
			return "", fmt.Errorf("find failed state: %w", err)
		}
		if len(failed) > 0 {
			return "", fmt.Errorf("state directory %s is missing beside %v: nothing is launched on an empty state", filepath.Join(base, stateDirName), failed)
		}
		// A Mate that has no state yet has nothing to copy: its first launch creates it.
		return "", nil
	}
	switch {
	case tag == "":
	case tag == lastGood:
		if err := dropStateCopy(base, tag); err != nil {
			return "", err
		}
		done = append(done, "dropped the committed copy for "+tag)
	case tag == launching:
		return "", nil
	case launching == lastGood:
		if err := restoreState(base, tag); err != nil {
			return "", err
		}
		return "restored the state from before " + tag, nil
	default:
		if err := dropStateCopy(base, tag); err != nil {
			return "", err
		}
		done = append(done, "dropped the copy for "+tag+", which "+launching+" replaced outside an update")
	}
	if launching != lastGood {
		if err := copyState(base, launching); err != nil {
			return "", err
		}
		done = append(done, "copied the state before "+launching)
	}
	return strings.Join(done, "; "), nil
}

// clearStateLeftovers removes what a start cut short left that no tag names:
// a copy still being built and a dropped copy still being removed.
func clearStateLeftovers(base string) error {
	if err := os.RemoveAll(filepath.Join(base, stateCopyTemp)); err != nil {
		return fmt.Errorf("clear state copy: %w", err)
	}
	return removeMatching(filepath.Join(base, stateDropPrefix+"*"))
}

func missing(path string) (bool, error) {
	_, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("read %s: %w", path, err)
	}
	return false, nil
}

// stateCopyTag names the version the one copy under base is tagged with,
// empty when there is none.
func stateCopyTag(base string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(base, stateDirName+stateCopyInfix+"*"))
	if err != nil {
		return "", fmt.Errorf("find state copy: %w", err)
	}
	switch len(matches) {
	case 0:
		return "", nil
	case 1:
		return strings.TrimPrefix(filepath.Base(matches[0]), stateDirName+stateCopyInfix), nil
	default:
		return "", fmt.Errorf("state copies %v: more than one, none is restored", matches)
	}
}

// copyState builds the copy in a temporary directory, syncs it and renames
// it into place; the state directory's rename, the tag, is the last. A copy
// that fails leaves no space taken. prepareState has cleared the temporary
// directory a start cut short left.
func copyState(base, version string) (retErr error) {
	tmp := filepath.Join(base, stateCopyTemp)
	defer func() {
		if retErr != nil {
			_ = os.RemoveAll(tmp)
			_ = dropStateCopy(base, version)
		}
	}()
	// An index copy without a tag is a copy cut short.
	if err := removeMatching(filepath.Join(base, uploadsIndexName+stateCopyInfix+"*")); err != nil {
		return err
	}
	if err := os.Mkdir(tmp, 0o700); err != nil {
		return fmt.Errorf("create state copy: %w", err)
	}
	for _, item := range swappedItems {
		skip := ""
		if item == stateDirName {
			skip = stateLogsName
		}
		if err := copyTree(filepath.Join(base, item), filepath.Join(tmp, item), skip); err != nil {
			return fmt.Errorf("copy %s: %w", item, err)
		}
	}
	if err := syncDir(tmp); err != nil {
		return err
	}
	for _, item := range swappedItems {
		if err := os.Rename(filepath.Join(tmp, item), filepath.Join(base, item+stateCopyInfix+version)); err != nil {
			return fmt.Errorf("place state copy: %w", err)
		}
	}
	if err := syncDir(base); err != nil {
		return err
	}
	if err := os.Remove(tmp); err != nil {
		return fmt.Errorf("remove state copy directory: %w", err)
	}
	return nil
}

// restoreState puts the copy tagged version back by renames: the live state
// directory aside first, then the index (live aside, copy into place), the
// tag into the live state directory last. From the first rename to the last
// no live state directory stands beside the tag, which is how prepareState
// knows a restore cut short and finishes it; each step skips what an earlier
// run already did. Files are never copied back over a live directory.
//
// A live item set aside replaces the item's previous failed set: a failed set
// holds the state of a version that ran behind the boot fence, which admits
// no work, so the copy put back holds all the work a person did (§2.1d).
func restoreState(base, version string) error {
	if err := setStateAside(base, stateDirName, version); err != nil {
		return err
	}
	for _, item := range swappedItems {
		if item == stateDirName {
			continue
		}
		saved, err := missing(filepath.Join(base, item+stateCopyInfix+version))
		if err != nil {
			return err
		}
		if saved {
			continue
		}
		if err := setStateAside(base, item, version); err != nil {
			return err
		}
		if err := putStateBack(base, item, version); err != nil {
			return err
		}
	}
	if err := putStateBack(base, stateDirName, version); err != nil {
		return err
	}
	// The logs are no state: they stay with the live directory.
	failedLogs := filepath.Join(base, stateDirName+stateFailedInfix+version, stateLogsName)
	liveLogs := filepath.Join(base, stateDirName, stateLogsName)
	if _, err := os.Lstat(liveLogs); errors.Is(err, fs.ErrNotExist) {
		_ = os.Rename(failedLogs, liveLogs)
	}
	return nil
}

// setStateAside renames the live item to its failed set for version, when
// there is a live item.
func setStateAside(base, item, version string) error {
	live := filepath.Join(base, item)
	gone, err := missing(live)
	if err != nil || gone {
		return err
	}
	if err := removeMatching(filepath.Join(base, item+stateFailedInfix+"*")); err != nil {
		return err
	}
	if err := os.Rename(live, filepath.Join(base, item+stateFailedInfix+version)); err != nil {
		return fmt.Errorf("restore %s: set the failed state aside: %w", item, err)
	}
	return syncDir(base)
}

// putStateBack renames the item's copy tagged version into the live item.
func putStateBack(base, item, version string) error {
	if err := os.Rename(filepath.Join(base, item+stateCopyInfix+version), filepath.Join(base, item)); err != nil {
		return fmt.Errorf("restore %s: %w", item, err)
	}
	return syncDir(base)
}

// dropStateCopy removes the copy tagged version. The tag stops naming it
// before any of it is removed: the tag, then the index copy, move into a new
// trash directory, the move is synced, and only then the trash is removed. A
// drop cut short leaves trash no tag names, which prepareState clears.
func dropStateCopy(base, version string) error {
	var held []string
	for i := len(swappedItems) - 1; i >= 0; i-- {
		item := swappedItems[i]
		gone, err := missing(filepath.Join(base, item+stateCopyInfix+version))
		if err != nil {
			return fmt.Errorf("drop state copy: %w", err)
		}
		if !gone {
			held = append(held, item)
		}
	}
	if len(held) == 0 {
		return nil
	}
	trash, err := os.MkdirTemp(base, stateDropPrefix+"*")
	if err != nil {
		return fmt.Errorf("drop state copy: %w", err)
	}
	for _, item := range held {
		if err := os.Rename(filepath.Join(base, item+stateCopyInfix+version), filepath.Join(trash, item)); err != nil {
			return fmt.Errorf("drop state copy: %w", err)
		}
	}
	if err := syncDir(base); err != nil {
		return err
	}
	if err := os.RemoveAll(trash); err != nil {
		return fmt.Errorf("drop state copy: %w", err)
	}
	return nil
}

func removeMatching(pattern string) error {
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("find %s: %w", pattern, err)
	}
	for _, m := range matches {
		if err := os.RemoveAll(m); err != nil {
			return fmt.Errorf("remove %s: %w", m, err)
		}
	}
	return nil
}

// copyTree copies the directory src to dst, which does not exist yet, file
// by file with each file and directory synced; the top-level entry skip is
// left out. A missing src copies as an empty directory: the state before a
// first boot is no state. Symlinks are copied as links; sockets and pipes
// are no state.
func copyTree(src, dst, skip string) error {
	info, err := os.Lstat(src)
	if errors.Is(err, fs.ErrNotExist) {
		if err := os.Mkdir(dst, 0o700); err != nil {
			return fmt.Errorf("create %s: %w", dst, err)
		}
		return syncDir(dst)
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", src, err)
	}
	if err := os.Mkdir(dst, info.Mode().Perm()); err != nil {
		return fmt.Errorf("create %s: %w", dst, err)
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("list %s: %w", src, err)
	}
	for _, e := range entries {
		if skip != "" && e.Name() == skip {
			continue
		}
		from, to := filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())
		switch t := e.Type(); {
		case t.IsDir():
			if err := copyTree(from, to, ""); err != nil {
				return err
			}
		case t&fs.ModeSymlink != 0:
			target, err := os.Readlink(from)
			if err != nil {
				return fmt.Errorf("read link %s: %w", from, err)
			}
			if err := os.Symlink(target, to); err != nil {
				return fmt.Errorf("copy link %s: %w", from, err)
			}
		case t.IsRegular():
			if err := copyFile(from, to); err != nil {
				return err
			}
		}
	}
	return syncDir(dst)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer func() { _ = in.Close() }()
	info, err := in.Stat()
	if err != nil {
		return fmt.Errorf("stat %s: %w", src, err)
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return fmt.Errorf("create %s: %w", dst, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return fmt.Errorf("copy %s: %w", src, err)
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		return fmt.Errorf("sync %s: %w", dst, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close %s: %w", dst, err)
	}
	return nil
}

func syncDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open %s: %w", dir, err)
	}
	defer func() { _ = f.Close() }()
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync %s: %w", dir, err)
	}
	return nil
}
