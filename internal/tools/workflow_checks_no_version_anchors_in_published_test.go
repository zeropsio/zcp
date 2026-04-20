package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runVersionAnchorsCheck invokes checkNoVersionAnchorsInPublishedContent
// and converts its output to the test-local shim type.
func runVersionAnchorsCheck(t *testing.T, mountRoot string) []workflowStepCheckShim {
	t.Helper()
	checks := checkNoVersionAnchorsInPublishedContent(mountRoot)
	out := make([]workflowStepCheckShim, 0, len(checks))
	for _, c := range checks {
		out = append(out, workflowStepCheckShim{Name: c.Name, Status: c.Status, Detail: c.Detail})
	}
	return out
}

// writePublished seeds a recipe project tree with the given relative file
// paths + contents. Intermediate dirs are auto-created.
func writePublished(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	return root
}

func TestNoVersionAnchorsInPublished_Table(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		files      map[string]string
		wantStatus string
		wantDetail []string
	}{
		{
			name: "clean tree passes",
			files: map[string]string{
				"apidev/README.md":             "# Api Dev\n\nRun `zsc noop`.\n",
				"apidev/CLAUDE.md":             "Dev loop: edit src, ssh apidev to restart.\n",
				"environments/local/README.md": "Local env.\n",
				"environments/prod/README.md":  "Prod env.\n",
			},
			wantStatus: "pass",
		},
		{
			name: "v33 leakage in CLAUDE.md fails",
			files: map[string]string{
				"apidev/CLAUDE.md": "Per v33 we rotated the creds...\n",
			},
			wantStatus: "fail",
			wantDetail: []string{"apidev/CLAUDE.md", "v33"},
		},
		{
			name: "v8.86 release-note anchor in README fails",
			files: map[string]string{
				"workerdev/README.md": "v8.86 shape: facts log scope=both.\n",
			},
			wantStatus: "fail",
			wantDetail: []string{"workerdev/README.md", "v8.86"},
		},
		{
			name: "environments/*/README.md scanned",
			files: map[string]string{
				"environments/local/README.md": "v20→v23 history: see changelog.\n",
			},
			wantStatus: "fail",
			wantDetail: []string{"environments/local/README.md", "v20"},
		},
		{
			name: "multiple anchors across surfaces",
			files: map[string]string{
				"apidev/README.md":            "v34 production notes.\n",
				"apidev/CLAUDE.md":            "Dev log: v8.103 bump.\n",
				"environments/prod/README.md": "v33 phantom tree clean.\n",
			},
			wantStatus: "fail",
			wantDetail: []string{"v34", "v8.103", "v33"},
		},
		{
			name: "version anchors inside hyphenated slugs still detected (word-boundary match)",
			files: map[string]string{
				"apidev/README.md": "Reference: nestjs-minimal-v3 slug.\n",
			},
			wantStatus: "fail",
			wantDetail: []string{"v3"},
		},
		{
			name: "non-version 'v' tokens not matched",
			files: map[string]string{
				"apidev/README.md": "Events: pageview, clickthrough, conversion.\nSee 'tv-output.txt'.\n",
			},
			wantStatus: "pass",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := writePublished(t, tt.files)
			got := runVersionAnchorsCheck(t, root)
			check := findCheckByName(got, "no_version_anchors_in_published_content")
			if check == nil {
				t.Fatalf("expected no_version_anchors_in_published_content check, got %+v", got)
			}
			if check.Status != tt.wantStatus {
				t.Errorf("status: got %q, want %q (detail: %s)", check.Status, tt.wantStatus, check.Detail)
			}
			for _, w := range tt.wantDetail {
				if !strings.Contains(check.Detail, w) {
					t.Errorf("detail missing %q; full: %s", w, check.Detail)
				}
			}
		})
	}
}

// TestNoVersionAnchorsInPublished_MissingRoot: missing mount root is a
// graceful pass (upstream concern). Downstream absence of README.md files
// is also a graceful pass here (the surface emptiness is the readme_exists
// check's concern).
func TestNoVersionAnchorsInPublished_MissingRoot(t *testing.T) {
	t.Parallel()
	got := runVersionAnchorsCheck(t, "/really/not/a/path")
	check := findCheckByName(got, "no_version_anchors_in_published_content")
	if check == nil {
		t.Fatal("expected check emitted even on missing root")
	}
	if check.Status != "pass" {
		t.Errorf("expected pass on missing root, got %q", check.Status)
	}
}
