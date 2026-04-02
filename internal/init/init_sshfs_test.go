package init_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	zcpinit "github.com/zeropsio/zcp/internal/init"
)

func TestRunSSHFS_SingleHostname(t *testing.T) {
	// Not parallel — mutates env and commandRunner.
	mountBase := t.TempDir()
	zcpinit.SetSSHFSMountBase(mountBase)
	t.Cleanup(func() { zcpinit.ResetSSHFSMountBase() })
	t.Setenv("ZCP_SSHFS_HOSTNAMES", "workerdev")

	var executed [][]string
	zcpinit.SetCommandRunner(func(name string, args ...string) error {
		cmd := append([]string{name}, args...)
		executed = append(executed, cmd)
		return nil
	})
	t.Cleanup(func() { zcpinit.ResetCommandRunner() })

	err := zcpinit.RunSSHFS()
	if err != nil {
		t.Fatalf("RunSSHFS() error: %v", err)
	}

	// Directory should be created.
	info, err := os.Stat(filepath.Join(mountBase, "workerdev"))
	if err != nil {
		t.Fatalf("mount dir should exist: %v", err)
	}
	if !info.IsDir() {
		t.Error("mount path should be a directory")
	}

	// Should have called zsc unit create.
	if len(executed) != 1 {
		t.Fatalf("expected 1 command, got %d: %v", len(executed), executed)
	}
	cmd := strings.Join(executed[0], " ")
	tests := []struct {
		name     string
		contains string
	}{
		{"has sudo", "sudo"},
		{"has zsc", "zsc"},
		{"has unit create", "unit create"},
		{"has unit name", "sshfs-workerdev"},
		{"has sshfs command", "sshfs -f"},
		{"has hostname", "workerdev:/var/www"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(cmd, tt.contains) {
				t.Errorf("command should contain %q, got: %s", tt.contains, cmd)
			}
		})
	}
}

func TestRunSSHFS_MultipleHostnames(t *testing.T) {
	// Not parallel — mutates env and commandRunner.
	mountBase := t.TempDir()
	zcpinit.SetSSHFSMountBase(mountBase)
	t.Cleanup(func() { zcpinit.ResetSSHFSMountBase() })
	t.Setenv("ZCP_SSHFS_HOSTNAMES", "app, worker, db")

	var unitNames []string
	zcpinit.SetCommandRunner(func(_ string, args ...string) error {
		// args: -E zsc unit create <unitName> <cmd>
		if len(args) >= 5 {
			unitNames = append(unitNames, args[4])
		}
		return nil
	})
	t.Cleanup(func() { zcpinit.ResetCommandRunner() })

	err := zcpinit.RunSSHFS()
	if err != nil {
		t.Fatalf("RunSSHFS() error: %v", err)
	}

	tests := []struct {
		name     string
		unitName string
		dirName  string
	}{
		{"app", "sshfs-app", "app"},
		{"worker", "sshfs-worker", "worker"},
		{"db", "sshfs-db", "db"},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if i >= len(unitNames) {
				t.Fatalf("expected unit %q but only %d commands executed", tt.unitName, len(unitNames))
			}
			if unitNames[i] != tt.unitName {
				t.Errorf("unit name: got %q, want %q", unitNames[i], tt.unitName)
			}
			if _, err := os.Stat(filepath.Join(mountBase, tt.dirName)); err != nil {
				t.Errorf("directory %s should exist: %v", tt.dirName, err)
			}
		})
	}
}

func TestRunSSHFS_EmptyEnv(t *testing.T) {
	// Not parallel — mutates env.
	// ZCP_SSHFS_HOSTNAMES not set — should skip gracefully.

	var called bool
	zcpinit.SetCommandRunner(func(_ string, _ ...string) error {
		called = true
		return nil
	})
	t.Cleanup(func() { zcpinit.ResetCommandRunner() })

	err := zcpinit.RunSSHFS()
	if err != nil {
		t.Fatalf("RunSSHFS() error: %v", err)
	}
	if called {
		t.Error("commandRunner should not be called when env is empty")
	}
}

func TestRunSSHFS_SkipsEmptyHostnames(t *testing.T) {
	// Not parallel — mutates env and commandRunner.
	mountBase := t.TempDir()
	zcpinit.SetSSHFSMountBase(mountBase)
	t.Cleanup(func() { zcpinit.ResetSSHFSMountBase() })
	t.Setenv("ZCP_SSHFS_HOSTNAMES", "app,,  , worker")

	var count int
	zcpinit.SetCommandRunner(func(_ string, _ ...string) error {
		count++
		return nil
	})
	t.Cleanup(func() { zcpinit.ResetCommandRunner() })

	err := zcpinit.RunSSHFS()
	if err != nil {
		t.Fatalf("RunSSHFS() error: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 mounts (app, worker), got %d", count)
	}
}
