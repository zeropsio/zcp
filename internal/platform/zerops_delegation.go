package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/zeropsio/zerops-go/apiError"
	"github.com/zeropsio/zerops-go/dto/input/body"
	"github.com/zeropsio/zerops-go/dto/input/path"
	"github.com/zeropsio/zerops-go/sdkBase"
	"github.com/zeropsio/zerops-go/types"
	"github.com/zeropsio/zerops-go/types/enum"
	"github.com/zeropsio/zerops-go/types/uuid"
)

// TokenDelegation is a one-time authorization for THIS integration token to
// mint a new integration token with the given permissions. Live shape: F2
// (plans/archive/token-delegation-implementation-spec-2026-07-10.md §1).
type TokenDelegation struct {
	ID                string
	TokenID           string
	RoleCode          string
	CanCreateProjects bool
	Created           string // RFC3339, informational only
}

// MintedToken is the result of consuming a delegation. Token is a live
// credential — P-LP-1 applies: never serialize into responses, state, or
// logs; json:"-" makes the carrier marshal-proof by construction (pinned by
// TestMintedToken_MarshalOmitsCredential). No Name field: recovery text uses
// the caller's locally-retained REQUESTED name, never the returned DTO
// (spec §3.1/§4.4).
type MintedToken struct {
	Token   string `json:"-"`
	TokenID string `json:"tokenId,omitempty"`
}

// apiCodeDelegationUnavailable / apiCodeDelegationUnavailableLegacy are the
// platform error codes MintDelegatedLaunchToken sees when this token has no
// unused delegation (never granted one, already consumed it, or it was
// revoked — F4). The legacy code is what pre-delegation platforms returned
// for the same call; both mean the same ZCP semantic.
const (
	apiCodeDelegationUnavailable       = "notAllowedForIntegrationTokenWithoutDelegation"
	apiCodeDelegationUnavailableLegacy = "notAllowedForIntegrationToken"
)

// tokenDelegationListResponse mirrors the hand-rolled GET
// .../integration-token/{tokenId}/delegation response body (F2). Not in the
// zerops-go SDK (verified absent in v1.0.20 and v1.0.21) — decoded by hand
// on the SDK's own transport (sdkBase.Get) so host normalization + bearer
// auth stay single-owner with every other call.
type tokenDelegationListResponse struct {
	List []tokenDelegationDTO `json:"list"`
}

// tokenDelegationDTO decodes the wire row. clientId/clientUserId/userId are
// on the wire but unused by ZCP, so they're not decoded.
type tokenDelegationDTO struct {
	ID               string                        `json:"id"`
	TokenID          string                        `json:"tokenId"`
	TokenPermissions tokenDelegationPermissionsDTO `json:"tokenPermissions"`
	Created          string                        `json:"created"`
}

// tokenDelegationPermissionsDTO decodes only the fields ZCP consumes
// (RoleCode for the honest status line, CanCreateProjects for availability).
// Finance flags and projectPermissions are on the wire but nothing reads
// them, so they're not decoded.
type tokenDelegationPermissionsDTO struct {
	RoleCode          string `json:"roleCode"`
	CanCreateProjects bool   `json:"canCreateProjects"`
}

// mapTokenDelegations projects the wire DTO onto the exported shape.
func mapTokenDelegations(items []tokenDelegationDTO) []TokenDelegation {
	out := make([]TokenDelegation, 0, len(items))
	for _, it := range items {
		out = append(out, TokenDelegation{
			ID:                it.ID,
			TokenID:           it.TokenID,
			RoleCode:          it.TokenPermissions.RoleCode,
			CanCreateProjects: it.TokenPermissions.CanCreateProjects,
			Created:           it.Created,
		})
	}
	return out
}

// ListOwnTokenDelegations returns the delegations attached to the token this
// client authenticates with. Fresh read every call — the platform is the
// sole source of delegation truth (D-1); ZCP never persists or infers
// availability locally.
func (z *ZeropsClient) ListOwnTokenDelegations(ctx context.Context) ([]TokenDelegation, error) {
	clientID, err := z.getClientID(ctx)
	if err != nil {
		return nil, fmt.Errorf("list own token delegations: %w", err)
	}
	tokenID, err := z.getTokenID(ctx)
	if err != nil {
		return nil, fmt.Errorf("list own token delegations: %w", err)
	}

	u := "/api/rest/public/client/" + clientID + "/integration-token/" + tokenID + "/delegation"
	sdkResp := sdkBase.Get(ctx, z.env, u)
	if sdkResp.Err != nil {
		return nil, fmt.Errorf("list own token delegations: %w", mapSDKError(sdkResp.Err, "delegation"))
	}

	decoder := json.NewDecoder(sdkResp.ResponseData)
	if sdkResp.HttpResponse.StatusCode < http.StatusMultipleChoices {
		var success tokenDelegationListResponse
		if err := decoder.Decode(&success); err != nil {
			return nil, fmt.Errorf("list own token delegations: decode response: %w", err)
		}
		return mapTokenDelegations(success.List), nil
	}

	responseString := sdkResp.ResponseData.String()
	apiErrResp := struct {
		Error apiError.Error `json:"error"`
	}{}
	if err := decoder.Decode(&apiErrResp); err != nil {
		return nil, fmt.Errorf("list own token delegations: %s: %s", sdkResp.HttpResponse.Status, responseString)
	}
	apiErrResp.Error.HttpStatusCode = sdkResp.HttpResponse.StatusCode
	return nil, fmt.Errorf("list own token delegations: %w", mapSDKError(apiErrResp.Error, "delegation"))
}

// MintDelegatedLaunchToken consumes the one-time delegation to mint a
// NO_ACCESS + canCreateProjects integration token named name. The returned
// Token value is shown by the platform exactly once (F3) — the caller owns
// the P-LP-14 staging discipline (never persisted here).
func (z *ZeropsClient) MintDelegatedLaunchToken(ctx context.Context, name string) (MintedToken, error) {
	clientID, err := z.getClientID(ctx)
	if err != nil {
		return MintedToken{}, fmt.Errorf("mint delegated launch token: %w", err)
	}

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
		return MintedToken{}, translateDelegationUnavailable(fmt.Errorf("mint delegated launch token: %w", mapSDKError(err, "client")))
	}
	out, err := resp.Output()
	if err != nil {
		return MintedToken{}, translateDelegationUnavailable(fmt.Errorf("mint delegated launch token output: %w", mapSDKError(err, "client")))
	}

	return MintedToken{
		Token:   out.Token.String(),
		TokenID: out.Id.TypedString().String(),
	}, nil
}

// translateDelegationUnavailable rewrites the generic 403 mapping into
// ErrDelegationUnavailable when the platform's apiCode says this token has
// no unused delegation (F4 — both the current and legacy pre-delegation
// codes). Mirrors the repo's one apiCode-translation precedent
// (apiCodeNoExternalRepositoryIntegration in zerops_integration.go) instead
// of adding a branch to mapAPIError's switch, which only branches on HTTP
// status + entityType (§3.3). Any other mapped error — including F5's
// roleLevelExceeded, which ZCP never triggers since it only ever requests
// the delegated shape — passes through untouched.
func translateDelegationUnavailable(err error) error {
	var pe *PlatformError
	if !errors.As(err, &pe) || !isDelegationUnavailableAPICode(pe.APICode) {
		return err
	}
	return &PlatformError{
		Code:       ErrDelegationUnavailable,
		Message:    pe.Message,
		Suggestion: "This token has no unused delegation — fall back to the manual launchKey path",
		APICode:    pe.APICode,
		APIMeta:    pe.APIMeta,
		Cause:      pe.Cause,
	}
}

func isDelegationUnavailableAPICode(code string) bool {
	return code == apiCodeDelegationUnavailable || code == apiCodeDelegationUnavailableLegacy
}
