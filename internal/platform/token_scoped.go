package platform

import (
	"context"
	"fmt"

	"github.com/zeropsio/zerops-go/dto/input/body"
	"github.com/zeropsio/zerops-go/dto/input/path"
	"github.com/zeropsio/zerops-go/types"
	"github.com/zeropsio/zerops-go/types/enum"
	"github.com/zeropsio/zerops-go/types/uuid"
)

// MintProjectScopedToken mints a project key: an integration token with
// organisation role NO_ACCESS and BASIC_USER on exactly one project, never
// ADMIN (projectKeyBody). POST /client/{clientId}/integration-token (SDK
// PostClientIntegrationToken) — docs/spec-eval-farm.md §2.1 FM-10. Used by
// the farm controller to give each run project its own scoped ZCP_API_KEY
// instead of relying on the platform's (nonexistent, for a REST-created
// service) first-class injection.
//
// A 403 with either delegation-unavailable apiCode (current or legacy) maps
// to ErrDelegationUnavailable: the minting credential must be a personal
// access token, not an integration token.
func (z *ZeropsClient) MintProjectScopedToken(ctx context.Context, clientID, projectID, name string) (MintedToken, error) {
	reqBody := projectKeyBody(name, projectID)

	resp, err := z.handler.PostClientIntegrationToken(ctx, path.ClientId{Id: uuid.ClientId(clientID)}, reqBody)
	if err != nil {
		return MintedToken{}, translateDelegationUnavailable(fmt.Errorf("mint project scoped token: %w", mapSDKError(err, "client")))
	}
	out, err := resp.Output()
	if err != nil {
		return MintedToken{}, translateDelegationUnavailable(fmt.Errorf("mint project scoped token output: %w", mapSDKError(err, "client")))
	}

	return MintedToken{
		Token:   out.Token.String(),
		TokenID: out.Id.TypedString().String(),
	}, nil
}

// projectKeyBody is the one shape of every project key zcp mints: NO_ACCESS
// in the organisation, no finance rights, no project creation, BASIC_USER on
// exactly projectID. A BASIC_USER key reads and writes its project's
// services, variables and logs and imports services; it cannot mint a token
// or reach another project.
func projectKeyBody(name, projectID string) body.ClientIntegrationToken {
	return body.ClientIntegrationToken{
		Name:              types.NewString(name),
		RoleCode:          enum.ClientUserRoleCodeEnumNoAccess,
		CanViewFinances:   types.NewBool(false),
		CanEditFinances:   types.NewBool(false),
		CanCreateProjects: types.NewBool(false),
		Projects: body.ClientIntegrationTokenProjects{
			{
				ProjectId: uuid.ProjectId(projectID),
				RoleCode:  enum.ClientUserRoleCodeEnumBasicUser,
			},
		},
	}
}
