package init

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
)

const defaultSSHFSMountBase = "/var/www"

// sshfsMountBase is the base directory for SSHFS mounts.
// Tests override this to avoid writing to /var/www.
var sshfsMountBase = defaultSSHFSMountBase

// leaseStateDir resolves the directory holding the lease records
// (.zcp/state under the working directory, as the server does); tests
// override it. leaseAliasFile is where the lease aliases are installed.
var (
	leaseStateDir  = defaultLeaseStateDir
	leaseAliasFile = ops.DefaultLeaseAliasFile
)

func defaultLeaseStateDir() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return filepath.Join(cwd, ".zcp", "state")
}

// RunSSHFS reads ZCP_SSHFS_HOSTNAMES env (comma-separated) and creates
// SSHFS mounts for each hostname via zsc unit create.
// Skips gracefully if the env var is not set.
//
// A per-hostname failure never fails the command: `zcp init sshfs` is a
// run.init command whose exit code gates the container start, and one
// unmountable service (stopped, deleted) must not keep the control plane down.
func RunSSHFS() error {
	raw := os.Getenv("ZCP_SSHFS_HOSTNAMES")
	if raw == "" {
		fmt.Fprintln(os.Stderr, "  (no service mounts — ZCP_SSHFS_HOSTNAMES not set)")
	}

	for hostname := range strings.SplitSeq(raw, ",") {
		hostname = strings.TrimSpace(hostname)
		if hostname == "" {
			continue
		}
		fmt.Fprintf(os.Stderr, "  → SSHFS mount: %s\n", hostname)
		if err := mountSSHFS(hostname, serviceMountOptions); err != nil {
			fmt.Fprintf(os.Stderr, "  ! sshfs mount %s: %v\n    (continuing — /var/www/%s stays unmounted)\n", hostname, err, hostname)
		}
	}
	restoreLeases()
	fmt.Fprintln(os.Stderr, "  ✓ SSHFS init complete")
	return nil
}

// restoreLeaseStep is restoreLeases as a `zcp init` step; it prints what it
// could not restore and never fails.
func restoreLeaseStep(string, runtime.Info) error {
	restoreLeases()
	return nil
}

// restoreLeases re-renders the lease ssh aliases from the lease records and
// remounts every lease, after the service mounts: a redeploy replaces the
// container, so its /etc and its mounts are gone while the records (and the
// orchestrator's reservations) live on. Nothing here fails the command (DP-2):
// a lease that cannot be restored is printed and skipped, and `zerops_lease
// action=status` then reports it unmounted. A parked lease keeps its alias and
// is not mounted (DP-28): its container is stopped, and its unpark mounts it.
func restoreLeases() {
	stateDir := leaseStateDir()
	if stateDir == "" {
		return
	}
	aliases, parked, err := ops.LeaseMounts(stateDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  ! lease records: %v\n    (continuing — leases stay unmounted)\n", err)
		return
	}
	if len(aliases) == 0 && len(parked) == 0 {
		return
	}
	run := func(_ context.Context, name string, args ...string) error { return commandRunner(name, args...) }
	if err := ops.InstallLeaseAliases(context.Background(), run, stateDir, leaseAliasFile); err != nil {
		// A mount whose alias does not resolve cannot work: skip them all.
		fmt.Fprintf(os.Stderr, "  ! lease aliases: %v\n    (continuing — leases stay unmounted)\n", err)
		return
	}
	for _, alias := range parked {
		fmt.Fprintf(os.Stderr, "  → lease %s is parked: not mounted (%s starts and mounts it)\n", alias, ops.LeaseUnparkCall(alias))
	}
	for _, alias := range aliases {
		fmt.Fprintf(os.Stderr, "  → SSHFS mount: lease %s\n", alias)
		if err := mountSSHFS(alias, platform.SSHFSMountOptions); err != nil {
			fmt.Fprintf(os.Stderr, "  ! sshfs mount lease %s: %v\n    (continuing — /var/www/%s stays unmounted)\n", alias, err, alias)
		}
	}
}

// serviceMountOptions are the sshfs options of a service mount made at boot
// (ZCP_SSHFS_HOSTNAMES); a lease mount takes platform.SSHFSMountOptions, the
// options acquire used.
const serviceMountOptions = "StrictHostKeyChecking=no,ServerAliveInterval=15,ServerAliveCountMax=3"

// mountSSHFS creates a directory and a zsc unit for one SSHFS mount.
//
// A mount left behind by a service that is now stopped answers stat with
// "transport endpoint is not connected", so MkdirAll fails with "file exists".
// That mount point is lazily unmounted and the mkdir retried once. sshfs runs
// as root under `sudo zsc`, so the unmount needs sudo too.
func mountSSHFS(hostname, options string) error {
	mountPath := filepath.Join(sshfsMountBase, hostname)
	if err := os.MkdirAll(mountPath, 0755); err != nil {
		if umountErr := commandRunner("sudo", "umount", "-l", mountPath); umountErr != nil {
			return fmt.Errorf("mkdir %s: %w (lazy unmount: %w)", mountPath, err, umountErr)
		}
		if err := os.MkdirAll(mountPath, 0755); err != nil {
			return fmt.Errorf("mkdir %s after lazy unmount: %w", mountPath, err)
		}
	}

	unitName := "sshfs-" + hostname
	sshfsCmd := fmt.Sprintf(
		"sshfs -f -o %s %s:/var/www %s",
		options, hostname, mountPath,
	)
	return commandRunner("sudo", "-E", "zsc", "unit", "create", unitName, sshfsCmd)
}
