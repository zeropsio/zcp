package tools

import (
	"os"
	"path/filepath"
	"testing"
)

// TestReadLocalZeropsYAML_YAMLPreferredOverYML verifies the .yaml
// preference (existing convention in workflow_export_probe.go).
func TestReadLocalZeropsYAML_YAMLPreferredOverYML(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "zerops.yaml"), []byte("from yaml\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "zerops.yml"), []byte("from yml\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := readLocalZeropsYAML(dir)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got != "from yaml" {
		t.Errorf("got %q want %q", got, "from yaml")
	}
}

// TestReadLocalZeropsYAML_YMLFallback verifies .yml fallback when .yaml is absent.
func TestReadLocalZeropsYAML_YMLFallback(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "zerops.yml"), []byte("from yml\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := readLocalZeropsYAML(dir)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got != "from yml" {
		t.Errorf("got %q want %q", got, "from yml")
	}
}

// TestReadLocalZeropsYAML_MissingReturnsEmpty verifies empty-not-error
// for missing file — caller surfaces the appropriate blocker, not a
// platform error.
func TestReadLocalZeropsYAML_MissingReturnsEmpty(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	got, err := readLocalZeropsYAML(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

// TestHasSetupNamed_Prod verifies the prod-setup detection used by the
// source-control gate (hasSetupNamed with the canonical "prod" name).
func TestHasSetupNamed_Prod(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"prod present", `zerops:
  - setup: prod
    build:
      base: nodejs@22
`, true},
		{"only dev", `zerops:
  - setup: dev
    build:
      base: nodejs@22
`, false},
		{"dev and prod", `zerops:
  - setup: dev
    build:
      base: nodejs@22
  - setup: prod
    build:
      base: nodejs@22
`, true},
		{"empty body", "", false},
		{"unrelated yaml", "foo: bar\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := hasSetupNamed(tc.body, "prod"); got != tc.want {
				t.Errorf("hasSetupNamed(prod): got %v want %v\nbody:\n%s", got, tc.want, tc.body)
			}
		})
	}
}
