package recipe

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestChainResolver_Showcase_LoadsMinimal(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	parent := filepath.Join(dir, "synth-minimal")
	codebase := filepath.Join(parent, "codebases", "app")
	if err := os.MkdirAll(codebase, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(codebase, "README.md"), "minimal README contents")
	writeFile(t, filepath.Join(codebase, "zerops.yaml"), "zerops: yaml")

	envFolder := filepath.Join(parent, "0 — AI Agent")
	if err := os.MkdirAll(envFolder, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(envFolder, "import.yaml"), "# tier 0 import")

	got, err := ResolveChain(Resolver{MountRoot: dir}, "synth-showcase")
	if err != nil {
		t.Fatalf("ResolveChain: %v", err)
	}
	if got == nil {
		t.Fatal("expected ParentRecipe, got nil")
	}
	if got.Slug != "synth-minimal" {
		t.Errorf("Slug = %q, want synth-minimal", got.Slug)
	}
	if got.Tier != "minimal" {
		t.Errorf("Tier = %q, want minimal", got.Tier)
	}
	cb, ok := got.Codebases["app"]
	if !ok {
		t.Fatal("expected codebase 'app' in parent")
	}
	if cb.README != "minimal README contents" {
		t.Errorf("codebase README not loaded")
	}
	if cb.ZeropsYAML != "zerops: yaml" {
		t.Errorf("codebase zerops.yaml not loaded")
	}
	if got.EnvImports["0"] != "# tier 0 import" {
		t.Errorf("env 0 import.yaml not loaded")
	}
}

func TestChainResolver_Minimal_NoParent(t *testing.T) {
	t.Parallel()

	got, err := ResolveChain(Resolver{MountRoot: t.TempDir()}, "synth-minimal")
	if !errors.Is(err, ErrNoParent) {
		t.Fatalf("ResolveChain minimal: err = %v, want ErrNoParent", err)
	}
	if got != nil {
		t.Errorf("minimal recipe should have no parent, got %+v", got)
	}
}

func TestChainResolver_HelloWorld_NoParent(t *testing.T) {
	t.Parallel()

	got, err := ResolveChain(Resolver{MountRoot: t.TempDir()}, "hello-world-bun")
	if !errors.Is(err, ErrNoParent) {
		t.Fatalf("ResolveChain hello-world: err = %v, want ErrNoParent", err)
	}
	if got != nil {
		t.Errorf("hello-world recipe should have no parent, got %+v", got)
	}
}

// TestChainResolver_EmbeddedParent_LoadsFromCorpus pins the run-40
// post-ship fix: parent recipes ship in the binary as
// `internal/knowledge/recipes/<slug>.md`. ResolveChain now reads from
// the embedded corpus FIRST so `parentStatus: "absent"` no longer
// fires on local dev boxes where `~/recipes/` doesn't exist —
// nestjs-showcase has nestjs-minimal in the embedded corpus.
func TestChainResolver_EmbeddedParent_LoadsFromCorpus(t *testing.T) {
	t.Parallel()

	// Empty MountRoot AND empty tempdir: no filesystem mount available.
	// Embedded `nestjs-minimal.md` from the knowledge corpus should be
	// the resolution source.
	got, err := ResolveChain(Resolver{MountRoot: t.TempDir()}, "nestjs-showcase")
	if err != nil {
		t.Fatalf("ResolveChain nestjs-showcase: %v", err)
	}
	if got == nil {
		t.Fatal("expected embedded ParentRecipe, got nil")
	}
	if got.Slug != "nestjs-minimal" {
		t.Errorf("Slug = %q, want nestjs-minimal", got.Slug)
	}
	if got.Tier != "minimal" {
		t.Errorf("Tier = %q, want minimal", got.Tier)
	}
	if !got.IsEmbedded() {
		t.Errorf("expected IsEmbedded()=true; got SourceRoot=%q EmbeddedBody-len=%d",
			got.SourceRoot, len(got.EmbeddedBody))
	}
	if got.EmbeddedBody == "" {
		t.Errorf("EmbeddedBody empty — embedded corpus load failed")
	}
	if got.SourceRoot != "" {
		t.Errorf("embedded parent should leave SourceRoot empty (downstream "+
			"composers fall back to appendEmbeddedParentBaseline on that "+
			"condition); got %q", got.SourceRoot)
	}
}

// TestChainResolver_EmbeddedParent_ParentStatus pins that the handler-
// side parentStatus tag reports "embedded" (not "absent") when the
// chain resolver returned an embedded parent.
func TestChainResolver_EmbeddedParent_ParentStatus(t *testing.T) {
	t.Parallel()
	got, err := ResolveChain(Resolver{MountRoot: ""}, "nestjs-showcase")
	if err != nil {
		t.Fatalf("ResolveChain: %v", err)
	}
	if status := parentStatus(got); status != "embedded" {
		t.Errorf("parentStatus = %q, want %q", status, "embedded")
	}
}

// TestChainResolver_EmbeddedPreferredOverFilesystem pins the
// resolution order: when BOTH the embedded corpus AND a filesystem
// mount carry the parent, the embedded body wins. Filesystem mount
// is now a legacy fallback for the CDE shape that wants the full
// published tree.
func TestChainResolver_EmbeddedPreferredOverFilesystem(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// Stage a competing filesystem-mount nestjs-minimal that, if the
	// resolver fell back to mount-first, would shadow the embedded body.
	mountParent := filepath.Join(dir, "nestjs-minimal")
	codebaseDir := filepath.Join(mountParent, "codebases", "api")
	if err := os.MkdirAll(codebaseDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(codebaseDir, "README.md"), "FILESYSTEM SHADOW — should not win")

	got, err := ResolveChain(Resolver{MountRoot: dir}, "nestjs-showcase")
	if err != nil {
		t.Fatalf("ResolveChain: %v", err)
	}
	if !got.IsEmbedded() {
		t.Errorf("expected embedded parent to win over filesystem mount; got SourceRoot=%q (mount-shape)", got.SourceRoot)
	}
	if got.Codebases["api"].README == "FILESYSTEM SHADOW — should not win" {
		t.Errorf("filesystem mount shadowed embedded corpus — resolution order regressed")
	}
}

func TestChainResolver_MissingParent_ReturnsNil(t *testing.T) {
	t.Parallel()

	// Showcase with no parent published yet returns ErrNoParent so the
	// workflow enters full first-time discovery rather than hard-erroring.
	got, err := ResolveChain(Resolver{MountRoot: t.TempDir()}, "synth-showcase")
	if !errors.Is(err, ErrNoParent) {
		t.Fatalf("ResolveChain missing-parent: err = %v, want ErrNoParent", err)
	}
	if got != nil {
		t.Errorf("missing parent: expected nil ParentRecipe, got %+v", got)
	}
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
