package sync

import (
	"strings"
	"testing"
)

// These tests verify that the fragment system is safe against common editing
// mistakes and doesn't silently corrupt content outside fragment boundaries.

func TestInjectFragment_PreservesContentOutsideMarkers(t *testing.T) {
	t.Parallel()

	readme := `# My App

Some important intro that must survive.

<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->
old kb content
<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->

## Manual Section

This was hand-written by a human and must not be touched.

<!-- #ZEROPS_EXTRACT_START:integration-guide# -->
old guide
<!-- #ZEROPS_EXTRACT_END:integration-guide# -->

## Another Manual Section

Also hand-written.`

	updated := InjectFragment(readme, "knowledge-base", "new kb content")

	tests := []struct {
		name      string
		mustExist string
	}{
		{"preserves_intro", "Some important intro that must survive."},
		{"preserves_manual_section", "This was hand-written by a human and must not be touched."},
		{"preserves_other_markers", "ZEROPS_EXTRACT_START:integration-guide"},
		{"preserves_old_guide", "old guide"},
		{"preserves_trailing_section", "Also hand-written."},
		{"injects_new_content", "new kb content"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if !strings.Contains(updated, tt.mustExist) {
				t.Errorf("content outside markers was corrupted — missing: %q\nfull output:\n%s", tt.mustExist, updated)
			}
		})
	}

	if strings.Contains(updated, "old kb content") {
		t.Error("old kb content should have been replaced")
	}
}

func TestInjectFragment_MultipleInjectionsDoNotCorrupt(t *testing.T) {
	t.Parallel()

	readme := `# App

<!-- #ZEROPS_EXTRACT_START:intro# -->
old intro
<!-- #ZEROPS_EXTRACT_END:intro# -->

## Guide

<!-- #ZEROPS_EXTRACT_START:integration-guide# -->
old guide
<!-- #ZEROPS_EXTRACT_END:integration-guide# -->

<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->
old kb
<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->`

	// Use injectAllFragments which skips intro (lossy round-trip protection)
	frags := recipeFragments{
		Intro:            "new intro",
		IntegrationGuide: "new guide",
		KnowledgeBase:    "new kb",
	}
	result := injectAllFragments(readme, frags)

	tests := []struct {
		name    string
		present string
		absent  string
	}{
		{"intro_preserved", "old intro", ""}, // intro is read-only — never pushed back
		{"new_guide", "new guide", "old guide"},
		{"new_kb", "new kb", "old kb"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if !strings.Contains(result, tt.present) {
				t.Errorf("missing %q", tt.present)
			}
			if tt.absent != "" && strings.Contains(result, tt.absent) {
				t.Errorf("should not contain %q — cross-contamination", tt.absent)
			}
		})
	}

	// Markers should appear exactly once each
	for _, marker := range []string{"intro", "integration-guide", "knowledge-base"} {
		starts := strings.Count(result, "ZEROPS_EXTRACT_START:"+marker)
		ends := strings.Count(result, "ZEROPS_EXTRACT_END:"+marker)
		if starts != 1 || ends != 1 {
			t.Errorf("marker %q: expected 1 start + 1 end, got %d starts + %d ends", marker, starts, ends)
		}
	}
}

func TestInjectFragment_HandlesNewlinesInFragment(t *testing.T) {
	t.Parallel()

	readme := "<!-- #ZEROPS_EXTRACT_START:kb# -->\nold\n<!-- #ZEROPS_EXTRACT_END:kb# -->"
	fragment := "line 1\n\nline 3\n\n\nline 6"

	result := InjectFragment(readme, "kb", fragment)

	if !strings.Contains(result, "line 1\n\nline 3\n\n\nline 6") {
		t.Error("multiline fragment content was altered")
	}
}

func TestInjectFragment_EmptyFragmentClearsContent(t *testing.T) {
	t.Parallel()

	readme := "<!-- #ZEROPS_EXTRACT_START:kb# -->\nold content here\n<!-- #ZEROPS_EXTRACT_END:kb# -->"
	result := InjectFragment(readme, "kb", "")

	if strings.Contains(result, "old content") {
		t.Error("empty fragment should clear old content")
	}
	if !strings.Contains(result, "ZEROPS_EXTRACT_START:kb") {
		t.Error("markers should be preserved even with empty fragment")
	}
}

func TestExtractKnowledgeBase_IgnoresCodeBlockHeadings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		mustHave string
	}{
		{
			"yaml_comment_in_code_block",
			"# Title\n\n## Gotchas\n\n```yaml\n## this is a yaml comment\nzerops:\n  - setup: prod\n```\n\n## zerops.yml\n\n```yaml\nfoo: bar\n```\n",
			"yaml comment",
		},
		{
			"zerops_yml_heading_in_code_block",
			"# Title\n\n## Gotchas\n\nExample:\n\n```markdown\n## zerops.yml\nThis is just an example heading\n```\n\nMore gotchas.\n\n## 1. Real integration guide\n",
			"More gotchas",
		},
		{
			"numbered_heading_in_code_block",
			"# Title\n\n## Gotchas\n\n```\n## 1. This is a code example\n```\n\nAfter code block.\n\n## 1. Real guide\n",
			"After code block",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ExtractKnowledgeBase(tt.content)
			if !strings.Contains(got, tt.mustHave) {
				t.Errorf("code block heading treated as real boundary — missing %q\ngot: %q", tt.mustHave, got)
			}
		})
	}
}

func TestExtractIntegrationGuide_NumberedHeading(t *testing.T) {
	t.Parallel()

	// Real-world pattern from API: "## 1. Adding zerops.yaml"
	content := "---\ndescription: \"test\"\nrepo: \"https://github.com/org/repo\"\n---\n\n# Title\n\n## Base Image\n\nContent\n\n## 1. Adding `zerops.yaml`\n\nThe config file.\n\n```yaml\nzerops:\n  - setup: prod\n```\n\n## 2. Environment Variables\n\nSet DB_HOST.\n\n## Service Definitions\n"

	kb := ExtractKnowledgeBase(content)
	ig := ExtractIntegrationGuide(content)

	// KB should only have Base Image
	if !strings.Contains(kb, "Content") {
		t.Error("KB should contain Base Image content")
	}
	if strings.Contains(kb, "config file") {
		t.Error("KB should NOT contain integration-guide content")
	}

	// IG should have both numbered sections
	if !strings.Contains(ig, "config file") {
		t.Error("IG should contain ## 1. content")
	}
	if !strings.Contains(ig, "Set DB_HOST") {
		t.Error("IG should contain ## 2. content")
	}
	if strings.Contains(ig, "Service Definitions") {
		t.Error("IG should NOT contain Service Definitions")
	}
}

func TestExtractFragments_RoundTrip_BunLike(t *testing.T) {
	t.Parallel()

	// Simulate the actual bun recipe structure from the API
	content := `---
description: "A minimal Bun application."
repo: "https://github.com/zerops-recipe-apps/bun-hello-world-app"
---

# Bun Hello World on Zerops

## Base Image

Includes: Bun, npm, yarn, git, bunx.
NOT included: pnpm.

## Gotchas

- BUN_INSTALL for caching
- Use bunx not npx

## 1. Adding ` + "`zerops.yaml`" + `
The main config file.

` + "```yaml" + `
zerops:
  - setup: prod
    build:
      base: bun@1.2
` + "```" + `

## 2. Environment Variables

Set DB_HOST and DB_PORT.

## Service Definitions

### Dev/Stage

` + "```yaml" + `
project:
  name: test
` + "```" + `
`

	frags := extractFragments(content)

	// KB: Base Image + Gotchas only
	if !strings.Contains(frags.KnowledgeBase, "Includes: Bun") {
		t.Error("KB missing Base Image")
	}
	if !strings.Contains(frags.KnowledgeBase, "BUN_INSTALL") {
		t.Error("KB missing Gotchas")
	}
	if strings.Contains(frags.KnowledgeBase, "config file") {
		t.Error("KB leaked integration-guide content")
	}

	// IG: numbered sections
	if !strings.Contains(frags.IntegrationGuide, "config file") {
		t.Error("IG missing section 1")
	}
	if !strings.Contains(frags.IntegrationGuide, "DB_HOST") {
		t.Error("IG missing section 2")
	}
	if strings.Contains(frags.IntegrationGuide, "Service Definitions") {
		t.Error("IG leaked service definitions")
	}

	// YAML: derived from IG
	if !strings.Contains(frags.ZeropsYAML, "base: bun@1.2") {
		t.Errorf("ZeropsYAML not derived from IG: %q", frags.ZeropsYAML)
	}

	// Intro
	if frags.Intro != "A minimal Bun application." {
		t.Errorf("Intro = %q", frags.Intro)
	}

	// Repo
	repo := ExtractRepo(content)
	if repo != "https://github.com/zerops-recipe-apps/bun-hello-world-app" {
		t.Errorf("Repo = %q", repo)
	}
}
