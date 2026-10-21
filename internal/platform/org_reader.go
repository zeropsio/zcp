package platform

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/zeropsio/zerops-go/dto/input/body"
	"github.com/zeropsio/zerops-go/dto/output"
	"github.com/zeropsio/zerops-go/types"
)

// OrgReader is org mode's only door to the Zerops API (docs/spec-mate.md
// §2.11): the Chief reads the whole organization through the org's
// token and never writes to it. Two things hold that in code rather than in
// prose:
//
//   - the method set is reads only, pinned name by name by
//     TestOrgReader_ExposesNoWriteMethod — a write added here fails it;
//   - every request leaves through readOnlyTransport, which refuses anything
//     but a GET or a search POST before it reaches the network, so even a
//     read method that grew a write underneath it could not send one.
//
// Every search is filtered by the org's clientId, so a token that reaches
// other organizations (a person's own, in the local dev check) reads only
// this one. By-id reads (a service's env, a project's log access, its
// routings) take ids the caller resolved from those org-scoped lists.
type OrgReader struct {
	z     *ZeropsClient
	orgID string
	// viaOrgAPI is set when every read goes to ZCP_ORG_API (HQ's Chief
	// proxy), which answers who the Chief's person is itself.
	viaOrgAPI bool
}

// orgSearchPage bounds one search request; ListServices pages past it.
const orgSearchPage = 1000

// OrgAccess is what org mode opens its reader with (docs/spec-mate.md §2.11).
type OrgAccess struct {
	// Token is ZCP_ORG_TOKEN, sent as the bearer on every read.
	Token string
	// APIHost is the region's API, read when OrgAPI is empty.
	APIHost string
	// OrgAPI is ZCP_ORG_API: a base URL mirroring the public API root
	// (HQ's Chief proxy) that every read goes to instead of APIHost.
	OrgAPI string
	// OrgID is ZCP_ORG_ID: the organization to read. Required with OrgAPI.
	OrgID string
}

// OpenOrgReader builds the read-only client and resolves the org it reads.
//
// Through the region's API the org is the one named, which the token must
// reach, or else the one org the token reaches — a token reaching several
// with none named is refused: org mode never guesses which organization it
// speaks for. Through OrgAPI nothing resolves the token (the proxy serves
// only OrgReads, not /user/info), so the org must be named; the proxy
// answers only for the organization its credential was issued for.
func OpenOrgReader(ctx context.Context, access OrgAccess) (*OrgReader, error) {
	named := strings.TrimSpace(access.OrgID)
	if access.OrgAPI != "" {
		base, err := parseOrgAPI(access.OrgAPI)
		if err != nil {
			return nil, err
		}
		if named == "" {
			return nil, NewPlatformError(ErrOrgAmbiguous,
				"ZCP_ORG_API is set and ZCP_ORG_ID names no organization",
				"Set ZCP_ORG_ID to the organization this Chief is for")
		}
		transport := orgAPITransport{base: base, next: readOnlyTransport{base: http.DefaultTransport}}
		httpClient := &http.Client{Timeout: DefaultAPITimeout, Transport: transport}
		return &OrgReader{z: newZeropsClientOn(access.Token, base.Scheme+"://"+base.Host+"/", httpClient), orgID: named, viaOrgAPI: true}, nil
	}
	httpClient := &http.Client{Timeout: DefaultAPITimeout, Transport: readOnlyTransport{base: http.DefaultTransport}}
	z := newZeropsClientOn(access.Token, resolveEndpoint(access.APIHost), httpClient)
	info, err := z.GetUserInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("org mode: read the token's organizations: %w", err)
	}
	reach := make([]string, 0, len(info.Memberships))
	for _, m := range info.Memberships {
		reach = append(reach, m.ClientID)
	}
	orgID, err := resolveOrgID(reach, named)
	if err != nil {
		return nil, err
	}
	return &OrgReader{z: z, orgID: orgID}, nil
}

// OrgRead is one Zerops API read org mode makes: an HTTP method and a path
// under the public API root (/api/rest/public), {id} standing for one id.
type OrgRead struct {
	Method string
	Path   string
}

// OrgReads is every read org mode makes through ZCP_ORG_API — exactly
// these, pinned by TestOrgReads_AreExactlyWhatOrgModeReads. It is the
// allowlist HQ's Chief proxy forwards; a read added to org mode is added here
// and to the proxy together. Every search carries the org's clientId, except
// the service-type catalogue, which is the platform's and no organization's.
// GET /chief/person is HQ's own answer, no Zerops read. Through the region's
// API the token's organization and its person are read from GET /user/info,
// which is not one of them.
func OrgReads() []OrgRead {
	return []OrgRead{
		{Method: http.MethodPost, Path: "/project/search"},
		{Method: http.MethodPost, Path: "/service-stack/search"},
		{Method: http.MethodPost, Path: "/process/search"},
		{Method: http.MethodPost, Path: "/app-version/search"},
		{Method: http.MethodPost, Path: "/service-stack-type/search"},
		{Method: http.MethodPost, Path: "/public-port-routing/search"},
		{Method: http.MethodPost, Path: "/transaction-debit/cost-search"},
		{Method: http.MethodPost, Path: "/transaction-debit/group-by-search"},
		{Method: http.MethodPost, Path: "/stats-history/group-by-search"},
		{Method: http.MethodGet, Path: "/chief/person"},
		{Method: http.MethodGet, Path: "/service-stack/{id}/env"},
		{Method: http.MethodGet, Path: "/project/{id}/log"},
		{Method: http.MethodGet, Path: "/project/{id}/public-http-routing"},
	}
}

// publicAPIRoot is where the SDK roots every Zerops API path.
const publicAPIRoot = "/api/rest/public"

// parseOrgAPI reads ZCP_ORG_API: an absolute http(s) URL.
func parseOrgAPI(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, NewPlatformError(ErrInvalidParameter,
			fmt.Sprintf("ZCP_ORG_API %q is not an http(s) URL", raw),
			"Set ZCP_ORG_API to HQ's Chief API, e.g. https://hq.example/api/chief/zerops")
	}
	return u, nil
}

// orgAPITransport sends a Zerops API request to ZCP_ORG_API instead: the
// path under the public API root lands under the base URL's path, the query
// and the bearer travel unchanged. A request outside the root is refused.
type orgAPITransport struct {
	base *url.URL
	next http.RoundTripper
}

func (t orgAPITransport) RoundTrip(r *http.Request) (*http.Response, error) {
	rest, ok := strings.CutPrefix(r.URL.Path, publicAPIRoot+"/")
	if !ok {
		if r.Body != nil {
			_ = r.Body.Close()
		}
		return nil, NewPlatformError(ErrOrgWriteRefused,
			fmt.Sprintf("org mode refused %s %s: not a Zerops API read", r.Method, r.URL.Path), "")
	}
	out := r.Clone(r.Context())
	u := *t.base
	u.Path = strings.TrimSuffix(t.base.Path, "/") + "/" + rest
	u.RawPath = ""
	u.RawQuery = r.URL.RawQuery
	out.URL = &u
	out.Host = u.Host
	return t.next.RoundTrip(out)
}

// resolveOrgID picks the organization org mode reads from those the token
// reaches.
func resolveOrgID(reach []string, named string) (string, error) {
	if named != "" {
		if slices.Contains(reach, named) {
			return named, nil
		}
		return "", NewPlatformError(ErrOrgNotReachable,
			fmt.Sprintf("ZCP_ORG_ID %s is not an organization ZCP_ORG_TOKEN reaches", named),
			"Set ZCP_ORG_ID to the organization the token was made for, or use that organization's token")
	}
	switch len(reach) {
	case 0:
		return "", NewPlatformError(ErrOrgNotReachable,
			"ZCP_ORG_TOKEN reaches no organization",
			"Use a token made for the organization (read only)")
	case 1:
		return reach[0], nil
	default:
		return "", NewPlatformError(ErrOrgAmbiguous,
			fmt.Sprintf("ZCP_ORG_TOKEN reaches %d organizations and ZCP_ORG_ID names none", len(reach)),
			"Set ZCP_ORG_ID to the organization this Chief is for")
	}
}

// readOnlyTransport lets through what reads: a GET, or a POST to a search
// endpoint (the API's searches are POSTs: …/search, …/cost-search,
// …/group-by-search). Anything else is refused here, so it never reaches the
// network.
type readOnlyTransport struct{ base http.RoundTripper }

func (t readOnlyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	switch {
	case r.Method == http.MethodGet, r.Method == http.MethodHead:
	case r.Method == http.MethodPost && isSearchPath(r.URL.Path):
	default:
		if r.Body != nil {
			_ = r.Body.Close()
		}
		return nil, NewPlatformError(ErrOrgWriteRefused,
			fmt.Sprintf("org mode refused %s %s: it only reads", r.Method, r.URL.Path),
			"Propose the change; the person approves it and their client makes it")
	}
	return t.base.RoundTrip(r)
}

// isSearchPath reports a path whose last segment is a search: search, or a
// name ending in -search.
func isSearchPath(path string) bool {
	path = strings.TrimSuffix(path, "/")
	last := path[strings.LastIndex(path, "/")+1:]
	return last == "search" || (strings.HasSuffix(last, "-search") && last != "-search")
}

// OrgID is the organization this reader is scoped to.
func (r *OrgReader) OrgID() string { return r.orgID }

// orgFilter is the clientId term every org search carries, plus extra terms.
func (r *OrgReader) orgFilter(limit int, terms ...body.EsSearchItem) body.EsFilter {
	search := body.EsFilterSearch{eq("clientId", r.orgID)}
	search = append(search, terms...)
	return body.EsFilter{
		Search: search,
		Sort:   body.EsFilterSort{body.EsSortItem{Name: types.NewString("created"), Ascending: types.NewBoolNull(false)}},
		Limit:  types.NewIntNull(limit),
	}
}

func eq(name, value string) body.EsSearchItem {
	return body.EsSearchItem{Name: types.NewString(name), Operator: types.NewString("eq"), Value: types.NewString(value)}
}

// ListProjects is every project of the organization, with its tags.
func (r *OrgReader) ListProjects(ctx context.Context) ([]Project, error) {
	resp, err := r.z.handler.PostProjectSearch(ctx, r.orgFilter(orgSearchPage))
	if err != nil {
		return nil, mapSDKError(err, "project")
	}
	out, err := resp.Output()
	if err != nil {
		return nil, mapSDKError(err, "project")
	}
	projects := make([]Project, 0, len(out.Items))
	for _, p := range out.Items {
		if p.ClientId.TypedString().String() != r.orgID {
			continue
		}
		description, _ := p.Description.Get()
		projects = append(projects, Project{
			ID:          p.Id.TypedString().String(),
			Name:        p.Name.String(),
			Status:      p.Status.String(),
			Description: description.Native(),
			Tags:        p.TagList.Native(),
		})
	}
	return projects, nil
}

// ListServices is the organization's services, or one project's when
// projectID is set, paged past the search's page size.
func (r *OrgReader) ListServices(ctx context.Context, projectID string) ([]ServiceStack, error) {
	var terms []body.EsSearchItem
	if projectID != "" {
		terms = append(terms, eq("projectId", projectID))
	}
	var services []ServiceStack
	for offset := 0; ; offset += orgSearchPage {
		filter := r.orgFilter(orgSearchPage, terms...)
		filter.Offset = types.NewIntNull(offset)
		resp, err := r.z.handler.PostServiceStackSearch(ctx, filter)
		if err != nil {
			return nil, mapSDKError(err, "service")
		}
		out, err := resp.Output()
		if err != nil {
			return nil, mapSDKError(err, "service")
		}
		for _, s := range out.Items {
			if s.ClientId.TypedString().String() != r.orgID {
				continue
			}
			services = append(services, mapEsServiceStack(s))
		}
		if len(out.Items) < orgSearchPage {
			return services, nil
		}
	}
}

// GetProjectEnv is a project's variables, read through the org-scoped
// project search (the project's env list rides on it).
func (r *OrgReader) GetProjectEnv(ctx context.Context, projectID string) ([]ProjectEnvVar, error) {
	resp, err := r.z.handler.PostProjectSearch(ctx, r.orgFilter(1, eq("id", projectID)))
	if err != nil {
		return nil, mapSDKError(err, "project")
	}
	out, err := resp.Output()
	if err != nil {
		return nil, mapSDKError(err, "project")
	}
	var project *output.EsProject
	for i := range out.Items {
		if out.Items[i].ClientId.TypedString().String() == r.orgID {
			project = &out.Items[i]
			break
		}
	}
	if project == nil {
		return nil, nil
	}
	envs := make([]ProjectEnvVar, 0, len(project.EnvList))
	for _, e := range project.EnvList {
		envs = append(envs, ProjectEnvVar{
			ID:        e.Id.TypedString().String(),
			Key:       e.Key.String(),
			Content:   string(e.Content),
			Type:      ProjectEnvType(e.Type),
			Sensitive: bool(e.Sensitive),
			Editable:  bool(e.Editable),
		})
	}
	return envs, nil
}

// GetServiceEnv is a service's variables (a GET by id).
func (r *OrgReader) GetServiceEnv(ctx context.Context, serviceID string) ([]ServiceEnvVar, error) {
	return r.z.GetServiceEnv(ctx, serviceID)
}

// GetProjectLog is a project's log-backend access (a GET by id).
func (r *OrgReader) GetProjectLog(ctx context.Context, projectID string) (*LogAccess, error) {
	return r.z.GetProjectLog(ctx, projectID)
}

// ListPublicHTTPRoutings is a project's custom-domain routings (a GET by id).
func (r *OrgReader) ListPublicHTTPRoutings(ctx context.Context, projectID string) ([]PublicHTTPRouting, error) {
	return r.z.ListPublicHTTPRoutings(ctx, projectID)
}

// SearchProcesses is the organization's newest platform processes, or one
// project's when projectID is set.
func (r *OrgReader) SearchProcesses(ctx context.Context, projectID string, limit int) ([]ProcessEvent, error) {
	var terms []body.EsSearchItem
	if projectID != "" {
		terms = append(terms, eq("projectId", projectID))
	}
	resp, err := r.z.handler.PostProcessSearch(ctx, r.orgFilter(limit, terms...))
	if err != nil {
		return nil, mapSDKError(err, "process")
	}
	out, err := resp.Output()
	if err != nil {
		return nil, mapSDKError(err, "process")
	}
	events := make([]ProcessEvent, 0, len(out.Items))
	for _, p := range out.Items {
		if p.ClientId.TypedString().String() != r.orgID {
			continue
		}
		events = append(events, mapEsProcessEvent(p))
	}
	return events, nil
}

// SearchAppVersions is the organization's newest builds and deploys, or one
// project's when projectID is set.
func (r *OrgReader) SearchAppVersions(ctx context.Context, projectID string, limit int) ([]AppVersionEvent, error) {
	var terms []body.EsSearchItem
	if projectID != "" {
		terms = append(terms, eq("projectId", projectID))
	}
	resp, err := r.z.handler.PostAppVersionSearch(ctx, r.orgFilter(limit, terms...))
	if err != nil {
		return nil, mapSDKError(err, "service")
	}
	out, err := resp.Output()
	if err != nil {
		return nil, mapSDKError(err, "service")
	}
	events := make([]AppVersionEvent, 0, len(out.Items))
	for _, av := range out.Items {
		if av.ClientId.TypedString().String() != r.orgID {
			continue
		}
		events = append(events, mapEsAppVersionEvent(av))
	}
	return events, nil
}

// ListActiveServiceTypeVersions is the platform's current service-type
// catalogue — not the org's data; the knowledge tool's schema overlay.
func (r *OrgReader) ListActiveServiceTypeVersions(ctx context.Context) ([]string, error) {
	return r.z.ActiveServiceTypeVersions(ctx)
}
