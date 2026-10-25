package ops

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
)

// DefaultLeaseAliasFile is the root-read ssh config zcp keeps the lease
// aliases in. Ubuntu's /etc/ssh/ssh_config includes ssh_config.d/*.conf, so
// root's sshfs resolves the same aliases as the zerops user (an alias in
// ~zerops/.ssh/config is invisible to root).
const DefaultLeaseAliasFile = "/etc/ssh/ssh_config.d/zcp-leases.conf"

// leaseAliasFileHeader opens the managed file; a file with no leases is just
// this comment, which is a valid ssh config.
const leaseAliasFileHeader = "# Managed by zcp: dev-pool lease aliases, rewritten from .zcp/state/leases. Do not edit.\n"

// CommandRunner runs one command by argv (never through a shell).
type CommandRunner func(ctx context.Context, name string, args ...string) error

// LeaseHost is the host side of a lease: the ssh alias file and the sshfs
// mount. The zero value does neither (zcp outside a container, where there is
// no sshfs and no sudo), which leaves a lease held but unaliased and unmounted.
type LeaseHost struct {
	// Mounter mounts and checks /var/www/<alias>; nil means no mounts.
	Mounter Mounter
	// Run runs the sudo calls that install the alias file; nil means no alias
	// file.
	Run CommandRunner
	// AliasFile overrides DefaultLeaseAliasFile (tests).
	AliasFile string
}

// NewLeaseHost is the host side inside a container: sudo through argv, and the
// given mounter.
func NewLeaseHost(m Mounter) LeaseHost {
	return LeaseHost{Mounter: m, Run: platform.RunCommand}
}

func (h LeaseHost) aliasFile() string {
	if h.AliasFile != "" {
		return h.AliasFile
	}
	return DefaultLeaseAliasFile
}

// renderLeaseAliases renders one stanza per record, sorted by alias. The file
// is root-read ssh config, so every alias and hostname is checked against the
// strict shapes first and one bad record refuses the whole render: nothing is
// returned for a partial set. The error never echoes the offending hostname.
func renderLeaseAliases(records []leaseRecord) (string, error) {
	sorted := slices.Clone(records)
	slices.SortFunc(sorted, func(a, b leaseRecord) int { return strings.Compare(a.alias(), b.alias()) })
	var b strings.Builder
	b.WriteString(leaseAliasFileHeader)
	for _, r := range sorted {
		if platform.ValidateLeaseAlias(r.alias()) != nil {
			return "", fmt.Errorf("lease alias %q is not <pool>-<number>", r.alias())
		}
		if platform.ValidateContainerHostname(r.ContainerHostname) != nil {
			return "", fmt.Errorf("lease %s: its container hostname is not node-id-<n>.runtime.<pool>.zerops", r.alias())
		}
		b.WriteString("\nHost " + r.alias() + "\n")
		b.WriteString("    HostName " + r.ContainerHostname + "\n")
		// The options of zcp's own ssh config template. %i keys the control
		// socket by local uid: root's sshfs and the zerops user must not share
		// one socket path (the socket is owned by whoever opened it).
		b.WriteString("    User zerops\n")
		b.WriteString("    StrictHostKeyChecking no\n")
		b.WriteString("    UserKnownHostsFile /dev/null\n")
		b.WriteString("    LogLevel ERROR\n")
		b.WriteString("    ControlMaster auto\n")
		b.WriteString("    ControlPath /tmp/ssh-mux-%i-%r@%h:%p\n")
		b.WriteString("    ControlPersist 600\n")
	}
	return b.String(), nil
}

// leaseAliasStagingSuffix names the file the alias file is installed to before
// it is renamed into place: `install` creates its target before it sets the
// mode, so a file ssh reads must never be its target, and <dest>.new is not
// matched by ssh's ssh_config.d/*.conf include.
const leaseAliasStagingSuffix = ".new"

// InstallLeaseAliases renders the alias file from the lease records in
// stateDir and installs it at dest with argv sudo calls: `sudo -n install` to
// a staging file beside dest, then `sudo -n mv -f` over dest, a rename within
// one directory, so a reader (any ssh, through its include) sees the old file
// or the new one, never one still being made. The content is written to a
// private file in the lease directory first (sudo is handed paths, never
// content), and only after every stanza passed both validators. With no
// records the file is the bare header, which removes every alias.
func InstallLeaseAliases(ctx context.Context, run CommandRunner, stateDir, dest string) error {
	if run == nil {
		return errors.New("no command runner")
	}
	// The records are read under the lock, so the install that lands last
	// rendered every record written before it.
	unlock, err := acquireLeaseAliasLock(stateDir)
	if err != nil {
		return err
	}
	defer unlock()
	records, err := readLeaseRecords(stateDir)
	if err != nil {
		return err
	}
	content, err := renderLeaseAliases(records)
	if err != nil {
		return fmt.Errorf("render lease aliases: %w", err)
	}
	dir := leaseDir(stateDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".aliases-*.conf")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	defer os.Remove(tmp.Name()) // best effort: sudo copied it already
	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", tmp.Name(), err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmp.Name(), err)
	}
	staging := dest + leaseAliasStagingSuffix
	if err := run(ctx, "sudo", "-n", "install", "-m", "0644", "-o", "root", "-g", "root", tmp.Name(), staging); err != nil {
		return fmt.Errorf("install %s: %w", staging, err)
	}
	if err := run(ctx, "sudo", "-n", "mv", "-f", staging, dest); err != nil {
		_ = run(ctx, "sudo", "-n", "rm", "-f", staging) // best effort: the include never reads it
		return fmt.Errorf("move %s over %s (it is unchanged): %w", staging, dest, err)
	}
	return nil
}

// LeaseMounts lists, sorted, the aliases of the lease records in stateDir
// that `zcp init` remounts after a redeploy, and apart the parked ones it
// leaves unmounted: a parked lease's container is stopped, and a mount of it
// would hang whatever reads it (DP-28).
func LeaseMounts(stateDir string) (mount, parked []string, err error) {
	records, err := readLeaseRecords(stateDir)
	if err != nil {
		return nil, nil, err
	}
	for _, r := range records {
		if r.Parked {
			parked = append(parked, r.alias())
			continue
		}
		mount = append(mount, r.alias())
	}
	slices.Sort(mount)
	slices.Sort(parked)
	return mount, parked, nil
}

// aliasInstalled reports whether the alias file holds a stanza for alias.
func (h LeaseHost) aliasInstalled(alias string) bool {
	f, err := os.Open(h.aliasFile())
	if err != nil {
		return false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if strings.TrimSpace(sc.Text()) == "Host "+alias {
			return true
		}
	}
	return false
}

// gap says what a held lease still lacks on this host ("" when nothing): its
// ssh alias, or its mount. It reads state and changes nothing.
func (h LeaseHost) gap(ctx context.Context, alias string) string {
	var missing []string
	if h.Run != nil && !h.aliasInstalled(alias) {
		missing = append(missing, "its ssh alias is not installed")
	}
	if h.Mounter != nil {
		if state, err := h.Mounter.CheckMount(ctx, filepath.Join(mountBase, alias)); err != nil || state != platform.MountStateActive {
			missing = append(missing, "it is not mounted at "+filepath.Join(mountBase, alias))
		}
	}
	if len(missing) == 0 {
		return ""
	}
	return "the lease is held but " + strings.Join(missing, " and ") + "; zcp init restores both after a redeploy"
}

// attach installs the alias file (which now includes rec) and mounts the
// lease. It returns a note when either step failed, "" when both worked or the
// host does neither.
func (h LeaseHost) attach(ctx context.Context, d LeaseDeps, rec leaseRecord) string {
	if h.Run != nil {
		if err := InstallLeaseAliases(ctx, h.Run, d.StateDir, h.aliasFile()); err != nil {
			return fmt.Sprintf("the lease is held but its ssh alias could not be installed: %v", err)
		}
	}
	return h.mountLease(ctx, rec)
}

// mountLease mounts the lease, whose alias is installed. It returns a note
// when the mount failed, "" when it worked or the host mounts nothing.
func (h LeaseHost) mountLease(ctx context.Context, rec leaseRecord) string {
	if h.Mounter == nil {
		return ""
	}
	if _, err := MountLease(ctx, h.Mounter, rec.alias()); err != nil {
		return fmt.Sprintf("the lease is held but %s could not be mounted: %v", filepath.Join(mountBase, rec.alias()), err)
	}
	return ""
}

// detachMount unmounts the lease before it is released. A failure is a note,
// not an error: the lease must still be released.
func (h LeaseHost) detachMount(ctx context.Context, d LeaseDeps, rec leaseRecord) string {
	if err := h.unmountLease(ctx, d, rec); err != nil {
		return fmt.Sprintf("the lease could not be unmounted first (%v); remove its mount unit with sudo zsc unit remove sshfs-%s (a fusermount alone is undone: the unit restarts sshfs), then fusermount -uz %s if it is still mounted", err, rec.alias(), filepath.Join(mountBase, rec.alias()))
	}
	return ""
}

// unmountLease unmounts the lease; nil when it is not mounted, or the host
// mounts nothing.
func (h LeaseHost) unmountLease(ctx context.Context, d LeaseDeps, rec leaseRecord) error {
	if h.Mounter == nil {
		return nil
	}
	if _, err := UnmountService(ctx, d.Client, d.ProjectID, h.Mounter, rec.alias()); err != nil {
		return fmt.Errorf("unmount %s: %w", filepath.Join(mountBase, rec.alias()), err)
	}
	return nil
}

// refreshAliases re-renders the alias file from the records that remain.
func (h LeaseHost) refreshAliases(ctx context.Context, d LeaseDeps) string {
	if h.Run == nil {
		return ""
	}
	if err := InstallLeaseAliases(ctx, h.Run, d.StateDir, h.aliasFile()); err != nil {
		return fmt.Sprintf("the ssh alias file could not be refreshed: %v", err)
	}
	return ""
}
