package platform

import (
	"context"
	"fmt"
)

// ListProjectsInEveryOrg lists the projects the token reaches in each org
// it belongs to, in membership order. A token's reach is the union over
// its orgs — listing only the first one misses every project of the rest.
func ListProjectsInEveryOrg(ctx context.Context, c Client, memberships []ClientMembership) ([]Project, error) {
	var projects []Project
	for _, m := range memberships {
		orgProjects, err := c.ListProjects(ctx, m.ClientID)
		if err != nil {
			return nil, err
		}
		projects = append(projects, orgProjects...)
	}
	return projects, nil
}

// soleOrg is the org an unbound client targets: the token's only
// membership ("" for none). A token in several orgs has no answer without
// a project naming one — ErrOrgAmbiguous, never Memberships[0].
func soleOrg(memberships []ClientMembership) (string, error) {
	switch len(memberships) {
	case 0:
		return "", nil
	case 1:
		return memberships[0].ClientID, nil
	default:
		return "", NewPlatformError(ErrOrgAmbiguous,
			fmt.Sprintf("Token belongs to %d organizations and no project names which one this call is for", len(memberships)),
			"Run against one project: the organization that owns it is the one searched")
	}
}
