package platform

import (
	"context"
	"errors"
	"fmt"

	"github.com/zeropsio/zerops-go/dto/input/body"
	"github.com/zeropsio/zerops-go/dto/input/path"
	"github.com/zeropsio/zerops-go/types"
	"github.com/zeropsio/zerops-go/types/enum"
	"github.com/zeropsio/zerops-go/types/uuid"
)

// MintedToken is a freshly minted integration token. Token is a live
// credential — P-LP-1 applies: never serialize into responses, state, or
// logs; json:"-" makes the carrier marshal-proof by construction (pinned by
// TestMintedToken_MarshalOmitsCredential). No Name field: callers keep the
// name they requested.
type MintedToken struct {
	Token   string `json:"-"`
	TokenID string `json:"tokenId,omitempty"`
}

// apiCodeTokenCannotMint / apiCodeTokenCannotMintLegacy are the platform
// error codes a mint answers when the minting credential is an integration
// token: only a person's own token mints (ZEROPS-060). The legacy code is
// what older platforms returned for the same call.
const (
	apiCodeTokenCannotMint       = "notAllowedForIntegrationTokenWithoutDelegation"
	apiCodeTokenCannotMintLegacy = "notAllowedForIntegrationToken"
)

// MintLaunchKey mints the eval farm's launch key in org clientID: a
// NO_ACCESS + canCreateProjects integration token a launch scenario hands its
// agent as launchKey, the stand-in for the token of the person who presses
// launch. The farm mints it with its operator's own token, consumes no
// delegation, and revokes it once the run's projects are gone
// (docs/spec-eval-farm.md §3.4 FM-23). The Token value is shown by the
// platform exactly once.
func (z *ZeropsClient) MintLaunchKey(ctx context.Context, clientID, name string) (MintedToken, error) {
	reqBody := body.ClientIntegrationToken{
		Name:              types.NewString(name),
		RoleCode:          enum.ClientUserRoleCodeEnumNoAccess,
		CanViewFinances:   types.NewBool(false),
		CanEditFinances:   types.NewBool(false),
		CanCreateProjects: types.NewBool(true),
		Projects:          body.ClientIntegrationTokenProjects{},
	}

	resp, err := z.handler.PostClientIntegrationToken(ctx, path.ClientId{Id: uuid.ClientId(clientID)}, reqBody)
	if err != nil {
		return MintedToken{}, translateMintRefused(fmt.Errorf("mint launch key: %w", mapSDKError(err, "client")))
	}
	out, err := resp.Output()
	if err != nil {
		return MintedToken{}, translateMintRefused(fmt.Errorf("mint launch key output: %w", mapSDKError(err, "client")))
	}

	return MintedToken{
		Token:   out.Token.String(),
		TokenID: out.Id.TypedString().String(),
	}, nil
}

// translateMintRefused rewrites the generic 403 mapping into
// ErrTokenCannotMint when the platform's apiCode says the minting credential
// is an integration token. Any other mapped error — roleLevelExceeded
// included — passes through untouched.
func translateMintRefused(err error) error {
	var pe *PlatformError
	if !errors.As(err, &pe) || !isTokenCannotMintAPICode(pe.APICode) {
		return err
	}
	return &PlatformError{
		Code:       ErrTokenCannotMint,
		Message:    pe.Message,
		Suggestion: "Mint with a person's own Zerops token: an integration token cannot mint another",
		APICode:    pe.APICode,
		APIMeta:    pe.APIMeta,
		Cause:      pe.Cause,
	}
}

func isTokenCannotMintAPICode(code string) bool {
	return code == apiCodeTokenCannotMint || code == apiCodeTokenCannotMintLegacy
}
