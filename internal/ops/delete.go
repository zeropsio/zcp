package ops

import (
	"context"

	"github.com/zeropsio/zcp/internal/platform"
)

// Delete deletes a service by hostname.
// Safety gate: confirmHostname must exactly match hostname.
func Delete(
	ctx context.Context,
	client platform.Client,
	projectID string,
	hostname string,
	confirmHostname string,
) (*platform.Process, error) {
	if confirmHostname != hostname {
		return nil, platform.NewPlatformError(
			platform.ErrConfirmRequired,
			"Deletion confirmation mismatch",
			"Set confirmHostname to exactly match serviceHostname",
		)
	}
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
