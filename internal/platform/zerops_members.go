package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"
)

// OrgMember is one row of an org's member list: a person, or an integration
// token, which the list carries as a member under the token's name
// (FullName) and its own address (Email, token-<id>@zerops.io). Every token
// of the org reads the whole list, a project-scoped one included.
type OrgMember struct {
	ID                string
	UserID            string
	Status            string
	RoleCode          string
	CanCreateProjects bool
	Email             string
	FullName          string
}

// membersAttempts bounds ListOrgMembers' tries: the list answers a passing
// 400 userNotFound now and then, which the next read does not repeat.
const membersAttempts = 3

const membersBackoff = 250 * time.Millisecond

// ListOrgMembers reads GET /client/{orgID}/user/list, retrying a passing
// refusal (userNotFound) or a transient failure up to membersAttempts times.
func (z *ZeropsClient) ListOrgMembers(ctx context.Context, orgID string) ([]OrgMember, error) {
	u := "/api/rest/public/client/" + url.PathEscape(orgID) + "/user/list"
	var lastErr error
	for attempt := range membersAttempts {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("org members: %w", ctx.Err())
			case <-time.After(membersBackoff << (attempt - 1)):
			}
		}
		raw, err := z.directGet(ctx, u, "org members")
		if err == nil {
			return decodeOrgMembers(raw)
		}
		lastErr = err
		if !membersRetryable(err) {
			break
		}
	}
	return nil, lastErr
}

// membersRetryable reports a failure of the member list worth reading it again for.
func membersRetryable(err error) bool {
	var pe *PlatformError
	return IsTransient(err) || (errors.As(err, &pe) && pe.APICode == "userNotFound")
}

func decodeOrgMembers(raw []byte) ([]OrgMember, error) {
	var out struct {
		ClientUserList []struct {
			ID                string `json:"id"`
			UserID            string `json:"userId"`
			Status            string `json:"status"`
			RoleCode          string `json:"roleCode"`
			CanCreateProjects bool   `json:"canCreateProjects"`
			User              struct {
				Email    string `json:"email"`
				FullName string `json:"fullName"`
			} `json:"user"`
		} `json:"clientUserList"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, withCause(NewPlatformError(ErrAPIError,
			"org members: malformed response",
			"Retry; if it persists the platform API changed — report it"), err)
	}
	members := make([]OrgMember, 0, len(out.ClientUserList))
	for _, m := range out.ClientUserList {
		members = append(members, OrgMember{
			ID:                m.ID,
			UserID:            m.UserID,
			Status:            m.Status,
			RoleCode:          m.RoleCode,
			CanCreateProjects: m.CanCreateProjects,
			Email:             m.User.Email,
			FullName:          m.User.FullName,
		})
	}
	return members, nil
}
