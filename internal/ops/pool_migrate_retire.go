package ops

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
)

// PoolMigrateEnvReferences names every env variable that references the dev
// service — `${<dev>_...}` in its value — the project's ("project KEY") and
// each service's but the dev's own ("<service> KEY"): a reference to a deleted
// service stays its literal text once its reader restarts, so the retire
// refuses while one remains. A value the platform withholds from zcp's token
// cannot be read for one; unread names those.
func PoolMigrateEnvReferences(ctx context.Context, client platform.Client, projectID, dev string, services []platform.ServiceStack) (refs, unread []string, err error) {
	needle := "${" + dev + "_"
	project, err := client.GetProjectEnv(ctx, projectID)
	if err != nil {
		return nil, nil, fmt.Errorf("references to %s: project env: %w", dev, err)
	}
	for _, v := range project {
		switch {
		case v.Content == platform.MaskedEnvValue:
			unread = append(unread, "project "+v.Key)
		case strings.Contains(v.Content, needle):
			refs = append(refs, "project "+v.Key)
		}
	}
	for _, svc := range services {
		if svc.Name == dev {
			continue
		}
		envs, err := FetchServiceEnv(ctx, client, svc.ID)
		if err != nil {
			return nil, nil, fmt.Errorf("references to %s: %s env: %w", dev, svc.Name, err)
		}
		for _, v := range envs {
			switch {
			case v.Masked && isPoolOrBoxCredential(v.Key, svc.ServiceStackTypeInfo.ServiceStackTypeVersionName):
				// A pool's or a Box's own credential is a token, never a
				// reference.
			case v.Masked:
				unread = append(unread, svc.Name+" "+v.Key)
			case strings.Contains(v.Content, needle):
				refs = append(refs, svc.Name+" "+v.Key)
			}
		}
	}
	slices.Sort(refs)
	slices.Sort(unread)
	return refs, unread, nil
}

// PoolMigrateDevDomains names the domains a public HTTP routing sends to the
// dev service, by its id: moving a domain is the person's dashboard work, and
// the retire refuses while one routes to the dev.
func PoolMigrateDevDomains(ctx context.Context, client platform.Client, projectID, devID string) ([]string, error) {
	routings, err := client.ListPublicHTTPRoutings(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("domains of the dev service: %w", err)
	}
	var domains []string
	for _, r := range routings {
		if !slices.ContainsFunc(r.Locations, func(l platform.PublicHTTPLocation) bool { return l.ServiceID == devID }) {
			continue
		}
		for _, d := range r.Domains {
			if !slices.Contains(domains, d.Name) {
				domains = append(domains, d.Name)
			}
		}
	}
	slices.Sort(domains)
	return domains, nil
}
