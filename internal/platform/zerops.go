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

// ZeropsClient implements the Client interface using the zerops-go SDK.
type ZeropsClient struct {
	handler  sdk.Handler
	once     sync.Once // thread-safe clientID caching
	cachedID string
	idErr    error
}

// NewZeropsClient creates a new ZeropsClient authenticated with the given token.
func NewZeropsClient(token, apiHost string) (*ZeropsClient, error) {
	endpoint := apiHost
	if !strings.HasPrefix(endpoint, "http") {
		endpoint = "https://" + endpoint
	}
	if !strings.HasSuffix(endpoint, "/") {
		endpoint += "/"
	}

	httpClient := &http.Client{Timeout: DefaultAPITimeout}
	config := sdkBase.DefaultConfig(sdkBase.WithCustomEndpoint(endpoint))
	handler := sdk.New(config, httpClient)
	handler = sdk.AuthorizeSdk(handler, token)

	return &ZeropsClient{handler: handler}, nil
}

// getClientID returns the cached clientId, using sync.Once for thread safety.
func (z *ZeropsClient) getClientID(ctx context.Context) (string, error) {
	z.once.Do(func() {
		info, err := z.GetUserInfo(ctx)
		if err != nil {
			z.idErr = err
			return
		}
		z.cachedID = info.ID
	})
	return z.cachedID, z.idErr
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
	if len(out.ClientUserList) > 0 {
		clientID = out.ClientUserList[0].ClientId.TypedString().String()
	}

	return &UserInfo{
		ID:       clientID,
		Email:    out.Email.Native(),
		FullName: out.FullName.String(),
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
