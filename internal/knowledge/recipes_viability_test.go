package knowledge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckViability_Rules(t *testing.T) {
	t.Parallel()

	// Synthetic body with 200 lines + required sections + one fence pair.
	okBody := strings.Repeat("line\n", 195) +
		"## Overview\n" +
		"## Deploy\n" +
		"## Verify\n" +
		"```yaml\nfoo: bar\n```\n"

	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{
			name:    "fails_when_too_short",
			content: "## Overview\n## Deploy\n## Verify\n```\n```\n",
			want:    false,
		},
		{
			name:    "fails_when_section_missing",
			content: strings.Repeat("line\n", 200) + "## Overview\n## Verify\n```\n```\n",
			want:    false,
		},
		{
			name:    "fails_when_no_code_fences",
			content: strings.Repeat("line\n", 200) + "## Overview\n## Deploy\n## Verify\n",
			want:    false,
		},
		{
			name:    "passes_on_full_body",
			content: okBody,
			want:    true,
		},
		{
			name:    "strips_frontmatter_before_counting",
			content: "---\nname: x\n---\n" + okBody,
			want:    true,
		},
		{
			name:    "section_match_is_case_insensitive",
			content: strings.Repeat("line\n", 195) + "## overview\n## DEPLOY\n## Verify\n```\n```\n",
			want:    true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := CheckViability(tt.content, DefaultRecipeViabilityRules)
			if got.Passed != tt.want {
				t.Errorf("Passed=%v, want=%v. reasons=%v", got.Passed, tt.want, got.Reasons)
			}
		})
	}
}

// TestCheckViability_StubRecipesFail proves the gate rejects recipe stubs that
// would otherwise be delivered as the happy path.
func TestCheckViability_StubRecipesFail(t *testing.T) {
	t.Parallel()

	root, err := filepath.Abs("recipes")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	stubs := []string{"laravel-minimal.md", "nextjs-ssr-hello-world.md"}
	for _, stub := range stubs {
		t.Run(stub, func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile(filepath.Join(root, stub))
			if err != nil {
				t.Skipf("recipe not present: %v", err)
			}
			got := CheckViability(string(data), DefaultRecipeViabilityRules)
			if got.Passed {
				t.Fatalf("stub %q unexpectedly passed viability gate", stub)
			}
		})
	}
}

// TestCheckViability_AuditedRecipesPass proves that every recipe currently
// marked "audited=yes" in docs/spec-recipe-quality-process.md Status satisfies
// the default gate. Calibration invariant.
func TestCheckViability_AuditedRecipesPass(t *testing.T) {
	t.Parallel()

	audited := []string{"laravel.md", "filament.md", "twill.md", "symfony.md"}
	root, err := filepath.Abs("recipes")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	for _, name := range audited {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile(filepath.Join(root, name))
			if err != nil {
				t.Skipf("recipe not present: %v", err)
			}
			got := CheckViability(string(data), DefaultRecipeViabilityRules)
			if !got.Passed {
				t.Fatalf("audited recipe %q failed viability gate: %v", name, got.Reasons)
			}
		})
	}
}
