package tools

import (
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/workflow"
)

// zcp sets up no new code volume (docs/spec-mate.md §2.8a.4a). A variant-B
// pair recorded before keeps deploying, running its dev servers and reading
// in status as it did, and those results carry one line saying so and naming
// the pool migration that moves the pair onto one dev pool per part
// (docs/spec-workflows.md §13 PM-13) — a result warning, never a refusal.

// codeVolumeSteer is that line for the pair meta records, "" when meta keeps
// no code volume — or is already keyed by its dev pool, whose migration is
// under way and whose volume the retire deletes.
func codeVolumeSteer(meta *workflow.ServiceMeta) string {
	if meta == nil || meta.CodeVolume == nil || meta.DevIsPool() {
		return ""
	}
	return fmt.Sprintf("zcp no longer sets up a shared code volume. %s's pair keeps working on the code volume %s; zerops_workflow workflow=\"pool-migrate\" service=%q moves it onto one dev pool per part.",
		meta.Hostname, meta.CodeVolume.Hostname, meta.Hostname)
}

// codeVolumeSteerFor is codeVolumeSteer for the pair hostname belongs to,
// "" when no pair records it or its record cannot be read.
func codeVolumeSteerFor(stateDir, hostname string) string {
	if stateDir == "" || hostname == "" {
		return ""
	}
	meta, err := workflow.FindServiceMeta(stateDir, hostname)
	if err != nil {
		return ""
	}
	return codeVolumeSteer(meta)
}

// codeVolumeStatusAddendum is the status guidance block naming every
// variant-B pair the project records, "" when it records none.
func codeVolumeStatusAddendum(stateDir string) string {
	if stateDir == "" {
		return ""
	}
	metas, err := workflow.ListServiceMetas(stateDir)
	if err != nil {
		return ""
	}
	var lines []string
	for _, m := range metas {
		if steer := codeVolumeSteer(m); steer != "" {
			lines = append(lines, steer)
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return "### Shared code volume\n\n" + strings.Join(lines, "\n") + "\n"
}
