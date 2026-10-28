package workflow

import (
	"fmt"
	"path"
	"slices"

	"github.com/zeropsio/zcp/internal/topology"
)

// WorkspaceRepos lists, sorted, the repositories a zcp container checks out
// under its workspace, as directories relative to it: the dev half of every
// pair the records in stateDir hold, which zcp mounts at /var/www/<host> —
// for a variant-B pair the volume holding its code, which shows under that
// mount at its subpath (topology.CodeVolume.MountSubpath). The workspace
// CLAUDE.md imports each one's CLAUDE.md (content.BuildClaudeWrapper,
// docs/spec-workflows.md §5.1).
//
// Every container mode counts (standard, dev, simple, a meta older than
// modes), complete or still bootstrapping: bootstrap mounts the dev half at
// provision. Never listed: a stage (built from the dev half's repository), a
// variant-B pair's further dev services (they mount the same volume), a
// local topology (locally the working directory is the repository), and a
// pool-keyed pair — its hostname names a pool zcp never mounts, and its
// checkouts are lease mounts, each the checkout of the conversation that
// took the lease, reached only right after the pool's orchestrator vouches
// for it (§8 DP-2), never a standing read a session start would make.
//
// A record zcp cannot read is an error, never a shorter list.
func WorkspaceRepos(stateDir string) ([]string, error) {
	metas, err := ListServiceMetas(stateDir)
	if err != nil {
		return nil, fmt.Errorf("workspace repositories: %w", err)
	}
	var dirs []string
	for _, m := range metas {
		if m == nil || m.Hostname == "" || m.DevIsPool() {
			continue
		}
		if m.Mode == topology.PlanModeLocalStage || m.Mode == topology.PlanModeLocalOnly {
			continue
		}
		dir := m.Hostname
		if m.CodeVolume != nil && topology.CodeVolumeMountPathError(m.CodeVolume.MountPath) == "" {
			dir = path.Join(dir, m.CodeVolume.MountSubpath())
		}
		dirs = append(dirs, dir)
	}
	slices.Sort(dirs)
	return slices.Compact(dirs), nil
}
