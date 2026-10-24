package platform

import (
	"context"
	"fmt"
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
	handler sdk.Handler
	// env is an authorized sdkBase.Environment mirroring what the SDK
	// builds internally (sdk.New + sdk.AuthorizeSdk), kept alongside the
	// handler for hand-rolled calls the SDK doesn't cover yet (the
	// delegation list — see zerops_delegation.go). handler.environment is
	// unexported, so this is the only way to reuse the same host
	// normalization + bearer auth without opening a parallel raw
	// net/http path.
	env sdkBase.Environment

	mu            sync.Mutex // guards cachedID/cachedTokenID lazy init with retry on error
	cachedID      string
	cachedTokenID string
}

// NewZeropsClient creates a new ZeropsClient authenticated with the given token.
// Empty apiHost falls back to defaultAPIHost — without this the SDK endpoint
// becomes the literal "https://" (no host), and every subsequent request fails
// with `http: no Host in request URL`.
func NewZeropsClient(token, apiHost string) (*ZeropsClient, error) {
	return newZeropsClientOn(token, resolveEndpoint(apiHost), &http.Client{Timeout: DefaultAPITimeout}), nil
}

// NewProjectClient is NewZeropsClient for a known project (a container's
// injected projectId): it reads the project and binds the client to the
// org that owns it, as auth.Resolve does for the CLI, so a key in several
// orgs still targets the project's.
func NewProjectClient(ctx context.Context, token, apiHost, projectID string) (*ZeropsClient, error) {
	z, err := NewZeropsClient(token, apiHost)
	if err != nil {
		return nil, err
	}
	project, err := z.GetProject(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("read the project's organization: %w", err)
	}
	z.BindOrg(project.ClientID)
	return z, nil
}

// newZeropsClientOn builds the client on a given HTTP client — the org
// reader hands it one whose transport only reads (OpenOrgReader).
func newZeropsClientOn(token, endpoint string, httpClient *http.Client) *ZeropsClient {
	config := sdkBase.DefaultConfig(sdkBase.WithCustomEndpoint(endpoint))
	handler := sdk.New(config, httpClient)
	handler = sdk.AuthorizeSdk(handler, token)
	env := sdkBase.NewEnvironment(config, httpClient).Authorize(token)
	return &ZeropsClient{handler: handler, env: env}
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

// BindOrg makes clientID — the org owning the project this client works
// on — the org every clientId-filtered call targets (service, env,
// process and appVersion searches, delegations). auth.Resolve binds it
// from the resolved project, so every entry point targets the project's
// org without a step of its own.
func (z *ZeropsClient) BindOrg(clientID string) {
	z.mu.Lock()
	z.cachedID = clientID
	z.mu.Unlock()
}

// OrgID returns the org clientId-filtered calls target: the bound
// one (BindOrg), else the token's only membership, fetched once and
// cached. A token in several orgs with no bound project has no answer —
// ErrOrgAmbiguous, never clientUserList[0], which need not own the
// project and turns every search empty. Failures are not cached, so the
// next call retries.
//
// The GetUserInfo round-trip runs WITHOUT the lock held: holding a mutex
// across a 30s API call would serialize every concurrent first-call behind
// one slow request (CLAUDE.md "never hold mutexes during I/O"). The small
// cost is a rare duplicate fetch when several goroutines race the cold
// cache — harmless, since GetUserInfo is an idempotent read and the stored
// value is identical.
func (z *ZeropsClient) OrgID(ctx context.Context) (string, error) {
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
	orgID, err := soleOrg(info.Memberships)
	if err != nil {
		return "", err
	}

	z.mu.Lock()
	defer z.mu.Unlock()
	if z.cachedID != "" {
		// A BindOrg (or a racing fetch) landed during the unlocked read.
		return z.cachedID, nil
	}
	z.cachedID = orgID
	return orgID, nil
}

// getTokenID returns the cached own-token id, fetching it once on the first
// cold-cache call. Sibling of OrgID — same mutex/retry-on-error
// pattern, same "never hold the lock across I/O" discipline (CLAUDE.md).
// F1: for an integration token, /user/info's id IS the {tokenId} used in
// delegation paths.
func (z *ZeropsClient) getTokenID(ctx context.Context) (string, error) {
	z.mu.Lock()
	cached := z.cachedTokenID
	z.mu.Unlock()
	if cached != "" {
		return cached, nil
	}

	info, err := z.GetUserInfo(ctx)
	if err != nil {
		return "", err
	}

	z.mu.Lock()
	z.cachedTokenID = info.UserID
	z.mu.Unlock()
	return info.UserID, nil
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

	memberships := make([]ClientMembership, 0, len(out.ClientUserList))
	for _, cu := range out.ClientUserList {
		memberships = append(memberships, ClientMembership{
			ClientID:     cu.ClientId.TypedString().String(),
			ClientUserID: cu.Id.TypedString().String(),
		})
	}

	return &UserInfo{
		Memberships: memberships,
		UserID:      out.Id.TypedString().String(),
		Email:       out.Email.Native(),
		FullName:    out.FullName.String(),
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
		description, _ := p.Description.Get()
		projects = append(projects, Project{
			ID:          p.Id.TypedString().String(),
			Name:        p.Name.String(),
			Status:      p.Status.String(),
			ClientID:    p.ClientId.TypedString().String(),
			Description: description.Native(),
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
		ClientID:      out.ClientId.TypedString().String(),
		SubdomainHost: subdomainHost,
		Mode:          string(out.Mode),
		LocationID:    out.PrimaryInstanceLocation.Id.Native(),
		LocationName:  out.PrimaryInstanceLocation.Name.Native(),
		Tags:          []string(out.TagList),
	}, nil
}

// ---------------------------------------------------------------------------
// Service discovery
// ---------------------------------------------------------------------------

func (z *ZeropsClient) ListServices(ctx context.Context, projectID string) ([]ServiceStack, error) {
	clientID, err := z.OrgID(ctx)
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
