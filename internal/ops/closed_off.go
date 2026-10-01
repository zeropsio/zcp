package ops

import (
	"context"
	"fmt"
	"slices"

	"github.com/zeropsio/zcp/internal/platform"
)

// ClosedOffTag is the project tag the person's press (or Finish setup)
// writes once it has closed a Mate's project off and read back
// envIsolation "service". zcp waits for it rather than reading the
// isolation itself: a new project reads "service" before its recipe opens
// it, so an early read of the isolation races.
const ClosedOffTag = "mate:closed-off"

// ProjectReader is the one read ReadProjectClosedOff needs.
type ProjectReader interface {
	GetProject(ctx context.Context, projectID string) (*platform.Project, error)
}

// ReadProjectClosedOff reports whether the project carries ClosedOffTag,
// read with GET /project/{id} — a read the Mate's own key is allowed.
func ReadProjectClosedOff(ctx context.Context, client ProjectReader, projectID string) (bool, error) {
	project, err := client.GetProject(ctx, projectID)
	if err != nil {
		return false, fmt.Errorf("read the project: %w", err)
	}
	return slices.Contains(project.Tags, ClosedOffTag), nil
}
