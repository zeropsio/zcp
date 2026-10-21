package platform

import "context"

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
