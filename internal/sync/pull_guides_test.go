package sync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPullGuides_WritesFiles(t *testing.T) {
	t.Parallel()

	// Setup: create a fake docs directory with .mdx files
	docsDir := t.TempDir()
	outDir := t.TempDir()

	mdxContent := "---\ntitle: My Guide\ndescription: \"A guide\"\n---\n\nimport Foo from './Foo'\n\n## Section\n\nContent here\n"
	if err := os.WriteFile(filepath.Join(docsDir, "my-guide.mdx"), []byte(mdxContent), 0644); err != nil {
		t.Fatal(err)
	}

	decisionMDX := "---\ntitle: Choose a DB\ndescription: \"Choose wisely\"\n---\n\n## Options\n\nPostgres or MySQL\n"
	if err := os.WriteFile(filepath.Join(docsDir, "choose-database.mdx"), []byte(decisionMDX), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := DefaultConfig()
	cfg.Paths.DocsLocal = docsDir
	cfg.Paths.Output = "" // use outDir as root directly

	results, err := PullGuides(cfg, outDir, "", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// Check guide was written
	guideContent, err := os.ReadFile(filepath.Join(outDir, "guides", "my-guide.md"))
	if err != nil {
		t.Fatalf("read guide: %v", err)
	}
	if !strings.Contains(string(guideContent), "# My Guide") {
		t.Error("expected title in guide")
	}
	if strings.Contains(string(guideContent), "import Foo") {
		t.Error("import line should be stripped")
	}

	// Check decision was written to decisions/
	decisionContent, err := os.ReadFile(filepath.Join(outDir, "decisions", "choose-database.md"))
	if err != nil {
		t.Fatalf("read decision: %v", err)
	}
	if !strings.Contains(string(decisionContent), "# Choose a DB") {
		t.Error("expected title in decision")
	}
}

func TestPullGuides_Filter(t *testing.T) {
	t.Parallel()

	docsDir := t.TempDir()
	outDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(docsDir, "guide-a.mdx"), []byte("---\ntitle: A\n---\n\nContent A\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docsDir, "guide-b.mdx"), []byte("---\ntitle: B\n---\n\nContent B\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := DefaultConfig()
	cfg.Paths.DocsLocal = docsDir
	cfg.Paths.Output = ""

	results, err := PullGuides(cfg, outDir, "guide-a", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result with filter, got %d", len(results))
	}
	if results[0].Slug != "guide-a" {
		t.Errorf("expected guide-a, got %s", results[0].Slug)
	}
}

func TestPullGuides_DryRun(t *testing.T) {
	t.Parallel()

	docsDir := t.TempDir()
	outDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(docsDir, "test.mdx"), []byte("---\ntitle: Test\n---\n\nContent\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := DefaultConfig()
	cfg.Paths.DocsLocal = docsDir
	cfg.Paths.Output = ""

	results, err := PullGuides(cfg, outDir, "", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Status != DryRun {
		t.Errorf("expected DryRun status, got %v", results[0].Status)
	}

	// Verify no files were written
	_, err = os.ReadFile(filepath.Join(outDir, "guides", "test.md"))
	if err == nil {
		t.Error("expected file not to exist in dry-run mode")
	}
}
