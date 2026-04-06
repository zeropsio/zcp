package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteProbe(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setup func(t *testing.T) string
		want  bool
	}{
		{
			name: "writable directory returns true",
			setup: func(t *testing.T) string {
				t.Helper()
				return t.TempDir()
			},
			want: true,
		},
		{
			name: "read-only directory returns false",
			setup: func(t *testing.T) string {
				t.Helper()
				dir := t.TempDir()
				if err := os.Chmod(dir, 0o555); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := os.Chmod(dir, 0o755); err != nil {
						t.Logf("cleanup chmod: %v", err)
					}
				})
				return dir
			},
			want: false,
		},
		{
			name: "nonexistent directory returns false",
			setup: func(t *testing.T) string {
				t.Helper()
				return filepath.Join(t.TempDir(), "does-not-exist")
			},
			want: false,
		},
		{
			name: "probe file is cleaned up after success",
			setup: func(t *testing.T) string {
				t.Helper()
				return t.TempDir()
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := tt.setup(t)
			got := writeProbe(dir)
			if got != tt.want {
				t.Errorf("writeProbe(%s) = %v, want %v", dir, got, tt.want)
			}
			if tt.name == "probe file is cleaned up after success" {
				probe := filepath.Join(dir, ".mount_probe")
				if _, err := os.Stat(probe); err == nil {
					t.Errorf("probe file %s should have been cleaned up", probe)
				}
			}
		})
	}
}
