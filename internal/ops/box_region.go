package ops

import (
	"context"
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// BoxRegionRefusal is the region pre-check of a Zerops Box: a Box can be
// created only in a project in one of topology.BoxRegions (prg1 today), and
// elsewhere its create fails on "hardware capacity" and the platform removes
// it. Given the Boxes an import or a bootstrap plan would create, it reads
// the project's record once — no read at all when boxes is empty — and, when
// the project's location cannot hold a Box, answers the refusal: outcome
// first ("Nothing was imported."), then each Box, the rule and the project's
// location as the platform names it. A read that fails, or a record that
// names no location, refuses nothing: the platform's own capacity failure
// and BoxRegionWarnings say it after the fact (docs/spec-workflows.md §8
// PS-10, PS-16).
func BoxRegionRefusal(ctx context.Context, client platform.Client, projectID string, boxes []string, outcome string) *platform.PlatformError {
	if len(boxes) == 0 {
		return nil
	}
	project, known := projectLocation(ctx, client, projectID)
	if !known || topology.BoxLocationAllowed(project.LocationID) {
		return nil
	}
	where := project.LocationName
	if strings.TrimSpace(where) == "" {
		where = project.LocationID
	} else {
		where = fmt.Sprintf("%s (location %s)", where, project.LocationID)
	}
	regions := strings.Join(topology.BoxRegions(), " or ")
	subject := boxes[0] + " is a Zerops Box"
	if len(boxes) > 1 {
		subject = strings.Join(boxes, ", ") + " are Zerops Boxes"
	}
	return platform.NewPlatformError(platform.ErrPrerequisiteMissing,
		fmt.Sprintf("%s %s, and a Box can be created only in a project in region %s. This project is in %s, where a Box's create fails with the platform's %q message and the platform removes the service.",
			outcome, subject, regions, where, boxCapacityMessage),
		fmt.Sprintf("Run the app and its Box in a project in region %s. In this project, import what you need without the Box.", regions))
}

// projectLocation reads the project's record for its location; known is
// false when the read fails or the record names no location — a region the
// pre-check cannot judge, so it refuses nothing.
func projectLocation(ctx context.Context, client platform.Client, projectID string) (platform.Project, bool) {
	project, err := client.GetProject(ctx, projectID)
	if err != nil || project == nil || strings.TrimSpace(project.LocationID) == "" {
		return platform.Project{}, false
	}
	return *project, true
}
