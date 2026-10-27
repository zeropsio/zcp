package platform

import (
	"context"
	"errors"
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
	m.mounted = func(string) (bool, error) { return true, nil }
	if err := m.ForceUnmount(context.Background(), "apppool-3", "/var/www/apppool-3"); err != nil {
		t.Fatalf("ForceUnmount: %v", err)
	}
	want := []recordedCommand{
		{"sudo", "-E", "zsc", "unit", "remove", "sshfs-apppool-3"},
		{"sudo", "systemctl", "reset-failed", "zerops@sshfs-apppool-3.service"},
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
	m.mounted = func(string) (bool, error) { return true, nil }
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
		{"sudo", "systemctl", "reset-failed", "zerops@sshfs-apppool-3.service"},
		{"sudo", "-E", "zsc", "unit", "remove", "sshfs-apppool-3"},
		{"sudo", "systemctl", "reset-failed", "zerops@sshfs-apppool-3.service"},
		{"fusermount", "-u", "/var/www/apppool-3"},
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

// The production mounter (no test seam) must reach the real command runner.
func TestSystemMounter_NoSeam_RunsRealCommand(t *testing.T) {
	t.Parallel()

	m := NewSystemMounter()
	if err := m.exec(context.Background(), time.Second, "true"); err != nil {
		t.Fatalf("exec(true) = %v, want nil", err)
	}
	if err := m.exec(context.Background(), time.Second, "false"); err == nil {
		t.Fatal("exec(false) = nil, want the command's failure")
	}
}

// failingCommands is a fake runner that fails the commands whose argv starts
// with one of fail, and records every command.
func failingCommands(got *[]recordedCommand, fail ...[]string) func(context.Context, time.Duration, string, ...string) error {
	return func(_ context.Context, _ time.Duration, name string, args ...string) error {
		argv := append(recordedCommand{name}, args...)
		*got = append(*got, argv)
		for _, f := range fail {
			if len(argv) >= len(f) && slices.Equal(argv[:len(f)], f) {
				return errors.New("exit status 1")
			}
		}
		return nil
	}
}

// TestUnmount_GoneOnceTheUnitStops_NoFusermount: the sshfs mount is the
// unit's process, so removing the unit first ends the mount; a fusermount of
// a mount that is gone would fail the unmount (live: the advice and the
// fallback both ran on a mount already gone).
func TestUnmount_GoneOnceTheUnitStops_NoFusermount(t *testing.T) {
	t.Parallel()
	for _, force := range []bool{false, true} {
		m, got := fakeExecMounter()
		m.mounted = func(string) (bool, error) { return false, nil }
		var err error
		if force {
			err = m.ForceUnmount(context.Background(), "apppool-3", "/var/www/apppool-3")
		} else {
			err = m.Unmount(context.Background(), "apppool-3", "/var/www/apppool-3")
		}
		if err != nil {
			t.Fatalf("force=%v: %v", force, err)
		}
		// The unit removed, then its instance's failed state cleared: zsc unit remove
		// leaves the instance listed failed until reset-failed (live 2026-10-24).
		remove := recordedCommand{"sudo", "-E", "zsc", "unit", "remove", "sshfs-apppool-3"}
		reset := recordedCommand{"sudo", "systemctl", "reset-failed", "zerops@sshfs-apppool-3.service"}
		if len(*got) != 2 || !slices.Equal((*got)[0], remove) || !slices.Equal((*got)[1], reset) {
			t.Errorf("force=%v: commands = %q, want only %q then %q", force, *got, remove, reset)
		}
	}
}

// TestUnmount_UnitRemoveFails_NoFusermountThatTheUnitWouldUndo: a unit that
// stays restarts sshfs after any fusermount (live: back within ~12 s), so the
// unmount fails naming the unit instead of unmounting what comes back.
func TestUnmount_UnitRemoveFails_NoFusermountThatTheUnitWouldUndo(t *testing.T) {
	t.Parallel()
	var got []recordedCommand
	m := &SystemMounter{run: failingCommands(&got, []string{"sudo", "-E", "zsc", "unit", "remove"})}
	m.mounted = func(string) (bool, error) { return true, nil }
	err := m.Unmount(context.Background(), "apppool-3", "/var/www/apppool-3")
	if err == nil || !strings.Contains(err.Error(), "zsc unit remove") {
		t.Fatalf("err = %v, want the unit removal's failure", err)
	}
	for _, c := range got {
		if c[0] == "fusermount" {
			t.Errorf("ran %q although the unit stays", c)
		}
	}
}

// TestHasUnit_AFailedInstanceIsNoUnit: an older zcp's zsc unit remove left
// every unmounted service's instance listed failed (no reset-failed). Status
// and unmount ask HasUnit of every unmounted service: a failed instance with
// no unit file is no unit there — never "pending", never UNIT_CLEANED, and
// one systemctl call per service, as before. Only a cleanup, after its
// removal, asks whether a failed instance is left (unitLeft).
func TestHasUnit_AFailedInstanceIsNoUnit(t *testing.T) {
	t.Parallel()
	var got []recordedCommand
	m := &SystemMounter{run: failingCommands(&got, []string{"systemctl", "cat"})}
	has, err := m.HasUnit(context.Background(), "apppool-3")
	if err != nil || has {
		t.Fatalf("HasUnit = %v, %v; want no unit for a failed instance without its unit file", has, err)
	}
	want := recordedCommand{"systemctl", "cat", "zerops@sshfs-apppool-3.service"}
	if len(got) != 1 || !slices.Equal(got[0], want) {
		t.Errorf("commands = %q, want only %q", got, want)
	}
}

// TestCleanupUnit_RemoveFailsButNothingIsLeft_Succeeds: a unit whose file is
// already gone makes zsc unit remove fail; the reset-failed clears the
// instance, and with no unit left the cleanup succeeded.
func TestCleanupUnit_RemoveFailsButNothingIsLeft_Succeeds(t *testing.T) {
	t.Parallel()
	var got []recordedCommand
	m := &SystemMounter{run: failingCommands(&got,
		[]string{"sudo", "-E", "zsc", "unit", "remove"},
		[]string{"systemctl", "cat"},
		[]string{"systemctl", "is-failed"},
	)}
	if err := m.CleanupUnit(context.Background(), "apppool-3"); err != nil {
		t.Fatalf("CleanupUnit: %v", err)
	}
	want := recordedCommand{"sudo", "systemctl", "reset-failed", "zerops@sshfs-apppool-3.service"}
	if len(got) < 2 || !slices.Equal(got[1], want) {
		t.Errorf("commands = %q, want the reset-failed after the remove", got)
	}
}

// TestForceUnmount_UnitRemoveFails_FailsWhileTheUnitStays: a stale mount is
// cleared by removing its unit first; a unit that stays restarts sshfs after
// the lazy unmount, so ForceUnmount fails naming the removal — never success
// with sshfs coming back (a park would then stop the container under a mount
// that hangs whatever reads it, DP-28). A removal that fails because no unit
// is left (an orphan stale mount) still clears the mount.
func TestForceUnmount_UnitRemoveFails_FailsWhileTheUnitStays(t *testing.T) {
	t.Parallel()
	remove := []string{"sudo", "-E", "zsc", "unit", "remove"}
	for _, tt := range []struct {
		name    string
		fail    [][]string
		wantErr bool
	}{
		{name: "the unit stays", fail: [][]string{remove}, wantErr: true},
		{name: "no unit is left", fail: [][]string{remove, {"systemctl", "cat"}, {"systemctl", "is-failed"}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got []recordedCommand
			m := &SystemMounter{run: failingCommands(&got, tt.fail...)}
			m.mounted = func(string) (bool, error) { return true, nil }

			err := m.ForceUnmount(context.Background(), "apppool-3", "/var/www/apppool-3")
			lazy := slices.ContainsFunc(got, func(c recordedCommand) bool { return c[0] == "fusermount" })
			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), "zsc unit remove") {
					t.Errorf("err = %v, want the unit removal's failure", err)
				}
				if lazy {
					t.Errorf("ran fusermount although the unit stays: %q", got)
				}
				return
			}
			if err != nil || !lazy {
				t.Errorf("err = %v, fusermount run = %v: want the orphan stale mount cleared", err, lazy)
			}
		})
	}
}
