package hq

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/zeropsio/zcp/internal/platform"
)

// A Chief's reads of its organisation (docs/spec-mate.md §3.11): HQ reads
// Zerops with the Chief's registered person's kept personal token and answers
// each list whole, complete or refused. The token never reaches zcp; the
// Chief presents only its project key. ListProjects, ListServices and
// ListMembers are ops.OrgSource, org mode's one source.

// chiefReadPath is HQ's Chief read.
const chiefReadPath = "/api/mate/chief/read"

// ListProjects is the organisation's projects the Chief's person reaches.
func (c Client) ListProjects(ctx context.Context) ([]platform.Project, error) {
	var answer struct {
		Projects *[]struct {
			ID     string   `json:"id"`
			Name   string   `json:"name"`
			Status string   `json:"status"`
			Tags   []string `json:"tags"`
		} `json:"projects"`
	}
	if err := c.chiefRead(ctx, "projects", &answer); err != nil {
		return nil, err
	}
	if answer.Projects == nil {
		return nil, malformedChiefRead("projects")
	}
	projects := make([]platform.Project, 0, len(*answer.Projects))
	for _, p := range *answer.Projects {
		if p.ID == "" {
			return nil, malformedChiefRead("projects")
		}
		projects = append(projects, platform.Project{ID: p.ID, Name: p.Name, Status: p.Status, Tags: p.Tags})
	}
	return projects, nil
}

// ListServices is every service of the projects the Chief's person reaches,
// the platform's own among them; a non-empty projectID narrows it to that
// project's.
func (c Client) ListServices(ctx context.Context, projectID string) ([]platform.ServiceStack, error) {
	var answer struct {
		Services *[]struct {
			ID        string `json:"id"`
			ProjectID string `json:"projectId"`
			Name      string `json:"name"`
			Status    string `json:"status"`
			Type      string `json:"type"`
			Category  string `json:"category"`
			Ports     *[]struct {
				Port        int    `json:"port"`
				Protocol    string `json:"protocol"`
				Scheme      string `json:"scheme"`
				PortRouting bool   `json:"portRouting"`
				HTTPRouting bool   `json:"httpRouting"`
			} `json:"ports"`
		} `json:"services"`
	}
	if err := c.chiefRead(ctx, "services", &answer); err != nil {
		return nil, err
	}
	if answer.Services == nil {
		return nil, malformedChiefRead("services")
	}
	services := []platform.ServiceStack{}
	for _, s := range *answer.Services {
		if s.ID == "" || s.ProjectID == "" || s.Ports == nil {
			return nil, malformedChiefRead("services")
		}
		if projectID != "" && s.ProjectID != projectID {
			continue
		}
		var ports []platform.Port
		for _, p := range *s.Ports {
			ports = append(ports, platform.NewPort(p.Port, p.Protocol, p.Scheme, p.PortRouting, p.HTTPRouting))
		}
		services = append(services, platform.ServiceStack{
			ID: s.ID, ProjectID: s.ProjectID, Name: s.Name, Status: s.Status, Ports: ports,
			// The category name unchanged: IsSystem keys on it (USER, CORE, BUILD, …).
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: s.Type, ServiceStackTypeCategoryName: s.Category},
		})
	}
	return services, nil
}

// ListMembers is the organisation's people as the Chief's person reads them.
func (c Client) ListMembers(ctx context.Context) ([]platform.OverviewMember, error) {
	var answer struct {
		Members *[]platform.OverviewMember `json:"members"`
	}
	if err := c.chiefRead(ctx, "members", &answer); err != nil {
		return nil, err
	}
	if answer.Members == nil {
		return nil, malformedChiefRead("members")
	}
	for _, m := range *answer.Members {
		if m.UserID == "" {
			return nil, malformedChiefRead("members")
		}
	}
	return *answer.Members, nil
}

// chiefRead is one Chief read of the list read, decoded strictly into out:
// a field HQ's wire does not name is an answer zcp does not read. HQ's
// refusal says what it means for the Chief, with HQ's own code and reason.
func (c Client) chiefRead(ctx context.Context, read string, out any) error {
	var raw json.RawMessage
	if err := c.call.json(ctx, http.MethodPost, chiefReadPath, c.authorization(), map[string]string{"read": read}, &raw); err != nil {
		return chiefRefusal(read, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return fmt.Errorf("%w: %w", malformedChiefRead(read), err)
	}
	if decoder.Decode(new(any)) != io.EOF {
		return malformedChiefRead(read)
	}
	return nil
}

// malformedChiefRead is an answer HQ gave that is not the Chief read's wire.
func malformedChiefRead(read string) error {
	return fmt.Errorf("hq %s %s: malformed answer", chiefReadPath, read)
}

// chiefRefusal is what HQ's no to a Chief read means for the Chief; any other
// failure is as every HQ call's (RefusedError, UnavailableError).
func chiefRefusal(read string, err error) error {
	var refused *RefusedError
	if !errors.As(err, &refused) {
		return err
	}
	switch {
	case refused.Code == "credential_required":
		return &platform.PlatformError{
			Code:       platform.ErrPrerequisiteMissing,
			Message:    fmt.Sprintf("HQ could not read the organisation's %s as this Chief's person: it holds no personal token of theirs that Zerops takes (%v)", read, err),
			Suggestion: "Tell your person to sign in to Mate once; HQ then keeps their token and the read works",
			Cause:      err,
		}
	case refused.Code == "forbidden" && refused.Reason == "not_chief":
		return &platform.PlatformError{
			Code:       platform.ErrPermissionDenied,
			Message:    fmt.Sprintf("HQ read nothing: this project is no person's Chief now (%v)", err),
			Suggestion: "A person registers this project as their Chief in Mate",
			Cause:      err,
		}
	case refused.Code == "forbidden" && refused.Reason != "":
		return &platform.PlatformError{
			Code:       platform.ErrPermissionDenied,
			Message:    fmt.Sprintf("HQ read nothing: this Chief's person may not operate it now (%v)", err),
			Suggestion: "Your person needs Basic user or more on this project, as an active member of the organisation; an Admin gives it",
			Cause:      err,
		}
	}
	return err
}
