package ops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/zeropsio/zcp/internal/platform"
)

const mountBase = "/var/www"

var hostnameRegex = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,62}$`)

// Mounter abstracts filesystem mount operations for testing.
type Mounter interface {
	CheckMount(ctx context.Context, path string) (platform.MountState, error)
	Mount(ctx context.Context, hostname, localPath string) error
	Unmount(ctx context.Context, hostname, path string) error
	ForceUnmount(ctx context.Context, hostname, path string) error
	IsWritable(ctx context.Context, path string) (bool, error)
	ListMountDirs(ctx context.Context, basePath string) ([]string, error)
	HasUnit(ctx context.Context, hostname string) (bool, error)
	CleanupUnit(ctx context.Context, hostname string) error
}

// MountResult is the result of a mount or unmount operation.
type MountResult struct {
	Status    string `json:"status"`
	Hostname  string `json:"hostname"`
	MountPath string `json:"mountPath"`
	Writable  bool   `json:"writable,omitempty"`
	Message   string `json:"message"`
}

// MountStatusResult is the result of a status query.
type MountStatusResult struct {
	Mounts []MountInfo `json:"mounts"`
}

// MountInfo describes the mount state of a single service.
type MountInfo struct {
	Hostname  string `json:"hostname"`
	MountPath string `json:"mountPath"`
	Mounted   bool   `json:"mounted"`
	Stale     bool   `json:"stale,omitempty"`
	Pending   bool   `json:"pending,omitempty"`
	Writable  bool   `json:"writable,omitempty"`
	Orphan    bool   `json:"orphan,omitempty"`
	Message   string `json:"message,omitempty"`
}

// MountService mounts a service's /var/www via SSHFS. Idempotent.
func MountService(
	ctx context.Context,
	client platform.Client,
	projectID string,
	mounter Mounter,
	hostname string,
) (*MountResult, error) {
	if err := validateHostname(hostname); err != nil {
		return nil, err
	}

	services, err := client.ListServices(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}
	if _, err := resolveServiceID(services, hostname); err != nil {
		return nil, err
	}

	mountPath := filepath.Join(mountBase, hostname)

	state, err := mounter.CheckMount(ctx, mountPath)
	if err != nil {
		return nil, platform.NewPlatformError(
			platform.ErrMountFailed,
			fmt.Sprintf("Failed to check mount status for %s: %v", hostname, err),
			"Check if the service is accessible",
		)
	}

	switch state {
	case platform.MountStateActive:
		writable, _ := mounter.IsWritable(ctx, mountPath)
		return &MountResult{
			Status:    "ALREADY_MOUNTED",
			Hostname:  hostname,
			MountPath: mountPath,
			Writable:  writable,
			Message:   fmt.Sprintf("Service %s is already mounted at %s", hostname, mountPath),
		}, nil

	case platform.MountStateStale:
		if err := mounter.ForceUnmount(ctx, hostname, mountPath); err != nil {
			return nil, platform.NewPlatformError(
				platform.ErrMountFailed,
				fmt.Sprintf("Failed to clear stale mount for %s: %v", hostname, err),
				"Try fusermount -uz manually",
			)
		}
		_ = os.Remove(mountPath)

	case platform.MountStateNotMounted:
		// Clean up orphan unit if present, before creating a new mount.
		if hasUnit, _ := mounter.HasUnit(ctx, hostname); hasUnit {
			_ = mounter.CleanupUnit(ctx, hostname)
			_ = os.Remove(mountPath)
		}
	}

	if err := mounter.Mount(ctx, hostname, mountPath); err != nil {
		return nil, platform.NewPlatformError(
			platform.ErrMountFailed,
			fmt.Sprintf("Failed to mount %s: %v", hostname, err),
			"Verify SSHFS is available and the service is running",
		)
	}

	writable, _ := mounter.IsWritable(ctx, mountPath)
	return &MountResult{
		Status:    "MOUNTED",
		Hostname:  hostname,
		MountPath: mountPath,
		Writable:  writable,
		Message:   fmt.Sprintf("Mounted %s at %s", hostname, mountPath),
	}, nil
}

// UnmountService unmounts a service's SSHFS mount. Idempotent.
func UnmountService(
	ctx context.Context,
	client platform.Client,
	projectID string,
	mounter Mounter,
	hostname string,
) (*MountResult, error) {
	if err := validateHostname(hostname); err != nil {
		return nil, err
	}

	mountPath := filepath.Join(mountBase, hostname)

	// Check mount state FIRST, before API call.
	state, err := mounter.CheckMount(ctx, mountPath)
	if err != nil {
		return nil, platform.NewPlatformError(
			platform.ErrUnmountFailed,
			fmt.Sprintf("Failed to check mount status for %s: %v", hostname, err),
			"Check mount state manually",
		)
	}

	if state == platform.MountStateNotMounted {
		// Check for orphan systemd unit (unit exists but no FUSE mount).
		hasUnit, _ := mounter.HasUnit(ctx, hostname)
		if hasUnit {
			if err := mounter.CleanupUnit(ctx, hostname); err != nil {
				return nil, platform.NewPlatformError(
					platform.ErrUnmountFailed,
					fmt.Sprintf("Failed to clean up orphan unit for %s: %v", hostname, err),
					"Try: sudo zsc unit remove sshfs-"+hostname,
				)
			}
			_ = os.Remove(mountPath)
			return &MountResult{
				Status:    "UNIT_CLEANED",
				Hostname:  hostname,
				MountPath: mountPath,
				Message:   fmt.Sprintf("Cleaned up orphan systemd unit for %s (no FUSE mount was active)", hostname),
			}, nil
		}
		return &MountResult{
			Status:    "NOT_MOUNTED",
			Hostname:  hostname,
			MountPath: mountPath,
			Message:   fmt.Sprintf("Service %s is not mounted", hostname),
		}, nil
	}

	// Stale mount — force unmount directly (service may be deleted).
	if state == platform.MountStateStale {
		if err := mounter.ForceUnmount(ctx, hostname, mountPath); err != nil {
			return nil, platform.NewPlatformError(
				platform.ErrUnmountFailed,
				fmt.Sprintf("Failed to force unmount stale %s: %v", hostname, err),
				"Try fusermount -uz manually",
			)
		}
		_ = os.Remove(mountPath)
		return &MountResult{
			Status:    "UNMOUNTED",
			Hostname:  hostname,
			MountPath: mountPath,
			Message:   fmt.Sprintf("Force unmounted stale %s from %s", hostname, mountPath),
		}, nil
	}

	// Active mount — try API lookup, fall back to force unmount if service deleted.
	services, err := client.ListServices(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}
	_, resolveErr := resolveServiceID(services, hostname)
	if resolveErr != nil {
		// Service deleted but mount still active — force unmount.
		var pe *platform.PlatformError
		if errors.As(resolveErr, &pe) && pe.Code == platform.ErrServiceNotFound {
			if err := mounter.ForceUnmount(ctx, hostname, mountPath); err != nil {
				return nil, platform.NewPlatformError(
					platform.ErrUnmountFailed,
					fmt.Sprintf("Failed to force unmount %s: %v", hostname, err),
					"Try fusermount -uz manually",
				)
			}
			_ = os.Remove(mountPath)
			return &MountResult{
				Status:    "UNMOUNTED",
				Hostname:  hostname,
				MountPath: mountPath,
				Message:   fmt.Sprintf("Force unmounted %s (service deleted) from %s", hostname, mountPath),
			}, nil
		}
		return nil, resolveErr
	}

	if err := mounter.Unmount(ctx, hostname, mountPath); err != nil {
		return nil, platform.NewPlatformError(
			platform.ErrUnmountFailed,
			fmt.Sprintf("Failed to unmount %s: %v", hostname, err),
			"Try fusermount -u manually",
		)
	}

	_ = os.Remove(mountPath)
	return &MountResult{
		Status:    "UNMOUNTED",
		Hostname:  hostname,
		MountPath: mountPath,
		Message:   fmt.Sprintf("Unmounted %s from %s", hostname, mountPath),
	}, nil
}

// MountStatus reports mount status for one or all services.
func MountStatus(
	ctx context.Context,
	client platform.Client,
	projectID string,
	mounter Mounter,
	hostname string,
) (*MountStatusResult, error) {
	services, err := client.ListServices(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}

	if hostname != "" {
		if err := validateHostname(hostname); err != nil {
			return nil, err
		}
		svc, err := resolveServiceID(services, hostname)
		if err != nil {
			return nil, err
		}
		info := checkMountInfo(ctx, mounter, svc.Name, false)
		return &MountStatusResult{Mounts: []MountInfo{info}}, nil
	}

	serviceNames := make(map[string]bool, len(services))
	mounts := make([]MountInfo, 0, len(services))
	for _, svc := range services {
		serviceNames[svc.Name] = true
		mounts = append(mounts, checkMountInfo(ctx, mounter, svc.Name, false))
	}

	// Detect orphan mount directories from deleted services.
	// Only report dirs that are actual SSHFS mounts (active or stale).
	// Plain directories (e.g. .claude, .zcp) are silently skipped.
	dirs, _ := mounter.ListMountDirs(ctx, mountBase)
	for _, dir := range dirs {
		if !serviceNames[dir] {
			info := checkMountInfo(ctx, mounter, dir, true)
			if info.Mounted || info.Stale || info.Pending {
				mounts = append(mounts, info)
			}
		}
	}

	return &MountStatusResult{Mounts: mounts}, nil
}

func checkMountInfo(ctx context.Context, mounter Mounter, hostname string, orphan bool) MountInfo {
	mountPath := filepath.Join(mountBase, hostname)
	info := MountInfo{
		Hostname:  hostname,
		MountPath: mountPath,
		Orphan:    orphan,
	}
	state, err := mounter.CheckMount(ctx, mountPath)
	if err != nil {
		return info
	}
	switch state {
	case platform.MountStateActive:
		info.Mounted = true
		info.Writable, _ = mounter.IsWritable(ctx, mountPath)
	case platform.MountStateStale:
		info.Stale = true
		if orphan {
			info.Message = "Service was deleted but mount is stale. Use unmount to clean up."
		} else {
			info.Message = "Mount is stale (transport disconnected). Will auto-reconnect when service is running. If service is stopped, start it first."
		}
	case platform.MountStateNotMounted:
		// Check for orphan systemd unit (unit exists but FUSE never connected).
		if hasUnit, _ := mounter.HasUnit(ctx, hostname); hasUnit {
			info.Pending = true
			if orphan {
				info.Message = "Orphan systemd unit exists but FUSE mount never connected. Use unmount to clean up."
			} else {
				info.Message = "Systemd unit exists but FUSE mount is not active. Use unmount to clean up, or mount to recreate."
			}
		}
	}
	return info
}

func validateHostname(hostname string) error {
	if hostname == "" {
		return platform.NewPlatformError(
			platform.ErrServiceRequired,
			"Service hostname is required",
			"Provide serviceHostname parameter",
		)
	}
	if !hostnameRegex.MatchString(hostname) {
		return platform.NewPlatformError(
			platform.ErrInvalidHostname,
			fmt.Sprintf("Invalid hostname format: %s", hostname),
			"Hostname must start with a letter and contain only letters, digits, hyphens, underscores (max 63 chars)",
		)
	}
	return nil
}
