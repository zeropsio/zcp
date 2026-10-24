package tools

import (
	"context"
	"errors"
	"fmt"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/workflow"
)

// fillCodeVolumeAfterDeploy puts the repository onto a variant-B pair's code
// volume once its dev half deployed — the deploy is what mounts the volume
// there (docs/spec-mate.md §2.8a) — and reloads every dev service's
// supervisor, which read a missing declaration until then. The stand-up does
// the same through fillAndReload; on the adopt route this is the only place
// the pair's volume ever gets its repository, so nobody copies it by hand.
// Other targets, pairs without a code volume and a volume that already holds
// the repository change nothing — and so does a pair keyed by its dev pool,
// which still names its code volume until the pool migration's retire has
// deleted it (docs/spec-workflows.md §13 PM-13): a deploy to the pool fills
// nothing, since the pool's hostname reaches a random container of it.
func fillCodeVolumeAfterDeploy(ctx context.Context, ssh ops.SSHDeployer, stateDir, target string, result *ops.DeployResult) {
	if ssh == nil || stateDir == "" {
		return
	}
	meta, _ := workflow.FindServiceMeta(stateDir, target)
	if meta == nil || meta.DevIsPool() || meta.CodeVolume == nil || meta.Hostname != target || meta.CodeVolume.MountPath == "" {
		return
	}
	filled, err := ops.FillCodeVolume(ctx, ssh, target, meta.CodeVolume.Hostname, meta.CodeVolume.MountPath)
	if err != nil {
		result.Warnings = append(result.Warnings, err.Error())
		return
	}
	if !filled {
		return
	}
	reloaded := 0
	for _, host := range append([]string{target}, meta.CodeVolume.DevServiceHostnames()...) {
		_, err := ops.ReloadSupervisor(ctx, ssh, host)
		var noSup *ops.NoSupervisorError
		switch {
		case err == nil:
			reloaded++
		case !errors.As(err, &noSup):
			result.Warnings = append(result.Warnings, fmt.Sprintf("reload the dev servers of %s: %v", host, err))
		}
	}
	result.Message += fmt.Sprintf(" The repository is now on the code volume %s at %s (filled from %s's /var/www; %d supervisor(s) reloaded) — edit and commit there, never copy files onto it by hand.",
		meta.CodeVolume.Hostname, meta.CodeVolume.MountPath, target, reloaded)
}
