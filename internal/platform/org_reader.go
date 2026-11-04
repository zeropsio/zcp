package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/zeropsio/zerops-go/dto/input/body"
	"github.com/zeropsio/zerops-go/types"
)

// OrgReader is org mode's only door to the organization (docs/spec-mate.md
// §2.11, OM-4): the Chief reads the Zerops API with the credential its
// session holds — the person's own token while a person drives it, the
// project's key otherwise — and never writes, and never asks HQ. Zerops cuts
// every answer to what that credential may see and refuses the rest; the
// reader shows a refusal in Zerops' words and asks once. It reads the
// allowlist and nothing else: the organization's projects and services with
// their state, and its members with their roles. Two things hold the reader
// to reads in code rather than in prose:
//
//   - the method set is reads only, pinned name by name by
//     TestOrgReader_ExposesNoWriteMethod — a write added here fails it;
//   - every request leaves through readOnlyTransport, which refuses anything
//     but a GET or a search POST before it reaches the network, so even a
//     read method that grew a write underneath it could not send one.
type OrgReader struct {
	z *ZeropsClient
	// projectID is the Chief's own project: the organization that owns it is
	// the one read.
	projectID string
}

// orgSearchPage bounds one search request; ListServices pages past it.
const orgSearchPage = 1000

// OrgAccess is what org mode opens its reader with.
type OrgAccess struct {
	// APIHost is the Zerops API host.
	APIHost string
	// ProjectID is the Chief's own project.
	ProjectID string
	// Token is the credential the session holds, sent as the bearer of every
	// read and nowhere else.
	Token string
}

// OpenOrgReader builds the read-only Zerops client of org mode. It makes no
// call: the first read finds the organization.
func OpenOrgReader(access OrgAccess) (*OrgReader, error) {
	token := strings.TrimSpace(access.Token)
	if token == "" {
		return nil, NewPlatformError(ErrInvalidParameter, "org mode has no Zerops credential to read with",
			"Set ZCP_API_KEY on the zcp service")
	}
	projectID := strings.TrimSpace(access.ProjectID)
	if projectID == "" {
		return nil, NewPlatformError(ErrInvalidParameter, "org mode does not know its own project",
			"Run zcp in the Chief's own Zerops service, which sets projectId")
	}
	httpClient := &http.Client{Timeout: DefaultAPITimeout, Transport: readOnlyTransport{base: http.DefaultTransport}}
	return &OrgReader{z: newZeropsClientOn(token, resolveEndpoint(access.APIHost), httpClient), projectID: projectID}, nil
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

// searchFilter is a search of at most limit items of the organization.
func searchFilter(orgID string, limit int, terms ...body.EsSearchItem) body.EsFilter {
	return body.EsFilter{
		Search: append(body.EsFilterSearch{eq("clientId", orgID)}, terms...),
		Sort:   body.EsFilterSort{body.EsSortItem{Name: types.NewString("created"), Ascending: types.NewBoolNull(false)}},
		Limit:  types.NewIntNull(limit),
	}
}

func eq(name, value string) body.EsSearchItem {
	return body.EsSearchItem{Name: types.NewString(name), Operator: types.NewString("eq"), Value: types.NewString(value)}
}

// orgID is the organization that owns the Chief's own project, read once
// and kept for the session.
func (r *OrgReader) orgID(ctx context.Context) (string, error) {
	r.z.mu.Lock()
	bound := r.z.cachedID
	r.z.mu.Unlock()
	if bound != "" {
		return bound, nil
	}
	project, err := r.z.GetProject(ctx, r.projectID)
	if err != nil {
		return "", fmt.Errorf("read the Chief's own project: %w", err)
	}
	r.z.BindOrg(project.ClientID)
	return project.ClientID, nil
}

// ListProjects is every project of the organization the credential sees,
// with its tags.
func (r *OrgReader) ListProjects(ctx context.Context) ([]Project, error) {
	orgID, err := r.orgID(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := r.z.handler.PostProjectSearch(ctx, searchFilter(orgID, orgSearchPage))
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
			ClientID:    p.ClientId.TypedString().String(),
			Description: description.Native(),
			Tags:        p.TagList.Native(),
		})
	}
	return projects, nil
}

// ListServices is the services of the organization the credential sees, or
// one project's when projectID is set, paged past the search's page size.
func (r *OrgReader) ListServices(ctx context.Context, projectID string) ([]ServiceStack, error) {
	orgID, err := r.orgID(ctx)
	if err != nil {
		return nil, err
	}
	var terms []body.EsSearchItem
	if projectID != "" {
		terms = append(terms, eq("projectId", projectID))
	}
	var services []ServiceStack
	for offset := 0; ; offset += orgSearchPage {
		filter := searchFilter(orgID, orgSearchPage, terms...)
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

// OverviewMember is a person of the organization with the role Zerops gives
// them.
type OverviewMember struct {
	UserID   string `json:"userId"`
	FullName string `json:"fullName"`
	Role     string `json:"role"`
	Status   string `json:"status"`
}

// tokenEmail is an integration token's own address on the member list
// (token-<id>@zerops.io): the list carries every token as a member, and a
// token is no person.
var tokenEmail = regexp.MustCompile(`(?i)^token-[^@]+@zerops\.io$`)

// ListMembers is the organization's people and their roles: one read of
// GET /client/{org}/user/list.
func (r *OrgReader) ListMembers(ctx context.Context) ([]OverviewMember, error) {
	orgID, err := r.orgID(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := r.z.directGet(ctx, "/api/rest/public/client/"+url.PathEscape(orgID)+"/user/list", "org members")
	if err != nil {
		return nil, err
	}
	var out struct {
		ClientUserList []struct {
			UserID   string `json:"userId"`
			Status   string `json:"status"`
			RoleCode string `json:"roleCode"`
			User     struct {
				Email    string `json:"email"`
				FullName string `json:"fullName"`
			} `json:"user"`
		} `json:"clientUserList"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, withCause(NewPlatformError(ErrAPIError, "org members: malformed response",
			"Retry; if it persists the platform API changed — report it"), err)
	}
	members := make([]OverviewMember, 0, len(out.ClientUserList))
	for _, m := range out.ClientUserList {
		if tokenEmail.MatchString(m.User.Email) {
			continue
		}
		members = append(members, OverviewMember{UserID: m.UserID, FullName: m.User.FullName, Role: m.RoleCode, Status: m.Status})
	}
	return members, nil
}
