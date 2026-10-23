package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
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

// TestLaunchSourceRead_Pool_Refused pins spec-workflows.md §8 PS-3 on the
// launch source: a Swarm pool (or a Zerops Box) named as the source runtime
// is refused by name before any SSH read — it is neither managed
// infrastructure nor the app, and a read by a pool's hostname reaches a
// random container.
func TestLaunchSourceRead_Pool_Refused(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, svcType, wantKind string
	}{
		{"swarm pool", "swarm@1", "Swarm pool"},
		{"swarm-vm pool", "swarm-vm@1", "Swarm pool"},
		{"zerops box", "ubuntu/zbox@1", "Zerops Box"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mock := newExportMock([]platform.ServiceStack{
				runtimeService("appdev", "php-apache@8.4", false),
				runtimeService("fleet", tt.svcType, false),
			}, nil)
			ssh := &exportCallLogSSH{}

			state, err := readSourceState(context.Background(), mock, ssh,
				runtime.Info{InContainer: true}, "proj1", "fleet", "")
			if err == nil {
				t.Fatalf("a %s as launch source must be refused, got state %+v", tt.svcType, state)
			}
			if !strings.Contains(err.Error(), "fleet") || !strings.Contains(err.Error(), tt.wantKind) {
				t.Errorf("refusal must name the service and say %q, got: %v", tt.wantKind, err)
			}
			if len(ssh.commands) != 0 {
				t.Errorf("no SSH command may run before the refusal, got: %v", ssh.commands)
			}
		})
	}
}
