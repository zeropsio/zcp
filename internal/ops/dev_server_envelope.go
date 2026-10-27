package ops

import (
	"context"
	"slices"
	"time"

	"github.com/zeropsio/zcp/internal/devsup"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// observe records a later supervisor answer of the same call.
func (ds *DevServers) observe(st devsup.State) {
	ds.State = st
	ds.ReadAt = time.Now()
}

// DevServerStates projects a call's read of a dev service's dev servers onto
// the envelope's devServers[] (docs/spec-mate.md §2.8a.3): every server the
// supervisor reports, declared ones in declaration order, with its status
// verbatim, its port's public URL while the service's subdomain serves that
// port, and — for the server result reports — this call's probe of it.
func DevServerStates(ctx context.Context, client platform.Client, projectID string, result *DevServerResult) []topology.DevServerState {
	ds := result.Read
	if ds == nil {
		return nil
	}
	urls := devServerPublicURLs(ctx, client, projectID, ds)
	previews := map[int]string{}
	previewOf := func(port int) string {
		if port == result.PreviewPort && result.PreviewURL != "" {
			return result.PreviewURL // built by the caller, who has just enabled the subdomain
		}
		if _, ok := previews[port]; !ok {
			previews[port] = LeasePreviewURL(ctx, client, projectID, result.Pool, port)
		}
		return previews[port]
	}
	reported := result.Name
	if reported == "" {
		reported = result.Hostname
	}
	names := make([]string, 0, len(ds.State.Servers))
	for _, s := range ds.Servers() {
		if _, ok := ds.State.Servers[s.Name]; ok {
			names = append(names, s.Name)
		}
	}
	var extra []string
	for name := range ds.State.Servers {
		if !slices.Contains(names, name) {
			extra = append(extra, name)
		}
	}
	slices.Sort(extra)
	names = append(names, extra...)

	out := make([]topology.DevServerState, 0, len(names))
	for _, name := range names {
		st := ds.State.Servers[name]
		declared, _ := ds.Server(name)
		entry := topology.DevServerState{
			Name:   name,
			Port:   declared.Port,
			URL:    urls[declared.Port],
			Status: string(st.Status),
			ReadAt: ds.ReadAt.UTC(),
		}
		if result.Pool != "" && st.PreviewPort > 0 {
			// A lease's server is public on its preview port, at its pool's
			// subdomain; the alias is no service to look the address up by.
			entry.URL = previewOf(st.PreviewPort)
		}
		if name == reported && result.HealthPath != "" {
			entry.Verify = &topology.DevServerProbe{OK: result.HealthOK, HTTPStatus: result.HealthStatus, At: ds.ReadAt.UTC()}
		}
		if e := st.Exit; e != nil {
			exit := &topology.DevServerExit{Signal: e.Signal, At: e.At.UTC()}
			if e.Signal == "" {
				code := e.Code
				exit.Code = &code
			}
			entry.LastExit = exit
		}
		out = append(out, entry)
	}
	return out
}

// devServerPublicURLs maps each declared port to its public subdomain URL —
// only while the service's subdomain is on and the port has HTTP support.
func devServerPublicURLs(ctx context.Context, client platform.Client, projectID string, ds *DevServers) map[int]string {
	if client == nil {
		return nil
	}
	services, err := client.ListServices(ctx, projectID)
	if err != nil {
		return nil
	}
	svc, err := FindService(services, ds.Hostname)
	if err != nil || !svc.SubdomainAccess {
		return nil
	}
	proj, err := client.GetProject(ctx, projectID)
	if err != nil || proj.SubdomainHost == "" {
		return nil
	}
	urls := map[int]string{}
	for _, s := range ds.Servers() {
		if s.Port == 0 || !slices.ContainsFunc(svc.Ports, func(p platform.Port) bool { return p.Port == s.Port && (p.HTTPSupport || isHTTPScheme(p)) }) {
			continue
		}
		if u := subdomainURLForPort(ctx, client, proj.SubdomainHost, svc, s.Port, nil); u != "" {
			urls[s.Port] = u
		}
	}
	return urls
}

// LeasePreviewURL is a lease's public address: the pool's subdomain on the
// lease's preview port (docs/spec-mate.md §2.8a.1 previewPortBase). The pool,
// not the lease alias, is the service that has the subdomain. It is "" while
// the pool's subdomain is off or the pool does not declare port with HTTP
// support, and for a lease with no preview port.
func LeasePreviewURL(ctx context.Context, client platform.Client, projectID, pool string, port int) string {
	return leasePreviewURL(ctx, client, projectID, pool, port, true)
}

// leasePreviewURL builds the address from the pool's REST-authoritative
// detail. requireOn false trusts a caller that has just enabled the subdomain
// (the flag takes a moment to show).
func leasePreviewURL(ctx context.Context, client platform.Client, projectID, pool string, port int, requireOn bool) string {
	if port <= 0 || client == nil {
		return ""
	}
	services, err := client.ListServices(ctx, projectID)
	if err != nil {
		return ""
	}
	svc, err := FindService(services, pool)
	if err != nil {
		return ""
	}
	detail, err := client.GetService(ctx, svc.ID)
	if err != nil || (requireOn && !detail.SubdomainAccess) {
		return ""
	}
	if !slices.ContainsFunc(detail.Ports, func(p platform.Port) bool { return p.Port == port && (p.HTTPSupport || isHTTPScheme(p)) }) {
		return ""
	}
	return SubdomainURLForPort(ctx, client, projectID, svc, port)
}
