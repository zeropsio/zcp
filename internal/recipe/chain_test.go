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
