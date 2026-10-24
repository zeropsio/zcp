package hq

import (
	"context"
	"fmt"
	"net/http"
)

// ProposeChief binds an offer to this Mate's project and the organisation's
// current Chief mark at HQ.
// HQ returns only an offer id: no message is sent.
func (c Client) ProposeChief(ctx context.Context) (string, error) {
	var answer struct {
		ID string `json:"id"`
	}
	if err := c.call.json(ctx, http.MethodPost, "/api/mate/chief/proposals", c.authorization(), nil, &answer); err != nil {
		return "", fmt.Errorf("offer Chief message: %w", err)
	}
	if answer.ID == "" {
		return "", fmt.Errorf("offer Chief message: HQ returned no proposal id")
	}
	return answer.ID, nil
}
