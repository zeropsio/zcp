//go:build unix

// Tests for: ops/pool_migrate_bundle.go — the bundle of a repository that
// sits on a filesystem other than its scratch directory's, as a variant-B
// pair's checkout on its code volume does (docs/spec-workflows.md §13 PM-3,
// PM-13).

package ops

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// otherFilesystemDir is a writable directory on a filesystem other than
// path's, for the scratch directory; the test is skipped where the machine
// offers none (a tmpfs at /dev/shm is one on most Linux hosts).
func otherFilesystemDir(t *testing.T, path string) string {
	t.Helper()
	var here syscall.Stat_t
	if err := syscall.Stat(path, &here); err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	for _, candidate := range []string{"/dev/shm"} {
		var there syscall.Stat_t
		if err := syscall.Stat(candidate, &there); err != nil || there.Dev == here.Dev {
			continue
		}
		dir, err := os.MkdirTemp(candidate, "zcp-bundle-test-") //nolint:usetesting // it must lie on another filesystem than t.TempDir's
		if err != nil {
			continue
		}
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
		return dir
	}
	t.Skip("no writable directory on another filesystem here")
	return ""
}

// TestPoolMigrateBundle_ScratchOnAnotherFilesystem_Bundles: the bundle of a
// repository whose scratch directory lies on another filesystem — the code
// volume's checkout bundled through /tmp — is written and verified, never a
// clone dying on a hard link across the two ("Invalid cross-device link").
func TestPoolMigrateBundle_ScratchOnAnotherFilesystem_Bundles(t *testing.T) {
	t.Parallel()
	_, root, _ := bundleFixture(t)
	work := filepath.Join(otherFilesystemDir(t, root), "zcp-pool-migrate")
	b, err := BuildPoolMigrateBundle(t.Context(), &bundleLocalSSH{}, PoolMigrateBundleInput{
		Dev: "appdev", Root: root, WorkDir: work, Path: filepath.Join(t.TempDir(), "dev.bundle"),
	})
	if err != nil {
		t.Fatalf("BuildPoolMigrateBundle with the scratch directory on another filesystem: %v", err)
	}
	if b.Refs == 0 || len(b.Trees) == 0 {
		t.Fatalf("bundle = %+v, want its refs and trees", b)
	}
}
