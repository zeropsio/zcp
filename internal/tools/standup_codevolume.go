package tools

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// A variant-B pair (docs/spec-mate.md §2.8a) keeps its repository on a code
// volume every dev service of the pair mounts. zcp sets up no new one
// (§2.8a.4a): the stand-up reads a tier's local-storage service as a managed
// service, and pairs several dev services of one repository as variant B
// only when zcp already records that pair (workflow.ParseMateTier) — a
// stand-up called again on a project set up before. For that pair it keeps
// the record with its code volume, reads the volume's mount from the dev
// setups once the repository is checked out, and deploys the dev half from
// the checkout in /var/www, then every further dev service with its own dev
// setup from the same checkout, then fills the volume once from it (left as
// it is when it already holds the repository) and reloads every dev
// service's supervisor. The pair's stages build from the volume, so they
// wait for the fill.

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

// tierCodeVolume is the code volume a recorded variant-B pair is adopted
// with again: the tier's volume and further dev services, and the mount the
// record already holds for that volume ("" until the dev setups are read,
// recordCodeVolumeMount). Nil for any other pair.
func tierCodeVolume(p workflow.MateTierPair, existing *workflow.ServiceMeta) *topology.CodeVolume {
	if p.CodeVolume == "" {
		return nil
	}
	v := &topology.CodeVolume{Hostname: p.CodeVolume}
	if existing != nil && existing.CodeVolume != nil && existing.CodeVolume.Hostname == p.CodeVolume {
		v.MountPath = existing.CodeVolume.MountPath
	}
	for _, ds := range p.DevServices {
		v.DevServices = append(v.DevServices, topology.DevServiceRef{Hostname: ds.Hostname, SetupName: ds.Setup})
	}
	return v
}

// recordCodeVolumeMount reads where every dev setup of a variant-B pair mounts
// its code volume — run.volume in the zerops.yaml the checkout put on the dev
// half's mount, the repository being the truth — and records it on the pair.
// A dev setup that does not mount the volume, or mounts it elsewhere than the
// others or outside /var/www, stops the pair: its code would not be shared.
func (d standupDeps) recordCodeVolumeMount(sp *standupPair) {
	p := sp.pair
	dev := p.Dev.Hostname
	doc, err := ops.ParseZeropsYml(filepath.Join(projectRootFromState(d.batch.stateDir), dev))
	if err != nil {
		sp.fail(fmt.Sprintf("%s keeps its repository on the code volume %s, and its zerops.yaml, which says where the dev setups mount it, cannot be read: %v", dev, p.CodeVolume, err),
			"Check the repository's zerops.yaml on main, then call zerops_standup again.")
		return
	}
	mountPath := ""
	for _, rt := range append([]workflow.MateTierRuntime{p.Dev}, p.DevServices...) {
		mounted := ""
		if entry := doc.FindEntry(rt.Setup); entry != nil && entry.Run.Volume.Hostname == p.CodeVolume {
			mounted = entry.Run.Volume.MountPath
		}
		var why string
		switch {
		case mounted == "":
			why = fmt.Sprintf("the dev setup %s of %s does not mount the code volume %s (run.volume)", rt.Setup, rt.Hostname, p.CodeVolume)
		case mountPath != "" && mounted != mountPath:
			why = fmt.Sprintf("the dev setup %s of %s mounts the code volume %s at %s, the others at %s", rt.Setup, rt.Hostname, p.CodeVolume, mounted, mountPath)
		default:
			why = topology.CodeVolumeMountPathError(mounted)
		}
		if why != "" {
			sp.fail(why, fmt.Sprintf("Mount it in every dev setup of the pair with run.volume {hostname: %s, mountPath: /var/www/<folder>} in zerops.yaml on main, then call zerops_standup again.", p.CodeVolume))
			return
		}
		mountPath = mounted
	}
	err = workflow.UpsertServiceMeta(d.batch.stateDir, dev, func(m *workflow.ServiceMeta, existed bool) error {
		if !existed || m.CodeVolume == nil {
			return fmt.Errorf("%s is not recorded with its code volume", dev)
		}
		m.CodeVolume.MountPath = mountPath
		return nil
	})
	if err != nil {
		sp.fail(fmt.Sprintf("recording where %s mounts its code volume failed: %v", dev, err), "Call zerops_standup again.")
	}
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
	filled, err := ops.FillCodeVolume(ctx, d.batch.sshDeployer, dev, cv.volume.Hostname, cv.volume.MountPath)
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
