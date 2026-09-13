package ops

import (
	"context"
	"fmt"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// PublicDomainRoute is one custom-domain routing detail for a service — one
// entry per domain × location pair from platform.PublicHTTPRouting
// (docs/spec-workflows.md §8 O3). Observed.Domains on the enclosing
// PublicAccessObservation carries just the de-duplicated names; this slice
// carries the per-route detail (port, path, DNS/SSL status) for surfaces
// that want it.
type PublicDomainRoute struct {
	Name           string
	Port           int
	Path           string
	DNSCheckStatus string
	SSLStatus      string
	SSLEnabled     bool
}

// PublicAccessObservation is svc's live public-access state (§8 O3): the
// subdomain on/off/enabling, the custom domains routed to it, and the
// subdomain URL. Read from platform surfaces only — never from local meta,
// never guessed from DTO port flags.
type PublicAccessObservation struct {
	// Observed carries the subdomain state and de-duplicated domain names.
	// Listener is caller-owned (this function never sets it — it has no
	// listener-detection read) and is left false.
	Observed topology.PublicAccessObserved
	// URL is the subdomain URL when Observed.Subdomain == on, else "".
	URL string
	// Domains is the per-route detail; Observed.Domains carries just the names.
	Domains []PublicDomainRoute
}

// ObservePublicAccess reads svc's live public-access state from the platform:
// the subdomain on/off/enabling, the custom domains routed to it (via
// ListPublicHTTPRoutings — the only surface that carries a service's public
// domain(s); the service DTO and export carry none), and the subdomain URL.
// It never mutates and never consults local meta (§8 O3).
//
// Subdomain state: svc.SubdomainAccess (REST-authoritative GetService field)
// ⇒ on; else a live (non-terminal) "stack.enableSubdomainAccess" process
// referencing svc.ID — read via ops.ProjectActivity, the direct lag-free
// process read, never the ES-backed search — ⇒ enabling; else off.
func ObservePublicAccess(ctx context.Context, client platform.Client, projectID string, svc *platform.ServiceStack) (PublicAccessObservation, error) {
	subdomain := topology.SubdomainOff
	switch {
	case svc.SubdomainAccess:
		subdomain = topology.SubdomainOn
	default:
		enabling, err := hasLiveSubdomainEnable(ctx, client, projectID, svc)
		if err != nil {
			return PublicAccessObservation{}, fmt.Errorf("observe public access: %w", err)
		}
		if enabling {
			subdomain = topology.SubdomainEnabling
		}
	}

	routings, err := client.ListPublicHTTPRoutings(ctx, projectID)
	if err != nil {
		return PublicAccessObservation{}, fmt.Errorf("observe public access: list public http routings: %w", err)
	}

	var routes []PublicDomainRoute
	var names []string
	seen := make(map[string]bool)
	for _, r := range routings {
		for _, loc := range r.Locations {
			if loc.ServiceID != svc.ID {
				continue
			}
			for _, d := range r.Domains {
				routes = append(routes, PublicDomainRoute{
					Name:           d.Name,
					Port:           loc.Port,
					Path:           loc.Path,
					DNSCheckStatus: d.DNSCheckStatus,
					SSLStatus:      d.SSLStatus,
					SSLEnabled:     r.SSLEnabled,
				})
				if !seen[d.Name] {
					seen[d.Name] = true
					names = append(names, d.Name)
				}
			}
		}
	}

	url := ""
	if subdomain == topology.SubdomainOn {
		url = ResolveSubdomainURL(ctx, client, projectID, svc)
	}

	return PublicAccessObservation{
		Observed: topology.PublicAccessObserved{
			Subdomain: subdomain,
			Domains:   names,
		},
		URL:     url,
		Domains: routes,
	}, nil
}

// hasLiveSubdomainEnable reports whether a live process is currently enabling
// svc's subdomain access — the transitional state between off and on. Reads
// via ops.ProjectActivity (the direct, lag-free process read) rather than
// duplicating the live-process query here.
func hasLiveSubdomainEnable(ctx context.Context, client platform.Client, projectID string, svc *platform.ServiceStack) (bool, error) {
	activity, err := ProjectActivity(ctx, client, projectID, map[string]string{svc.ID: svc.Name})
	if err != nil {
		return false, err
	}
	for _, op := range activity[svc.Name] {
		if op.Action == "subdomain-enable" {
			return true, nil
		}
	}
	return false, nil
}
