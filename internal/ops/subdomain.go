package ops

import (
	"context"
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
)

// Subdomain* status values surface the short-circuit path the tool took.
// Callers use these to skip post-enable work (HTTP readiness probes) when
// no platform call actually happened.
const (
	SubdomainStatusAlreadyEnabled  = "already_enabled"
	SubdomainStatusAlreadyDisabled = "already_disabled"
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

// Subdomain enables or disables the zerops.app subdomain for a service.
//
// Enable is idempotent via check-before-enable: GetService (REST,
// authoritative) reports current SubdomainAccess, and we short-circuit when
// the subdomain is already active. The previous version called
// EnableSubdomainAccess blindly; the platform accepts the call and creates a
// garbage FAILED process with error.code=noSubdomainPorts for every such
// redundant invocation — the process pollutes the event log and triggers
// GUI error notifications even though ZCP reported success. Empirical
// evidence: plans/archive/subdomain-robustness.md §1.1.
//
// TOCTOU race (subdomain flips to enabled between our GetService and our
// EnableSubdomainAccess) is handled by the tool-layer FAILED-normalization
// workaround at internal/tools/subdomain.go — when the platform returns a
// FAILED process but URLs resolve, the tool normalizes to already_enabled
// and preserves FailReason in Warnings. No dedicated error-code branch in
// the ops layer: the platform simply does not emit SUBDOMAIN_ALREADY_ENABLED
// as an error code for enable anymore.
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
			result.Status = SubdomainStatusAlreadyEnabled
			attachSubdomainUrlsToResult(ctx, client, result, projectID, svc.ID)
			return result, nil
		}
		proc, err := client.EnableSubdomainAccess(ctx, svc.ID)
		if err != nil {
			// No special-case for SUBDOMAIN_ALREADY_ENABLED: the platform
			// doesn't emit that code — empirically (plan §1.2) a redundant
			// enable call surfaces as an HTTP 200 with a FAILED Process, not
			// as an error. The check-before-enable gate above catches the
			// normal idempotent case; the FAILED Process belt-and-suspenders
			// at the tool layer covers the TOCTOU race window.
			return nil, err
		}
		result.Process = proc
		attachSubdomainUrlsToResult(ctx, client, result, projectID, svc.ID)
	} else {
		if !detail.SubdomainAccess {
			// Symmetric to enable: skip the platform API call when the
			// subdomain is already disabled. Platform behavior on redundant
			// disable is not empirically characterized but the same garbage
			// FAILED process pattern is plausible; short-circuiting is safe
			// defense in depth either way.
			result.Status = SubdomainStatusAlreadyDisabled
			return result, nil
		}
		proc, err := client.DisableSubdomainAccess(ctx, svc.ID)
		if err != nil {
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
