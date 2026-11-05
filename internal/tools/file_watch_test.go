// Tests for: tools/file_watch.go — the Mate's service wakes when the
// Mate's server renames a `state` frame's changes into place, including when
// the link's directory did not exist yet as the watch began.
package tools

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWatchMateChanges_DirectoryCreatedLater_RenameWakes: with no directory
// for the file yet, the watch waits on the nearest one that exists and
// follows each directory created on the way, so the first frame renamed into
// place wakes the service.
func TestWatchMateChanges_DirectoryCreatedLater_RenameWakes(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), ".zcp", "hq", "changes.json")
	changed, err := watchReplacedFile(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".tmp", []byte(`{"changes":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".tmp", path); err != nil {
		t.Fatal(err)
	}
	<-changed
}

// TestWatchMateChanges_EntryVanishesWhileWatched_RenameWakes: an entry of
// the directory that cannot be followed as the watch is placed (a writer's
// temporary file renamed away, here a link to nothing) does not end the
// watch: the next frame renamed into place still wakes the service.
func TestWatchMateChanges_EntryVanishesWhileWatched_RenameWakes(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), ".zcp", "hq")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "gone"), filepath.Join(dir, "changes.json.old")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "changes.json")
	changed, err := watchReplacedFile(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".tmp", []byte(`{"changes":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".tmp", path); err != nil {
		t.Fatal(err)
	}
	<-changed
}
