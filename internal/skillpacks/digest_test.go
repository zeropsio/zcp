package skillpacks

import (
	"os"
	"path/filepath"
	"testing"
)

// digestOf hashes the fixture tree every test in this file builds under
// cwd/tree.
func digestOf(t *testing.T, cwd string) string {
	t.Helper()
	root, err := openWorkspaceRoot(cwd)
	if err != nil {
		t.Fatalf("openWorkspaceRoot: %v", err)
	}
	defer func() { _ = root.Close() }()
	d, err := treeDigest(root, "tree")
	if err != nil {
		t.Fatalf("treeDigest: %v", err)
	}
	return d
}

func TestTreeDigest_SameContentSameDigest(t *testing.T) {
	t.Parallel()
	a := t.TempDir()
	writeFile(t, filepath.Join(a, "tree", "SKILL.md"), "# x\n")
	writeFile(t, filepath.Join(a, "tree", "sub", "y.txt"), "y\n")
	b := t.TempDir()
	writeFile(t, filepath.Join(b, "tree", "SKILL.md"), "# x\n")
	writeFile(t, filepath.Join(b, "tree", "sub", "y.txt"), "y\n")

	if digestOf(t, a) != digestOf(t, b) {
		t.Error("identical trees produced different digests")
	}
}

func TestTreeDigest_DifferentContent_DifferentDigest(t *testing.T) {
	t.Parallel()
	a := t.TempDir()
	writeFile(t, filepath.Join(a, "tree", "SKILL.md"), "# x\n")
	b := t.TempDir()
	writeFile(t, filepath.Join(b, "tree", "SKILL.md"), "# different content\n")

	if digestOf(t, a) == digestOf(t, b) {
		t.Error("different content produced the same digest")
	}
}

func TestTreeDigest_ExcludesRootMarkerOnly(t *testing.T) {
	t.Parallel()
	without := t.TempDir()
	writeFile(t, filepath.Join(without, "tree", "SKILL.md"), "# x\n")
	digestWithout := digestOf(t, without)

	withMarker := t.TempDir()
	writeFile(t, filepath.Join(withMarker, "tree", "SKILL.md"), "# x\n")
	writeFile(t, filepath.Join(withMarker, "tree", markerFileName), `{"schemaVersion":2}`)
	digestWith := digestOf(t, withMarker)

	if digestWithout != digestWith {
		t.Error("the root marker file must be excluded from the digest")
	}
}

func TestTreeDigest_NestedFileNamedLikeMarker_NotExcluded(t *testing.T) {
	t.Parallel()
	// The exclusion applies ONLY at the tree root — a same-named file
	// nested deeper is ordinary content and must still be hashed.
	without := t.TempDir()
	writeFile(t, filepath.Join(without, "tree", "SKILL.md"), "# x\n")
	digestWithout := digestOf(t, without)

	withNested := t.TempDir()
	writeFile(t, filepath.Join(withNested, "tree", "SKILL.md"), "# x\n")
	writeFile(t, filepath.Join(withNested, "tree", "sub", markerFileName), `{"not":"excluded"}`)
	digestWith := digestOf(t, withNested)

	if digestWithout == digestWith {
		t.Error("a nested file merely sharing the marker's name must still affect the digest")
	}
}

func TestTreeDigest_EmptyDirCounts(t *testing.T) {
	t.Parallel()
	without := t.TempDir()
	writeFile(t, filepath.Join(without, "tree", "SKILL.md"), "# x\n")
	digestWithout := digestOf(t, without)

	withEmptyDir := t.TempDir()
	writeFile(t, filepath.Join(withEmptyDir, "tree", "SKILL.md"), "# x\n")
	if err := os.MkdirAll(filepath.Join(withEmptyDir, "tree", "empty"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	digestWithEmpty := digestOf(t, withEmptyDir)

	if digestWithout == digestWithEmpty {
		t.Error("an added empty directory must change the digest")
	}
}

func TestTreeDigest_PermissionBitsDoNotAffectDigest(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("skipping under root: permission bits are not enforced/meaningful")
	}
	t.Parallel()
	cwd := t.TempDir()
	writeFile(t, filepath.Join(cwd, "tree", "SKILL.md"), "# x\n")
	before := digestOf(t, cwd)

	if err := os.Chmod(filepath.Join(cwd, "tree", "SKILL.md"), 0o600); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	after := digestOf(t, cwd)

	if before != after {
		t.Error("a permission-bit-only change must not affect the digest")
	}
}

func TestTreeDigest_SymlinkEntry_HashesTargetWithoutFollowing(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	writeFile(t, filepath.Join(cwd, "tree", "SKILL.md"), "# x\n")
	outside := filepath.Join(t.TempDir(), "secret.txt")
	writeFile(t, outside, "must not be read into the digest\n")
	writeSymlinkOrSkip(t, outside, filepath.Join(cwd, "tree", "link"))

	// Must not error (a symlink inside an EXISTING installed copy is a
	// normal "modified" signal at status-check time, not a hard failure)
	// and must not equal the no-symlink digest.
	withLink := digestOf(t, cwd)

	without := t.TempDir()
	writeFile(t, filepath.Join(without, "tree", "SKILL.md"), "# x\n")
	withoutLink := digestOf(t, without)

	if withLink == withoutLink {
		t.Error("adding a symlink must change the digest")
	}
}
