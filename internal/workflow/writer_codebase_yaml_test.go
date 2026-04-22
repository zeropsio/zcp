package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWriterBrief_IncludesPerCodebaseZeropsYAML — v39 Commit 3c(i).
// The writer dispatch brief MUST inline each codebase's `zerops.yaml`
// as a pre-loaded input block so gotcha / IG-item authoring
// pattern-matches against the actual yaml rather than inventing
// mechanism patterns. Works against a temp-dir project root so
// tests run without the real SSHFS mount.
func TestWriterBrief_IncludesPerCodebaseZeropsYAML(t *testing.T) {
	t.Parallel()

	tempRoot := t.TempDir()
	// Seed per-codebase zerops.yaml files at the paths the render path
	// reads. testShowcasePlanForBrief has apidev + appdev + workerdev;
	// worker is shared-codebase so no workerdev entry.
	writeCodebaseYAML(t, tempRoot, "api", `zerops:
  - setup: prod
    build:
      base: nodejs@22
      buildCommands:
        - npm ci
        - npm run build
      deployFiles: ./dist/~
    run:
      start: node dist/main.js
      httpSupport: true
`)
	writeCodebaseYAML(t, tempRoot, "app", `zerops:
  - setup: prod
    build:
      base: static
      deployFiles: ./build/~
`)

	plan := testShowcasePlanForBrief()
	// Override ProjectRoot via a bespoke context so the render path
	// reads from tempRoot, not /var/www.
	ctx := RenderContextFromPlan(plan, "")
	ctx.ProjectRoot = tempRoot

	got, err := buildWriterBriefRendered(ctx, "/tmp/zcp-facts-test.jsonl")
	if err != nil {
		t.Fatalf("buildWriterBriefRendered: %v", err)
	}

	if !strings.Contains(got, "Pre-loaded input — per-codebase `zerops.yaml`") {
		t.Error("writer brief missing per-codebase zerops.yaml section header")
	}
	for _, want := range []string{
		"deployFiles: ./dist/~",
		"deployFiles: ./build/~",
		"httpSupport: true",
		"npm ci",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("writer brief missing yaml snippet %q — inlining didn't capture the real file", want)
		}
	}
}

// TestWriterBrief_MissingYAMLGracefullyNoted — when a codebase's
// zerops.yaml isn't on disk (pre-scaffold path, unexpected state),
// the brief MUST note it explicitly instead of silently dropping the
// codebase from the input block. Silent drop would let the writer
// author content against a codebase with no ground truth, defeating
// the point of the injection.
func TestWriterBrief_MissingYAMLGracefullyNoted(t *testing.T) {
	t.Parallel()

	tempRoot := t.TempDir()
	// Only seed apidev; appdev intentionally absent.
	writeCodebaseYAML(t, tempRoot, "api", "zerops:\n  - setup: prod\n    run:\n      start: node main.js\n")

	plan := testShowcasePlanForBrief()
	ctx := RenderContextFromPlan(plan, "")
	ctx.ProjectRoot = tempRoot

	got, err := buildWriterBriefRendered(ctx, "")
	if err != nil {
		t.Fatalf("buildWriterBriefRendered: %v", err)
	}
	if !strings.Contains(got, "`app` — `zerops.yaml` not yet present") {
		t.Error("writer brief should note missing app (appdev) zerops.yaml explicitly")
	}
	if !strings.Contains(got, "node main.js") {
		t.Error("writer brief should still inline apidev yaml content")
	}
}

// TestWriterBrief_FactsLogGuidesByRouteTo — v39 Commit 3c(ii). The
// Input files section MUST tell the writer to prioritize facts
// with routeTo=content_gotcha or routeTo=content_ig (the surfaces
// the writer authors directly). Facts routed elsewhere inform
// cross-surface decisions but aren't written from directly.
func TestWriterBrief_FactsLogGuidesByRouteTo(t *testing.T) {
	t.Parallel()

	plan := testShowcasePlanForBrief()
	ctx := RenderContextFromPlan(plan, "")
	ctx.ProjectRoot = t.TempDir() // empty tempdir so yaml block renders "not present"

	got, err := buildWriterBriefRendered(ctx, "/tmp/zcp-facts.jsonl")
	if err != nil {
		t.Fatalf("buildWriterBriefRendered: %v", err)
	}
	for _, want := range []string{
		"routeTo: content_gotcha",
		"routeTo: content_ig",
		"claude_md",
		"zerops_yaml_comment",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("facts-log prose missing %q — writer won't know which records to prioritize", want)
		}
	}
}

func writeCodebaseYAML(t *testing.T, root, host, body string) {
	t.Helper()
	dir := filepath.Join(root, host+"dev")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "zerops.yaml"), []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}
