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
)

var safeHostname = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,62}$`)

// SystemMounter implements ops.Mounter using real system commands.
// Only works on Zerops containers where sshfs, zsc, and mountpoint are available.
type SystemMounter struct{}

// NewSystemMounter creates a new SystemMounter.
func NewSystemMounter() *SystemMounter {
	return &SystemMounter{}
}

// CheckMount checks the mount state of a path: active, stale, or not mounted.
func (m *SystemMounter) CheckMount(ctx context.Context, path string) (MountState, error) {
	err := exec.CommandContext(ctx, "mountpoint", "-q", path).Run()
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
	out, err := exec.CommandContext(ctx, "mount").Output()
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

	err := exec.CommandContext(ctx, "sudo", "-E", "zsc", "unit", "create", unitName, sshfsCmd).Run()
	if err != nil {
		return fmt.Errorf("zsc unit create: %w", err)
	}
	return nil
}

// Unmount removes the SSHFS mount and zsc unit.
func (m *SystemMounter) Unmount(ctx context.Context, hostname, path string) error {
	if !safeHostname.MatchString(hostname) {
		return fmt.Errorf("unsafe hostname: %s", hostname)
	}

	unitName := "sshfs-" + hostname

	// Remove the systemd unit first.
	if err := exec.CommandContext(ctx, "sudo", "-E", "zsc", "unit", "remove", unitName).Run(); err != nil {
		return fmt.Errorf("zsc unit remove: %w", err)
	}

	// Then unmount the FUSE filesystem.
	if err := exec.CommandContext(ctx, "fusermount", "-u", path).Run(); err != nil {
		return fmt.Errorf("fusermount: %w", err)
	}
	return nil
}

// ForceUnmount performs a lazy unmount without requiring a zsc unit.
// Used for stale mounts where the transport endpoint is disconnected.
func (m *SystemMounter) ForceUnmount(ctx context.Context, path string) error {
	if err := exec.CommandContext(ctx, "fusermount", "-uz", path).Run(); err != nil {
		return fmt.Errorf("fusermount lazy unmount: %w", err)
	}
	return nil
}

// IsWritable checks if the mount point is writable by creating and removing a test file.
func (m *SystemMounter) IsWritable(ctx context.Context, path string) (bool, error) {
	testFile := filepath.Join(path, ".mount_test")
	err := exec.CommandContext(ctx, "touch", testFile).Run()
	if err != nil {
		return false, fmt.Errorf("writable check: %w", err)
	}
	_ = exec.CommandContext(ctx, "rm", "-f", testFile).Run()
	return true, nil
}

func isExecNotFound(err error) bool {
	var execErr *exec.Error
	return errors.As(err, &execErr)
}
