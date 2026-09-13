package platform

import (
	"context"
	"fmt"

	"github.com/zeropsio/zerops-go/dto/input/path"
	"github.com/zeropsio/zerops-go/dto/input/query"
	"github.com/zeropsio/zerops-go/dto/output"
	"github.com/zeropsio/zerops-go/types"
	"github.com/zeropsio/zerops-go/types/uuid"
)

// ListPublicHTTPRoutings reads the project's custom-domain routing list
// (GET /project/{id}/public-http-routing, SDK GetProjectPublicHttpRouting).
// The service-stack DTO and /service-stack/{id}/export carry no domain
// field, so this is the only read that surfaces a service's public
// domain(s) (docs/spec-workflows.md §8 O3). The platform's TotalCount is
// unreliable (live-verified: read 0 with a non-empty List) — iterate List,
// never trust TotalCount.
func (z *ZeropsClient) ListPublicHTTPRoutings(ctx context.Context, projectID string) ([]PublicHTTPRouting, error) {
	pathParam := path.ProjectId{Id: uuid.ProjectId(projectID)}
	queryParam := query.ListProjectPublicHttpRoutings{
		Limit: types.NewIntNull(directListLimit),
	}

	resp, err := z.handler.GetProjectPublicHttpRouting(ctx, pathParam, queryParam)
	if err != nil {
		return nil, fmt.Errorf("list public http routings: %w", mapSDKError(err, "project"))
	}
	out, err := resp.Output()
	if err != nil {
		return nil, fmt.Errorf("list public http routings: %w", mapSDKError(err, "project"))
	}

	routings := make([]PublicHTTPRouting, 0, len(out.List))
	for _, r := range out.List {
		routings = append(routings, mapPublicHTTPRouting(r))
	}
	return routings, nil
}

func mapPublicHTTPRouting(r output.PublicHttpRouting) PublicHTTPRouting {
	domains := make([]PublicHTTPDomain, 0, len(r.Domains))
	for _, d := range r.Domains {
		domains = append(domains, PublicHTTPDomain{
			Name:           d.DomainName.String(),
			DNSCheckStatus: d.DnsCheckStatus.String(),
			SSLStatus:      d.SslStatus.String(),
		})
	}

	locations := make([]PublicHTTPLocation, 0, len(r.Locations))
	for _, l := range r.Locations {
		locations = append(locations, PublicHTTPLocation{
			Path:      l.Path.String(),
			Port:      l.Port.Native(),
			ServiceID: l.ServiceStackId.Native(),
		})
	}

	return PublicHTTPRouting{
		ID:         r.Id.Native(),
		SSLEnabled: r.SslEnabled.Native(),
		IsSynced:   r.IsSynced.Native(),
		Domains:    domains,
		Locations:  locations,
	}
}
