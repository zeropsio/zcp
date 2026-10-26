package hq

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

// SignersInput identifies the authority used by this client's signer read:
// replacing HQ, project or key changes it. Only the digest is persisted
// beside a failed seed, never the key.
func (c Client) SignersInput() string { return c.id.Fingerprint() }

// Signers reads the linked project's signer metadata from HQ once, without
// retrying an unavailable answer. HQ omits signers when its record is empty.
func (c Client) Signers(ctx context.Context, projectID string) (map[string]string, error) {
	if projectID == "" || c.id.ProjectID != projectID {
		return nil, errors.New("HQ signers: identity does not name this container's project")
	}
	var state struct {
		ProjectID string            `json:"projectId"`
		Signers   map[string]string `json:"signers"`
	}
	if _, err := c.call.sendOnce(ctx, http.MethodGet, "/api/mate/self", c.authorization(), "", nil, &state); err != nil {
		return nil, fmt.Errorf("HQ signers: %w", err)
	}
	if state.ProjectID != projectID {
		return nil, errors.New("HQ signers: answer does not name this container's project")
	}
	return state.Signers, nil
}
