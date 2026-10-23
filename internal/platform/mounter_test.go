package platform

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestWriteProbe(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		setup  func(t *testing.T) string
		want   bool
		verify func(t *testing.T, dir string)
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
			verify: func(t *testing.T, dir string) {
				t.Helper()
				entries, err := os.ReadDir(dir)
				if err != nil {
					t.Fatalf("readdir: %v", err)
				}
				for _, e := range entries {
					if e.Name() != "." && e.Name() != ".." {
						t.Errorf("probe file %s should have been cleaned up", e.Name())
					}
				}
			},
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
			if tt.verify != nil {
				tt.verify(t, dir)
			}
		})
	}
}

// recordedCommand is one argv a fake runner was asked to execute.
type recordedCommand []string

// fakeExecMounter is a SystemMounter whose commands are recorded, not run, and
// whose readiness wait is a no-op.
func fakeExecMounter() (*SystemMounter, *[]recordedCommand) {
	var got []recordedCommand
	m := &SystemMounter{
		run: func(_ context.Context, _ time.Duration, name string, args ...string) error {
			argv := append(recordedCommand{name}, args...)
			got = append(got, argv)
			return nil
		},
		ready: func(context.Context, string) error { return nil },
	}
	return m, &got
}

func TestMount_LeaseAlias_UnitAndSource(t *testing.T) {
	t.Parallel()

	m, got := fakeExecMounter()
	local := filepath.Join(t.TempDir(), "apppool-3")

	if err := m.Mount(context.Background(), "apppool-3", local); err != nil {
		t.Fatalf("Mount(lease alias): %v", err)
	}
	if len(*got) != 1 {
		t.Fatalf("commands = %v, want exactly one zsc unit create", *got)
	}
	argv := (*got)[0]
	wantHead := []string{"sudo", "-E", "zsc", "unit", "create", "sshfs-apppool-3"}
	if len(argv) != len(wantHead)+1 || !slices.Equal(argv[:len(wantHead)], wantHead) {
		t.Fatalf("argv = %q, want %q followed by the sshfs command", argv, wantHead)
	}
	if cmd := argv[len(argv)-1]; !strings.HasPrefix(cmd, "sshfs -f -o ") || !strings.HasSuffix(cmd, " apppool-3:/var/www "+local) {
		t.Errorf("sshfs command = %q, want source apppool-3:/var/www and mount %s", cmd, local)
	}
}

func TestMount_InvalidName_RunsNothing(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"apppool-3; rm -rf /", "app-dev-3", "", "apppool-3\nHost x"} {
		m, got := fakeExecMounter()
		if err := m.Mount(context.Background(), name, t.TempDir()); err == nil {
			t.Errorf("Mount(%q) accepted an invalid name", name)
		}
		if len(*got) != 0 {
			t.Errorf("Mount(%q) ran %v", name, *got)
		}
	}
}

func TestForceUnmount_LeaseAlias_RemovesUnit(t *testing.T) {
	t.Parallel()

	m, got := fakeExecMounter()
	if err := m.ForceUnmount(context.Background(), "apppool-3", "/var/www/apppool-3"); err != nil {
		t.Fatalf("ForceUnmount: %v", err)
	}
	want := []recordedCommand{
		{"sudo", "-E", "zsc", "unit", "remove", "sshfs-apppool-3"},
		{"fusermount", "-uz", "/var/www/apppool-3"},
	}
	if len(*got) != len(want) {
		t.Fatalf("commands = %q, want %q", *got, want)
	}
	for i := range want {
		if !slices.Equal((*got)[i], want[i]) {
			t.Errorf("command %d = %q, want %q", i, (*got)[i], want[i])
		}
	}
}

func TestMounter_LeaseAlias_UnitLifecycle(t *testing.T) {
	t.Parallel()

	m, got := fakeExecMounter()
	ctx := context.Background()
	if _, err := m.HasUnit(ctx, "apppool-3"); err != nil {
		t.Fatalf("HasUnit: %v", err)
	}
	if err := m.CleanupUnit(ctx, "apppool-3"); err != nil {
		t.Fatalf("CleanupUnit: %v", err)
	}
	if err := m.Unmount(ctx, "apppool-3", "/var/www/apppool-3"); err != nil {
		t.Fatalf("Unmount: %v", err)
	}
	want := []recordedCommand{
		{"systemctl", "cat", "zerops@sshfs-apppool-3.service"},
		{"sudo", "-E", "zsc", "unit", "remove", "sshfs-apppool-3"},
		{"fusermount", "-u", "/var/www/apppool-3"},
		{"sudo", "-E", "zsc", "unit", "remove", "sshfs-apppool-3"},
	}
	if len(*got) != len(want) {
		t.Fatalf("commands = %q, want %q", *got, want)
	}
	for i := range want {
		if !slices.Equal((*got)[i], want[i]) {
			t.Errorf("command %d = %q, want %q", i, (*got)[i], want[i])
		}
	}
}
