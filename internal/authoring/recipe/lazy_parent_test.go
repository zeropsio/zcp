package recipe

import (
	"path/filepath"
	"testing"
)

// Run-40 post-ship — lazy parent resolution. sess.Parent starts nil
// and the chain resolver runs only on first sess.LoadParent() call.
// Brief-composition dispatch is the load-bearing consumer; research /
// provision / feature phases that don't read parent skip the resolution
// entirely.

// TestOpenOrCreate_DoesNotEagerLoadParent pins the lazy contract: a
// freshly-created session must NOT have parent populated. Pre-fix the
// session start eagerly resolved the parent even for phases whose
// consumers never read it.
func TestOpenOrCreate_DoesNotEagerLoadParent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := NewStore("")
	sess, err := store.OpenOrCreate("nestjs-showcase", filepath.Join(dir, "run"))
	if err != nil {
		t.Fatalf("OpenOrCreate: %v", err)
	}
	if sess.Parent != nil {
		t.Errorf("expected sess.Parent nil before LoadParent; got %+v", sess.Parent)
	}
}

// TestLoadParent_ResolvesEmbeddedParent — explicit LoadParent call
// surfaces the embedded `nestjs-minimal.md` for `nestjs-showcase`.
// Validates the embedded-corpus path through the session resolver.
func TestLoadParent_ResolvesEmbeddedParent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := NewStore("")
	sess, err := store.OpenOrCreate("nestjs-showcase", filepath.Join(dir, "run"))
	if err != nil {
		t.Fatalf("OpenOrCreate: %v", err)
	}
	parent, err := sess.LoadParent()
	if err != nil {
		t.Fatalf("LoadParent: %v", err)
	}
	if parent == nil {
		t.Fatal("expected embedded parent ParentRecipe; got nil")
	}
	if parent.Slug != "nestjs-minimal" {
		t.Errorf("parent.Slug = %q, want nestjs-minimal", parent.Slug)
	}
	if !parent.IsEmbedded() {
		t.Errorf("expected IsEmbedded()=true; got SourceRoot=%q, body-len=%d",
			parent.SourceRoot, len(parent.EmbeddedBody))
	}
	if parent.EmbeddedBody == "" {
		t.Error("parent.EmbeddedBody empty — embedded corpus load failed")
	}
	if sess.Parent != parent {
		t.Errorf("session cache should hold the loaded parent; sess.Parent=%p, returned=%p", sess.Parent, parent)
	}
}

// TestLoadParent_Idempotent — calling LoadParent twice returns the
// same instance without re-walking the embed.FS.
func TestLoadParent_Idempotent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := NewStore("")
	sess, err := store.OpenOrCreate("nestjs-showcase", filepath.Join(dir, "run"))
	if err != nil {
		t.Fatalf("OpenOrCreate: %v", err)
	}
	first, err := sess.LoadParent()
	if err != nil {
		t.Fatalf("first LoadParent: %v", err)
	}
	second, err := sess.LoadParent()
	if err != nil {
		t.Fatalf("second LoadParent: %v", err)
	}
	if first != second {
		t.Errorf("LoadParent not idempotent — got distinct pointers (%p vs %p)", first, second)
	}
}

// TestLoadParent_CachesNoParentDecision — slugs without a chain
// parent (hello-world / minimal) cache the nil outcome so repeat
// callers don't re-probe.
func TestLoadParent_CachesNoParentDecision(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := NewStore("")
	sess, err := store.OpenOrCreate("nestjs-minimal", filepath.Join(dir, "run"))
	if err != nil {
		t.Fatalf("OpenOrCreate: %v", err)
	}
	got, err := sess.LoadParent()
	if err != nil {
		t.Fatalf("LoadParent: %v", err)
	}
	if got != nil {
		t.Errorf("nestjs-minimal should have no parent; got %+v", got)
	}
	if !sess.parentResolved {
		t.Errorf("parentResolved should be true after no-parent outcome to short-circuit retries")
	}
}

// TestPredictParentStatus_Embedded — predictParentStatus returns
// "embedded" for *-showcase slugs whose parent ships in the corpus,
// "absent" otherwise. Used by the start handler to give the agent an
// accurate signal without loading the body.
func TestPredictParentStatus_Embedded(t *testing.T) {
	t.Parallel()
	tests := []struct {
		slug string
		want string
	}{
		{"nestjs-showcase", "embedded"},
		{"nestjs-minimal", "absent"},
		{"hello-world-bun", "absent"},
		{"synth-showcase", "absent"}, // parent not in embedded corpus
	}
	for _, tc := range tests {
		t.Run(tc.slug, func(t *testing.T) {
			t.Parallel()
			if got := predictParentStatus(tc.slug); got != tc.want {
				t.Errorf("predictParentStatus(%q) = %q, want %q", tc.slug, got, tc.want)
			}
		})
	}
}

// TestStart_PredictsParentStatusWithoutLoadingBody — the start
// handler returns parentStatus tag but does NOT populate r.Parent
// (lazy contract — body lands on first build-subagent-prompt call).
func TestStart_PredictsParentStatusWithoutLoadingBody(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := NewStore("")
	res := dispatch(t.Context(), store, RecipeInput{
		Action: "start", Slug: "nestjs-showcase", OutputRoot: filepath.Join(dir, "run"),
	})
	if !res.OK {
		t.Fatalf("start failed: %+v", res)
	}
	if res.ParentStatus != "embedded" {
		t.Errorf("ParentStatus = %q, want embedded", res.ParentStatus)
	}
	if res.Parent != nil {
		t.Errorf("start response should NOT carry parent body (lazy contract); got %+v", res.Parent)
	}
	// Verify the session itself didn't eager-load.
	sess, _ := store.Get("nestjs-showcase")
	if sess.Parent != nil {
		t.Errorf("sess.Parent should be nil after start (lazy); got %+v", sess.Parent)
	}
}
