package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGhPATScopeRecommendation_ActionsCarriesFullSet pins the single-owner
// scope set: the actions-track recommendation MUST name every scope the CD
// flow needs (the p.txt #6 / p2 #4 failure was an under-scoped token) plus the
// settings link.
func TestGhPATScopeRecommendation_ActionsCarriesFullSet(t *testing.T) {
	t.Parallel()
	got := ghPATScopeRecommendation("me/app", true)
	for _, want := range []string{
		"Contents: Read and write",
		"Workflows: Read and write",
		"Secrets: Read and write",
		"Actions: Read",
		"Checks: Read",
		ghPATSettingsURL,
		"me/app",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("actions recommendation missing %q:\n%s", want, got)
		}
	}
}

// TestGhPATScopeRecommendation_PushOnlyIsMinimal pins that the git-push-only
// recommendation is the Contents minimum + link — and does NOT over-scope to
// the actions-only permissions (validation set ≠ presentation set).
func TestGhPATScopeRecommendation_PushOnlyIsMinimal(t *testing.T) {
	t.Parallel()
	got := ghPATScopeRecommendation("me/app", false)
	if !strings.Contains(got, "Contents: Read and write") {
		t.Errorf("push-only recommendation must name Contents: %s", got)
	}
	if !strings.Contains(got, ghPATSettingsURL) {
		t.Errorf("push-only recommendation must carry the settings link: %s", got)
	}
	for _, unwanted := range []string{"Workflows: Read and write", "Actions: Read", "Checks: Read"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("push-only recommendation must NOT over-scope with %q: %s", unwanted, got)
		}
	}
}

// TestGhPATScope_AtomsAgreeWithOwner is the tell==check drift guard: the
// human-readable atoms that recommend a GitHub PAT must carry the SAME scope
// set + settings link as the Go owner (ghPATScopeRecommendation). When the
// owner changes, an atom left behind fails here instead of shipping a silently
// under-scoped recommendation (the original defect).
func TestGhPATScope_AtomsAgreeWithOwner(t *testing.T) {
	t.Parallel()
	// Tokens every actions-track PAT recommendation must name, derived from
	// the single owner so this list can't drift from the helper.
	owner := ghPATScopeRecommendation("me/app", true)
	required := []string{
		"Workflows: Read and write",
		"Actions: Read",
		"Checks: Read",
		ghPATSettingsURL,
		// #2 preventive trap: every PAT recommendation must steer away from the
		// read-only "Public repositories" repo-access default (a fail→regenerate
		// round-trip). Pinned so the atoms can't drop it while the owner carries it.
		"Public repositories",
	}
	for _, tok := range required {
		if !strings.Contains(owner, tok) {
			t.Fatalf("owner recommendation unexpectedly missing %q — fix ghPATScopeRecommendation", tok)
		}
	}

	atoms := []string{
		"setup-git-push-container.md",
		"setup-build-integration-actions.md",
	}
	for _, name := range atoms {
		path := filepath.Join("..", "content", "atoms", name)
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read atom %s: %v", name, err)
		}
		for _, tok := range required {
			if !strings.Contains(string(body), tok) {
				t.Errorf("atom %s missing PAT-scope token %q — drifted from ghPATScopeRecommendation", name, tok)
			}
		}
	}
}
