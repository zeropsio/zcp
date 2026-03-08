package ops

import (
	"context"

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

	svc, err := resolveServiceID(services, hostname)
	if err != nil {
		return nil, err
	}

	return client.DeleteService(ctx, svc.ID)
}
