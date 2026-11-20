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

// A Chief's reads of its organisation (docs/spec-mate.md §3.11): HQ asks
// Zerops' private API, under HQ's own identity, for the projects and services
// of the Chief's registered person and answers each list whole, complete or
// refused. The Chief presents only its project key. ListProjects and
// ListServices are ops.OrgSource, org mode's one source.

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

// chiefRead is one Chief read of the list read, decoded strictly into out:
// a field HQ's wire does not name is an answer zcp does not read. HQ's
// refusal says what it means for the Chief, with HQ's own code and reason.
func (c Client) chiefRead(ctx context.Context, read string, out any) error {
	var raw json.RawMessage
	if err := c.call.json(ctx, http.MethodPost, chiefReadPath, c.authorization(), map[string]string{"read": read}, &raw); err != nil {
		return chiefRefusal(err)
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
func chiefRefusal(err error) error {
	var refused *RefusedError
	if !errors.As(err, &refused) {
		return err
	}
	var words, suggestion string
	code := platform.ErrPermissionDenied
	switch refused.Code + " " + refused.Reason {
	case "forbidden not_chief":
		words = "HQ read nothing: this project is no person's Chief now"
		suggestion = "A person registers this project as their Chief in Mate"
	case "forbidden not_mate_operator":
		words = "HQ read nothing: this Chief's person is below Basic user on this project"
		suggestion = "An Admin gives your person Basic user or more on this project"
	case "forbidden not_active_member":
		words = "HQ read nothing: this Chief's person is not an active member of the organisation"
		suggestion = "An Admin of the organisation makes your person an active member again"
	case "forbidden project_gone":
		words = "HQ read nothing: this Chief's project is gone in Zerops"
	default:
		return err
	}
	return &platform.PlatformError{Code: code, Message: fmt.Sprintf("%s (%v)", words, err), Suggestion: suggestion, Cause: err}
}
