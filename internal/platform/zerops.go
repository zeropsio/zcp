package platform

import (
	"context"
	"net/http"
	"strings"
	"sync"

	"github.com/zeropsio/zerops-go/dto/input/body"
	"github.com/zeropsio/zerops-go/dto/input/path"
	"github.com/zeropsio/zerops-go/sdk"
	"github.com/zeropsio/zerops-go/sdkBase"
	"github.com/zeropsio/zerops-go/types"
	"github.com/zeropsio/zerops-go/types/uuid"
)

// Compile-time interface check.
var _ Client = (*ZeropsClient)(nil)

// defaultAPIHost is the production Zerops REST endpoint used when callers
// don't supply an explicit host. Single source of truth for the empty-host
// fallback — every constructor that wraps NewZeropsClient inherits it
// rather than duplicating the constant.
const defaultAPIHost = "api.app-prg1.zerops.io"

// ZeropsClient implements the Client interface using the zerops-go SDK.
type ZeropsClient struct {
	handler  sdk.Handler
	mu       sync.Mutex // guards cachedID lazy init with retry on error
	cachedID string
}

// NewZeropsClient creates a new ZeropsClient authenticated with the given token.
// Empty apiHost falls back to defaultAPIHost — without this the SDK endpoint
// becomes the literal "https://" (no host), and every subsequent request fails
// with `http: no Host in request URL`.
func NewZeropsClient(token, apiHost string) (*ZeropsClient, error) {
	endpoint := resolveEndpoint(apiHost)

	httpClient := &http.Client{Timeout: DefaultAPITimeout}
	config := sdkBase.DefaultConfig(sdkBase.WithCustomEndpoint(endpoint))
	handler := sdk.New(config, httpClient)
	handler = sdk.AuthorizeSdk(handler, token)

	return &ZeropsClient{handler: handler}, nil
}

// resolveEndpoint normalizes the apiHost argument into the SDK endpoint
// URL. Empty input falls back to defaultAPIHost; missing scheme gets
// "https://"; missing trailing slash gets one appended. Extracted so the
// empty-host fallback can be pinned in unit tests without standing up a
// mock HTTP server.
func resolveEndpoint(apiHost string) string {
	endpoint := apiHost
	if endpoint == "" {
		endpoint = defaultAPIHost
	}
	if !strings.HasPrefix(endpoint, "http") {
		endpoint = "https://" + endpoint
	}
	if !strings.HasSuffix(endpoint, "/") {
		endpoint += "/"
	}
	return endpoint
}

// getClientID returns the cached clientId, fetching it once on the first
// cold-cache call. On success the ID is cached permanently (it never changes
// for a session); failures are not cached, so the next call retries.
//
// The GetUserInfo round-trip runs WITHOUT the lock held: holding a mutex
// across a 30s API call would serialize every concurrent first-call behind
// one slow request (CLAUDE.md "never hold mutexes during I/O"). The small
// cost is a rare duplicate fetch when several goroutines race the cold
// cache — harmless, since GetUserInfo is an idempotent read and the stored
// value is identical.
func (z *ZeropsClient) getClientID(ctx context.Context) (string, error) {
	z.mu.Lock()
	cached := z.cachedID
	z.mu.Unlock()
	if cached != "" {
		return cached, nil
	}

	info, err := z.GetUserInfo(ctx)
	if err != nil {
		return "", err
	}

	z.mu.Lock()
	z.cachedID = info.ID
	z.mu.Unlock()
	return info.ID, nil
}

// ---------------------------------------------------------------------------
// Auth
// ---------------------------------------------------------------------------

func (z *ZeropsClient) GetUserInfo(ctx context.Context) (*UserInfo, error) {
	resp, err := z.handler.GetUserInfo(ctx)
	if err != nil {
		return nil, mapSDKError(err, "auth")
	}
	out, err := resp.Output()
	if err != nil {
		return nil, mapSDKError(err, "auth")
	}

	clientID := ""
	clientUserID := ""
	if len(out.ClientUserList) > 0 {
		clientID = out.ClientUserList[0].ClientId.TypedString().String()
		// ClientUserId is the link between user and client — distinct
		// from both userId and clientId. Used by project.userRoles[].id
		// at launch-production import time to grant per-project ADMIN
		// on the freshly-created project (A.10 spike finding).
		clientUserID = out.ClientUserList[0].Id.TypedString().String()
	}

	return &UserInfo{
		ID:           clientID,
		ClientUserID: clientUserID,
		Email:        out.Email.Native(),
		FullName:     out.FullName.String(),
	}, nil
}

// ---------------------------------------------------------------------------
// Project discovery
// ---------------------------------------------------------------------------

func (z *ZeropsClient) ListProjects(ctx context.Context, clientID string) ([]Project, error) {
	filter := body.EsFilter{
		Search: body.EsFilterSearch{
			body.EsSearchItem{
				Name:     types.NewString("clientId"),
				Operator: types.NewString("eq"),
				Value:    types.NewString(clientID),
			},
		},
		Sort: body.EsFilterSort{},
	}

	resp, err := z.handler.PostProjectSearch(ctx, filter)
	if err != nil {
		return nil, mapSDKError(err, "project")
	}
	out, err := resp.Output()
	if err != nil {
		return nil, mapSDKError(err, "project")
	}

	projects := make([]Project, 0, len(out.Items))
	for _, p := range out.Items {
		projects = append(projects, Project{
			ID:     p.Id.TypedString().String(),
			Name:   p.Name.String(),
			Status: p.Status.String(),
		})
	}
	return projects, nil
}

func (z *ZeropsClient) GetProject(ctx context.Context, projectID string) (*Project, error) {
	pathParam := path.ProjectId{Id: uuid.ProjectId(projectID)}
	resp, err := z.handler.GetProject(ctx, pathParam)
	if err != nil {
		return nil, mapSDKError(err, "project")
	}
	out, err := resp.Output()
	if err != nil {
		return nil, mapSDKError(err, "project")
	}

	subdomainHost := ""
	if sh, ok := out.ZeropsSubdomainHost.Get(); ok {
		subdomainHost = sh.String()
	}

	return &Project{
		ID:            out.Id.TypedString().String(),
		Name:          out.Name.String(),
		Status:        out.Status.String(),
		SubdomainHost: subdomainHost,
	}, nil
}

// ---------------------------------------------------------------------------
// Service discovery
// ---------------------------------------------------------------------------

func (z *ZeropsClient) ListServices(ctx context.Context, projectID string) ([]ServiceStack, error) {
	clientID, err := z.getClientID(ctx)
	if err != nil {
		return nil, err
	}

	// Scope to the project server-side. The clientId-only filter spans the
	// whole account; on the org-wide launch key (projectAdminClient) that
	// means every service in every project, which the server default page
	// size can truncate before the client-side projectID filter below runs.
	// zcli sends the same projectId term on service-stack search.
	filter := body.EsFilter{
		Search: body.EsFilterSearch{
			body.EsSearchItem{
				Name:     types.NewString("clientId"),
				Operator: types.NewString("eq"),
				Value:    types.NewString(clientID),
			},
			body.EsSearchItem{
				Name:     types.NewString("projectId"),
				Operator: types.NewString("eq"),
				Value:    types.NewString(projectID),
			},
		},
		Sort: body.EsFilterSort{},
	}

	resp, err := z.handler.PostServiceStackSearch(ctx, filter)
	if err != nil {
		return nil, mapSDKError(err, "service")
	}
	out, err := resp.Output()
	if err != nil {
		return nil, mapSDKError(err, "service")
	}

	services := make([]ServiceStack, 0, len(out.Items))
	for _, s := range out.Items {
		svc := mapEsServiceStack(s)
		if svc.ProjectID == projectID {
			services = append(services, svc)
		}
	}
	return services, nil
}

func (z *ZeropsClient) GetService(ctx context.Context, serviceID string) (*ServiceStack, error) {
	pathParam := path.ServiceStackId{Id: uuid.ServiceStackId(serviceID)}
	resp, err := z.handler.GetServiceStack(ctx, pathParam)
	if err != nil {
		return nil, mapSDKError(err, "service")
	}
	out, err := resp.Output()
	if err != nil {
		return nil, mapSDKError(err, "service")
	}
	svc := mapFullServiceStack(out)
	return &svc, nil
}
