package ops

import (
	"context"
	"errors"

	"github.com/zeropsio/zcp/internal/platform"
)

// SubdomainResult represents the result of a subdomain enable/disable operation.
type SubdomainResult struct {
	Process       *platform.Process `json:"process,omitempty"`
	Hostname      string            `json:"serviceHostname"`
	ServiceID     string            `json:"serviceId"`
	Action        string            `json:"action"`
	Status        string            `json:"status,omitempty"`
	SubdomainUrls []string          `json:"subdomainUrls,omitempty"`
}

// Error codes for idempotent handling.
const (
	errSubdomainAlreadyEnabled  = "SUBDOMAIN_ALREADY_ENABLED"
	errSubdomainAlreadyDisabled = "SUBDOMAIN_ALREADY_DISABLED"
)

// Subdomain enables or disables the zerops.app subdomain for a service.
// Idempotent: already-enabled/disabled is treated as success.
func Subdomain(
	ctx context.Context,
	client platform.Client,
	projectID string,
	hostname string,
	action string,
) (*SubdomainResult, error) {
	if action != "enable" && action != "disable" {
		return nil, platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"action must be 'enable' or 'disable'",
			"Use action='enable' or action='disable'",
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

	result := &SubdomainResult{
		Hostname:  hostname,
		ServiceID: svc.ID,
		Action:    action,
	}

	if action == "enable" {
		proc, err := client.EnableSubdomainAccess(ctx, svc.ID)
		if err != nil {
			if isAlreadyEnabled(err) {
				result.Status = "already_enabled"
				attachSubdomainUrlsToResult(ctx, client, result, projectID, svc.ID)
				return result, nil
			}
			return nil, err
		}
		result.Process = proc
		attachSubdomainUrlsToResult(ctx, client, result, projectID, svc.ID)
	} else {
		proc, err := client.DisableSubdomainAccess(ctx, svc.ID)
		if err != nil {
			if isAlreadyDisabled(err) {
				result.Status = "already_disabled"
				return result, nil
			}
			return nil, err
		}
		result.Process = proc
	}

	return result, nil
}

// attachSubdomainUrlsToResult fetches project and service detail to compute subdomain URLs.
func attachSubdomainUrlsToResult(ctx context.Context, client platform.Client, result *SubdomainResult, projectID, serviceID string) {
	proj, err := client.GetProject(ctx, projectID)
	if err != nil || proj.SubdomainHost == "" {
		return
	}
	detail, err := client.GetService(ctx, serviceID)
	if err != nil || len(detail.Ports) == 0 {
		return
	}
	urls := make([]string, 0, len(detail.Ports))
	for _, p := range detail.Ports {
		urls = append(urls, BuildSubdomainURL(result.Hostname, proj.SubdomainHost, p.Port))
	}
	result.SubdomainUrls = urls
}

func isAlreadyEnabled(err error) bool {
	var pe *platform.PlatformError
	if errors.As(err, &pe) {
		return pe.Code == errSubdomainAlreadyEnabled
	}
	return false
}

func isAlreadyDisabled(err error) bool {
	var pe *platform.PlatformError
	if errors.As(err, &pe) {
		return pe.Code == errSubdomainAlreadyDisabled
	}
	return false
}
