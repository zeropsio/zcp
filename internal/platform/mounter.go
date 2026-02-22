package platform

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var safeHostname = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,62}$`)

// Command timeout constants.
const (
	mountCheckTimeout  = 10 * time.Second
	mountCreateTimeout = 30 * time.Second
	unmountTimeout     = 10 * time.Second
)

// SystemMounter implements ops.Mounter using real system commands.
// Only works on Zerops containers where sshfs, zsc, and mountpoint are available.
type SystemMounter struct{}

// NewSystemMounter creates a new SystemMounter.
func NewSystemMounter() *SystemMounter {
	return &SystemMounter{}
}

// CheckMount checks the mount state of a path: active, stale, or not mounted.
func (m *SystemMounter) CheckMount(ctx context.Context, path string) (MountState, error) {
	err := execWithTimeout(ctx, mountCheckTimeout, "mountpoint", "-q", path)
	if err == nil {
		return MountStateActive, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		switch exitErr.ExitCode() {
		case 1:
			return MountStateNotMounted, nil
		case 32:
			return MountStateStale, nil
		}
	}
	// mountpoint command not found — try fallback.
	if isExecNotFound(err) {
		return m.checkMountFallback(ctx, path)
	}
	return MountStateNotMounted, fmt.Errorf("mountpoint check: %w", err)
}

func (m *SystemMounter) checkMountFallback(ctx context.Context, path string) (MountState, error) {
	out, err := outputWithTimeout(ctx, mountCheckTimeout, "mount")
	if err != nil {
		return MountStateNotMounted, fmt.Errorf("mount list: %w", err)
	}
	if strings.Contains(string(out), path) {
		return MountStateActive, nil
	}
	return MountStateNotMounted, nil
}

// Mount creates an SSHFS mount via zsc systemd unit.
func (m *SystemMounter) Mount(ctx context.Context, hostname, localPath string) error {
	if !safeHostname.MatchString(hostname) {
		return fmt.Errorf("unsafe hostname: %s", hostname)
	}

	if err := os.MkdirAll(localPath, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", localPath, err)
	}

	unitName := "sshfs-" + hostname
	sshfsCmd := fmt.Sprintf(
		"sshfs -f -o reconnect,StrictHostKeyChecking=no,ServerAliveInterval=15,ServerAliveCountMax=3 %s:/var/www %s",
		hostname, localPath,
	)

	if err := execWithTimeout(ctx, mountCreateTimeout, "sudo", "-E", "zsc", "unit", "create", unitName, sshfsCmd); err != nil {
		return fmt.Errorf("zsc unit create: %w", err)
	}
	return nil
}

// Unmount removes the SSHFS mount and zsc unit.
// Order: fusermount -u → fallback fusermount -uz → zsc unit remove.
// This prevents partial failures where the unit is removed but FUSE remains.
func (m *SystemMounter) Unmount(ctx context.Context, hostname, path string) error {
	if !safeHostname.MatchString(hostname) {
		return fmt.Errorf("unsafe hostname: %s", hostname)
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
	if safeHostname.MatchString(hostname) {
		unitName := "sshfs-" + hostname
		_ = execWithTimeout(ctx, unmountTimeout, "sudo", "-E", "zsc", "unit", "remove", unitName)
	}
	if err := execWithTimeout(ctx, unmountTimeout, "fusermount", "-uz", path); err != nil {
		return fmt.Errorf("fusermount lazy unmount: %w", err)
	}
	return nil
}

// IsWritable checks if the mount point is writable by creating and removing a test file.
func (m *SystemMounter) IsWritable(ctx context.Context, path string) (bool, error) {
	testFile := filepath.Join(path, ".mount_test")
	if err := execWithTimeout(ctx, mountCheckTimeout, "touch", testFile); err != nil {
		return false, fmt.Errorf("writable check: %w", err)
	}
	_ = execWithTimeout(ctx, mountCheckTimeout, "rm", "-f", testFile)
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

// execWithTimeout runs a command with a timeout derived from the parent context.
func execWithTimeout(ctx context.Context, timeout time.Duration, name string, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).Run()
}

// outputWithTimeout runs a command with a timeout and returns its stdout.
func outputWithTimeout(ctx context.Context, timeout time.Duration, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).Output()
}

func isExecNotFound(err error) bool {
	var execErr *exec.Error
	return errors.As(err, &execErr)
}
