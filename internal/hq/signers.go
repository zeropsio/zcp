package hq

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// SignersInput identifies the authority used by this client's signer read.
// Naming a Zerops key does not change it; replacing HQ, project or credential
// does. Only the digest is persisted beside a failed seed, never the credential.
func (c Client) SignersInput() string {
	input := strings.Join([]string{c.enrollment.HQ, c.enrollment.ProjectID, c.enrollment.Credential}, "\x00")
	return fmt.Sprintf("%x", sha256.Sum256([]byte(input)))
}

// Signers reads the enrolled project's signer metadata from HQ once, without
// retrying an unavailable answer. HQ omits signers when its record is empty.
func (c Client) Signers(ctx context.Context, projectID string) (map[string]string, error) {
	if projectID == "" || c.enrollment.ProjectID != projectID {
		return nil, errors.New("HQ signers: enrollment does not name this container's project")
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
