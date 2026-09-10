package platform

import (
	"context"
	"fmt"

	"github.com/zeropsio/zerops-go/dto/input/path"
	"github.com/zeropsio/zerops-go/types/uuid"
)

// RevokeIntegrationToken deletes the client integration token tokenID on
// client clientID: DELETE /client/{clientId}/integration-token/{tokenId}
// (SDK DeleteClientIntegrationToken), docs/spec-eval-farm.md §3.4 FM-23. The
// farm controller calls this once a launch scenario's run and prod projects
// are both deleted, storing only the token id returned by
// MintDelegatedLaunchToken — the token value itself never crosses this call.
func (z *ZeropsClient) RevokeIntegrationToken(ctx context.Context, clientID, tokenID string) error {
	pathParam := path.IntegrationTokenId{
		Id:      uuid.ClientId(clientID),
		TokenId: uuid.UserId(tokenID),
	}
	resp, err := z.handler.DeleteClientIntegrationToken(ctx, pathParam)
	if err != nil {
		return fmt.Errorf("revoke integration token: %w", mapSDKError(err, "client"))
	}
	if _, err := resp.Output(); err != nil {
		return fmt.Errorf("revoke integration token output: %w", mapSDKError(err, "client"))
	}
	return nil
}
