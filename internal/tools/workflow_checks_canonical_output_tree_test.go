package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runCanonicalOutputTreeCheck invokes checkCanonicalOutputTreeOnly against
// a tempdir root (standing in for /var/www in local tests).
func runCanonicalOutputTreeCheck(t *testing.T, mountRoot string) []workflowStepCheckShim {
	t.Helper()
	checks := checkCanonicalOutputTreeOnly(mountRoot)
	out := make([]workflowStepCheckShim, 0, len(checks))
	for _, c := range checks {
		out = append(out, workflowStepCheckShim{Name: c.Name, Status: c.Status, Detail: c.Detail})
	}
	return out
}

// makeDirs creates each listed relative dir under root.
func makeDirs(t *testing.T, root string, rels ...string) {
	t.Helper()
	for _, r := range rels {
		if err := os.MkdirAll(filepath.Join(root, r), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", r, err)
		}
	}
}

func TestCanonicalOutputTreeOnly_Table(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		dirs        []string
		wantStatus  string
		wantMatches []string
	}{
		{
			name:       "clean tree passes",
			dirs:       []string{"apidev", "appdev", "workerdev", "db", "cache"},
			wantStatus: "pass",
		},
		{
			name:        "phantom recipe-slug tree fails (v33 class)",
			dirs:        []string{"apidev", "recipe-nestjs-showcase"},
			wantStatus:  "fail",
			wantMatches: []string{"recipe-nestjs-showcase"},
		},
		{
			name:        "multiple phantom trees all reported",
			dirs:        []string{"apidev", "recipe-laravel-minimal", "recipe-bun-hello"},
			wantStatus:  "fail",
			wantMatches: []string{"recipe-laravel-minimal", "recipe-bun-hello"},
		},
		{
			name:       "nested recipe-* below maxdepth=2 allowed (not a canonical-tree violation)",
			dirs:       []string{"apidev/packages/recipe-helper"},
			wantStatus: "pass",
		},
		{
			name:       "non-recipe prefix passes",
			dirs:       []string{"apidev", "zerops-recipes", "my-recipe-app"},
			wantStatus: "pass",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			makeDirs(t, root, tt.dirs...)
			got := runCanonicalOutputTreeCheck(t, root)
			check := findCheckByName(got, "canonical_output_tree_only")
			if check == nil {
				t.Fatalf("expected canonical_output_tree_only check, got %+v", got)
			}
			if check.Status != tt.wantStatus {
				t.Errorf("status: got %q, want %q (detail: %s)", check.Status, tt.wantStatus, check.Detail)
			}
			for _, w := range tt.wantMatches {
				if !strings.Contains(check.Detail, w) {
					t.Errorf("detail missing %q; full: %s", w, check.Detail)
				}
			}
		})
	}
}

// TestCanonicalOutputTreeOnly_MissingRoot: mountRoot may not exist in
// some contexts (minimal-tier test fixtures). Graceful pass rather than
// error so the check doesn't block the finalize step on a missing mount.
func TestCanonicalOutputTreeOnly_MissingRoot(t *testing.T) {
	t.Parallel()
	got := runCanonicalOutputTreeCheck(t, "/nonexistent/absolutely/nowhere")
	check := findCheckByName(got, "canonical_output_tree_only")
	if check == nil {
		t.Fatal("expected check emitted even on missing root")
	}
	if check.Status != "pass" {
		t.Errorf("expected pass on missing root, got %q", check.Status)
	}
}
