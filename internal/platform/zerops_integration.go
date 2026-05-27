package platform

import (
	"context"
	"errors"
	"fmt"

	"github.com/zeropsio/zerops-go/dto/input/path"
	"github.com/zeropsio/zerops-go/types/uuid"
)

// GetServiceStackIntegrationStatus reads the platform-side
// external-repository integration config attached to a service stack
// and projects it onto IntegrationStatus. Public-read endpoint; same
// shape ProjectAdminClient exposes for the launch-window read.
//
// Setup-name discovery uses this for cascade step 2: when a service
// has a configured GH/GitLab integration, the `ZeropsYamlSetup` field
// is the platform-recorded setup-block name and gets cached into local
// meta on first read (see workflow.ResolveCanonicalSetup in P1).
//
// HTTP 400 with `code: noExternalRepositoryIntegration` maps to
// IntegrationStatus{State: IntegrationNotConfigured} per Phase A B.1
// finding; other errors propagate.
func (z *ZeropsClient) GetServiceStackIntegrationStatus(ctx context.Context, serviceID string) (IntegrationStatus, error) {
	if serviceID == "" {
		return IntegrationStatus{}, errors.New("get integration status: serviceID empty")
	}
	pathParam := path.ServiceStackId{Id: uuid.ServiceStackId(serviceID)}
	resp, err := z.handler.GetServiceStackExternalRepositoryIntegrationStatus(ctx, pathParam)
	if err != nil {
		return IntegrationStatus{}, fmt.Errorf("get integration status: %w", mapSDKError(err, "service-stack"))
	}
	out, err := resp.Output()
	if err != nil {
		mapped := mapSDKError(err, "service-stack")
		var pe *PlatformError
		if errors.As(mapped, &pe) && pe.APICode == apiCodeNoExternalRepositoryIntegration {
			return IntegrationStatus{State: IntegrationNotConfigured}, nil
		}
		return IntegrationStatus{}, fmt.Errorf("get integration status output: %w", mapped)
	}
	return mapIntegrationOutput(out.GithubIntegration, out.GitlabIntegration), nil
}
