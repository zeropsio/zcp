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
