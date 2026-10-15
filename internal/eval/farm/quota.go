package farm

import (
	"context"
	"fmt"
)

// OrgProjectLimit is the platform's project limit per org: a create past it
// fails with "You have reached the limit of 50 projects".
const OrgProjectLimit = 50

// QuotaHeadroom is how many project slots a batch leaves free for launch
// runs' production targets and for other users of the same org.
const QuotaHeadroom = 4

// Quota is the batch preflight's reading of the org: how many projects it
// holds and the run window that fits beside them.
type Quota struct {
	Projects int
	Window   int
}

// QuotaWindow reads clientID's live project count and caps requested (0 =
// unlimited) to OrgProjectLimit − projects − QuotaHeadroom, so a batch
// waits for a free slot instead of failing runs on the platform's limit. It
// refuses when not even one run fits.
func QuotaWindow(ctx context.Context, client PlatformClient, clientID string, requested int) (Quota, error) {
	projects, err := client.ListProjects(ctx, clientID)
	if err != nil {
		return Quota{}, fmt.Errorf("farm run: quota preflight: list projects: %w", err)
	}
	q := Quota{Projects: len(projects)}
	free := OrgProjectLimit - q.Projects - QuotaHeadroom
	if free < 1 {
		return q, fmt.Errorf("farm run: quota preflight: org %s holds %d of %d projects, no room for a run project with %d kept free", clientID, q.Projects, OrgProjectLimit, QuotaHeadroom)
	}
	q.Window = requested
	if requested == 0 || requested > free {
		q.Window = free
	}
	return q, nil
}
