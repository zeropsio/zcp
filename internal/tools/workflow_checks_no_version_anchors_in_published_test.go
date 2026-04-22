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
			name: "compound identifier tail (nestjs-minimal-v3) is not a version anchor (Cx-6)",
			files: map[string]string{
				"apidev/README.md": "Reference: nestjs-minimal-v3 slug.\n",
			},
			wantStatus: "pass",
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

// TestVersionAnchor_SkipsFencedCodeBlock is the Cx-VERSION-ANCHOR-SHARPEN
// RED→GREEN test. v37 F-22: the check fired on `bootstrap-seed-v1`
// execOnce keys inside YAML code blocks — a false positive that forced
// a cross-file rename. Published porter-facing content should be
// dateless; YAML examples inside fenced code blocks carry identifier
// strings that don't map to recipe-internal release anchors.
func TestVersionAnchor_SkipsFencedCodeBlock(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		"apidev/README.md": "Migration setup.\n\n" +
			"```yaml\n" +
			"initCommands:\n" +
			"  - execOnce: bootstrap-seed-v1\n" +
			"    command: php artisan migrate --seed\n" +
			"```\n" +
			"\nThe execOnce key prevents re-runs.\n",
	}
	root := writePublished(t, files)
	got := runVersionAnchorsCheck(t, root)
	check := findCheckByName(got, "no_version_anchors_in_published_content")
	if check == nil {
		t.Fatal("expected check emitted")
	}
	if check.Status != "pass" {
		t.Errorf("fenced code block content should not trigger version-anchor check; got %q (detail: %s)", check.Status, check.Detail)
	}
}

// TestVersionAnchor_AcceptsCompoundIdentifier: identifiers carrying a
// `-vN` suffix as part of a compound token (e.g. `bootstrap-seed-v1`
// in prose, not a code block) are slug-class identifiers that should
// not trip the check. Only bare `v\d+` in prose — where the author
// could have dropped it — counts as a recipe-run anchor leak.
func TestVersionAnchor_AcceptsCompoundIdentifier(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		"apidev/README.md": "Use the `bootstrap-seed-v1` key when you need the seed to run once.\n",
	}
	root := writePublished(t, files)
	got := runVersionAnchorsCheck(t, root)
	check := findCheckByName(got, "no_version_anchors_in_published_content")
	if check == nil {
		t.Fatal("expected check emitted")
	}
	if check.Status != "pass" {
		t.Errorf("compound identifier tail should not trigger version-anchor check; got %q (detail: %s)", check.Status, check.Detail)
	}
}

// TestVersionAnchor_RejectsBareProseVersion: the check still catches
// the recipe-run anchor class (v33, v8.86 etc.) in prose — that's
// what F-22's sharpen must preserve, not loosen.
func TestVersionAnchor_RejectsBareProseVersion(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		"apidev/README.md": "Now on v2 of the rollout.\n",
	}
	root := writePublished(t, files)
	got := runVersionAnchorsCheck(t, root)
	check := findCheckByName(got, "no_version_anchors_in_published_content")
	if check == nil {
		t.Fatal("expected check emitted")
	}
	if check.Status != "fail" {
		t.Errorf("bare prose version should still fail; got %q (detail: %s)", check.Status, check.Detail)
	}
	if !strings.Contains(check.Detail, "v2") {
		t.Errorf("detail should name the bare match; got %s", check.Detail)
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
