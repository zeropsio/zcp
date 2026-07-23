package content

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const templatesHardcodedServiceVersionPattern = `[a-z][a-z0-9.+-]*@[0-9]`

// TestTemplatesContent_NoHardcodedVersions pins the agent-facing content
// version rule from docs/spec-onboarding.md §5 over the committed template and
// playbook trees.
func TestTemplatesContent_NoHardcodedVersions(t *testing.T) {
	t.Parallel()

	versionRe := regexp.MustCompile(templatesHardcodedServiceVersionPattern)
	tests := []struct {
		name      string
		content   string
		wantMatch bool
	}{
		{
			name:      "synthetic hardcoded version is caught",
			content:   "Provision postgresql@16 for the application.",
			wantMatch: true,
		},
		{
			name:      "knowledge tool call passes",
			content:   `zerops_knowledge uri="zerops://themes/model"`,
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := versionRe.MatchString(tt.content); got != tt.wantMatch {
				t.Errorf("hardcoded-version match = %v, want %v for %q", got, tt.wantMatch, tt.content)
			}
		})
	}

	t.Run("tracked markdown tree has no hardcoded versions", func(t *testing.T) {
		t.Parallel()

		repoRoot := findRepoRoot(t)
		files := gitTrackedFiles(t, repoRoot)
		dirs := []string{
			"internal/content/templates/",
			"internal/knowledge/playbooks/",
		}
		for _, rel := range files {
			if !strings.HasSuffix(rel, ".md") || !underAnyDir(rel, dirs) {
				continue
			}
			body, err := os.ReadFile(filepath.Join(repoRoot, rel))
			if err != nil {
				t.Fatalf("read %s: %v", rel, err)
			}
			if match := versionRe.Find(body); len(match) != 0 {
				t.Errorf("%s: hardcoded service version %q — route the version to its live owner, never pin it in content", rel, match)
			}
		}
	})
}
