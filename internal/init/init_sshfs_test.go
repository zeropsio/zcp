package init_test

import (
	"errors"
	"fmt"
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

func TestRunSSHFS_MountFailure_ContinuesAndSucceeds(t *testing.T) {
	// Not parallel — mutates env and commandRunner.
	// `zcp init sshfs` is a run.init command: its exit code gates the
	// container start, so one unmountable service (stopped, deleted) must
	// never keep the control plane down.
	mountBase := t.TempDir()
	zcpinit.SetSSHFSMountBase(mountBase)
	t.Cleanup(func() { zcpinit.ResetSSHFSMountBase() })
	t.Setenv("ZCP_SSHFS_HOSTNAMES", "listmonk,app")

	var unitNames []string
	zcpinit.SetCommandRunner(func(_ string, args ...string) error {
		// args: -E zsc unit create <unitName> <cmd>
		if len(args) >= 5 && args[4] == "sshfs-listmonk" {
			return errors.New("zsc unit create failed")
		}
		if len(args) >= 5 {
			unitNames = append(unitNames, args[4])
		}
		return nil
	})
	t.Cleanup(func() { zcpinit.ResetCommandRunner() })

	if err := zcpinit.RunSSHFS(); err != nil {
		t.Fatalf("RunSSHFS() must not fail on a per-hostname mount error, got: %v", err)
	}
	if len(unitNames) != 1 || unitNames[0] != "sshfs-app" {
		t.Errorf("hostnames after the failed one must still mount, got units %v", unitNames)
	}
}

func TestRunSSHFS_StaleMountPoint_UnmountsAndRetries(t *testing.T) {
	// Not parallel — mutates env and commandRunner.
	// A FUSE mount left behind by a service that is now stopped answers
	// stat with "transport endpoint is not connected", so MkdirAll fails
	// with "file exists". A regular file at the mount path reproduces the
	// same MkdirAll failure without FUSE.
	mountBase := t.TempDir()
	zcpinit.SetSSHFSMountBase(mountBase)
	t.Cleanup(func() { zcpinit.ResetSSHFSMountBase() })
	t.Setenv("ZCP_SSHFS_HOSTNAMES", "listmonk")

	mountPath := filepath.Join(mountBase, "listmonk")
	if err := os.WriteFile(mountPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	var executed []string
	zcpinit.SetCommandRunner(func(name string, args ...string) error {
		cmd := strings.Join(append([]string{name}, args...), " ")
		executed = append(executed, cmd)
		if cmd == "sudo umount -l "+mountPath {
			return os.Remove(mountPath)
		}
		return nil
	})
	t.Cleanup(func() { zcpinit.ResetCommandRunner() })

	if err := zcpinit.RunSSHFS(); err != nil {
		t.Fatalf("RunSSHFS() error: %v", err)
	}
	if len(executed) != 2 {
		t.Fatalf("expected lazy unmount then unit create, got %v", executed)
	}
	if executed[0] != "sudo umount -l "+mountPath {
		t.Errorf("first command: got %q, want lazy unmount of %s", executed[0], mountPath)
	}
	if !strings.Contains(executed[1], "unit create sshfs-listmonk") {
		t.Errorf("second command: got %q, want unit create", executed[1])
	}
	if info, err := os.Stat(mountPath); err != nil || !info.IsDir() {
		t.Errorf("mount path should be a directory after retry: %v", err)
	}
}

// writeLeaseRecordFile writes a lease record the way zerops_lease does (JSON,
// mode 0600, <stateDir>/leases/<alias>.json); the layout is the lease
// contract, so it is written out here rather than through the ops writer.
func writeLeaseRecordFile(t *testing.T, stateDir, pool string, number int, hostname string) {
	t.Helper()
	dir := filepath.Join(stateDir, "leases")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"poolHostname":%q,"poolServiceId":"svc-pool","containerId":"c-%d","containerNumber":%d,"containerHostname":%q,"lease":"0123456789abcdef0123456789abcdef","acquiredAt":"2026-10-23T10:00:00Z"}`,
		pool, number, number, hostname)
	if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%s-%d.json", pool, number)), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestInitSSHFS_RemountsLeases(t *testing.T) {
	// Not parallel — mutates env, commandRunner and the sshfs/lease package vars.
	mountBase := t.TempDir()
	stateDir := t.TempDir()
	aliasFile := filepath.Join(t.TempDir(), "zcp-leases.conf")
	zcpinit.SetSSHFSMountBase(mountBase)
	zcpinit.SetLeaseStateDir(stateDir)
	zcpinit.SetLeaseAliasFile(aliasFile)
	t.Cleanup(func() { zcpinit.ResetSSHFSMountBase(); zcpinit.ResetLeaseStateDir(); zcpinit.ResetLeaseAliasFile() })
	t.Setenv("ZCP_SSHFS_HOSTNAMES", "appdev")
	writeLeaseRecordFile(t, stateDir, "apppool", 3, "node-id-3.runtime.apppool.zerops")
	writeLeaseRecordFile(t, stateDir, "webpool", 12, "node-id-12.runtime.webpool.zerops")

	var executed [][]string
	var installedFile string
	zcpinit.SetCommandRunner(func(name string, args ...string) error {
		executed = append(executed, append([]string{name}, args...))
		if len(args) > 2 && args[1] == "install" {
			raw, err := os.ReadFile(args[len(args)-2])
			if err != nil {
				t.Errorf("alias source unreadable during install: %v", err)
			}
			installedFile = string(raw)
		}
		return nil
	})
	t.Cleanup(func() { zcpinit.ResetCommandRunner() })

	if err := zcpinit.RunSSHFS(); err != nil {
		t.Fatalf("RunSSHFS() error: %v", err)
	}

	if len(executed) != 4 {
		t.Fatalf("commands = %q, want service mount, alias install, two lease mounts", executed)
	}
	if !strings.Contains(strings.Join(executed[0], " "), "unit create sshfs-appdev") {
		t.Errorf("first command = %q, want the service mount before any lease work", executed[0])
	}
	inst := executed[1]
	if inst[0] != "sudo" || inst[1] != "-n" || inst[2] != "install" || inst[len(inst)-1] != aliasFile {
		t.Errorf("second command = %q, want sudo -n install ... %s", inst, aliasFile)
	}
	for _, want := range []string{"Host apppool-3\n    HostName node-id-3.runtime.apppool.zerops\n    User zerops\n", "Host webpool-12\n    HostName node-id-12.runtime.webpool.zerops\n    User zerops\n"} {
		if !strings.Contains(installedFile, want) {
			t.Errorf("installed alias file lacks %q:\n%s", want, installedFile)
		}
	}
	for i, alias := range []string{"apppool-3", "webpool-12"} {
		cmd := executed[2+i]
		joined := strings.Join(cmd, " ")
		if cmd[0] != "sudo" || cmd[5] != "sshfs-"+alias || !strings.Contains(joined, " "+alias+":/var/www "+filepath.Join(mountBase, alias)) {
			t.Errorf("lease mount %d = %q, want unit sshfs-%s of %s:/var/www at %s", i, cmd, alias, alias, filepath.Join(mountBase, alias))
		}
		if info, err := os.Stat(filepath.Join(mountBase, alias)); err != nil || !info.IsDir() {
			t.Errorf("mount dir for %s missing: %v", alias, err)
		}
	}
}

func TestInitSSHFS_LeaseFailure_Continues(t *testing.T) {
	// Not parallel — mutates env, commandRunner and the sshfs/lease package vars.
	// DP-2: a lease that cannot be restored is printed and skipped; it never
	// keeps the control plane (run.init) down.
	mountBase := t.TempDir()
	stateDir := t.TempDir()
	zcpinit.SetSSHFSMountBase(mountBase)
	zcpinit.SetLeaseStateDir(stateDir)
	zcpinit.SetLeaseAliasFile(filepath.Join(t.TempDir(), "zcp-leases.conf"))
	t.Cleanup(func() { zcpinit.ResetSSHFSMountBase(); zcpinit.ResetLeaseStateDir(); zcpinit.ResetLeaseAliasFile() })
	t.Setenv("ZCP_SSHFS_HOSTNAMES", "")
	writeLeaseRecordFile(t, stateDir, "apppool", 3, "node-id-3.runtime.apppool.zerops")
	writeLeaseRecordFile(t, stateDir, "apppool", 4, "node-id-4.runtime.apppool.zerops")

	var units []string
	zcpinit.SetCommandRunner(func(_ string, args ...string) error {
		if len(args) >= 5 && args[2] == "unit" {
			if args[4] == "sshfs-apppool-3" {
				return errors.New("zsc unit create failed")
			}
			units = append(units, args[4])
		}
		return nil
	})
	t.Cleanup(func() { zcpinit.ResetCommandRunner() })

	if err := zcpinit.RunSSHFS(); err != nil {
		t.Fatalf("RunSSHFS() must not fail on a lease mount error, got: %v", err)
	}
	if len(units) != 1 || units[0] != "sshfs-apppool-4" {
		t.Errorf("units after the failed lease = %v, want sshfs-apppool-4 still mounted", units)
	}
}

func TestInitSSHFS_AliasInstallFails_SkipsLeaseMounts(t *testing.T) {
	// Not parallel — mutates env, commandRunner and the sshfs/lease package vars.
	// A mount without its alias cannot resolve, so none is attempted; the
	// command still succeeds.
	stateDir := t.TempDir()
	zcpinit.SetSSHFSMountBase(t.TempDir())
	zcpinit.SetLeaseStateDir(stateDir)
	zcpinit.SetLeaseAliasFile(filepath.Join(t.TempDir(), "zcp-leases.conf"))
	t.Cleanup(func() { zcpinit.ResetSSHFSMountBase(); zcpinit.ResetLeaseStateDir(); zcpinit.ResetLeaseAliasFile() })
	t.Setenv("ZCP_SSHFS_HOSTNAMES", "")
	writeLeaseRecordFile(t, stateDir, "apppool", 3, "node-id-3.runtime.apppool.zerops")

	var executed [][]string
	zcpinit.SetCommandRunner(func(name string, args ...string) error {
		executed = append(executed, append([]string{name}, args...))
		return errors.New("sudo: a password is required")
	})
	t.Cleanup(func() { zcpinit.ResetCommandRunner() })

	if err := zcpinit.RunSSHFS(); err != nil {
		t.Fatalf("RunSSHFS() must not fail when the alias install fails, got: %v", err)
	}
	if len(executed) != 1 {
		t.Errorf("commands = %q, want only the failed alias install", executed)
	}
}
