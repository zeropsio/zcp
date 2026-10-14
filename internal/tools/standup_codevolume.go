package tools

import (
	"context"
	"fmt"

	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// A variant-B pair (docs/spec-mate.md §2.8a) keeps its repository on a code
// volume every dev service of the pair mounts. Its stand-up deploys the dev
// half from the checkout in /var/www, then every further dev service with its
// own dev setup from the same checkout, then fills the volume once from it
// and reloads every dev service's supervisor, which read a missing
// declaration until then. The pair's stage builds from the volume, so it
// waits for the fill. The stand-up recognises the pair by its record — the
// tier's pair of a further dev service folds into it — and never guesses it
// from the tier.

// standupCodeVolume is a variant-B pair's code volume through the stand-up.
type standupCodeVolume struct {
	volume      topology.CodeVolume
	devServices []*standupDevServiceRun
	// Filled is standupNow or standupAlready once the volume holds the
	// repository; Reason says why it does not.
	Filled string `json:"filled,omitempty"`
	Reason string `json:"reason,omitempty"`
	// Reloads is, by dev service, why its supervisor did not reload; a
	// reloaded one is absent.
	Reloads map[string]string `json:"reloads,omitempty"`
	done    chan struct{}
}

// standupDevServiceRun is one further dev service's deploy.
type standupDevServiceRun struct {
	ref    topology.DevServiceRef
	deploy *standupDeploy
}

// standupFolded is a tier pair whose dev half is a further dev service of
// owner's variant-B pair: it stands up with that pair, and its stage is one
// more stage of it.
type standupFolded struct {
	pair  workflow.MateTierPair
	owner string
}

// foldCodeVolumePairs takes out of the tier every pair whose dev half the
// record names a further dev service of another pair: adopting it as a pair
// of its own would record that service twice.
func foldCodeVolumePairs(stateDir string, tier workflow.MateTier) (workflow.MateTier, []standupFolded) {
	if stateDir == "" {
		return tier, nil
	}
	var folded []standupFolded
	kept := make([]workflow.MateTierPair, 0, len(tier.Pairs))
	for _, p := range tier.Pairs {
		meta, err := workflow.FindServiceMeta(stateDir, p.Dev.Hostname)
		if err == nil && meta != nil && meta.Hostname != p.Dev.Hostname {
			if _, ok := meta.CodeVolume.DevService(p.Dev.Hostname); ok {
				folded = append(folded, standupFolded{pair: p, owner: meta.Hostname})
				continue
			}
		}
		kept = append(kept, p)
	}
	tier.Pairs = kept
	return tier, folded
}

// codeVolumeOf is the pair's code volume as its record holds it, nil for a
// pair without one.
func codeVolumeOf(stateDir string, sp *standupPair) *standupCodeVolume {
	if stateDir == "" {
		return nil
	}
	meta, err := workflow.ReadServiceMeta(stateDir, sp.pair.Dev.Hostname)
	if err != nil || meta == nil || meta.CodeVolume == nil {
		return nil
	}
	cv := &standupCodeVolume{volume: *meta.CodeVolume, done: make(chan struct{})}
	for _, ref := range meta.CodeVolume.DevServices {
		cv.devServices = append(cv.devServices, &standupDevServiceRun{ref: ref})
	}
	return cv
}

// devStands reports every further dev service running code on a filled
// volume; true for a pair without a code volume.
func (cv *standupCodeVolume) devStands() bool {
	if cv == nil {
		return true
	}
	for _, ds := range cv.devServices {
		if !ds.deploy.running() {
			return false
		}
	}
	return cv.Filled != ""
}

// fillAndReload fills the pair's code volume once its dev half runs code and
// reloads the supervisor of every dev service that does. A dev service whose
// supervisor does not answer is reported, never fatal: its dev servers start
// by hand.
func (d standupDeps) fillAndReload(ctx context.Context, sp *standupPair, progress *standupProgress) {
	cv := sp.codeVolume
	dev := sp.pair.Dev.Hostname
	if sp.failed != "" {
		return
	}
	if !sp.devDeploy.running() {
		cv.Reason = fmt.Sprintf("%s did not deploy, and the code volume is filled from its checkout", dev)
		return
	}
	progress.say(fmt.Sprintf("filling the code volume %s at %s from %s", cv.volume.Hostname, cv.volume.MountPath, dev))
	filled, err := ops.FillCodeVolume(ctx, d.batch.sshDeployer, dev, cv.volume.MountPath)
	if err != nil {
		cv.Reason = err.Error()
		progress.st().step(dev, mate.StepVerify, mate.StepFailed, "", cv.Reason)
		return
	}
	cv.Filled = standupAlready
	if filled {
		cv.Filled = standupNow
	}
	hosts := []string{dev}
	for _, ds := range cv.devServices {
		if ds.deploy.running() {
			hosts = append(hosts, ds.ref.Hostname)
		}
	}
	for _, host := range hosts {
		if _, err := ops.ReloadSupervisor(ctx, d.batch.sshDeployer, host); err != nil {
			if cv.Reloads == nil {
				cv.Reloads = map[string]string{}
			}
			cv.Reloads[host] = err.Error()
		}
	}
}
