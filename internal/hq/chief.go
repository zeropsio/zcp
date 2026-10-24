package hq

import (
	"context"
	"fmt"
	"net/http"
)

// MateChief is HQ's canonical person and Chief for this linked Mate.
type MateChief struct {
	OwnerUserID string `json:"ownerUserId"`
	ProjectID   string `json:"projectId"`
}

// Chief reads whose Chief this Mate may ask; nil means its person has none.
func (c Client) Chief(ctx context.Context) (*MateChief, error) {
	var answer struct {
		Chief *MateChief `json:"chief"`
	}
	if err := c.call.json(ctx, http.MethodGet, "/api/mate/chief", c.authorization(), nil, &answer); err != nil {
		return nil, fmt.Errorf("mate Chief: %w", err)
	}
	return answer.Chief, nil
}

// ProposeChief binds an offer to this Mate's current person and Chief at HQ.
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
