package ops

import (
	"context"
	"errors"
	"fmt"
	"strings"

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
	NextActions   string            `json:"nextActions,omitempty"`
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
// Falls back to the zeropsSubdomain env var when SubdomainHost is a bare prefix (no domain suffix).
func attachSubdomainUrlsToResult(ctx context.Context, client platform.Client, result *SubdomainResult, projectID, serviceID string) {
	proj, err := client.GetProject(ctx, projectID)
	if err != nil || proj.SubdomainHost == "" {
		return
	}
	detail, err := client.GetService(ctx, serviceID)
	if err != nil || len(detail.Ports) == 0 {
		return
	}

	// Try building URLs from SubdomainHost directly.
	urls := make([]string, 0, len(detail.Ports))
	for _, p := range detail.Ports {
		u := BuildSubdomainURL(result.Hostname, proj.SubdomainHost, p.Port)
		urls = append(urls, u)
	}

	// If all URLs are empty, SubdomainHost was a bare prefix — fall back to env var.
	if allEmpty(urls) {
		domain := extractDomainFromEnv(ctx, client, serviceID)
		if domain == "" {
			return
		}
		urls = urls[:0]
		for _, p := range detail.Ports {
			if p.Port == 80 {
				urls = append(urls, fmt.Sprintf("https://%s-%s.%s", result.Hostname, proj.SubdomainHost, domain))
			} else {
				urls = append(urls, fmt.Sprintf("https://%s-%s-%d.%s", result.Hostname, proj.SubdomainHost, p.Port, domain))
			}
		}
	}

	result.SubdomainUrls = urls
}

// extractDomainFromEnv reads the zeropsSubdomain env var and extracts the domain suffix.
func extractDomainFromEnv(ctx context.Context, client platform.Client, serviceID string) string {
	envs, err := client.GetServiceEnv(ctx, serviceID)
	if err != nil {
		return ""
	}
	for _, env := range envs {
		if env.Key == "zeropsSubdomain" {
			return parseSubdomainDomain(env.Content)
		}
	}
	return ""
}

// parseSubdomainDomain extracts the domain suffix from a zerops subdomain URL.
// E.g. "https://app-1df2-3000.prg1.zerops.app" → "prg1.zerops.app".
func parseSubdomainDomain(url string) string {
	if url == "" {
		return ""
	}
	// Strip scheme.
	host := url
	if idx := strings.Index(host, "://"); idx >= 0 {
		host = host[idx+3:]
	}
	// Strip path.
	if idx := strings.IndexByte(host, '/'); idx >= 0 {
		host = host[:idx]
	}
	// Domain is everything after the first dot.
	_, domain, found := strings.Cut(host, ".")
	if !found || domain == "" {
		return ""
	}
	return domain
}

// allEmpty returns true if all strings in the slice are empty.
func allEmpty(ss []string) bool {
	for _, s := range ss {
		if s != "" {
			return false
		}
	}
	return true
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
