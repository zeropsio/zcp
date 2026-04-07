package init

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const defaultSSHFSMountBase = "/var/www"

// sshfsMountBase is the base directory for SSHFS mounts.
// Tests override this to avoid writing to /var/www.
var sshfsMountBase = defaultSSHFSMountBase

// RunSSHFS reads ZCP_SSHFS_HOSTNAMES env (comma-separated) and creates
// SSHFS mounts for each hostname via zsc unit create.
// Skips gracefully if the env var is not set.
func RunSSHFS() error {
	raw := os.Getenv("ZCP_SSHFS_HOSTNAMES")
	if raw == "" {
		fmt.Fprintln(os.Stderr, "  (skipped — ZCP_SSHFS_HOSTNAMES not set)")
		return nil
	}

	for hostname := range strings.SplitSeq(raw, ",") {
		hostname = strings.TrimSpace(hostname)
		if hostname == "" {
			continue
		}
		fmt.Fprintf(os.Stderr, "  → SSHFS mount: %s\n", hostname)
		if err := mountSSHFS(hostname); err != nil {
			return fmt.Errorf("sshfs mount %s: %w", hostname, err)
		}
	}
	fmt.Fprintln(os.Stderr, "  ✓ SSHFS init complete")
	return nil
}

// mountSSHFS creates a directory and a zsc unit for one SSHFS mount.
func mountSSHFS(hostname string) error {
	mountPath := filepath.Join(sshfsMountBase, hostname)
	if err := os.MkdirAll(mountPath, 0755); err != nil {
		return fmt.Errorf("mkdir %s: %w", mountPath, err)
	}

	unitName := "sshfs-" + hostname
	sshfsCmd := fmt.Sprintf(
		"sshfs -f -o StrictHostKeyChecking=no,ServerAliveInterval=15,ServerAliveCountMax=3 %s:/var/www %s",
		hostname, mountPath,
	)
	return commandRunner("sudo", "-E", "zsc", "unit", "create", unitName, sshfsCmd)
}
