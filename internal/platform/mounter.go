package platform

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Command timeout constants.
const (
	mountCheckTimeout  = 10 * time.Second
	mountCreateTimeout = 30 * time.Second
	unmountTimeout     = 10 * time.Second
)

// SSHFSMountOptions are the sshfs options of a mount of a container's
// /var/www: reconnect after a dropped connection, keepalives, and symlinks
// rewritten to stay inside the mount. `zcp init` restores a lease mount with
// the same options acquire used.
const SSHFSMountOptions = "reconnect,StrictHostKeyChecking=no,ServerAliveInterval=15,ServerAliveCountMax=3,transform_symlinks,no_check_root"

// SystemMounter implements ops.Mounter using real system commands.
// Only works on Zerops containers where sshfs, zsc, and /proc/mounts are available.
type SystemMounter struct {
	// run, ready and mounted are test seams; nil means the real command
	// runner, the real /proc/mounts readiness wait and the real /proc/mounts
	// read.
	run     func(ctx context.Context, timeout time.Duration, name string, args ...string) error
	ready   func(ctx context.Context, path string) error
	mounted func(path string) (bool, error)

	probeMu      sync.Mutex
	probes       map[string]*mountProbe
	probeCommand func(name string, args ...string) *exec.Cmd
}

// isMounted reports whether path still has an sshfs mount.
func (m *SystemMounter) isMounted(path string) (bool, error) {
	if m.mounted != nil {
		return m.mounted(path)
	}
	return isSshfsMount(path)
}

// mountUnit is the systemd unit `zsc unit create sshfs-<name>` makes. It runs
// sshfs in the foreground and restarts it when it exits, so a fusermount alone
// is undone within seconds (live-verified 2026-10-24); removing the unit is
// what ends a mount.
func mountUnit(hostname string) string { return "zerops@sshfs-" + hostname + ".service" }

// resetFailed clears the failed state zsc unit remove leaves on the unit's
// instance (live 2026-10-24: every unmount left zerops@sshfs-<name>.service
// listed failed). Best-effort: an instance that is not failed has nothing to
// clear.
func (m *SystemMounter) resetFailed(ctx context.Context, hostname string) {
	_ = m.exec(ctx, unmountTimeout, "sudo", "systemctl", "reset-failed", mountUnit(hostname))
}

// NewSystemMounter creates a new SystemMounter.
func NewSystemMounter() *SystemMounter {
	return &SystemMounter{}
}

// exec runs one command with a timeout through the mounter's runner.
func (m *SystemMounter) exec(ctx context.Context, timeout time.Duration, name string, args ...string) error {
	if m.run != nil {
		return m.run(ctx, timeout, name, args...)
	}
	return execWithTimeout(ctx, timeout, name, args...)
}

// CheckMount checks the mount state of a path: active, stale, or not mounted.
// Uses /proc/mounts (kernel-authoritative) instead of mountpoint(1), which
// returns exit 32 for ALL directories in LXC/Incus containers.
func (m *SystemMounter) CheckMount(ctx context.Context, path string) (MountState, error) {
	mounted, err := m.isMounted(path)
	if err != nil {
		return MountStateNotMounted, fmt.Errorf("proc mounts check: %w", err)
	}
	if !mounted {
		return MountStateNotMounted, nil
	}
	if err := m.observePath(ctx, path, false); err != nil {
		// Only an observed disconnected/stale endpoint authorizes stale cleanup.
		// Permission errors, interrupted probes and expired waits are unknown.
		var exit *exec.ExitError
		if errors.As(err, &exit) && (strings.Contains(err.Error(), "Transport endpoint is not connected") || strings.Contains(err.Error(), "Stale file handle")) {
			return MountStateStale, nil
		}
		return MountStateNotMounted, fmt.Errorf("mount usability: %w", err)
	}
	return MountStateActive, nil
}

// isSshfsMount reads /proc/mounts and returns true if path has a fuse.sshfs entry.
func isSshfsMount(path string) (bool, error) {
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return false, fmt.Errorf("open /proc/mounts: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 3 {
			continue
		}
		// fields[1] = mount point, fields[2] = filesystem type
		if fields[1] == path && fields[2] == "fuse.sshfs" {
			return true, nil
		}
	}
	return false, scanner.Err()
}

// Mount creates an SSHFS mount of a service hostname or a dev-pool lease alias via zsc systemd unit and waits for readiness.
// Returns only after the mount is fully ready for I/O: appears in /proc/mounts,
// a bounded path read succeeds, AND a write probe confirms the SSH channel is operational.
// Blocks up to mountCreateTimeout.
func (m *SystemMounter) Mount(ctx context.Context, hostname, localPath string) error {
	if err := ValidateMountName(hostname); err != nil {
		return err
	}

	if err := os.MkdirAll(localPath, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", localPath, err)
	}

	unitName := "sshfs-" + hostname
	sshfsCmd := fmt.Sprintf(
		"sshfs -f -o %s %s:/var/www %s",
		SSHFSMountOptions, hostname, localPath,
	)

	if err := m.exec(ctx, mountCreateTimeout, "sudo", "-E", "zsc", "unit", "create", unitName, sshfsCmd); err != nil {
		return fmt.Errorf("zsc unit create: %w", err)
	}

	// Wait for mount to appear in /proc/mounts and become accessible.
	// zsc unit create returns immediately; the SSHFS connection establishes async.
	waitReady := m.waitForReady
	if m.ready != nil {
		waitReady = m.ready
	}
	if err := waitReady(ctx, localPath); err != nil {
		return fmt.Errorf("mount readiness: %w", err)
	}
	return nil
}

// waitForReady polls until the SSHFS mount is fully ready for I/O.
// Ready means: appears in /proc/mounts, a bounded path read succeeds, AND a write probe
// confirms the SSH channel is operational. Polls every 500ms up to mountCreateTimeout.
func (m *SystemMounter) waitForReady(ctx context.Context, path string) error {
	ctx, cancel := context.WithTimeout(ctx, mountCreateTimeout)
	defer cancel()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		state, err := m.CheckMount(ctx, path)
		if err == nil && state == MountStateActive {
			if writable, _ := m.IsWritable(ctx, path); writable {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("mount readiness observation: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

// Unmount removes the SSHFS mount and zsc unit.
// Order: zsc unit remove (its sshfs exits and the mount usually goes with
// it), then — only while the mount is still there — fusermount -u, falling
// back to fusermount -uz. A unit that cannot be removed fails the unmount
// before any fusermount, since the unit would restart sshfs after it.
// This prevents partial failures where the unit is removed but FUSE remains.
func (m *SystemMounter) Unmount(ctx context.Context, hostname, path string) error {
	if err := ValidateMountName(hostname); err != nil {
		return err
	}
	if err := m.exec(ctx, unmountTimeout, "sudo", "-E", "zsc", "unit", "remove", "sshfs-"+hostname); err != nil {
		return fmt.Errorf("zsc unit remove: %w", err)
	}
	m.resetFailed(ctx, hostname)
	if still, err := m.isMounted(path); err == nil && !still {
		return nil
	}
	if err := m.exec(ctx, unmountTimeout, "fusermount", "-u", path); err != nil {
		if fallbackErr := m.exec(ctx, unmountTimeout, "fusermount", "-uz", path); fallbackErr != nil {
			return fmt.Errorf("fusermount: %w (lazy fallback: %w)", err, fallbackErr)
		}
	}
	return nil
}

// ForceUnmount removes the zsc unit, then performs a lazy unmount. Used for
// stale mounts where the transport endpoint is disconnected. A removal that
// fails is ignored only once no unit is left (an orphan mount may have none):
// a unit that stays restarts sshfs after the lazy unmount, so it fails the
// unmount before any fusermount, as Unmount does.
func (m *SystemMounter) ForceUnmount(ctx context.Context, hostname, path string) error {
	if ValidateMountName(hostname) == nil {
		removeErr := m.exec(ctx, unmountTimeout, "sudo", "-E", "zsc", "unit", "remove", "sshfs-"+hostname)
		m.resetFailed(ctx, hostname)
		if removeErr != nil && m.unitLeft(ctx, hostname) {
			return fmt.Errorf("zsc unit remove: %w", removeErr)
		}
	}
	if still, err := m.isMounted(path); err == nil && !still {
		return nil
	}
	if err := m.exec(ctx, unmountTimeout, "fusermount", "-uz", path); err != nil {
		return fmt.Errorf("fusermount lazy unmount: %w", err)
	}
	return nil
}

// IsWritable checks if the mount point is writable by creating and removing a temp file.
func (m *SystemMounter) IsWritable(ctx context.Context, path string) (bool, error) {
	if err := m.observePath(ctx, path, true); err != nil {
		return false, fmt.Errorf("writable check: %w", err)
	}
	return true, nil
}

// ListMountDirs returns directory names under basePath.
// Used to detect orphan mount directories from deleted services.
func (m *SystemMounter) ListMountDirs(_ context.Context, basePath string) ([]string, error) {
	entries, err := os.ReadDir(basePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read dir %s: %w", basePath, err)
	}
	dirs := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	return dirs, nil
}

// HasUnit checks if a systemd unit exists for the given hostname.
// Uses "systemctl cat" which does not require sudo. A failed instance with
// no unit file is no unit: an older zcp's zsc unit remove left one on every
// unmount, and status and unmount ask this of every unmounted service.
func (m *SystemMounter) HasUnit(ctx context.Context, hostname string) (bool, error) {
	if err := ValidateMountName(hostname); err != nil {
		return false, err
	}
	err := m.exec(ctx, mountCheckTimeout, "systemctl", "cat", mountUnit(hostname))
	if err == nil {
		return true, nil
	}
	if strings.Contains(err.Error(), "No files found for "+mountUnit(hostname)) {
		return false, nil
	}
	return false, fmt.Errorf("read mount unit: %w", err)
}

// unitLeft reports whether a removal left the unit: its file, or — zsc unit
// remove deletes the file, but a failed instance stays listed until
// reset-failed — its failed instance. Only a cleanup asks it, after its
// removal and reset.
func (m *SystemMounter) unitLeft(ctx context.Context, hostname string) bool {
	if has, err := m.HasUnit(ctx, hostname); has || err != nil {
		// An unanswered unit read cannot prove removal succeeded.
		return true
	}
	return m.exec(ctx, mountCheckTimeout, "systemctl", "is-failed", "--quiet", mountUnit(hostname)) == nil
}

// CleanupUnit removes the zsc systemd unit without touching the FUSE mount.
// Used to clean up orphan units where no FUSE mount was established.
func (m *SystemMounter) CleanupUnit(ctx context.Context, hostname string) error {
	if err := ValidateMountName(hostname); err != nil {
		return err
	}
	removeErr := m.exec(ctx, unmountTimeout, "sudo", "-E", "zsc", "unit", "remove", "sshfs-"+hostname)
	m.resetFailed(ctx, hostname)
	if removeErr == nil {
		return nil
	}
	if m.unitLeft(ctx, hostname) {
		return removeErr
	}
	return nil
}

// execWithTimeout runs a command with a timeout derived from the parent
// context. On failure the combined stdout+stderr is folded into the error so
// MOUNT_FAILED carries the actual diagnosis (zsc / fusermount / systemctl
// stderr) instead of a bare "exit status 1" — mirroring the SSH deployer's
// SSHExecError.Output discipline.
func execWithTimeout(ctx context.Context, timeout time.Duration, name string, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		if trimmed := strings.TrimSpace(string(out)); trimmed != "" {
			return fmt.Errorf("%w: %s", err, trimmed)
		}
		return err
	}
	return nil
}

// RunCommand runs one command by argv (no shell) with the mount command
// timeout, folding its output into the error.
func RunCommand(ctx context.Context, name string, args ...string) error {
	return execWithTimeout(ctx, mountCreateTimeout, name, args...)
}
