package ops

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
)

// SubdomainResult represents the result of a subdomain enable/disable operation.
//
// Warnings collects non-fatal anomalies the caller should surface without
// treating the whole call as failed — e.g. a process that finished in a
// degenerate state (FAILED + URLs-present belt-and-suspenders normalization),
// a poll timeout whose outcome is unknown, or an HTTP readiness probe that
// timed out. Callers that ignore Warnings are unaffected; callers that want
// diagnostic provenance see what the platform actually did.
type SubdomainResult struct {
	Process       *platform.Process `json:"process,omitempty"`
	Hostname      string            `json:"serviceHostname"`
	ServiceID     string            `json:"serviceId"`
	Action        string            `json:"action"`
	Status        string            `json:"status,omitempty"`
	SubdomainUrls []string          `json:"subdomainUrls,omitempty"`
	NextActions   string            `json:"nextActions,omitempty"`
	Warnings      []string          `json:"warnings,omitempty"`
}

// Error codes for idempotent handling.
const (
	errSubdomainAlreadyEnabled  = "SUBDOMAIN_ALREADY_ENABLED"
	errSubdomainAlreadyDisabled = "SUBDOMAIN_ALREADY_DISABLED"
)

// Subdomain enables or disables the zerops.app subdomain for a service.
//
// Enable is idempotent via check-before-enable: we read SubdomainAccess from a
// fresh REST-authoritative GetService call and short-circuit when the
// subdomain is already active. The previous version called
// EnableSubdomainAccess blindly; the platform accepts the call and creates a
// garbage FAILED process with error.code=noSubdomainPorts for every such
// redundant invocation. Those processes pollute the project event log and
// trigger GUI error notifications even though the ZCP response indicated
// success. Empirical evidence: plans/archive/subdomain-robustness.md §1.1.
//
// The isAlreadyEnabled error branch stays as belt-and-suspenders against a
// TOCTOU race (concurrent admin action between GetService and
// EnableSubdomainAccess), but in normal single-caller operation the platform
// never sees a redundant enable call.
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

	// Authoritative state — GetService is REST-backed, unlike ListServices
	// which reads from Elasticsearch and can lag by seconds.
	detail, err := client.GetService(ctx, svc.ID)
	if err != nil {
		return nil, err
	}

	result := &SubdomainResult{
		Hostname:  hostname,
		ServiceID: svc.ID,
		Action:    action,
	}

	if action == "enable" {
		if detail.SubdomainAccess {
			// Already enabled on the platform side — do NOT call
			// EnableSubdomainAccess. This prevents the garbage-FAILED-process
			// generation documented above.
			result.Status = "already_enabled"
			attachSubdomainUrlsToResult(ctx, client, result, projectID, svc.ID)
			return result, nil
		}
		proc, err := client.EnableSubdomainAccess(ctx, svc.ID)
		if err != nil {
			if isAlreadyEnabled(err) {
				// Belt-and-suspenders: TOCTOU race (subdomain got enabled
				// between our GetService and this call). Treat as success.
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
	if AllEmpty(urls) {
		domain := ExtractDomainFromEnv(ctx, client, serviceID)
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

// ExtractDomainFromEnv reads the zeropsSubdomain env var and extracts the domain suffix.
func ExtractDomainFromEnv(ctx context.Context, client platform.Client, serviceID string) string {
	envs, err := client.GetServiceEnv(ctx, serviceID)
	if err != nil {
		return ""
	}
	for _, env := range envs {
		if env.Key == envKeyZeropsSubdomain {
			return ParseSubdomainDomain(env.Content)
		}
	}
	return ""
}

// ParseSubdomainDomain extracts the domain suffix from a zerops subdomain URL.
// E.g. "https://app-1df2-3000.prg1.zerops.app" → "prg1.zerops.app".
func ParseSubdomainDomain(url string) string {
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

// AllEmpty returns true if all strings in the slice are empty.
func AllEmpty(ss []string) bool {
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
