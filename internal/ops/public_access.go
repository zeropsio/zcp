package ops

import (
	"context"
	"fmt"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// liveOpActionSubdomainEnable is the normalized LiveOp.Action for an
// in-flight "stack.enableSubdomainAccess" process (see events.go's
// actionNameMap, the single source of the normalization) — checked by both
// hasLiveSubdomainEnable and ObservePublicAccessAll's batched fan-out.
const liveOpActionSubdomainEnable = "subdomain-enable"

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

// PublicAccessSummary is the PA-5 payload every URL-bearing surface renders
// (docs/spec-workflows.md §8 O3 PA-5): deploy, dev-server, status/close
// RCO-7, and discover each fill one from the observation they already made
// (ObservePublicAccess / ObservePublicAccessAll) plus the persisted/derived
// intent. Intent and Subdomain are topology.PublicAccessIntent /
// topology.SubdomainState rendered as plain strings so callers outside ops
// (tools/, workflow/) don't need a topology import just to read the field.
type PublicAccessSummary struct {
	Intent    string   `json:"intent"`            // auto|subdomain|domain|none
	Subdomain string   `json:"subdomain"`         // on|off|enabling
	URL       string   `json:"url,omitempty"`     // subdomain URL when Subdomain==on
	Domains   []string `json:"domains,omitempty"` // custom domains routed to the service
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
	subdomain, err := observeSubdomainState(ctx, client, projectID, svc)
	if err != nil {
		return PublicAccessObservation{}, fmt.Errorf("observe public access: %w", err)
	}

	routings, err := client.ListPublicHTTPRoutings(ctx, projectID)
	if err != nil {
		return PublicAccessObservation{}, fmt.Errorf("observe public access: list public http routings: %w", err)
	}

	return buildPublicAccessObservation(ctx, client, projectID, svc, subdomain, routings), nil
}

// ObservePublicAccessAll is ObservePublicAccess batched across every svc in
// svcs: the project's routing list (ListPublicHTTPRoutings) and the live
// subdomain-enabling activity (ops.ProjectActivity, via
// GetProjectProcessesDirect) are each read exactly ONCE regardless of how
// many services are passed, then fanned out per service — the per-service
// ObservePublicAccess would otherwise repeat the routing-list read once per
// HTTP-class runtime. Used by surfaces that report every service in a
// project in one call (discover, status/close RCO-7).
func ObservePublicAccessAll(ctx context.Context, client platform.Client, projectID string, svcs []*platform.ServiceStack) (map[string]PublicAccessObservation, error) {
	routings, err := client.ListPublicHTTPRoutings(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("observe public access all: list public http routings: %w", err)
	}

	// Only services not already REST-confirmed on need the live-activity
	// check (svc.SubdomainAccess is authoritative on its own); batch that
	// check into one ops.ProjectActivity call across all of them.
	idToName := make(map[string]string)
	for _, svc := range svcs {
		if !svc.SubdomainAccess {
			idToName[svc.ID] = svc.Name
		}
	}
	var activity map[string][]LiveOp
	if len(idToName) > 0 {
		activity, err = ProjectActivity(ctx, client, projectID, idToName)
		if err != nil {
			return nil, fmt.Errorf("observe public access all: %w", err)
		}
	}

	out := make(map[string]PublicAccessObservation, len(svcs))
	for _, svc := range svcs {
		subdomain := topology.SubdomainOff
		switch {
		case svc.SubdomainAccess:
			subdomain = topology.SubdomainOn
		default:
			for _, op := range activity[svc.Name] {
				if op.Action == liveOpActionSubdomainEnable {
					subdomain = topology.SubdomainEnabling
					break
				}
			}
		}
		out[svc.Name] = buildPublicAccessObservation(ctx, client, projectID, svc, subdomain, routings)
	}
	return out, nil
}

// observeSubdomainState reads svc's live subdomain on/off/enabling state —
// the single-service half of ObservePublicAccess's read, factored out so
// ObservePublicAccessAll can share the per-service routing-list fan-out
// (buildPublicAccessObservation) without duplicating it.
func observeSubdomainState(ctx context.Context, client platform.Client, projectID string, svc *platform.ServiceStack) (topology.SubdomainState, error) {
	if svc.SubdomainAccess {
		return topology.SubdomainOn, nil
	}
	enabling, err := hasLiveSubdomainEnable(ctx, client, projectID, svc)
	if err != nil {
		return "", err
	}
	if enabling {
		return topology.SubdomainEnabling, nil
	}
	return topology.SubdomainOff, nil
}

// buildPublicAccessObservation assembles svc's PublicAccessObservation from
// an already-known subdomain state and an already-fetched project routing
// list — the part ObservePublicAccess and ObservePublicAccessAll share.
func buildPublicAccessObservation(
	ctx context.Context,
	client platform.Client,
	projectID string,
	svc *platform.ServiceStack,
	subdomain topology.SubdomainState,
	routings []platform.PublicHTTPRouting,
) PublicAccessObservation {
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
	}
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
		if op.Action == liveOpActionSubdomainEnable {
			return true, nil
		}
	}
	return false, nil
}
