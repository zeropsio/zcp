package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runVisualStyleCheck invokes checkVisualStyleASCIIOnly and converts its
// output to the test-local shim type shared across the tools test suite.
func runVisualStyleCheck(t *testing.T, dir, hostname string) []workflowStepCheckShim {
	t.Helper()
	checks := checkVisualStyleASCIIOnly(dir, hostname)
	out := make([]workflowStepCheckShim, 0, len(checks))
	for _, c := range checks {
		out = append(out, workflowStepCheckShim{Name: c.Name, Status: c.Status, Detail: c.Detail})
	}
	return out
}

// writeYAMLBytes writes content to {dir}/zerops.yaml. Returns the dir.
func writeYAMLBytes(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "zerops.yaml"), []byte(content), 0o644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	return dir
}

func TestVisualStyleAsciiOnly_Table(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		yaml       string
		wantStatus string
		wantDetail []string // substrings expected in Detail on fail
	}{
		{
			name: "plain ascii passes",
			yaml: `zerops:
  - setup: dev
    # Production wisdom: minContainers:2 because rolling deploys
    # need at least one surviving container to avoid dropping
    # in-flight requests during a redeploy.
    build:
      base: nodejs@22
    run:
      base: nodejs@22
      start: zsc noop --silent
`,
			wantStatus: "pass",
		},
		{
			name: "unicode box-drawing fails (v33 class)",
			yaml: `zerops:
  # ┌─ env tiers ─┐
  # │  dev │ prod │
  # └──────┴──────┘
  - setup: dev
    build:
      base: nodejs@22
`,
			wantStatus: "fail",
			wantDetail: []string{"Box Drawing", "U+2500"},
		},
		{
			name:       "box drawing lower bound U+2500",
			yaml:       "zerops:\n  # \u2500 separator line\n  - setup: dev\n",
			wantStatus: "fail",
			wantDetail: []string{"U+2500"},
		},
		{
			name:       "box drawing upper bound U+257F",
			yaml:       "zerops:\n  # \u257F corner\n  - setup: dev\n",
			wantStatus: "fail",
			wantDetail: []string{"U+257F"},
		},
		{
			name:       "non-box-drawing unicode allowed (emoji not in range)",
			yaml:       "zerops:\n  # readme badge \u2728\n  - setup: dev\n",
			wantStatus: "pass",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := writeYAMLBytes(t, tt.yaml)
			got := runVisualStyleCheck(t, dir, "api")
			check := findCheckByName(got, "api_visual_style_ascii_only")
			if check == nil {
				t.Fatalf("expected api_visual_style_ascii_only check, got %+v", got)
			}
			if check.Status != tt.wantStatus {
				t.Errorf("status: got %q, want %q (detail: %s)", check.Status, tt.wantStatus, check.Detail)
			}
			for _, w := range tt.wantDetail {
				if !strings.Contains(check.Detail, w) {
					t.Errorf("detail missing %q; full detail: %s", w, check.Detail)
				}
			}
		})
	}
}

// TestVisualStyleAsciiOnly_MissingYAML_Skips: a mount without zerops.yaml
// is not the visual-style check's concern — another check fails earlier.
// This check should no-op-pass or emit a skip note.
func TestVisualStyleAsciiOnly_MissingYAML_Skips(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	got := runVisualStyleCheck(t, dir, "api")
	check := findCheckByName(got, "api_visual_style_ascii_only")
	if check == nil {
		t.Fatal("expected check to emit (pass/skip) even when zerops.yaml absent")
	}
	if check.Status != "pass" {
		t.Errorf("expected pass (file absent is upstream concern), got %q", check.Status)
	}
}

// TestVisualStyleAsciiOnly_YAMLFallback: accepts `zerops.yml` as a fallback
// (matches existing generate-check convention in workflow_checks_recipe.go).
func TestVisualStyleAsciiOnly_YAMLFallback(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	content := "zerops:\n  # \u2500 line\n"
	if err := os.WriteFile(filepath.Join(dir, "zerops.yml"), []byte(content), 0o644); err != nil {
		t.Fatalf("write yml: %v", err)
	}
	got := runVisualStyleCheck(t, dir, "api")
	check := findCheckByName(got, "api_visual_style_ascii_only")
	if check == nil {
		t.Fatal("expected check")
	}
	if check.Status != "fail" {
		t.Errorf("expected fail on zerops.yml fallback, got %q", check.Status)
	}
}
