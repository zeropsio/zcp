package mate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The versions a container holds, and the one choice made from them
// (docs/spec-mate.md §2.1a). Every fact lives under Prefix(): `current` names
// the version the unit starts, `last-good` the newest version that came up
// under this zcp, `failed-version` the newest version that did not, versions/<v>
// the downloaded versions, and manifest.json the release list as last read. The start job
// (Start) is the one writer of the two links and the failed version.

// LastGoodLink names the newest version that came up under this zcp.
func LastGoodLink() string { return filepath.Join(Prefix(), "last-good") }

// FailedVersionPath holds the newest version that did not come up, as text.
func FailedVersionPath() string { return filepath.Join(Prefix(), "failed-version") }

// Versions is what a container holds about which Mate runs. An empty field is
// a fact that is not there: nothing installed, nothing came up, nothing failed.
type Versions struct {
	Current  string
	LastGood string
	Failed   string
}

// ReadVersions reads the versions the container holds. A link or record that
// cannot be read is one that is not there.
func ReadVersions() Versions {
	var v Versions
	v.Current, _ = InstalledVersion()
	v.LastGood, _ = LastGoodVersion()
	if raw, err := os.ReadFile(FailedVersionPath()); err == nil {
		v.Failed = strings.TrimSpace(string(raw))
	}
	return v
}

// VersionToRun is the one answer to "which Mate runs", read by the start job
// that launches it and by the keep job that restarts for it: the release the
// release list names (latest, "" when the list was not read) when it is newer than
// last-good, newer than the failed version and not older than current;
// otherwise current, or last-good when current is missing or older. It never
// moves current back: a release list that is missing or goes backwards
// changes nothing, and a current newer than last-good runs and is judged by
// its start, whose verdict alone returns it to last-good (Start.down). A dev
// build at or above MinimumMateVersion in current is kept as it is
// (eval/scripts/mate-dev-push.sh). "" when nothing is installed.
func VersionToRun(v Versions, latest string) string {
	if IsDevVersion(v.Current) && !BelowFloor(v.Current) {
		return v.Current
	}
	if latest != "" && newerThan(latest, v.LastGood) && newerThan(latest, v.Failed) && !VersionOlder(latest, v.Current) {
		return latest
	}
	if v.Current == "" || VersionOlder(v.Current, v.LastGood) {
		return v.LastGood
	}
	return v.Current
}

// newerThan says version is newer than other; every version is newer than none.
func newerThan(version, other string) bool {
	return other == "" || VersionOlder(other, version)
}

// LastGoodVersion is the version last-good names.
func LastGoodVersion() (string, error) {
	dir, err := linkedVersionDir(LastGoodLink())
	if err != nil {
		return "", err
	}
	return installedVersionIn(dir)
}

// canBeLastGood says whether version may become last-good: never a dev build,
// nor a release below MinimumMateVersion, a contract this zcp no longer drives.
func canBeLastGood(version string) bool {
	return !IsDevVersion(version) && !VersionOlder(version, MinimumMateVersion)
}

// writeFailedVersion records version as the newest version that did not come
// up: written under a temporary name and renamed, so a crash leaves the old
// record or the new one.
func writeFailedVersion(version string) error {
	tmp := FailedVersionPath() + ".tmp"
	if err := os.WriteFile(tmp, []byte(version+"\n"), 0o600); err != nil {
		return fmt.Errorf("write failed version: %w", err)
	}
	if err := os.Rename(tmp, FailedVersionPath()); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("record failed version: %w", err)
	}
	return syncPrefix()
}

// forgetFailedVersion drops a failed version that version has left behind.
func forgetFailedVersion(version string) error {
	raw, err := os.ReadFile(FailedVersionPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read failed version: %w", err)
	}
	if VersionOlder(version, strings.TrimSpace(string(raw))) {
		return nil
	}
	if err := os.Remove(FailedVersionPath()); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("forget failed version: %w", err)
	}
	return syncPrefix()
}

func replaceLink(link, dir string) error {
	target, err := filepath.Rel(Prefix(), dir)
	if err != nil {
		return fmt.Errorf("version link path: %w", err)
	}
	tmp := link + ".tmp"
	_ = os.Remove(tmp)
	if err := os.Symlink(target, tmp); err != nil {
		return fmt.Errorf("create version link: %w", err)
	}
	if err := os.Rename(tmp, link); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace version link: %w", err)
	}
	return syncPrefix()
}

func syncPrefix() error {
	f, err := os.Open(Prefix())
	if err != nil {
		return fmt.Errorf("open %s: %w", Prefix(), err)
	}
	defer func() { _ = f.Close() }()
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync %s: %w", Prefix(), err)
	}
	return nil
}

func linkedVersionDir(link string) (string, error) {
	target, err := os.Readlink(link)
	if err != nil {
		return "", fmt.Errorf("read version link: %w", err)
	}
	if filepath.IsAbs(target) {
		return target, nil
	}
	return filepath.Join(Prefix(), target), nil
}
