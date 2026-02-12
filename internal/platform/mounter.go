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

// IsMounted checks if a path is an active mount point.
func (m *SystemMounter) IsMounted(ctx context.Context, path string) (bool, error) {
	err := exec.CommandContext(ctx, "mountpoint", "-q", path).Run()
	if err == nil {
		return true, nil
	}
	// Exit code 1 means not a mount point (expected).
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	// mountpoint command not found — try fallback.
	if isExecNotFound(err) {
		return m.isMountedFallback(ctx, path)
	}
	return false, fmt.Errorf("mountpoint check: %w", err)
}

func (m *SystemMounter) isMountedFallback(ctx context.Context, path string) (bool, error) {
	out, err := exec.CommandContext(ctx, "mount").Output()
	if err != nil {
		return false, fmt.Errorf("mount list: %w", err)
	}
	return strings.Contains(string(out), path), nil
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

	err := exec.CommandContext(ctx, "sudo", "-E", "zsc", "unit", "create", unitName, sshfsCmd).Run() //nolint:gosec // args validated by safeHostname
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
