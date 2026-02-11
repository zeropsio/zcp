package ops

import (
	"context"

	"github.com/zeropsio/zcp/internal/platform"
)

// Delete deletes a service by hostname.
// Safety gate: confirm must be true.
func Delete(
	ctx context.Context,
	client platform.Client,
	projectID string,
	hostname string,
	confirm bool,
) (*platform.Process, error) {
	if !confirm {
		return nil, platform.NewPlatformError(
			platform.ErrConfirmRequired,
			"Deletion requires confirmation",
			"Set confirm=true to proceed with deletion",
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
