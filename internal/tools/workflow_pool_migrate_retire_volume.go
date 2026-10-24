package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// A variant-B pair (docs/spec-mate.md §2.8a.4a) keeps its repository on a code
// volume — a local-storage service — that every dev service of the pair
// mounts through run.volume. Its retire (docs/spec-workflows.md §13 PM-13)
// bundles the volume's checkout through the dev's mount and takes the
// volume's own reading of it in its container at /data; it deletes every dev
// service, each awaited until it reads gone by id; and it deletes the volume
// last — the only step that loses data, since a dev service's delete leaves
// the volume and its files, and a volume re-created under the same hostname is
// new and empty. The platform refuses the volume's delete while any service
// still mounts it, asynchronously: the delete answers a process that fails
// with containerAction "still mounted", naming no service. CodeVolume leaves
// the pair only once the volume reads gone.

// poolRetireGoneWait and poolRetireGoneEvery bound how long a delete whose
// process finished waits for its service to read gone by id: a deleted
// service reads DELETING for a few seconds after its process (live-verified:
// the by-id read answers serviceStackNotFound about two seconds after it).
const (
	poolRetireGoneWait  = 30 * time.Second
	poolRetireGoneEvery = 2 * time.Second
)

// poolRetireMountTaken is ops.UnmountService's status for a mount it took
// down.
const poolRetireMountTaken = "UNMOUNTED"

// poolRetireVolumeBundleSuffix names the bundle the retire takes from the
// volume itself when its checkout moved after the first one.
const poolRetireVolumeBundleSuffix = ".volume.bundle"

// poolRetireService is a service the retire deletes, by the id read directly.
type poolRetireService struct {
	Hostname  string `json:"hostname"`
	ServiceID string `json:"serviceId"`
}

// poolRetireVolume is a variant-B pair's code volume in the retire's record.
type poolRetireVolume struct {
	Hostname  string `json:"hostname"`
	ServiceID string `json:"serviceId"`
	MountPath string `json:"mountPath"`
	// Fingerprint is the checkout as the volume's own container reads it at
	// /data, taken with the bundle and re-read right before the volume's
	// delete; never compared with a dev service's reading, whose paths
	// differ.
	Fingerprint *ops.PoolMigrateFingerprint `json:"fingerprint,omitempty"`
	// Bundle is the second bundle the retire took from the volume itself,
	// when its checkout moved after the first, with what restores it.
	Bundle  *ops.PoolMigrateBundle `json:"bundle,omitempty"`
	Restore []string               `json:"restore,omitempty"`
	// Refused is the platform's last refusal of the volume's delete: some
	// service still mounts it.
	Refused string `json:"refused,omitempty"`
}

// poolRetireVolumeView is the code volume as an answer carries it.
type poolRetireVolumeView struct {
	Hostname  string   `json:"hostname"`
	ServiceID string   `json:"serviceId"`
	MountPath string   `json:"mountPath,omitempty"`
	Refused   string   `json:"refused,omitempty"`
	Bundle    string   `json:"bundle,omitempty"`
	Restore   []string `json:"restore,omitempty"`
}

func (v *poolRetireVolume) view() *poolRetireVolumeView {
	if v == nil {
		return nil
	}
	out := &poolRetireVolumeView{Hostname: v.Hostname, ServiceID: v.ServiceID, MountPath: v.MountPath, Refused: v.Refused, Restore: v.Restore}
	if v.Bundle != nil {
		out.Bundle = v.Bundle.Path
	}
	return out
}

// devs are the dev services the retire deletes, the dev first.
func (rec *poolRetireRecord) devs() []poolRetireService {
	return append([]poolRetireService{{Hostname: rec.Dev, ServiceID: rec.DevServiceID}}, rec.DevServices...)
}

// targets are every service the retire deletes, in order: the dev services,
// then the code volume.
func (rec *poolRetireRecord) targets() []poolRetireService {
	out := rec.devs()
	if v := rec.CodeVolume; v != nil {
		out = append(out, poolRetireService{Hostname: v.Hostname, ServiceID: v.ServiceID})
	}
	return out
}

// root is where the dev's repository lives on it: /var/www, or a variant-B
// pair's code volume mount.
func (rec *poolRetireRecord) root() string {
	if v := rec.CodeVolume; v != nil && v.MountPath != "" {
		return v.MountPath
	}
	return topology.CodeMountRoot
}

// volumeName is the code volume's hostname, "" for variant A.
func (rec *poolRetireRecord) volumeName() string {
	if rec.CodeVolume == nil {
		return ""
	}
	return rec.CodeVolume.Hostname
}

// resumedDestruction is the destruction a resume past the bundle states from
// the record alone: every service still to delete by its recorded id.
func (rec *poolRetireRecord) resumedDestruction(input WorkflowInput) DiagnosedDestruction {
	cause := fmt.Sprintf("service id %s; the retire is at %s: the pair is %s and the bundle %s is written", rec.DevServiceID, rec.Phase, rec.keyedBy(), poolRetireBundleName(rec))
	if v := rec.CodeVolume; v != nil {
		cause += "; " + poolRetireCodeVolumeCause(rec.DevServices, *v)
	}
	expected := poolRetireDestructionOf(rec.targets(), rec.KeptInBundle, rec.NotKept, input, cause)
	expected.Loss.UncommittedCode = len(rec.KeptInBundle) > 0
	if expected.Retry != nil {
		expected.Retry.PatchHints = nil
	}
	expected.Then = ""
	return expected
}

// poolRetireServiceNames names services by hostname, none when empty.
func poolRetireServiceNames(services []poolRetireService, none string) string {
	if len(services) == 0 {
		return none
	}
	names := make([]string, 0, len(services))
	for _, s := range services {
		names = append(names, s.Hostname)
	}
	return strings.Join(names, ", ")
}

// poolRetireCodeVolumeCause is what a variant-B retire deletes beyond the
// dev, in order.
func poolRetireCodeVolumeCause(devServices []poolRetireService, v poolRetireVolume) string {
	each := make([]string, 0, len(devServices))
	for _, s := range devServices {
		each = append(each, fmt.Sprintf("%s (id %s)", s.Hostname, s.ServiceID))
	}
	devs := "no further dev service"
	if len(each) > 0 {
		devs = strings.Join(each, ", ")
	}
	return fmt.Sprintf("then %s, each awaited until it reads gone by id; and last the code volume %s (id %s) with the checkout on it — the bundle is that checkout's only copy once it is gone, and the platform refuses the volume's delete while any service still mounts it",
		devs, v.Hostname, v.ServiceID)
}

// poolRetireCodeVolumeNotKept names what no copy keeps of a variant-B pair
// beyond what the dev's own retire loses; nil for variant A.
func poolRetireCodeVolumeNotKept(devServices []poolRetireService, volume string) []string {
	if volume == "" {
		return nil
	}
	var out []string
	for _, s := range devServices {
		out = append(out,
			s.Hostname+"'s own URLs: its subdomain embeds its hostname, and dies with it",
			"files on "+s.Hostname+"'s container outside the code volume")
	}
	return append(out, "the code volume "+volume+" itself: a volume created later under its hostname is new and empty, and files on it outside the checkout's repository are kept only as the census names them")
}

// readCodeVolume reads a variant-B pair's further dev services and its code
// volume by id — the ids their deletes use — and the services the checkout's
// zerops.yaml says mount the volume besides the dev services: one still
// mounting it would hold the volume's delete after every dev service is gone,
// when the checkout can no longer change.
func (g *poolRetireGuards) readCodeVolume(ctx context.Context, d poolRetireDeps, byName map[string]platform.ServiceStack) {
	cv := g.call.meta.CodeVolume
	for _, host := range cv.DevServiceHostnames() {
		svc, listed := byName[host]
		if !listed {
			g.resp.Warnings = append(g.resp.Warnings, fmt.Sprintf("%s, a dev service of the pair, is not among the project's services (read directly): there is nothing of it to delete.", host))
			continue
		}
		switch state, err := poolRetireDevState(ctx, d.client, svc.ID); {
		case err != nil:
			g.refuse(poolMigrateRefuseDevNotLive, fmt.Sprintf("%s could not be read by its id %s: %v. Call again.", host, svc.ID, err))
		case state == poolRetireDevDeleting:
			g.refuse(poolRetireRefuseDevDeleting, fmt.Sprintf("%s (id %s) reads DELETING by its id: a delete outside the retire is running. Call the flow again once it finishes.", host, svc.ID))
		case state == poolRetireDevLive:
			g.devServices = append(g.devServices, poolRetireService{Hostname: host, ServiceID: svc.ID})
		}
	}
	vol, listed := byName[cv.Hostname]
	if !listed {
		g.refuse(poolRetireRefuseVolumeNotLive, fmt.Sprintf("the code volume %s is not among the project's services (read directly), yet the pair names it: call again, or adopt the pair again.", cv.Hostname))
		return
	}
	switch state, err := poolRetireDevState(ctx, d.client, vol.ID); {
	case err != nil:
		g.refuse(poolRetireRefuseVolumeNotLive, fmt.Sprintf("the code volume %s could not be read by its id %s: %v. Call again.", cv.Hostname, vol.ID, err))
		return
	case state == poolRetireDevDeleting:
		g.refuse(poolRetireRefuseDevDeleting, fmt.Sprintf("the code volume %s (id %s) reads DELETING by its id: a delete outside the retire is running.", cv.Hostname, vol.ID))
		return
	case state == poolRetireDevGone:
		g.refuse(poolRetireRefuseVolumeNotLive, fmt.Sprintf("the code volume %s (id %s) no longer exists, and with it the checkout.", cv.Hostname, vol.ID))
		return
	}
	g.volume = &poolRetireVolume{Hostname: cv.Hostname, ServiceID: vol.ID, MountPath: cv.MountPath}
	g.readVolumeMounts(ctx, d)
}

// readVolumeMounts refuses while the checkout's zerops.yaml mounts the code
// volume in the setup of a stage or of a part's pool: the platform refuses to
// delete a volume any service still mounts, and the retire deletes it last,
// once no dev service is left to change the checkout through.
func (g *poolRetireGuards) readVolumeMounts(ctx context.Context, d poolRetireDeps) {
	dev, volume := g.call.dev.Name, g.call.meta.CodeVolume.Hostname
	file, setups, err := ops.CodeVolumeMountingSetups(ctx, d.ssh, dev, poolMigrateCodeRoot(g.call.meta), volume)
	if err != nil {
		g.refuse(poolRetireRefuseVolumeMounted, withSSHStderr("the checkout's config file, which says which services mount the code volume "+volume+", could not be read", err)+". Call again.")
		return
	}
	runners := poolRetireSetupRunners(g.call.meta.Stages, g.partSetups())
	var mounted []string
	for _, setup := range setups {
		for _, svc := range runners[setup] {
			mounted = append(mounted, fmt.Sprintf("%s (setup %s)", svc, setup))
		}
	}
	if len(mounted) > 0 {
		verb, setups, deploy := "mount", "those setups", "each service"
		if len(mounted) == 1 {
			verb, setups, deploy = "mounts", "that setup", "the service"
		}
		g.refuse(poolRetireRefuseVolumeMounted, fmt.Sprintf("%s %s the code volume %s by the checkout's %s (run.volume): the platform refuses to delete a volume while any service still mounts it, and the retire deletes it last, after every dev service. Remove run.volume from %s, commit, and deploy %s — a deploy replaces its container without the mount — then call the retire again.",
			poolRetireJoinAnd(mounted), verb, volume, file, setups, deploy))
	}
}

// poolRetireJoinAnd joins names as "a", "a and b", "a, b and c".
func poolRetireJoinAnd(names []string) string {
	if len(names) < 2 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

// partSetups are the setups the parts' pools deploy with, by pool.
func (g *poolRetireGuards) partSetups() map[string]string {
	out := make(map[string]string, len(g.parts))
	for _, p := range g.parts {
		out[p.pool.Name] = p.poolSetup
	}
	return out
}

// poolRetireSetupRunners are the services known to run a setup of the
// pair's repository — its stages and its parts' pools — by setup.
func poolRetireSetupRunners(stages []topology.StageRef, poolSetups map[string]string) map[string][]string {
	out := map[string][]string{}
	for _, st := range stages {
		if st.SetupName != "" {
			out[st.SetupName] = append(out[st.SetupName], "stage "+st.Hostname)
		}
	}
	for _, pool := range slices.Sorted(func(yield func(string) bool) {
		for p := range poolSetups {
			if !yield(p) {
				return
			}
		}
	}) {
		setup := poolSetups[pool]
		if setup == "" {
			setup = pool
		}
		out[setup] = append(out[setup], "pool "+pool)
	}
	return out
}

// awaitGone reads the service by id until it reads gone, while it reads
// DELETING and at most goneWait: a deleted service lingers DELETING a few
// seconds after its delete's process finished.
func (r *poolRetireRun) awaitGone(ctx context.Context, id string) (string, error) {
	wait, every := r.d.goneWait, r.d.goneEvery
	if wait == 0 {
		wait = poolRetireGoneWait
	}
	if every == 0 {
		every = poolRetireGoneEvery
	}
	deadline := time.Now().Add(wait)
	for {
		state, err := poolRetireDevState(ctx, r.d.client, id)
		if err != nil || state != poolRetireDevDeleting || time.Now().After(deadline) {
			return state, err
		}
		select {
		case <-ctx.Done():
			return state, nil
		case <-time.After(every):
		}
	}
}

// devStates reads every dev service of the record by its id.
func (r *poolRetireRun) devStates(ctx context.Context) (map[string]string, error) {
	states := map[string]string{}
	for _, s := range r.rec.devs() {
		state, err := poolRetireDevState(ctx, r.d.client, s.ServiceID)
		if err != nil {
			return nil, fmt.Errorf("%s could not be read by its id: %w", s.Hostname, err)
		}
		states[s.Hostname] = state
	}
	return states, nil
}

// deleteDevs deletes every dev service of a variant-B pair by its recorded
// id, the dev first, each awaited until it reads gone. While none is deleted
// yet the dev's checkout is fenced and its fingerprint re-checked first, as a
// single dev's is — a change still aborts the retire cleanly; once one is
// gone the volume's own reading guards the volume's delete instead. A dev
// service already gone is never deleted again.
func (r *poolRetireRun) deleteDevs(ctx context.Context) *mcp.CallToolResult {
	devs := r.rec.devs()
	states, err := r.devStates(ctx)
	if err != nil {
		return r.callAgain(err.Error())
	}
	var deleting, live []poolRetireService
	for _, s := range devs {
		switch states[s.Hostname] {
		case poolRetireDevDeleting:
			deleting = append(deleting, s)
		case poolRetireDevLive:
			live = append(live, s)
		}
	}
	if len(deleting) > 0 {
		return r.callAgain(poolRetireServiceNames(deleting, "") + " read DELETING by id")
	}
	switch {
	case len(live) == len(devs):
		if err := r.fence(ctx); err != nil {
			return r.abort(ctx, poolRetireAbortFence, fmt.Sprintf("a dev service's mount could not be taken down again: %v.", err))
		}
		if !r.rec.holdsBundle() {
			return r.abort(ctx, poolRetireAbortUnreadable, "the record holds no bundle on this container to check the repository against.")
		}
		now, err := ops.ReadPoolMigrateFingerprint(ctx, r.d.ssh, r.rec.Dev, r.rec.root(), r.workDir())
		if err != nil {
			return r.abort(ctx, poolRetireAbortUnreadable, withSSHStderr("the repository could not be read again right before the delete", err)+"; nothing is deleted that zcp cannot prove the bundle holds.")
		}
		if moved := r.rec.Bundle.Fingerprint.Moved(now); len(moved) > 0 {
			return r.abort(ctx, poolRetireAbortFingerprint, fmt.Sprintf("%s's repository moved after its bundle was taken — %s. Something still writes to it (the Mate app's shell or crew, an agent over SSH); call the retire again once it is done: a new bundle is taken.", r.rec.Dev, strings.Join(moved, "; ")))
		}
	case len(live) > 0:
		if err := r.fence(ctx); err != nil {
			return r.callAgain(fmt.Sprintf("a dev service's mount could not be taken down: %v", err))
		}
	}
	for _, s := range live {
		proc, err := ops.DeleteByID(ctx, r.d.client, s.ServiceID)
		if err != nil {
			if st, _ := poolRetireDevState(ctx, r.d.client, s.ServiceID); st == poolRetireDevGone {
				continue
			}
			return r.callAgain(fmt.Sprintf("the delete of %s (id %s) failed: %v", s.Hostname, s.ServiceID, err))
		}
		pollManageProcess(ctx, r.d.client, proc, nil)
		state, err := r.awaitGone(ctx, s.ServiceID)
		switch {
		case err != nil:
			return r.callAgain(fmt.Sprintf("%s could not be read by its id after the delete: %v", s.Hostname, err))
		case state == poolRetireDevDeleting:
			return r.callAgain(fmt.Sprintf("the delete of %s runs: it reads DELETING by its id", s.Hostname))
		case state == poolRetireDevLive:
			return r.callAgain(fmt.Sprintf("the delete of %s did not take: it still reads live by its id", s.Hostname))
		}
	}
	return r.devsGone(ctx)
}

// devsGone moves a variant-B retire whose every dev service reads gone on to
// its code volume: the pair re-keyed (a no-op once done) and naming none of
// the dev services any more — the volume still recorded — the dev's HQ
// backoff record dropped, any stale mount cleared.
func (r *poolRetireRun) devsGone(ctx context.Context) *mcp.CallToolResult {
	if err := r.rekey(); err != nil && !errors.Is(err, workflow.ErrServiceMetaNotFound) {
		return r.callAgain(fmt.Sprintf("the dev services are gone, and the pair could not be %s yet: %v", r.rec.keyedBy(), err))
	}
	if err := workflow.SettleMigratedCodeVolume(r.d.stateDir, r.rec.Dev, r.rec.pools(), false); err != nil {
		return r.callAgain(fmt.Sprintf("the dev services are gone, and the pair could not forget them yet: %v", err))
	}
	if err := forgetHQPairState(r.d.stateDir, r.rec.Dev); err != nil {
		r.resp.Warnings = append(r.resp.Warnings, err.Error())
	}
	if r.d.mounter != nil {
		for _, s := range r.rec.devs() {
			_, _ = ops.UnmountService(ctx, r.d.client, r.d.projectID, r.d.mounter, s.Hostname)
		}
	}
	if err := r.enter(poolRetireDeletingVolume); err != nil {
		return r.saveFailed(err)
	}
	return r.deleteVolume(ctx)
}

// deleteVolume deletes a variant-B pair's code volume by its recorded id once
// every dev service is gone — the retire's last step, and the only one that
// loses data — and reads its delete's process: finished and the volume gone
// by id finishes the retire, CodeVolume cleared; failed with the platform's
// "still mounted" refusal keeps CodeVolume and the record, saying some
// service still mounts the volume. Right before the delete it re-reads the
// checkout in the volume's own container: one that moved since the bundle is
// deleted only once this call carries force, after a new bundle of it.
func (r *poolRetireRun) deleteVolume(ctx context.Context) *mcp.CallToolResult {
	v := r.rec.CodeVolume
	state, err := poolRetireDevState(ctx, r.d.client, v.ServiceID)
	switch {
	case err != nil:
		return r.callAgain(fmt.Sprintf("the code volume %s could not be read by its id: %v", v.Hostname, err))
	case state == poolRetireDevGone:
		v.Refused = ""
		return r.finish(ctx)
	case state == poolRetireDevDeleting:
		return r.callAgain(fmt.Sprintf("the code volume %s reads DELETING by its id", v.Hostname))
	}
	now, err := ops.ReadPoolMigrateFingerprint(ctx, r.d.ssh, v.Hostname, ops.CodeVolumeDataRoot, r.workDir())
	if err != nil {
		return r.callAgain(withSSHStderr("the checkout on the code volume "+v.Hostname+" could not be read in its own container at "+ops.CodeVolumeDataRoot, err) +
			"; nothing on it is deleted that zcp cannot check against its bundle")
	}
	if moved := poolRetireVolumeMoved(v, now); len(moved) > 0 {
		if !r.forceNow {
			return r.volumeHeld(poolRetireAbortFingerprint, fmt.Sprintf("The checkout on the code volume %s moved after its bundle was taken — %s — while the dev services were being deleted, so the bundle does not hold it and nothing on the volume is deleted. Every dev service is gone: only once the person agreed to deleting the volume as it is now, re-send the retire with force=true — a new verified bundle is taken from the volume itself before its delete.",
				v.Hostname, strings.Join(moved, "; ")))
		}
		if result := r.rebundleVolume(ctx); result != nil {
			return result
		}
	}
	proc, err := ops.DeleteByID(ctx, r.d.client, v.ServiceID)
	if err != nil {
		if st, _ := poolRetireDevState(ctx, r.d.client, v.ServiceID); st == poolRetireDevGone {
			v.Refused = ""
			return r.finish(ctx)
		}
		return r.callAgain(fmt.Sprintf("the delete of the code volume %s (id %s) failed: %v", v.Hostname, v.ServiceID, err))
	}
	final, timedOut := pollManageProcess(ctx, r.d.client, proc, nil)
	if !timedOut && ops.VolumeStillMounted(final) {
		v.Refused = *final.FailReason
		if err := savePoolRetireRecord(r.d.stateDir, r.rec); err != nil {
			return r.saveFailed(err)
		}
		return r.volumeHeld(poolRetireRefuseVolumeMounted, r.volumeMountedMessage(ctx))
	}
	if !timedOut && final != nil && final.Status == platform.ProcessStatusFailed {
		reason := "no reason given"
		if final.FailReason != nil {
			reason = *final.FailReason
		}
		return r.callAgain(fmt.Sprintf("the delete of the code volume %s failed: %s", v.Hostname, reason))
	}
	state, err = r.awaitGone(ctx, v.ServiceID)
	switch {
	case err != nil:
		return r.callAgain(fmt.Sprintf("the code volume %s could not be read by its id after the delete: %v", v.Hostname, err))
	case state == poolRetireDevGone:
		v.Refused = ""
		return r.finish(ctx)
	case state == poolRetireDevDeleting:
		return r.callAgain(fmt.Sprintf("the delete of the code volume %s runs: it reads DELETING by its id", v.Hostname))
	}
	return r.callAgain(fmt.Sprintf("the delete of the code volume %s did not take: it still reads live by its id", v.Hostname))
}

// poolRetireVolumeMoved names what moved in the volume's checkout since its
// own reading was recorded; a record with none counts as moved.
func poolRetireVolumeMoved(v *poolRetireVolume, now ops.PoolMigrateFingerprint) []string {
	if v.Fingerprint == nil {
		return []string{"the record holds no reading of the volume's checkout to compare with"}
	}
	return v.Fingerprint.Moved(now)
}

// rebundleVolume takes a verified bundle of the checkout from the volume's
// own container, beside the first, and records its fingerprint as the one
// the volume's delete is checked against; nil once both are recorded.
func (r *poolRetireRun) rebundleVolume(ctx context.Context) *mcp.CallToolResult {
	v := r.rec.CodeVolume
	b, err := ops.BuildPoolMigrateBundle(ctx, r.d.ssh, ops.PoolMigrateBundleInput{
		Dev: v.Hostname, Root: ops.CodeVolumeDataRoot, WorkDir: r.workDir(),
		Path: filepath.Join(r.d.stateDir, "migrate", r.rec.Dev+poolRetireVolumeBundleSuffix),
	})
	if err != nil {
		return r.callAgain(withSSHStderr("the bundle of the checkout on the code volume "+v.Hostname+" could not be written and verified", err) +
			"; the volume is deleted only once one is")
	}
	v.Bundle = b
	v.Restore = ops.PoolMigrateRestoreSteps(b, r.rec.Origin)
	v.Fingerprint = &b.Fingerprint
	r.rec.Forced = true
	if err := savePoolRetireRecord(r.d.stateDir, r.rec); err != nil {
		return r.saveFailed(err)
	}
	return nil
}

// bundleFromVolume takes the retire's bundle from the volume's own container
// when the dev went before the retire bundled its repository — outside the
// retire — and no dev service is left to read it through: the census there
// holds the retire as it would on the dev unless forced, and the bundle's
// fingerprint is the one the volume's delete is checked against.
func (r *poolRetireRun) bundleFromVolume(ctx context.Context) *mcp.CallToolResult {
	v := r.rec.CodeVolume
	census, err := ops.ReadPoolMigrateCensus(ctx, r.d.ssh, v.Hostname, ops.CodeVolumeDataRoot)
	if err != nil {
		return r.callAgain(withSSHStderr("the checkout on the code volume "+v.Hostname+" could not be read in its own container at "+ops.CodeVolumeDataRoot, err))
	}
	if blocking := poolRetireBlocking(census); len(blocking) > 0 && !r.force {
		return r.volumeHeld(poolRetireRefuseUnsaved, poolRetireUnsavedRefusal(v.Hostname, blocking).Message)
	}
	b, err := ops.BuildPoolMigrateBundle(ctx, r.d.ssh, ops.PoolMigrateBundleInput{
		Dev: v.Hostname, Root: ops.CodeVolumeDataRoot, WorkDir: r.workDir(), Path: poolRetireBundlePath(r.d.stateDir, r.rec.Dev),
	})
	if err != nil {
		return r.callAgain(withSSHStderr("the bundle of the checkout on the code volume "+v.Hostname+" could not be written and verified", err) +
			"; nothing is deleted without one")
	}
	r.rec.Bundle = b
	r.rec.Restore = ops.PoolMigrateRestoreSteps(b, r.rec.Origin)
	r.rec.KeptInBundle = poolRetireKeptInBundle(census)
	r.rec.NotKept = append(poolRetireNotKept(r.rec.Dev, census, b), poolRetireCodeVolumeNotKept(r.rec.DevServices, v.Hostname)...)
	r.rec.Forced = r.rec.Forced || (r.force && len(poolRetireBlocking(census)) > 0)
	v.Fingerprint = &b.Fingerprint
	if err := savePoolRetireRecord(r.d.stateDir, r.rec); err != nil {
		return r.saveFailed(err)
	}
	return nil
}

// volumeHeld answers a variant-B retire held at its code volume: the record
// stays, the step is retiring, and the refusal says what holds the delete.
func (r *poolRetireRun) volumeHeld(reason, message string) *mcp.CallToolResult {
	resp := poolMigrateAt(r.resp, poolMigrateStepRetiring, nil,
		fmt.Sprintf("%s The retire keeps its record at %s: once that is resolved, call zerops_workflow workflow=%q action=%q service=%q again with the same acknowledgment.",
			message, r.rec.Phase, workflowPoolMigrate, actionRetire, r.rec.Dev))
	resp.Action = actionRetire
	resp.Refusals = []poolMigrateRefusal{{Reason: reason, Message: message}}
	resp.Retire = r.rec.view()
	return jsonResult(resp)
}

// volumeMountedMessage says the platform refused the volume's delete because
// a service still mounts it — which one it does not say — naming the services
// the checkout's zerops.yaml says mount it, as far as it can be read in the
// volume's container.
func (r *poolRetireRun) volumeMountedMessage(ctx context.Context) string {
	v := r.rec.CodeVolume
	msg := fmt.Sprintf("The platform refused to delete the code volume %s (id %s): some service still mounts it, and the platform does not say which (%s). Every dev service of the pair is gone; the volume stays, still working, its checkout intact, and the pair keeps naming it.",
		v.Hostname, v.ServiceID, v.Refused)
	if hint := r.volumeMountHint(ctx); hint != "" {
		msg += " " + hint
	}
	return msg + " Remove run.volume from that service's setup in zerops.yaml and deploy it — a deploy replaces its container without the mount — or delete the service; then call the retire again."
}

// volumeMountHint names the stages and pools whose setup in the checkout's
// zerops.yaml — as the volume's container reads it — mounts the volume; ""
// when it names none or cannot be read.
func (r *poolRetireRun) volumeMountHint(ctx context.Context) string {
	v := r.rec.CodeVolume
	file, setups, err := ops.CodeVolumeMountingSetups(ctx, r.d.ssh, v.Hostname, ops.CodeVolumeDataRoot, v.Hostname)
	if err != nil {
		return ""
	}
	var stages []topology.StageRef
	poolSetups := map[string]string{}
	for _, pool := range r.rec.pools() {
		m, err := workflow.ReadServiceMeta(r.d.stateDir, pool)
		if err != nil || m == nil {
			continue
		}
		stages = append(stages, m.Stages...)
		poolSetups[pool] = m.PrimarySetupName
	}
	runners := poolRetireSetupRunners(stages, poolSetups)
	var named []string
	for _, setup := range setups {
		for _, svc := range runners[setup] {
			named = append(named, fmt.Sprintf("%s (setup %s)", svc, setup))
		}
	}
	if len(named) == 0 {
		return ""
	}
	return fmt.Sprintf("The checkout's %s mounts it in the setup of %s.", file, poolRetireJoinAnd(named))
}

// resumeCodeVolume resumes a variant-B retire whose dev is gone — deleted by
// the retire, or outside it — from the states its further dev services and
// its code volume read by id: anything DELETING keeps the record (call
// again); nothing left finishes; whatever is left to delete is deleted only
// on the acknowledged call — the pair re-keyed first, so a stage never stays
// under a dead key, and the repository bundled from the volume itself when
// the dev went before the retire bundled it.
func (r *poolRetireRun) resumeCodeVolume(ctx context.Context, input WorkflowInput) *mcp.CallToolResult {
	rec := r.rec
	states, err := r.devStates(ctx)
	if err != nil {
		return r.callAgain(err.Error())
	}
	volume, err := poolRetireDevState(ctx, r.d.client, rec.CodeVolume.ServiceID)
	if err != nil {
		return r.callAgain(fmt.Sprintf("the code volume %s could not be read by its id: %v", rec.CodeVolume.Hostname, err))
	}
	var deleting, live []string
	for _, s := range rec.devs() {
		switch states[s.Hostname] {
		case poolRetireDevDeleting:
			deleting = append(deleting, s.Hostname)
		case poolRetireDevLive:
			live = append(live, s.Hostname)
		}
	}
	if volume == poolRetireDevDeleting {
		deleting = append(deleting, rec.CodeVolume.Hostname)
	}
	if len(deleting) > 0 {
		return r.callAgain(strings.Join(deleting, ", ") + " read DELETING by id")
	}
	if len(live) == 0 && volume == poolRetireDevGone {
		return r.held(ctx, func() *mcp.CallToolResult { return r.finish(ctx) })
	}
	expected := rec.resumedDestruction(input)
	if err := ValidateDestructiveAck(input.ConfirmDestructive, expected); err != nil {
		return convertError(err, WithWouldDestroy(&expected), WithRecoveryStatus())
	}
	return r.held(ctx, func() *mcp.CallToolResult {
		if err := r.rekey(); err != nil && !errors.Is(err, workflow.ErrServiceMetaNotFound) {
			return r.callAgain(fmt.Sprintf("%s is gone, and the pair could not be %s yet: %v", rec.Dev, rec.keyedBy(), err))
		}
		if !rec.holdsBundle() {
			if result := r.bundleFromVolume(ctx); result != nil {
				return result
			}
		}
		// The volume goes last: a dev service still live is deleted first,
		// whatever phase the record reached.
		if rec.Phase != poolRetireDeleting && (rec.Phase != poolRetireDeletingVolume || len(live) > 0) {
			if err := r.enter(poolRetireDeleting); err != nil {
				return r.saveFailed(err)
			}
		}
		return r.advance(ctx)
	})
}

// poolRetireRecordNaming is the retire record in flight that deletes service
// as a variant-B pair's further dev service or its code volume; nil for none.
// zerops_delete refuses such a service as it refuses the dev (PM-7).
func poolRetireRecordNaming(stateDir, service string) *poolRetireRecord {
	if stateDir == "" || service == "" {
		return nil
	}
	paths, _ := filepath.Glob(filepath.Join(stateDir, "migrate", "*.json"))
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var rec poolRetireRecord
		if json.Unmarshal(raw, &rec) != nil || !poolRetireInFlight(rec.Phase) || rec.CodeVolume == nil {
			continue
		}
		if rec.CodeVolume.Hostname == service || slices.ContainsFunc(rec.DevServices, func(s poolRetireService) bool { return s.Hostname == service }) {
			return &rec
		}
	}
	return nil
}
