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
func PoolMigrateEnvReferences(ctx context.Context, client platform.Client, projectID, dev string, services []platform.ServiceStack, skip PoolMigrateReferenceSkip) (refs, unread []string, err error) {
	byHost, unread, err := PoolMigrateEnvReferencesTo(ctx, client, projectID, []string{dev}, services, skip)
	if err != nil {
		return nil, nil, err
	}
	return byHost[dev], unread, nil
}

// PoolMigrateReferenceSkip is what the reference check leaves out because zcp
// itself holds or wrote it, never a reference to the dev: the env of the
// services named — the zcp service's own (its API key, its editor password,
// the pools' bearer it takes by reference) — and, on any service, a masked
// key in zcp's own ZCP_ namespace (the Mate credential zcp puts on a pool),
// and on a pool the secrets zcp's env step wrote there by its record under
// StateDir (each read resolved on the dev, so never a reference).
type PoolMigrateReferenceSkip struct {
	Services []string
	StateDir string
}

// zcpOwnedEnvPrefix is zcp's and the Mate's own env namespace.
const zcpOwnedEnvPrefix = "ZCP_"

// PoolMigrateEnvReferencesTo is PoolMigrateEnvReferences for every service a
// retire deletes — a variant-B pair's dev services and its code volume — in
// one read of the project's env and each other service's: by retired
// hostname, the variables that reference it. The retired services' own env
// is never read: it goes with them; nor is what skip leaves out.
func PoolMigrateEnvReferencesTo(ctx context.Context, client platform.Client, projectID string, retired []string, services []platform.ServiceStack, skip PoolMigrateReferenceSkip) (map[string][]string, []string, error) {
	named := strings.Join(retired, ", ")
	refs := map[string][]string{}
	var unread []string
	match := func(name, content string) {
		for _, host := range retired {
			if strings.Contains(content, "${"+host+"_") {
				refs[host] = append(refs[host], name)
			}
		}
	}
	project, err := client.GetProjectEnv(ctx, projectID)
	if err != nil {
		return nil, nil, fmt.Errorf("references to %s: project env: %w", named, err)
	}
	for _, v := range project {
		if v.Content == platform.MaskedEnvValue {
			unread = append(unread, "project "+v.Key)
			continue
		}
		match("project "+v.Key, v.Content)
	}
	for _, svc := range services {
		if slices.Contains(retired, svc.Name) || slices.Contains(skip.Services, svc.Name) {
			continue
		}
		envs, err := FetchServiceEnv(ctx, client, svc.ID)
		if err != nil {
			return nil, nil, fmt.Errorf("references to %s: %s env: %w", named, svc.Name, err)
		}
		var written map[string]string
		if rec, found := readPoolEnvRecord(skip.StateDir, svc.Name); found && rec.PoolServiceID == svc.ID {
			written = rec.Fingerprints
		}
		for _, v := range envs {
			_, zcpWrote := written[v.Key]
			switch {
			case v.Masked && isPoolOrBoxCredential(v.Key, svc.ServiceStackTypeInfo.ServiceStackTypeVersionName):
				// A pool's or a Box's own credential is a token, never a
				// reference.
			case v.Masked && (strings.HasPrefix(v.Key, zcpOwnedEnvPrefix) || zcpWrote):
				// What zcp itself put there is no reference either.
			case v.Masked:
				unread = append(unread, svc.Name+" "+v.Key)
			default:
				match(svc.Name+" "+v.Key, v.Content)
			}
		}
	}
	for host := range refs {
		slices.Sort(refs[host])
	}
	slices.Sort(unread)
	return refs, unread, nil
}

// PoolMigrateDevDomains names the custom domains a public HTTP routing sends
// to the dev service, by its id: moving a domain is the person's dashboard
// work, and the retire refuses while one routes to the dev. The dev's own
// zerops.app subdomains (IsPlatformSubdomain) are none: they die with it.
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
			if !IsPlatformSubdomain(d.Name) && !slices.Contains(domains, d.Name) {
				domains = append(domains, d.Name)
			}
		}
	}
	slices.Sort(domains)
	return domains, nil
}
