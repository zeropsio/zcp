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

// MintProjectScopedToken mints a NO_ACCESS account-level integration token
// scoped to ADMIN on exactly one project: POST
// /client/{clientId}/integration-token (SDK PostClientIntegrationToken),
// body {name, roleCode:"NO_ACCESS", canViewFinances:false,
// canEditFinances:false, canCreateProjects:false, projects:[{projectId,
// roleCode:"ADMIN"}]} — docs/spec-eval-farm.md §2.1 FM-10. Used by the farm
// controller to give each run project its own scoped ZCP_API_KEY instead of
// relying on the platform's (nonexistent, for a REST-created service)
// first-class injection.
//
// A 403 with either delegation-unavailable apiCode (current or legacy) maps
// to ErrDelegationUnavailable — the same typed code
// MintDelegatedLaunchToken uses for the identical platform restriction: the
// minting credential must be a personal access token, not an integration
// token (brief §"The minting credential must be a PERSONAL access token").
func (z *ZeropsClient) MintProjectScopedToken(ctx context.Context, clientID, projectID, name string) (MintedToken, error) {
	reqBody := body.ClientIntegrationToken{
		Name:              types.NewString(name),
		RoleCode:          enum.ClientUserRoleCodeEnumNoAccess,
		CanViewFinances:   types.NewBool(false),
		CanEditFinances:   types.NewBool(false),
		CanCreateProjects: types.NewBool(false),
		Projects: body.ClientIntegrationTokenProjects{
			{
				ProjectId: uuid.ProjectId(projectID),
				RoleCode:  enum.ClientUserRoleCodeEnumAdmin,
			},
		},
	}

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
