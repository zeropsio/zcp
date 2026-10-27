package ops

import (
	"context"
	"errors"
	"fmt"

	"github.com/zeropsio/zcp/internal/platform"
)

// Delete deletes a service by hostname.
func Delete(
	ctx context.Context,
	client platform.Client,
	projectID string,
	hostname string,
) (*platform.Process, error) {
	if hostname == "" {
		return nil, platform.NewPlatformError(
			platform.ErrServiceRequired,
			"Service hostname is required",
			"Provide serviceHostname parameter",
		)
	}

	services, err := client.ListServices(ctx, projectID)
	if err != nil {
		return nil, err
	}

	svc, err := FindUserVisibleService(services, hostname)
	if err != nil {
		return nil, err
	}

	return client.DeleteService(ctx, svc.ID)
}

// DeleteByID deletes the service with the id the caller read directly — by id,
// or from the direct service list — and resolves no hostname: the service
// search lags (docs/spec-workflows.md §3.5) and could name another service by
// the hostname, or none, and a service that later takes the hostname has
// another id, so it is never touched.
func DeleteByID(ctx context.Context, client platform.Client, serviceID string) (*platform.Process, error) {
	if serviceID == "" {
		return nil, platform.NewPlatformError(
			platform.ErrServiceRequired,
			"Service id is required",
			"Read the service by id first",
		)
	}
	proc, err := client.DeleteService(ctx, serviceID)
	if err != nil {
		return nil, fmt.Errorf("delete service %s: %w", serviceID, err)
	}
	return proc, nil
}

// The API codes a by-id read of a deleted service answers with: a deleted
// service stack reads HTTP 400 serviceStackNotFound (live-verified), not a
// 404.
const (
	apiCodeServiceStackNotFound = "serviceStackNotFound"
	apiCodeServiceNotFound      = "serviceNotFound"
)

// IsServiceGone reports an error of a by-id read or delete that says the
// service no longer exists, whichever way the platform spells it: a 404, or a
// 400 carrying serviceStackNotFound or serviceNotFound.
func IsServiceGone(err error) bool {
	var pe *platform.PlatformError
	if !errors.As(err, &pe) {
		return false
	}
	return pe.Code == platform.ErrServiceNotFound || pe.APICode == apiCodeServiceStackNotFound || pe.APICode == apiCodeServiceNotFound
}
