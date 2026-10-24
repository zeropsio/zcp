package init_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	zcpinit "github.com/zeropsio/zcp/internal/init"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
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

	if err := zcpinit.RunSSHFS(zcpinit.WithLeaseCheck(leaseCheckOurs)); err != nil {
		t.Fatalf("RunSSHFS() error: %v", err)
	}

	if len(executed) != 5 {
		t.Fatalf("commands = %q, want service mount, alias install, its rename into place, two lease mounts", executed)
	}
	if !strings.Contains(strings.Join(executed[0], " "), "unit create sshfs-appdev") {
		t.Errorf("first command = %q, want the service mount before any lease work", executed[0])
	}
	// The alias file is installed beside its place under a name ssh's
	// ssh_config.d/*.conf include does not match, then renamed over it.
	inst := executed[1]
	if inst[0] != "sudo" || inst[1] != "-n" || inst[2] != "install" || inst[len(inst)-1] != aliasFile+".new" {
		t.Errorf("second command = %q, want sudo -n install ... %s.new", inst, aliasFile)
	}
	if mv := executed[2]; !slices.Equal(mv, []string{"sudo", "-n", "mv", "-f", aliasFile + ".new", aliasFile}) {
		t.Errorf("third command = %q, want sudo -n mv -f %s.new %s", mv, aliasFile, aliasFile)
	}
	for _, want := range []string{"Host apppool-3\n    HostName node-id-3.runtime.apppool.zerops\n    User zerops\n", "Host webpool-12\n    HostName node-id-12.runtime.webpool.zerops\n    User zerops\n"} {
		if !strings.Contains(installedFile, want) {
			t.Errorf("installed alias file lacks %q:\n%s", want, installedFile)
		}
	}
	for i, alias := range []string{"apppool-3", "webpool-12"} {
		cmd := executed[3+i]
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

	if err := zcpinit.RunSSHFS(zcpinit.WithLeaseCheck(leaseCheckOurs)); err != nil {
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
	var asked int
	check := func(ctx context.Context, stateDir, alias, what string) (ops.LeaseTargetInfo, bool, error) {
		asked++
		return leaseCheckOurs(ctx, stateDir, alias, what)
	}

	if err := zcpinit.RunSSHFS(zcpinit.WithLeaseCheck(check)); err != nil {
		t.Fatalf("RunSSHFS() must not fail when the alias install fails, got: %v", err)
	}
	if len(executed) != 1 {
		t.Errorf("commands = %q, want only the failed alias install", executed)
	}
	if asked != 0 {
		t.Errorf("the orchestrator was asked %d times with nothing it could mount", asked)
	}
}

// TestInitSSHFS_LeaseMount_SameOptionsAsAcquire: a lease mount restored at
// boot behaves like the one acquire made — the server mounter's sshfs
// options, reconnect included.
func TestInitSSHFS_LeaseMount_SameOptionsAsAcquire(t *testing.T) {
	// Not parallel — mutates commandRunner and the sshfs/lease package vars.
	stateDir := t.TempDir()
	zcpinit.SetSSHFSMountBase(t.TempDir())
	zcpinit.SetLeaseStateDir(stateDir)
	zcpinit.SetLeaseAliasFile(filepath.Join(t.TempDir(), "zcp-leases.conf"))
	t.Cleanup(func() { zcpinit.ResetSSHFSMountBase(); zcpinit.ResetLeaseStateDir(); zcpinit.ResetLeaseAliasFile() })
	t.Setenv("ZCP_SSHFS_HOSTNAMES", "")
	writeLeaseRecordFile(t, stateDir, "apppool", 3, "node-id-3.runtime.apppool.zerops")
	var mountCmd string
	zcpinit.SetCommandRunner(func(name string, args ...string) error {
		if joined := strings.Join(args, " "); strings.Contains(joined, "unit create sshfs-apppool-3") {
			mountCmd = joined
		}
		return nil
	})
	t.Cleanup(func() { zcpinit.ResetCommandRunner() })

	if err := zcpinit.RunSSHFS(zcpinit.WithLeaseCheck(leaseCheckOurs)); err != nil {
		t.Fatalf("RunSSHFS() error: %v", err)
	}
	if !strings.Contains(mountCmd, "-o "+platform.SSHFSMountOptions+" ") {
		t.Errorf("lease mount %q does not use the server mounter's options %q", mountCmd, platform.SSHFSMountOptions)
	}
}

// TestInitSSHFS_ParkedLease_SkippedNotFatal: a lease whose record says it is
// parked (DP-28) keeps its ssh alias — its unpark reaches it — and is not
// mounted, nor its orchestrator asked: its container is stopped, and a mount
// of it would hang whatever reads it. The other leases are restored as before
// and the step never fails.
func TestInitSSHFS_ParkedLease_SkippedNotFatal(t *testing.T) {
	// Not parallel — mutates env, commandRunner and the sshfs/lease package vars.
	mountBase := t.TempDir()
	stateDir := t.TempDir()
	zcpinit.SetSSHFSMountBase(mountBase)
	zcpinit.SetLeaseStateDir(stateDir)
	zcpinit.SetLeaseAliasFile(filepath.Join(t.TempDir(), "zcp-leases.conf"))
	t.Cleanup(func() { zcpinit.ResetSSHFSMountBase(); zcpinit.ResetLeaseStateDir(); zcpinit.ResetLeaseAliasFile() })
	t.Setenv("ZCP_SSHFS_HOSTNAMES", "")
	writeLeaseRecordFile(t, stateDir, "apppool", 3, "node-id-3.runtime.apppool.zerops")
	parked := fmt.Sprintf(`{"poolHostname":"apppool","poolServiceId":"svc-pool","containerId":"c-4","containerNumber":4,"containerHostname":%q,"lease":"0123456789abcdef0123456789abcdef","acquiredAt":"2026-10-23T10:00:00Z","parked":true,"parkedAt":"2026-10-24T09:00:00Z"}`,
		"node-id-4.runtime.apppool.zerops")
	if err := os.WriteFile(filepath.Join(stateDir, "leases", "apppool-4.json"), []byte(parked), 0o600); err != nil {
		t.Fatal(err)
	}

	var executed []string
	var installedFile string
	zcpinit.SetCommandRunner(func(name string, args ...string) error {
		executed = append(executed, strings.Join(append([]string{name}, args...), " "))
		if len(args) > 2 && args[1] == "install" {
			raw, _ := os.ReadFile(args[len(args)-2])
			installedFile = string(raw)
		}
		return nil
	})
	t.Cleanup(func() { zcpinit.ResetCommandRunner() })
	orch := newBootOrch(map[string]platform.SwarmContainer{
		"c-3": {ID: "c-3", Number: 3, Status: platform.SwarmContainerActive, LockState: "reserved"},
		"c-4": {ID: "c-4", Number: 4, Status: platform.SwarmContainerStopped, LockState: "reserved"},
	})

	if err := zcpinit.RunSSHFS(zcpinit.WithLeaseCheck(orchestratorCheck(orch))); err != nil {
		t.Fatalf("RunSSHFS() error: %v (a parked lease never fails the boot)", err)
	}
	if got := orch.renewed(); !slices.Equal(got, []string{"c-3"}) {
		t.Errorf("renews = %q, want c-3 only: a parked record is not mounted, so nothing is asked of it", got)
	}
	if !strings.Contains(installedFile, "Host apppool-4\n") || !strings.Contains(installedFile, "Host apppool-3\n") {
		t.Errorf("installed alias file lacks a lease's stanza (the parked one keeps its alias):\n%s", installedFile)
	}
	var mounts []string
	for _, cmd := range executed {
		if strings.Contains(cmd, "unit create sshfs-") {
			mounts = append(mounts, cmd)
		}
	}
	if len(mounts) != 1 || !strings.Contains(mounts[0], "sshfs-apppool-3 ") {
		t.Errorf("mounts = %q, want apppool-3 only (apppool-4 is parked)", mounts)
	}
	if _, err := os.Stat(filepath.Join(mountBase, "apppool-4")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a mount dir was made for the parked lease: %v", err)
	}
}

// bootOrchToken is the bearer of the pool the lease restore tests hold;
// bootLease is the lease every record writeLeaseRecordFile writes carries.
const (
	bootOrchToken = "orch-api-token-0001"
	bootLease     = "0123456789abcdef0123456789abcdef"
)

// bootOrch is a pool orchestrator the lease restore tests control, answering
// as the live one does and as the ops lease tests' fake models it: a renew of
// a container reserved by the lease answers it; one another holder reserves
// (foreign) or nobody does is refused unknown-lease, and one it does not know
// container-not-found; renewErr fails every renew. A restore only asks: any
// call that would change a container is recorded in changed and refused.
type bootOrch struct {
	mu         sync.Mutex
	containers map[string]platform.SwarmContainer
	foreign    map[string]bool
	renewErr   error
	renews     []string
	changed    []string
}

func newBootOrch(containers map[string]platform.SwarmContainer) *bootOrch {
	return &bootOrch{containers: containers, foreign: map[string]bool{}}
}

func (o *bootOrch) renewed() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return slices.Clone(o.renews)
}

func (o *bootOrch) changes() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return slices.Clone(o.changed)
}

func notFound() error {
	return &platform.SwarmError{Status: 400, Code: "containerAction", Reason: platform.SwarmReasonContainerNotFound, Message: "container not found"}
}

func (o *bootOrch) Renew(_ context.Context, id, _ string) (platform.SwarmContainer, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.renews = append(o.renews, id)
	if o.renewErr != nil {
		return platform.SwarmContainer{}, o.renewErr
	}
	c, ok := o.containers[id]
	switch {
	case !ok:
		return platform.SwarmContainer{}, notFound()
	case c.LockState == "" || o.foreign[id]:
		return platform.SwarmContainer{}, &platform.SwarmError{Status: 400, Code: "containerAction", Reason: platform.SwarmReasonUnknownLease,
			Message: "no reservation of this container is held by that lease - acquire it again"}
	}
	return c, nil
}

func (o *bootOrch) Container(_ context.Context, id string) (platform.SwarmContainer, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if c, ok := o.containers[id]; ok {
		return c, nil
	}
	return platform.SwarmContainer{}, notFound()
}

func (o *bootOrch) Containers(context.Context) ([]platform.SwarmContainer, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]platform.SwarmContainer, 0, len(o.containers))
	for _, c := range o.containers {
		out = append(out, c)
	}
	return out, nil
}

func (o *bootOrch) Pool(context.Context) (platform.SwarmPool, error) {
	return platform.SwarmPool{MaximumContainerCount: 4}, nil
}

func (o *bootOrch) Rollout(context.Context) (platform.SwarmRollout, error) {
	return platform.SwarmRollout{}, nil
}

func (o *bootOrch) change(what string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.changed = append(o.changed, what)
	return &platform.SwarmError{Status: 400, Code: "containerAction", Message: "a lease restore changes no container"}
}

func (o *bootOrch) Acquire(context.Context, platform.SwarmAcquireRequest) (platform.SwarmAcquired, error) {
	return platform.SwarmAcquired{}, o.change("acquire")
}

func (o *bootOrch) Release(_ context.Context, id, _ string, _ bool) (platform.SwarmContainer, error) {
	return platform.SwarmContainer{}, o.change("release " + id)
}

func (o *bootOrch) Delete(_ context.Context, id, _ string) error { return o.change("delete " + id) }

func (o *bootOrch) Create(context.Context, int) ([]platform.SwarmContainer, error) {
	return nil, o.change("create")
}

func (o *bootOrch) Stop(_ context.Context, id, _ string) (platform.SwarmContainer, error) {
	return platform.SwarmContainer{}, o.change("stop " + id)
}

func (o *bootOrch) Start(_ context.Context, id, _ string) (platform.SwarmContainer, error) {
	return platform.SwarmContainer{}, o.change("start " + id)
}

// orchestratorCheck is the lease check the command layer hands `zcp init`,
// over a project holding the pool apppool with orch behind its factory:
// ops.LeaseTargetChecked, the one every tool that reaches a lease's container
// asks (docs/spec-workflows.md §8 DP-2). A nil orch is a zcp that has no
// orchestrator to ask.
func orchestratorCheck(orch *bootOrch) zcpinit.LeaseCheck {
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "proj"}).
		WithServices([]platform.ServiceStack{
			{ID: "svc-pool", Name: "apppool", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1", ServiceStackTypeCategoryName: "USER"}},
		}).
		WithServiceEnv("svc-pool", []platform.ServiceEnvVar{{Key: "API_TOKEN", Content: bootOrchToken, Sensitive: true}})
	var swarm ops.SwarmFactory
	if orch != nil {
		swarm = func(string, string) ops.SwarmAPI { return orch }
	}
	return func(ctx context.Context, stateDir, alias, what string) (ops.LeaseTargetInfo, bool, error) {
		return ops.LeaseTargetChecked(ctx, ops.LeaseDeps{
			Client: mock, ProjectID: "p1", StateDir: stateDir, Swarm: swarm,
			// Hermetic: the machine's own env never supplies a pool token.
			Env: func(string) (string, bool) { return "", false },
		}, alias, what)
	}
}

// leaseCheckOurs is a lease check whose orchestrator gives every lease's
// container to zcp, running, as its record reads.
func leaseCheckOurs(_ context.Context, stateDir, alias, _ string) (ops.LeaseTargetInfo, bool, error) {
	return ops.LeaseTarget(stateDir, alias)
}

// leaseRestoreFixture is a container booting with one lease record,
// apppool-3, and an empty mount base; run runs `zcp init sshfs` with opts and
// returns its output and the lease mounts it made.
type leaseRestoreFixture struct {
	mountBase, stateDir string
}

func newLeaseRestoreFixture(t *testing.T) *leaseRestoreFixture {
	t.Helper()
	fx := &leaseRestoreFixture{mountBase: t.TempDir(), stateDir: t.TempDir()}
	zcpinit.SetSSHFSMountBase(fx.mountBase)
	zcpinit.SetLeaseStateDir(fx.stateDir)
	zcpinit.SetLeaseAliasFile(filepath.Join(t.TempDir(), "zcp-leases.conf"))
	t.Cleanup(func() { zcpinit.ResetSSHFSMountBase(); zcpinit.ResetLeaseStateDir(); zcpinit.ResetLeaseAliasFile() })
	t.Setenv("ZCP_SSHFS_HOSTNAMES", "")
	writeLeaseRecordFile(t, fx.stateDir, "apppool", 3, "node-id-3.runtime.apppool.zerops")
	return fx
}

func (fx *leaseRestoreFixture) run(t *testing.T, opts ...zcpinit.Option) (out string, mounts []string) {
	t.Helper()
	var mu sync.Mutex
	zcpinit.SetCommandRunner(func(name string, args ...string) error {
		if len(args) >= 5 && args[2] == "unit" && args[3] == "create" {
			mu.Lock()
			mounts = append(mounts, args[4])
			mu.Unlock()
		}
		return nil
	})
	t.Cleanup(func() { zcpinit.ResetCommandRunner() })
	out = captureStderr(t, func() {
		if err := zcpinit.RunSSHFS(opts...); err != nil {
			t.Errorf("RunSSHFS() = %v: a lease that is not restored never fails the boot", err)
		}
	})
	return out, mounts
}

// TestInitSSHFS_LeaseRestore_AsksItsOrchestratorFirst: before zcp init
// remounts a lease after a redeploy it asks the pool's orchestrator, once,
// whether the container is still the lease's (docs/spec-workflows.md §8 DP-2),
// as every tool that reaches a lease's container does: the lease may have
// ended outside zcp and the pool handed the container to another holder, whose
// workspace a remount would put at /var/www/<alias>. Ours is mounted; one
// whose container is stopped is parked and not mounted (DP-28); a lost one —
// another holder's, freed, gone — is not mounted and is printed with the
// release that drops its record; one the orchestrator does not answer about,
// or that this zcp cannot ask, is not mounted and is printed with why and the
// mount that asks again. Nothing is changed on any container, the lease never
// reaches the output, and the step never fails.
func TestInitSSHFS_LeaseRestore_AsksItsOrchestratorFirst(t *testing.T) {
	// Not parallel — mutates env, commandRunner, os.Stderr and the sshfs/lease package vars.
	const (
		release = "zerops_lease action=release lease=apppool-3"
		mount   = "zerops_mount action=mount serviceHostname=apppool-3"
		unpark  = "zerops_lease action=unpark lease=apppool-3"
	)
	reserved := func(status string) map[string]platform.SwarmContainer {
		return map[string]platform.SwarmContainer{"c-3": {ID: "c-3", Number: 3, Status: status, LockState: "reserved"}}
	}
	for _, tt := range []struct {
		name    string
		orch    *bootOrch
		check   zcpinit.LeaseCheck // instead of the orchestrator's
		mounted bool
		want    []string
		notWant []string
		renews  int
	}{
		{name: "ours", orch: newBootOrch(reserved(platform.SwarmContainerActive)), mounted: true,
			want: []string{"SSHFS mount: lease apppool-3"}, notWant: []string{release, mount, "!"}, renews: 1},
		{name: "stopped outside zcp: parked", orch: newBootOrch(reserved(platform.SwarmContainerStopped)),
			want: []string{"apppool-3 is parked", unpark}, notWant: []string{release, "SSHFS mount: lease"}, renews: 1},
		{name: "another holder's", orch: func() *bootOrch {
			o := newBootOrch(reserved(platform.SwarmContainerActive))
			o.foreign["c-3"] = true
			return o
		}(), want: []string{"apppool-3", "another holder", release}, notWant: []string{mount, "SSHFS mount: lease"}, renews: 1},
		{name: "released elsewhere", orch: newBootOrch(map[string]platform.SwarmContainer{"c-3": {ID: "c-3", Number: 3, Status: platform.SwarmContainerActive}}),
			want: []string{"apppool-3", release}, notWant: []string{mount, "SSHFS mount: lease"}, renews: 1},
		{name: "gone", orch: newBootOrch(map[string]platform.SwarmContainer{}),
			want: []string{"apppool-3", "gone", release}, notWant: []string{mount, "SSHFS mount: lease"}, renews: 1},
		{name: "the orchestrator does not answer", orch: func() *bootOrch {
			o := newBootOrch(reserved(platform.SwarmContainerActive))
			o.renewErr = &platform.SwarmError{Status: 503, Code: "serviceUnavailable", Message: "the orchestrator could not read its reservations"}
			return o
		}(), want: []string{"apppool-3", "orchestrator", "could not read its reservations", mount}, notWant: []string{release, "SSHFS mount: lease"}, renews: 1},
		{name: "no orchestrator to ask", want: []string{"apppool-3", "orchestrator", mount}, notWant: []string{release, "SSHFS mount: lease"}},
		{name: "no API client to ask with", check: func(_ context.Context, _, alias, what string) (ops.LeaseTargetInfo, bool, error) {
			return ops.LeaseTargetInfo{}, true, platform.NewPlatformError(platform.ErrPrerequisiteMissing,
				"Lease "+alias+" was not "+what+": zcp could not build a Zerops API client to ask its pool's orchestrator (auth: no ZCP_API_KEY)", "")
		}, want: []string{"apppool-3", "no ZCP_API_KEY", mount}, notWant: []string{release, "SSHFS mount: lease"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fx := newLeaseRestoreFixture(t)
			check := tt.check
			if check == nil {
				check = orchestratorCheck(tt.orch)
			}

			out, mounts := fx.run(t, zcpinit.WithLeaseCheck(check))

			if got := len(mounts) == 1 && mounts[0] == "sshfs-apppool-3"; got != tt.mounted || len(mounts) > 1 {
				t.Errorf("lease mounts = %q, want mounted %v", mounts, tt.mounted)
			}
			if _, err := os.Stat(filepath.Join(fx.mountBase, "apppool-3")); !tt.mounted && !errors.Is(err, os.ErrNotExist) {
				t.Errorf("a mount dir was made for a lease that is not mounted: %v", err)
			}
			for _, w := range tt.want {
				if !strings.Contains(out, w) {
					t.Errorf("output lacks %q:\n%s", w, out)
				}
			}
			for _, w := range tt.notWant {
				if strings.Contains(out, w) {
					t.Errorf("output says %q:\n%s", w, out)
				}
			}
			if strings.Contains(out, bootLease) || strings.Contains(out, bootOrchToken) {
				t.Errorf("the lease or the pool's bearer reached the output:\n%s", out)
			}
			if tt.orch != nil {
				if got := len(tt.orch.renewed()); got != tt.renews {
					t.Errorf("renews = %d, want %d: the orchestrator is asked once per lease", got, tt.renews)
				}
				if changed := tt.orch.changes(); len(changed) != 0 {
					t.Errorf("the restore changed containers: %q", changed)
				}
			}
		})
	}
}

// TestInitSSHFS_NoLeaseCheck_NothingMountedNotFatal: a zcp init handed no way
// to ask a lease's orchestrator mounts no lease — nothing vouches for its
// container (DP-2) — keeps the aliases, prints why and the mount that asks
// again, and never fails.
func TestInitSSHFS_NoLeaseCheck_NothingMountedNotFatal(t *testing.T) {
	// Not parallel — mutates env, commandRunner, os.Stderr and the sshfs/lease package vars.
	fx := newLeaseRestoreFixture(t)

	out, mounts := fx.run(t)

	if len(mounts) != 0 {
		t.Errorf("lease mounts = %q, want none: nothing vouched for the container", mounts)
	}
	for _, w := range []string{"apppool-3", "orchestrator", "zerops_mount action=mount serviceHostname=apppool-3"} {
		if !strings.Contains(out, w) {
			t.Errorf("output lacks %q:\n%s", w, out)
		}
	}
}

// TestInitSSHFS_LeaseChecks_AskedAtOnce: zcp init asks every lease's
// orchestrator at once, not one lease after another: two checks that each
// answer only once the other is asked both answer, and both leases mount.
func TestInitSSHFS_LeaseChecks_AskedAtOnce(t *testing.T) {
	// Not parallel — mutates env, commandRunner, os.Stderr and the sshfs/lease package vars.
	fx := newLeaseRestoreFixture(t)
	writeLeaseRecordFile(t, fx.stateDir, "apppool", 4, "node-id-4.runtime.apppool.zerops")
	// A restore that asks one lease after another fails fast, not after the
	// boot's whole budget.
	zcpinit.SetLeaseCheckTimeout(2 * time.Second)
	t.Cleanup(zcpinit.ResetLeaseCheckTimeout)
	var arrived sync.WaitGroup
	arrived.Add(2)
	both := make(chan struct{})
	go func() { arrived.Wait(); close(both) }()
	rendezvous := func(ctx context.Context, stateDir, alias, what string) (ops.LeaseTargetInfo, bool, error) {
		arrived.Done()
		select {
		case <-both:
			return leaseCheckOurs(ctx, stateDir, alias, what)
		case <-ctx.Done():
			return ops.LeaseTargetInfo{}, true, fmt.Errorf("asked alone: %w", ctx.Err())
		}
	}

	_, mounts := fx.run(t, zcpinit.WithLeaseCheck(rendezvous))

	slices.Sort(mounts)
	if !slices.Equal(mounts, []string{"sshfs-apppool-3", "sshfs-apppool-4"}) {
		t.Errorf("lease mounts = %q, want both: the leases are asked at once", mounts)
	}
}

// TestInitSSHFS_LeaseCheckHangs_Bounded: an orchestrator that never answers
// holds the boot no longer than the restore's budget; its lease is not
// mounted and is printed as unverified, with the mount that asks again.
func TestInitSSHFS_LeaseCheckHangs_Bounded(t *testing.T) {
	// Not parallel — mutates env, commandRunner, os.Stderr and the sshfs/lease package vars.
	fx := newLeaseRestoreFixture(t)
	zcpinit.SetLeaseCheckTimeout(200 * time.Millisecond)
	t.Cleanup(zcpinit.ResetLeaseCheckTimeout)
	hang := func(ctx context.Context, _, _, _ string) (ops.LeaseTargetInfo, bool, error) {
		<-ctx.Done()
		return ops.LeaseTargetInfo{}, true, fmt.Errorf("ask the orchestrator: %w", ctx.Err())
	}

	start := time.Now()
	out, mounts := fx.run(t, zcpinit.WithLeaseCheck(hang))
	took := time.Since(start)

	if took > 2*time.Second {
		t.Errorf("the restore took %s: a check that never answers must be bounded by the budget", took)
	}
	if len(mounts) != 0 {
		t.Errorf("lease mounts = %q, want none", mounts)
	}
	for _, w := range []string{"apppool-3", "deadline exceeded", "zerops_mount action=mount serviceHostname=apppool-3"} {
		if !strings.Contains(out, w) {
			t.Errorf("output lacks %q:\n%s", w, out)
		}
	}
}

// TestRunSSHFS_NameNoServiceCanHave_NeverReachesTheUnitCommand: the unit
// command zsc runs is composed from the hostname, so a ZCP_SSHFS_HOSTNAMES
// entry no platform service can have — one carrying shell syntax — creates no
// unit and no directory; it is named and skipped, the valid names around it
// are mounted, and the command never fails (it gates the container start).
func TestRunSSHFS_NameNoServiceCanHave_NeverReachesTheUnitCommand(t *testing.T) {
	// Not parallel — mutates env and commandRunner.
	mountBase := t.TempDir()
	zcpinit.SetSSHFSMountBase(mountBase)
	t.Cleanup(func() { zcpinit.ResetSSHFSMountBase() })
	t.Setenv("ZCP_SSHFS_HOSTNAMES", "workerdev,app;touch /tmp/pwned,$(id),../etc,apidev")

	var executed []string
	zcpinit.SetCommandRunner(func(name string, args ...string) error {
		executed = append(executed, strings.Join(append([]string{name}, args...), " "))
		return nil
	})
	t.Cleanup(func() { zcpinit.ResetCommandRunner() })

	if err := zcpinit.RunSSHFS(); err != nil {
		t.Fatalf("RunSSHFS() error: %v", err)
	}
	if len(executed) != 2 || !strings.Contains(executed[0], "sshfs-workerdev ") || !strings.Contains(executed[1], "sshfs-apidev ") {
		t.Fatalf("commands = %q, want the units of workerdev and apidev only", executed)
	}
	for _, cmd := range executed {
		for _, bad := range []string{";", "$(", "..", "pwned"} {
			if strings.Contains(cmd, bad) {
				t.Errorf("command %q carries %q from a name no service can have", cmd, bad)
			}
		}
	}
	entries, err := os.ReadDir(mountBase)
	if err != nil {
		t.Fatal(err)
	}
	var dirs []string
	for _, e := range entries {
		dirs = append(dirs, e.Name())
	}
	if !slices.Equal(dirs, []string{"apidev", "workerdev"}) {
		t.Errorf("mount dirs = %q, want apidev and workerdev only", dirs)
	}
}
