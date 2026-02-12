package ops

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"

	"github.com/zeropsio/zcp/internal/platform"
)

const mountBase = "/var/www"

var hostnameRegex = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,62}$`)

// Mounter abstracts filesystem mount operations for testing.
type Mounter interface {
	IsMounted(ctx context.Context, path string) (bool, error)
	Mount(ctx context.Context, hostname, localPath string) error
	Unmount(ctx context.Context, hostname, path string) error
	IsWritable(ctx context.Context, path string) (bool, error)
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
	Writable  bool   `json:"writable,omitempty"`
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

	mounted, err := mounter.IsMounted(ctx, mountPath)
	if err != nil {
		return nil, platform.NewPlatformError(
			platform.ErrMountFailed,
			fmt.Sprintf("Failed to check mount status for %s: %v", hostname, err),
			"Check if the service is accessible",
		)
	}
	if mounted {
		writable, _ := mounter.IsWritable(ctx, mountPath)
		return &MountResult{
			Status:    "ALREADY_MOUNTED",
			Hostname:  hostname,
			MountPath: mountPath,
			Writable:  writable,
			Message:   fmt.Sprintf("Service %s is already mounted at %s", hostname, mountPath),
		}, nil
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

	services, err := client.ListServices(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}
	if _, err := resolveServiceID(services, hostname); err != nil {
		return nil, err
	}

	mountPath := filepath.Join(mountBase, hostname)

	mounted, err := mounter.IsMounted(ctx, mountPath)
	if err != nil {
		return nil, platform.NewPlatformError(
			platform.ErrUnmountFailed,
			fmt.Sprintf("Failed to check mount status for %s: %v", hostname, err),
			"Check mount state manually",
		)
	}
	if !mounted {
		return &MountResult{
			Status:    "NOT_MOUNTED",
			Hostname:  hostname,
			MountPath: mountPath,
			Message:   fmt.Sprintf("Service %s is not mounted", hostname),
		}, nil
	}

	if err := mounter.Unmount(ctx, hostname, mountPath); err != nil {
		return nil, platform.NewPlatformError(
			platform.ErrUnmountFailed,
			fmt.Sprintf("Failed to unmount %s: %v", hostname, err),
			"Try fusermount -u manually",
		)
	}

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
		info := checkMountInfo(ctx, mounter, svc.Name)
		return &MountStatusResult{Mounts: []MountInfo{info}}, nil
	}

	mounts := make([]MountInfo, 0, len(services))
	for _, svc := range services {
		mounts = append(mounts, checkMountInfo(ctx, mounter, svc.Name))
	}
	return &MountStatusResult{Mounts: mounts}, nil
}

func checkMountInfo(ctx context.Context, mounter Mounter, hostname string) MountInfo {
	mountPath := filepath.Join(mountBase, hostname)
	info := MountInfo{
		Hostname:  hostname,
		MountPath: mountPath,
	}
	mounted, err := mounter.IsMounted(ctx, mountPath)
	if err != nil {
		return info
	}
	info.Mounted = mounted
	if mounted {
		info.Writable, _ = mounter.IsWritable(ctx, mountPath)
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
