package object

import (
	"context"
	"errors"
	"testing"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
)

// S20 — object folder honesty (D-07). A folder is an S3 prefix with children,
// never a literal object. A single-key Delete/Rename targeting one must refuse
// clearly (recursive folder ops are a bigger feature, out of scope for v1 —
// honesty over a silent single-key surprise), distinct from both a false "ok"
// (Delete's old bug, OBJ-AUD-02, fixed in S26) and a bare not_found (which lies
// that a folder with live children is "not there"). A real leaf still works.

// TestDelete_FolderPrefix_Refused pins the folder refusal: audit_/dir is a
// prefix (children audit_/dir/a.txt + b.txt, no literal audit_/dir key), so
// Delete must return the clear "this is a folder, not a single object" refusal
// (ErrUnsupported), NOT ErrNotFound, NOT a false success — and the children
// stay untouched (the refusal precedes any RemoveObject).
func TestDelete_FolderPrefix_Refused(t *testing.T) {
	t.Parallel()
	existing := map[string]bool{"audit_/dir/a.txt": true, "audit_/dir/b.txt": true}
	srv := newFakeS3Server(t, "obj-folder-bucket", existing)
	p := newFakeS3Provider(t, srv, "obj-folder-bucket")
	ctx := context.Background()

	err := p.Delete(ctx, provider.Path{Segments: []string{"audit_", "dir"}})
	if !errors.Is(err, provider.ErrUnsupported) {
		t.Fatalf("Delete(folder) = %v, want ErrUnsupported (cannot delete a folder)", err)
	}
	if errors.Is(err, provider.ErrNotFound) {
		t.Fatalf("Delete(folder) = %v, must not read as not_found (the folder has children)", err)
	}

	// Children untouched — still listable through the provider.
	nodes, _, lerr := p.List(ctx, provider.Path{Segments: []string{"audit_", "dir"}}, provider.Page{Limit: 100})
	if lerr != nil {
		t.Fatalf("List(folder) after refused Delete: %v", lerr)
	}
	if len(nodes) != 2 {
		t.Errorf("List(folder) = %d nodes, want 2 (children untouched by the refused Delete)", len(nodes))
	}
}

// TestRename_FolderPrefix_Refused is the Rename companion: renaming a folder is
// equally a single-key op on the wrong thing, refused the same way. Before S20,
// Rename 404'd here (CopyObject errors on a missing source) — honest that
// nothing moved, but silent about WHY (it is a folder, not a missing key).
func TestRename_FolderPrefix_Refused(t *testing.T) {
	t.Parallel()
	existing := map[string]bool{"audit_/dir/a.txt": true, "audit_/dir/b.txt": true}
	srv := newFakeS3Server(t, "obj-folder-bucket", existing)
	p := newFakeS3Provider(t, srv, "obj-folder-bucket")

	err := p.Rename(context.Background(),
		provider.Path{Segments: []string{"audit_", "dir"}},
		provider.Path{Segments: []string{"audit_", "dir2"}})
	if !errors.Is(err, provider.ErrUnsupported) {
		t.Fatalf("Rename(folder) = %v, want ErrUnsupported (cannot rename a folder)", err)
	}
	if errors.Is(err, provider.ErrNotFound) {
		t.Fatalf("Rename(folder) = %v, must not read as not_found (the folder has children)", err)
	}
}

// TestRename_MissingKey_ReturnsNotFound preserves honest-not-found for a key
// that is neither a leaf nor a folder — the folder-detection probe must not
// turn a genuinely-missing target into the folder refusal.
func TestRename_MissingKey_ReturnsNotFound(t *testing.T) {
	t.Parallel()
	srv := newFakeS3Server(t, "obj-folder-bucket", map[string]bool{})
	p := newFakeS3Provider(t, srv, "obj-folder-bucket")

	err := p.Rename(context.Background(),
		provider.Path{Segments: []string{"never-existed.txt"}},
		provider.Path{Segments: []string{"never2.txt"}})
	if !errors.Is(err, provider.ErrNotFound) {
		t.Fatalf("Rename(missing) = %v, want ErrNotFound", err)
	}
}

// TestRename_ExistingLeaf_Succeeds pins that a real object still renames after
// the stat-then-classify guard is added: copy to the new key, remove the old.
// The old path is gone and the new path is present afterward.
func TestRename_ExistingLeaf_Succeeds(t *testing.T) {
	t.Parallel()
	srv := newFakeS3Server(t, "obj-folder-bucket", map[string]bool{"file.txt": true})
	p := newFakeS3Provider(t, srv, "obj-folder-bucket")
	ctx := context.Background()
	src := provider.Path{Segments: []string{"file.txt"}}
	dst := provider.Path{Segments: []string{"renamed.txt"}}

	if err := p.Rename(ctx, src, dst); err != nil {
		t.Fatalf("Rename(leaf) = %v, want nil", err)
	}
	if _, err := p.Stat(ctx, src); !errors.Is(err, provider.ErrNotFound) {
		t.Errorf("Stat(old path) after Rename = %v, want ErrNotFound", err)
	}
	if _, err := p.Stat(ctx, dst); err != nil {
		t.Errorf("Stat(new path) after Rename = %v, want nil", err)
	}
}
