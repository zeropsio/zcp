package workflow

import (
	"path/filepath"
	"testing"
)

func TestWriteReadRecipeMeta(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	meta := &RecipeMeta{
		Slug:        "laravel-hello-world",
		Framework:   "laravel",
		Tier:        RecipeTierMinimal,
		RuntimeType: "php-nginx@8.4",
		CreatedAt:   "2026-04-02",
		OutputDir:   "/tmp/recipes/laravel",
	}

	if err := WriteRecipeMeta(dir, meta); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := ReadRecipeMeta(dir, "laravel-hello-world")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil meta")
	}
	if got.Slug != meta.Slug {
		t.Errorf("slug = %q, want %q", got.Slug, meta.Slug)
	}
	if got.Framework != meta.Framework {
		t.Errorf("framework = %q, want %q", got.Framework, meta.Framework)
	}
	if got.Tier != meta.Tier {
		t.Errorf("tier = %q, want %q", got.Tier, meta.Tier)
	}
	if got.RuntimeType != meta.RuntimeType {
		t.Errorf("runtimeType = %q, want %q", got.RuntimeType, meta.RuntimeType)
	}
	if got.OutputDir != meta.OutputDir {
		t.Errorf("outputDir = %q, want %q", got.OutputDir, meta.OutputDir)
	}
}

func TestReadRecipeMeta_NotFound(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	got, err := ReadRecipeMeta(dir, "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Error("expected nil for nonexistent meta")
	}
}

func TestListRecipeMetas_Empty(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	metas, err := ListRecipeMetas(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(metas) != 0 {
		t.Errorf("expected 0 metas, got %d", len(metas))
	}
}

func TestListRecipeMetas_Multiple(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	slugs := []string{"bun-hello-world", "laravel-hello-world", "nestjs-showcase"}
	for _, slug := range slugs {
		meta := &RecipeMeta{
			Slug:      slug,
			Framework: slug[:3],
			Tier:      RecipeTierMinimal,
		}
		if err := WriteRecipeMeta(dir, meta); err != nil {
			t.Fatalf("write %s: %v", slug, err)
		}
	}

	metas, err := ListRecipeMetas(dir)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(metas) != 3 {
		t.Errorf("expected 3 metas, got %d", len(metas))
	}
}

func TestWriteRecipeMeta_CreatesDir(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "nested", "path")
	meta := &RecipeMeta{Slug: "test-hello-world", Framework: "test"}

	if err := WriteRecipeMeta(dir, meta); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := ReadRecipeMeta(dir, "test-hello-world")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil meta after write to nested path")
	}
}

func TestWriteRecipeMeta_Overwrite(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	meta := &RecipeMeta{Slug: "bun-hello-world", Framework: "bun", Tier: RecipeTierMinimal}
	if err := WriteRecipeMeta(dir, meta); err != nil {
		t.Fatalf("write 1: %v", err)
	}

	meta.Tier = RecipeTierShowcase
	if err := WriteRecipeMeta(dir, meta); err != nil {
		t.Fatalf("write 2: %v", err)
	}

	got, err := ReadRecipeMeta(dir, "bun-hello-world")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.Tier != RecipeTierShowcase {
		t.Errorf("tier = %q, want %q after overwrite", got.Tier, RecipeTierShowcase)
	}
}
