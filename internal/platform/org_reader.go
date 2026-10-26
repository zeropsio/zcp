package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/zeropsio/zerops-go/dto/input/body"
	"github.com/zeropsio/zerops-go/types"
)

// OrgReader is org mode's only door to the organization (docs/spec-mate.md
// §2.11, §3.11): the Chief reads HQ's overview with its project key and never
// writes. HQ answers from its organization view, cut by kind of data to
// projects, services and their state, and members with their roles, and only
// while the Chief the caller answers to is its own project. Two things hold the reader
// to reads in code rather than in prose:
//
//   - the method set is reads only, pinned name by name by
//     TestOrgReader_ExposesNoWriteMethod — a write added here fails it;
//   - every request leaves through readOnlyTransport, which refuses anything
//     but a GET or a search POST before it reaches the network, so even a
//     read method that grew a write underneath it could not send one.
//
// HQ pins every search to the organization and drops what is not its own, so
// the reader sends no organization of its own.
type OrgReader struct {
	z *ZeropsClient
	// members reads the members, which is no Zerops API call.
	members *http.Client
	origin  string
}

// orgSearchPage bounds one search request; ListServices pages past it.
const orgSearchPage = 1000

// Paths of HQ's Chief overview (mate-hq spec/protocol.md "Chief"): exact,
// under HQ's origin.
const (
	overviewZeropsRoot = "/api/chief/zerops"
	overviewMembers    = "/api/chief/members"
)

// publicAPIRoot is where the SDK roots every Zerops API path.
const publicAPIRoot = "/api/rest/public"

// OrgAccess is what org mode opens its reader with.
type OrgAccess struct {
	// Origin is HQ's origin, an absolute http(s) URL.
	Origin string
	// Key is the project's ZCP_API_KEY, sent in the Authorization header of
	// every read and nowhere else.
	Key string
}

// OpenOrgReader builds the read-only client of HQ's overview. It makes no
// call: the first read finds out whether this project is still a Chief.
func OpenOrgReader(access OrgAccess) (*OrgReader, error) {
	base, err := parseOrigin(access.Origin)
	if err != nil {
		return nil, err
	}
	key := strings.TrimSpace(access.Key)
	if key == "" {
		return nil, NewPlatformError(ErrInvalidParameter, "org mode has no project key to present to HQ",
			"Set ZCP_API_KEY on the zcp service")
	}
	keyed := projectKeyTransport{key: key, next: http.DefaultTransport}
	sdkClient := &http.Client{
		Timeout:   DefaultAPITimeout,
		Transport: overviewTransport{base: base, next: readOnlyTransport{base: keyed}},
	}
	return &OrgReader{
		z:       newZeropsClientOn(key, base.Scheme+"://"+base.Host+"/", sdkClient),
		members: &http.Client{Timeout: DefaultAPITimeout, Transport: readOnlyTransport{base: keyed}},
		origin:  strings.TrimRight(base.String(), "/"),
	}, nil
}

// OrgRead is one request org mode makes: an HTTP method and the path under
// HQ's origin.
type OrgRead struct {
	Method string
	Path   string
}

// OrgReads is every read org mode makes — exactly these, pinned by
// TestOrgReads_AreExactlyWhatOrgModeReads: the organization's projects and
// services with their state, and its members with their roles
// (docs/spec-mate.md §3.11). Nothing else of the organization is read: no
// variable, log, cost, process, routing or token list.
func OrgReads() []OrgRead {
	return []OrgRead{
		{Method: http.MethodPost, Path: overviewZeropsRoot + "/project/search"},
		{Method: http.MethodPost, Path: overviewZeropsRoot + "/service-stack/search"},
		{Method: http.MethodGet, Path: overviewMembers},
	}
}

// parseOrigin reads HQ's origin: an absolute http(s) URL.
func parseOrigin(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, NewPlatformError(ErrInvalidParameter,
			fmt.Sprintf("HQ's origin %q is not an http(s) URL", raw),
			"Set T3CODE_ZEROPS_HQ_ORIGIN to HQ's origin, e.g. https://hq.example")
	}
	return u, nil
}

// overviewTransport sends the SDK's Zerops API request to HQ's overview
// instead: the path under the public API root lands under HQ's overview root,
// the query travels unchanged. A request outside the root is refused.
type overviewTransport struct {
	base *url.URL
	next http.RoundTripper
}

func (t overviewTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	rest, ok := strings.CutPrefix(r.URL.Path, publicAPIRoot+"/")
	if !ok {
		if r.Body != nil {
			_ = r.Body.Close()
		}
		return nil, NewPlatformError(ErrOrgWriteRefused,
			fmt.Sprintf("org mode refused %s %s: not an overview read", r.Method, r.URL.Path), "")
	}
	out := r.Clone(r.Context())
	u := *t.base
	u.Path = strings.TrimSuffix(t.base.Path, "/") + overviewZeropsRoot + "/" + rest
	u.RawPath = ""
	u.RawQuery = r.URL.RawQuery
	out.URL = &u
	out.Host = u.Host
	return t.next.RoundTrip(out)
}

// projectKeyTransport presents the project key the way every call to HQ does,
// "Authorization: Project <key>", replacing whatever the SDK put there.
type projectKeyTransport struct {
	key  string
	next http.RoundTripper
}

func (t projectKeyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	out := r.Clone(r.Context())
	out.Header.Set("Authorization", "Project "+t.key)
	return t.next.RoundTrip(out)
}

// readOnlyTransport lets through what reads: a GET, or a POST to a search
// endpoint (the API's searches are POSTs: …/search). Anything else is refused
// here, so it never reaches the network.
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

// searchFilter is a search of at most limit items; HQ adds the organization.
func searchFilter(limit int, terms ...body.EsSearchItem) body.EsFilter {
	return body.EsFilter{
		Search: append(body.EsFilterSearch{}, terms...),
		Sort:   body.EsFilterSort{body.EsSortItem{Name: types.NewString("created"), Ascending: types.NewBoolNull(false)}},
		Limit:  types.NewIntNull(limit),
	}
}

func eq(name, value string) body.EsSearchItem {
	return body.EsSearchItem{Name: types.NewString(name), Operator: types.NewString("eq"), Value: types.NewString(value)}
}

// ListProjects is every project of the organization, with its tags.
func (r *OrgReader) ListProjects(ctx context.Context) ([]Project, error) {
	resp, err := r.z.handler.PostProjectSearch(ctx, searchFilter(orgSearchPage))
	if err != nil {
		return nil, mapSDKError(err, "project")
	}
	out, err := resp.Output()
	if err != nil {
		return nil, mapSDKError(err, "project")
	}
	projects := make([]Project, 0, len(out.Items))
	for _, p := range out.Items {
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
		filter := searchFilter(orgSearchPage, terms...)
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
			services = append(services, mapEsServiceStack(s))
		}
		if len(out.Items) < orgSearchPage {
			return services, nil
		}
	}
}

// OverviewMember is a person of the organization with the role Zerops gives them,
// as HQ's overview lists them.
type OverviewMember struct {
	UserID   string `json:"userId"`
	FullName string `json:"fullName"`
	Role     string `json:"role"`
	Status   string `json:"status"`
}

// ListMembers is the organization's people and their roles.
func (r *OrgReader) ListMembers(ctx context.Context) ([]OverviewMember, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.origin+overviewMembers, nil)
	if err != nil {
		return nil, fmt.Errorf("read the organization's members: %w", err)
	}
	resp, err := r.members.Do(req)
	if err != nil {
		return nil, fmt.Errorf("read the organization's members: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read the organization's members: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		var refusal struct {
			Code   string `json:"code"`
			Reason string `json:"reason"`
		}
		_ = json.Unmarshal(raw, &refusal)
		return nil, NewPlatformError(ErrAPIError,
			fmt.Sprintf("HQ refused the members read: %d %s %s", resp.StatusCode, refusal.Code, refusal.Reason), "")
	}
	var answer struct {
		Members []OverviewMember `json:"members"`
	}
	if err := json.Unmarshal(raw, &answer); err != nil {
		return nil, fmt.Errorf("read the organization's members: malformed answer: %w", err)
	}
	return answer.Members, nil
}
