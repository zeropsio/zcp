package platform

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Command timeout constants.
const (
	mountCheckTimeout  = 10 * time.Second
	mountCreateTimeout = 30 * time.Second
	unmountTimeout     = 10 * time.Second
)

// SystemMounter implements ops.Mounter using real system commands.
// Only works on Zerops containers where sshfs, zsc, and /proc/mounts are available.
type SystemMounter struct{}

// NewSystemMounter creates a new SystemMounter.
func NewSystemMounter() *SystemMounter {
	return &SystemMounter{}
}

// CheckMount checks the mount state of a path: active, stale, or not mounted.
// Uses /proc/mounts (kernel-authoritative) instead of mountpoint(1), which
// returns exit 32 for ALL directories in LXC/Incus containers.
func (m *SystemMounter) CheckMount(_ context.Context, path string) (MountState, error) {
	mounted, err := isSshfsMount(path)
	if err != nil {
		return MountStateNotMounted, fmt.Errorf("proc mounts check: %w", err)
	}
	if !mounted {
		return MountStateNotMounted, nil
	}
	// SSHFS entry exists — probe to distinguish active vs stale.
	_, err = os.Stat(path)
	if err == nil {
		return MountStateActive, nil
	}
	return MountStateStale, nil
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

// Mount creates an SSHFS mount via zsc systemd unit and waits for readiness.
// Returns only after the mount is fully ready for I/O: appears in /proc/mounts,
// os.Stat succeeds, AND a write probe confirms the SSH channel is operational.
// Blocks up to mountCreateTimeout.
func (m *SystemMounter) Mount(ctx context.Context, hostname, localPath string) error {
	if err := ValidateHostname(hostname); err != nil {
		return err
	}

	if err := os.MkdirAll(localPath, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", localPath, err)
	}

	unitName := "sshfs-" + hostname
	sshfsCmd := fmt.Sprintf(
		"sshfs -f -o reconnect,StrictHostKeyChecking=no,ServerAliveInterval=15,ServerAliveCountMax=3,transform_symlinks,no_check_root %s:/var/www %s",
		hostname, localPath,
	)

	if err := execWithTimeout(ctx, mountCreateTimeout, "sudo", "-E", "zsc", "unit", "create", unitName, sshfsCmd); err != nil {
		return fmt.Errorf("zsc unit create: %w", err)
	}

	// Wait for mount to appear in /proc/mounts and become accessible.
	// zsc unit create returns immediately; the SSHFS connection establishes async.
	if err := m.waitForReady(ctx, localPath); err != nil {
		return fmt.Errorf("mount readiness: %w", err)
	}
	return nil
}

// waitForReady polls until the SSHFS mount is fully ready for I/O.
// Ready means: appears in /proc/mounts, os.Stat succeeds, AND a write probe
// confirms the SSH channel is operational. Polls every 500ms up to mountCreateTimeout.
func (m *SystemMounter) waitForReady(ctx context.Context, path string) error {
	deadline := time.Now().Add(mountCreateTimeout)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		state, err := m.CheckMount(ctx, path)
		if err == nil && state == MountStateActive && writeProbe(path) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("mount at %s not ready after %v", path, mountCreateTimeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// writeProbe verifies a path is writable using Go stdlib (no shell).
// Creates a unique temp file, closes it, and removes it. Returns true only if
// all three operations succeed — a failed Close indicates a broken transport.
func writeProbe(path string) bool {
	f, err := os.CreateTemp(path, ".mount_probe_*")
	if err != nil {
		return false
	}
	name := f.Name()
	if err := f.Close(); err != nil {
		os.Remove(name)
		return false
	}
	os.Remove(name)
	return true
}

// Unmount removes the SSHFS mount and zsc unit.
// Order: fusermount -u → fallback fusermount -uz → zsc unit remove.
// This prevents partial failures where the unit is removed but FUSE remains.
func (m *SystemMounter) Unmount(ctx context.Context, hostname, path string) error {
	if err := ValidateHostname(hostname); err != nil {
		return err
	}

	// Unmount FUSE first; fallback to lazy unmount on failure.
	if err := execWithTimeout(ctx, unmountTimeout, "fusermount", "-u", path); err != nil {
		if fallbackErr := execWithTimeout(ctx, unmountTimeout, "fusermount", "-uz", path); fallbackErr != nil {
			return fmt.Errorf("fusermount: %w (lazy fallback: %w)", err, fallbackErr)
		}
	}

	// Then remove the systemd unit.
	unitName := "sshfs-" + hostname
	if err := execWithTimeout(ctx, unmountTimeout, "sudo", "-E", "zsc", "unit", "remove", unitName); err != nil {
		return fmt.Errorf("zsc unit remove: %w", err)
	}
	return nil
}

// ForceUnmount performs a lazy unmount and best-effort zsc unit cleanup.
// Used for stale mounts where the transport endpoint is disconnected.
func (m *SystemMounter) ForceUnmount(ctx context.Context, hostname, path string) error {
	// Best-effort: remove zsc unit if it exists (ignore errors — unit may not exist for orphan mounts).
	if ValidateHostname(hostname) == nil {
		unitName := "sshfs-" + hostname
		_ = execWithTimeout(ctx, unmountTimeout, "sudo", "-E", "zsc", "unit", "remove", unitName)
	}
	if err := execWithTimeout(ctx, unmountTimeout, "fusermount", "-uz", path); err != nil {
		return fmt.Errorf("fusermount lazy unmount: %w", err)
	}
	return nil
}

// IsWritable checks if the mount point is writable by creating and removing a temp file.
func (m *SystemMounter) IsWritable(_ context.Context, path string) (bool, error) {
	if !writeProbe(path) {
		return false, fmt.Errorf("writable check: cannot write to %s", path)
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
// Uses "systemctl cat" which does not require sudo.
func (m *SystemMounter) HasUnit(ctx context.Context, hostname string) (bool, error) {
	if err := ValidateHostname(hostname); err != nil {
		return false, err
	}
	unitName := "zerops@sshfs-" + hostname + ".service"
	err := execWithTimeout(ctx, mountCheckTimeout, "systemctl", "cat", unitName)
	return err == nil, nil
}

// CleanupUnit removes the zsc systemd unit without touching the FUSE mount.
// Used to clean up orphan units where no FUSE mount was established.
func (m *SystemMounter) CleanupUnit(ctx context.Context, hostname string) error {
	if err := ValidateHostname(hostname); err != nil {
		return err
	}
	return execWithTimeout(ctx, unmountTimeout, "sudo", "-E", "zsc", "unit", "remove", "sshfs-"+hostname)
}

// execWithTimeout runs a command with a timeout derived from the parent context.
func execWithTimeout(ctx context.Context, timeout time.Duration, name string, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).Run()
}
