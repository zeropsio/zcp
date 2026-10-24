package ops

import (
	"context"
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
