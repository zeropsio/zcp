package init

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

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
// SSHFS mounts for each hostname via zsc unit create, then restores the
// leases (restoreLeases) with the check WithLeaseCheck hands it.
// Skips gracefully if the env var is not set.
//
// A per-hostname failure never fails the command: `zcp init sshfs` is a
// run.init command whose exit code gates the container start, and one
// unmountable service (stopped, deleted) must not keep the control plane down.
func RunSSHFS(opts ...Option) error {
	o := buildOptions(opts)
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
	restoreLeases(o.leaseCheck)
	fmt.Fprintln(os.Stderr, "  ✓ SSHFS init complete")
	return nil
}

// LeaseCheck asks a lease's pool orchestrator, once, whether the container
// the lease alias names is still that lease's, reading the record from
// stateDir: ops.LeaseTargetChecked with its API client and orchestrators
// wired in (docs/spec-workflows.md §8 DP-2), what being what the caller would
// do with the container. The command layer builds it — init grows no API
// plumbing — and hands it in with WithLeaseCheck.
type LeaseCheck func(ctx context.Context, stateDir, alias, what string) (ops.LeaseTargetInfo, bool, error)

// WithLeaseCheck hands the lease restore the check it asks before it mounts a
// lease. Without one no lease is mounted: nothing vouches for its container.
func WithLeaseCheck(check LeaseCheck) Option {
	return func(o *options) { o.leaseCheck = check }
}

// defaultLeaseCheckTimeout bounds every lease check of one restore, together:
// they run at once, at container start, and an API or orchestrator that does
// not answer must not hold the boot. leaseCheckTimeout is it; tests shorten it.
const defaultLeaseCheckTimeout = 15 * time.Second

var leaseCheckTimeout = defaultLeaseCheckTimeout

// leaseMountWhat is what the restore does with a lease's container, as a
// lease check's refusal names it.
const leaseMountWhat = "mounted at boot"

// restoreLeaseStep is restoreLeases with the check WithLeaseCheck handed in,
// as a `zcp init` step; it prints what it could not restore and never fails.
func (o options) restoreLeaseStep(string, runtime.Info) error {
	restoreLeases(o.leaseCheck)
	return nil
}

// restoreLeases re-renders the lease ssh aliases from the lease records and
// remounts every lease its orchestrator vouches for, after the service
// mounts: a redeploy replaces the container, so its /etc and its mounts are
// gone while the records (and the orchestrator's reservations) live on. A
// lease may have ended outside zcp meanwhile and the pool handed its
// container to another holder, so before a lease is mounted check asks its
// orchestrator whether the container is still the lease's (DP-2) — every
// lease at once, within leaseCheckTimeout. Ours is mounted; one whose record
// says parked (not asked) or whose container the orchestrator answers stopped
// keeps its alias and is not mounted (DP-28): its unpark mounts it. A lost
// lease is not mounted and is printed with the release that drops its record;
// one the orchestrator does not answer about — or that init cannot ask, with
// no check or no API client — is not mounted and is printed with why and the
// mount that asks again. Nothing here fails the command: a lease that cannot
// be restored is printed and skipped, and `zerops_lease action=status` then
// reports it.
func restoreLeases(check LeaseCheck) {
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
		printParkedLease(alias)
	}
	for i, v := range askLeases(stateDir, aliases, check) {
		alias := aliases[i]
		switch {
		case v.err != nil && ops.IsLeaseLost(v.err):
			fmt.Fprintf(os.Stderr, "  ! %v\n    (continuing — /var/www/%s stays unmounted; zerops_lease action=release lease=%s drops its record)\n", v.err, alias, alias)
		case v.err != nil:
			printUnverifiedLease(alias, v.err.Error())
		case !v.ok:
			fmt.Fprintf(os.Stderr, "  ! Lease %s was not %s: its record went while zcp init read it\n    (continuing — zerops_lease action=status lists the leases this zcp holds)\n", alias, leaseMountWhat)
		case v.lease.Parked:
			printParkedLease(alias)
		default:
			fmt.Fprintf(os.Stderr, "  → SSHFS mount: lease %s\n", alias)
			if err := mountSSHFS(alias, platform.SSHFSMountOptions); err != nil {
				fmt.Fprintf(os.Stderr, "  ! sshfs mount lease %s: %v\n    (continuing — /var/www/%s stays unmounted)\n", alias, err, alias)
			}
		}
	}
}

// leaseAnswer is what a lease check answered of one lease.
type leaseAnswer struct {
	lease ops.LeaseTargetInfo
	ok    bool
	err   error
}

// askLeases asks check about every alias at once, within leaseCheckTimeout,
// and returns the answers in the order of aliases. Without a check every
// lease is unverified: nothing vouches for its container.
func askLeases(stateDir string, aliases []string, check LeaseCheck) []leaseAnswer {
	answers := make([]leaseAnswer, len(aliases))
	if check == nil {
		for i, alias := range aliases {
			answers[i].err = platform.NewPlatformError(platform.ErrPrerequisiteMissing,
				fmt.Sprintf("Lease %s was not %s: zcp init was given no way to ask the pool's orchestrator whether its container is still this lease's", alias, leaseMountWhat), "")
		}
		return answers
	}
	ctx, cancel := context.WithTimeout(context.Background(), leaseCheckTimeout)
	defer cancel()
	var wg sync.WaitGroup
	for i, alias := range aliases {
		wg.Go(func() {
			lease, ok, err := check(ctx, stateDir, alias, leaseMountWhat)
			answers[i] = leaseAnswer{lease: lease, ok: ok, err: err}
		})
	}
	wg.Wait()
	return answers
}

// printParkedLease says a lease is parked and not mounted, naming its unpark.
func printParkedLease(alias string) {
	fmt.Fprintf(os.Stderr, "  → lease %s is parked: not mounted (%s starts and mounts it)\n", alias, ops.LeaseUnparkCall(alias))
}

// printUnverifiedLease says a lease the restore could not get vouched for is
// not mounted, why, and the mount that asks its orchestrator again.
func printUnverifiedLease(alias, why string) {
	fmt.Fprintf(os.Stderr, "  ! %s\n    (continuing — /var/www/%s stays unmounted; zerops_mount action=mount serviceHostname=%s asks the pool's orchestrator again and mounts it once it vouches for the container)\n", why, alias, alias)
}

// serviceMountOptions are the sshfs options of a service mount made at boot
// (ZCP_SSHFS_HOSTNAMES); a lease mount takes platform.SSHFSMountOptions, the
// options acquire used.
const serviceMountOptions = "StrictHostKeyChecking=no,ServerAliveInterval=15,ServerAliveCountMax=3"

// mountSSHFS creates a directory and a zsc unit for one SSHFS mount.
//
// The hostname is composed into the unit command zsc runs and into the mount
// path, so it is held first to what platform.SystemMounter holds it to: a
// service hostname or a lease alias, [a-z0-9] and a hyphen only. A name no
// service can have — ZCP_SSHFS_HOSTNAMES is read unchecked — creates nothing.
// The values are not shell-quoted: the command is zsc's unit command, whose
// quoting zsc defines, and a validated name carries nothing to quote.
//
// A mount left behind by a service that is now stopped answers stat with
// "transport endpoint is not connected", so MkdirAll fails with "file exists".
// That mount point is lazily unmounted and the mkdir retried once. sshfs runs
// as root under `sudo zsc`, so the unmount needs sudo too.
func mountSSHFS(hostname, options string) error {
	if err := platform.ValidateMountName(hostname); err != nil {
		return err
	}
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
