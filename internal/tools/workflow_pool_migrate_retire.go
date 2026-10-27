package tools

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// The retire is the pool migration's only permanent act: it deletes the part's
// old dev service once the pool took over, after the person agreed by name
// (docs/spec-workflows.md §13 PM-3, PM-5…PM-8). zerops_workflow
// workflow="pool-migrate" action="retire" service=<dev> first diagnoses —
// every guard re-read, the destruction named in wouldDestroy — and goes ahead
// only on a re-call carrying confirmDestructive {operation: "pool-retire",
// acknowledgedTargets: [<dev>]}, plus force when the dev holds unsaved work.
// It then walks phases, each saved in the record before the work it names:
// started, fenced (the dev unmounted), bundled (a verified bundle of its
// repository on this container, the fingerprint taken with it), rewritten (the
// pair re-keyed onto the pool), deleting (the fingerprint re-read equal, then
// the delete by the id read directly), retired. Every later call resumes from
// the record against the dev's state by id: live, DELETING or gone. A resume
// does the phase it reads again, so a record at bundled — which holds no
// bundle when the bundle was cut short — takes a new one, and nothing is
// deleted on a resume whose record holds no verified bundle. A
// variant-B pair's retire deletes every dev service at deleting and its code
// volume last, at deleting-volume (workflow_pool_migrate_retire_volume.go).

// poolRetireOperation names the retire's destructive operation
// (DiagnosedDestruction.Operation, confirmDestructive.operation).
const poolRetireOperation = "pool-retire"

// actionRetire is the retire's call: zerops_workflow workflow="pool-migrate"
// action="retire".
const actionRetire = "retire"

// The phases a retire record walks (docs/spec-workflows.md §13 PM-6).
const (
	poolRetireStarted   = "started"
	poolRetireFenced    = "fenced"
	poolRetireBundled   = "bundled"
	poolRetireRewritten = "rewritten"
	poolRetireDeleting  = "deleting"
	// poolRetireDeletingVolume is a variant-B retire whose dev services are
	// gone: its code volume is deleted last (PM-13).
	poolRetireDeletingVolume = "deleting-volume"
	poolRetireRetired        = "retired"
	poolRetireAborted        = "aborted"
)

// Response statuses of the retire beside the flow's own.
const (
	poolMigrateStatusRetireRefused = "retire-refused"
	poolMigrateStatusAborted       = "aborted"
)

// Why a retire is refused before it starts, or aborted after.
const (
	poolRetireRefuseNotReady        = "not-ready"
	poolRetireRefuseDevDeleting     = "dev-deleting"
	poolRetireRefuseDevBusy         = "dev-busy"
	poolRetireRefuseLaunch          = "launch-active"
	poolRetireRefuseEnvReference    = "env-reference"
	poolRetireRefuseDomain          = "domain"
	poolRetireRefuseNoLease         = "no-lease"
	poolRetireRefuseLeaseUnverified = "lease-unverified"
	poolRetireRefuseLeaseUnreadable = "lease-unreadable"
	poolRetireRefuseUnsaved         = "unsaved-work"
	poolRetireRefusePairBusy        = "pair-busy"
	poolRetireRefuseVolumeMounted   = "volume-mounted"
	poolRetireRefuseVolumeNotLive   = "volume-not-live"
	poolRetireAbortFence            = "fence-failed"
	poolRetireAbortBundle           = "bundle-failed"
	poolRetireAbortRekey            = "rekey-failed"
	poolRetireAbortFingerprint      = "fingerprint-changed"
	poolRetireAbortUnreadable       = "fingerprint-unreadable"
)

// poolRetireAtomID carries the lease step, the retire and what it leaves.
const poolRetireAtomID = "pool-migrate-retire"

// leaseProbeAction is zerops_lease's acquire.
const leaseProbeAction = "acquire"

// The dev service's state by id, as the resume table reads it.
const (
	poolRetireDevLive     = "live"
	poolRetireDevDeleting = "deleting"
	poolRetireDevGone     = "gone"
)

// poolRetireRecord is the retire's record, .zcp/state/migrate/<dev>.json
// (mode 0600): what it acts on, by the ids read directly, where it is, and
// what it kept. Never a credential.
type poolRetireRecord struct {
	Dev           string   `json:"dev"`
	DevServiceID  string   `json:"devServiceId"`
	Pool          string   `json:"pool"`
	PoolServiceID string   `json:"poolServiceId"`
	PoolSetup     string   `json:"poolSetup,omitempty"`
	Stages        []string `json:"stages,omitempty"`
	// Parts are the parts of a dev serving several, each with its own pool:
	// the retire splits the pair into one pool-keyed pair per part. Empty for
	// a single part, whose pool Pool, PoolServiceID and PoolSetup name.
	Parts []poolRetirePart `json:"parts,omitempty"`
	// DevServices are a variant-B pair's further dev services, by the ids read
	// directly: the retire deletes each after the dev. CodeVolume is its code
	// volume, deleted last (PM-13). Both absent for variant A.
	DevServices []poolRetireService `json:"devServices,omitempty"`
	CodeVolume  *poolRetireVolume   `json:"codeVolume,omitempty"`
	// Origin is the repository the dev's work moved through: the bundle
	// carries no remote URL.
	Origin    string `json:"origin,omitempty"`
	Phase     string `json:"phase"`
	Forced    bool   `json:"forced,omitempty"`
	StartedAt string `json:"startedAt"`
	UpdatedAt string `json:"updatedAt,omitempty"`
	// Bundle is the verified bundle of the dev's repository, with the
	// fingerprint the delete re-checks; Restore puts it back.
	Bundle       *ops.PoolMigrateBundle `json:"bundle,omitempty"`
	Restore      []string               `json:"restore,omitempty"`
	KeptInBundle []string               `json:"keptInBundle,omitempty"`
	NotKept      []string               `json:"notKept,omitempty"`
	AbortReason  string                 `json:"abortReason,omitempty"`
	RetiredAt    string                 `json:"retiredAt,omitempty"`
	// DeletesSent are the dev services whose delete the retire sent, by
	// service id, each with its delete's process id — "" while the send's
	// answer is not recorded: a resume never aborts while one may still run.
	DeletesSent map[string]string `json:"deletesSent,omitempty"`
}

// poolRetirePart is one part of a dev serving several in the retire's record:
// its stage, and the pool keying its pair after the retire, by the id read
// directly, with the setup the pool's image deploys with.
type poolRetirePart struct {
	Stage         string `json:"stage"`
	Pool          string `json:"pool"`
	PoolServiceID string `json:"poolServiceId"`
	PoolSetup     string `json:"poolSetup,omitempty"`
}

// pools are the pools the retire keys the pair by: the one, or every part's.
func (rec *poolRetireRecord) pools() []string {
	if len(rec.Parts) == 0 {
		return []string{rec.Pool}
	}
	out := make([]string, 0, len(rec.Parts))
	for _, p := range rec.Parts {
		out = append(out, p.Pool)
	}
	return out
}

// partMoves are the split's parts, for a dev serving several.
func (rec *poolRetireRecord) partMoves() []workflow.PoolPartMove {
	out := make([]workflow.PoolPartMove, 0, len(rec.Parts))
	for _, p := range rec.Parts {
		out = append(out, workflow.PoolPartMove{Stage: p.Stage, Pool: p.Pool, PoolServiceID: p.PoolServiceID, PoolSetup: p.PoolSetup})
	}
	return out
}

// keyedBy says how the retire keys the pair: "keyed by <pool>", or split into
// one pair per part, each named.
func (rec *poolRetireRecord) keyedBy() string {
	if len(rec.Parts) == 0 {
		return "keyed by " + rec.Pool
	}
	each := make([]string, 0, len(rec.Parts))
	for _, p := range rec.Parts {
		each = append(each, p.Stage+"'s keyed by "+p.Pool)
	}
	return "split into one pair per part — " + strings.Join(each, ", ")
}

// partStates lists a record's parts for an answer, each at step.
func (rec *poolRetireRecord) partStates(step string) []poolMigratePartState {
	if len(rec.Parts) == 0 {
		return nil
	}
	out := make([]poolMigratePartState, 0, len(rec.Parts))
	for _, p := range rec.Parts {
		out = append(out, poolMigratePartState{Stage: p.Stage, Pool: p.Pool, Step: step})
	}
	return out
}

// poolRetireView is the record as an answer carries it.
type poolRetireView struct {
	Phase        string                `json:"phase"`
	DevServiceID string                `json:"devServiceId"`
	Forced       bool                  `json:"forced,omitempty"`
	Bundle       *poolRetireBundleView `json:"bundle,omitempty"`
	Restore      []string              `json:"restore,omitempty"`
	KeptInBundle []string              `json:"keptInBundle,omitempty"`
	NotKept      []string              `json:"notKept,omitempty"`
	AbortReason  string                `json:"abortReason,omitempty"`
	// CodeVolume is a variant-B pair's code volume: what the retire knows of
	// its delete.
	CodeVolume *poolRetireVolumeView `json:"codeVolume,omitempty"`
	// Gaps are what a migrated part does not have yet.
	Gaps []string `json:"gaps,omitempty"`
}

type poolRetireBundleView struct {
	Path       string   `json:"path"`
	SHA256     string   `json:"sha256"`
	Bytes      int64    `json:"bytes"`
	Refs       int      `json:"refs"`
	Stashes    int      `json:"stashes,omitempty"`
	ReflogTips int      `json:"reflogTips,omitempty"`
	Trees      []string `json:"trees"`
}

// poolRetireGaps are the follow-ups a migrated part waits for.
func poolRetireGaps() []string {
	return []string{
		"The group recipe's tiers on main still name this part's dev service until a person merges the change zcp proposes on its next recipe pass, giving the part its dev pool there (zerops_workflow action=\"group-recipe\" proposes it now); a tier a person or another Mate wrote is left as it is.",
		"Launching or exporting this part to production waits for a follow-up: launch and export refuse a part whose dev half is a pool.",
	}
}

func (rec *poolRetireRecord) view() *poolRetireView {
	v := &poolRetireView{
		Phase: rec.Phase, DevServiceID: rec.DevServiceID, Forced: rec.Forced,
		Restore: rec.Restore, KeptInBundle: rec.KeptInBundle, NotKept: rec.NotKept, AbortReason: rec.AbortReason,
	}
	if b := rec.Bundle; b != nil {
		bv := &poolRetireBundleView{Path: b.Path, SHA256: b.SHA256, Bytes: b.Bytes, Refs: b.Refs, Stashes: b.Stashes, ReflogTips: b.ReflogTips}
		for _, t := range b.Trees {
			bv.Trees = append(bv.Trees, t.Path)
		}
		v.Bundle = bv
	}
	v.CodeVolume = rec.CodeVolume.view()
	if rec.Phase == poolRetireRetired {
		v.Gaps = poolRetireGaps()
	}
	return v
}

// holdsBundle reports a record whose verified bundle is on this container:
// recorded — the bundle step records one only once it is written and
// verified — and its file there at the size it was verified at; one whose
// file is gone proves nothing.
func (rec *poolRetireRecord) holdsBundle() bool {
	if rec.Bundle == nil {
		return false
	}
	info, err := os.Stat(rec.Bundle.Path)
	return err == nil && info.Mode().IsRegular() && info.Size() == rec.Bundle.Bytes
}

// resumeRebundles reports a resume that takes the bundle again before it
// goes on (PM-7): a record at bundled does that phase again, since the phase
// is saved before its bundle and one cut short left none; a record past it
// whose verified bundle is not on this container takes one too, so nothing
// is deleted that no bundle holds. A record at started or fenced bundles as
// its phases run.
func (rec *poolRetireRecord) resumeRebundles() bool {
	switch rec.Phase {
	case poolRetireBundled:
		return true
	case poolRetireRewritten, poolRetireDeleting:
		return !rec.holdsBundle()
	}
	return false
}

// poolRetireInFlight reports a phase a retire resumes from.
func poolRetireInFlight(phase string) bool {
	switch phase {
	case poolRetireStarted, poolRetireFenced, poolRetireBundled, poolRetireRewritten, poolRetireDeleting, poolRetireDeletingVolume:
		return true
	}
	return false
}

func poolRetireRecordPath(stateDir, dev string) string {
	return filepath.Join(stateDir, "migrate", dev+".json")
}

// poolRetireBundlePath is where the retire keeps the dev's bundle.
func poolRetireBundlePath(stateDir, dev string) string {
	return filepath.Join(stateDir, "migrate", dev+".bundle")
}

// readPoolRetireRecord reads dev's retire record; nil when there is none.
func readPoolRetireRecord(stateDir, dev string) (*poolRetireRecord, error) {
	if stateDir == "" || platform.ValidateHostname(dev) != nil {
		return nil, nil //nolint:nilnil,nilerr // no record: a name no service can have holds none
	}
	raw, err := os.ReadFile(poolRetireRecordPath(stateDir, dev))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil //nolint:nilnil // no record: no retire of dev started
	}
	if err != nil {
		return nil, fmt.Errorf("read the retire record of %s: %w", dev, err)
	}
	var rec poolRetireRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		return nil, fmt.Errorf("read the retire record of %s: %w", dev, err)
	}
	if rec.Dev != dev {
		return nil, fmt.Errorf("the retire record of %s names %q", dev, rec.Dev)
	}
	return &rec, nil
}

// savePoolRetireRecord writes rec atomically, mode 0600.
func savePoolRetireRecord(stateDir string, rec *poolRetireRecord) error {
	if err := platform.ValidateHostname(rec.Dev); err != nil {
		return fmt.Errorf("save the retire record: %s", err.Message)
	}
	return writePoolRetireRecord(poolRetireRecordPath(stateDir, rec.Dev), rec)
}

// keepRetiredApart moves a retired — or aborted — record of a dev whose
// hostname another service holds now out of the way of that service's
// retire: the record goes
// to <dev>.<the id it retired>.json and its bundles — the only copies of the
// old dev's work, at the paths the new retire bundles to — to the same name's
// .bundle and, for a variant-B retire that rebundled its code volume, its
// .volume.bundle, the newer and only copy of the late writes; the restore
// steps name each where it lies now. No call reads a kept record again; it is
// there to restore from.
func keepRetiredApart(stateDir string, rec *poolRetireRecord) error {
	id := strings.Map(func(r rune) rune {
		if r == '-' || r == '_' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			return r
		}
		return -1
	}, rec.DevServiceID)
	base := filepath.Join(stateDir, "migrate", rec.Dev+"."+cmp.Or(id, "unknown"))
	kept := *rec
	type move struct{ from, to string }
	var moves []move
	// keep re-points b and its restore steps when b lies at shared, the
	// path a new retire of the hostname writes to.
	keep := func(b *ops.PoolMigrateBundle, restore []string, shared, to string) (*ops.PoolMigrateBundle, []string) {
		if b == nil || b.Path != shared {
			return b, restore
		}
		moved := *b
		moved.Path = to
		steps := make([]string, len(restore))
		for i, step := range restore {
			steps[i] = strings.ReplaceAll(step, shared, to)
		}
		moves = append(moves, move{shared, to})
		return &moved, steps
	}
	kept.Bundle, kept.Restore = keep(rec.Bundle, rec.Restore, poolRetireBundlePath(stateDir, rec.Dev), base+".bundle")
	if v := rec.CodeVolume; v != nil {
		volume := *v
		volume.Bundle, volume.Restore = keep(v.Bundle, v.Restore,
			filepath.Join(stateDir, "migrate", rec.Dev+poolRetireVolumeBundleSuffix), base+poolRetireVolumeBundleSuffix)
		kept.CodeVolume = &volume
	}
	if err := writePoolRetireRecord(base+".json", &kept); err != nil {
		return fmt.Errorf("keep the retired record of %s (%s) apart: %w", rec.Dev, rec.DevServiceID, err)
	}
	for _, m := range moves {
		if err := os.Rename(m.from, m.to); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("keep the bundle of %s (%s) apart: %w", rec.Dev, rec.DevServiceID, err)
		}
	}
	return nil
}

// writePoolRetireRecord writes rec to path atomically, mode 0600.
func writePoolRetireRecord(path string, rec *poolRetireRecord) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("save the retire record of %s: %w", rec.Dev, err)
	}
	rec.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fmt.Errorf("save the retire record of %s: %w", rec.Dev, err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".retire-*.tmp")
	if err != nil {
		return fmt.Errorf("save the retire record of %s: %w", rec.Dev, err)
	}
	defer os.Remove(tmp.Name()) // gone after a successful rename
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("save the retire record of %s: %w", rec.Dev, err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("save the retire record of %s: %w", rec.Dev, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("save the retire record of %s: %w", rec.Dev, err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("save the retire record of %s: %w", rec.Dev, err)
	}
	return nil
}

// findPoolRetireRecord is the retire record a call naming service means: the
// dev's own, or — named by a stage — the record of the dev its pair is keyed
// by, or was moved from; or one in flight deleting service as a variant-B
// pair's further dev service or code volume, which no pair names any more.
func findPoolRetireRecord(stateDir, service string) (*poolRetireRecord, error) {
	if rec, err := readPoolRetireRecord(stateDir, service); rec != nil || err != nil {
		return rec, err
	}
	meta, err := workflow.FindServiceMeta(stateDir, service)
	if err == nil && meta == nil {
		return poolRetireRecordNaming(stateDir, service), nil
	}
	if err != nil {
		return nil, nil //nolint:nilerr,nilnil // an unreadable meta names no record; the read side answers for it
	}
	if meta.DevIsPool() && meta.DevPool.MigratedFrom != nil {
		return readPoolRetireRecord(stateDir, meta.DevPool.MigratedFrom.Hostname)
	}
	if meta.Hostname != service {
		return readPoolRetireRecord(stateDir, meta.Hostname)
	}
	return nil, nil //nolint:nilnil // no record: service names no retired or retiring dev
}

// poolRetireDeps is what the retire works with beside the flow's own.
type poolRetireDeps struct {
	poolMigrateDeps
	mounter ops.Mounter
	// swarm reaches a pool's orchestrator: the retire asks it whether zcp's
	// lease is still held. Nil: zcp cannot ask, and the retire refuses.
	swarm ops.SwarmFactory
	// leaseEnv reads one key of this container's env (the pool's bearer);
	// nil reads the live env.
	leaseEnv func(key string) (string, bool)
	// workDir is the scratch directory on the dev service; "" means
	// ops.PoolMigrateWorkDir.
	workDir string
	// goneWait is how long a delete whose process finished waits for the
	// service to read gone by id (DELETING lingers a few seconds); goneEvery
	// how often it reads. Zero means poolRetireGoneWait and
	// poolRetireGoneEvery.
	goneWait, goneEvery time.Duration
}

// handlePoolMigrateRetire is zerops_workflow workflow="pool-migrate"
// action="retire".
func handlePoolMigrateRetire(
	ctx context.Context,
	projectID string,
	client platform.Client,
	sshDeployer ops.SSHDeployer,
	mounter ops.Mounter,
	swarm ops.SwarmFactory,
	input WorkflowInput,
	stateDir string,
	rt runtime.Info,
) (*mcp.CallToolResult, any, error) {
	return poolMigrateRetireRun(ctx, poolRetireDeps{
		poolMigrateDeps: poolMigrateDeps{projectID: projectID, client: client, ssh: sshDeployer, stateDir: stateDir, rt: rt, hqAddress: func() string { return hqAddress(rt) }},
		mounter:         mounter,
		swarm:           swarm,
	}, input)
}

func poolMigrateRetireRun(ctx context.Context, d poolRetireDeps, input WorkflowInput) (*mcp.CallToolResult, any, error) {
	if wf := strings.TrimSpace(input.Workflow); wf != "" && wf != workflowPoolMigrate {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf(`action="retire" belongs to workflow="pool-migrate", not %q`, wf),
			`Call zerops_workflow workflow="pool-migrate" action="retire" service="<dev>".`,
		), WithRecoveryStatus()), nil, nil
	}
	service := strings.TrimSpace(input.Service)
	if service == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			`action="retire" needs service: the dev service the part moved off`,
			poolMigrateCandidatesHint(d.stateDir),
		), WithRecoveryStatus()), nil, nil
	}
	rec, err := findPoolRetireRecord(d.stateDir, service)
	if err != nil {
		return convertError(fmt.Errorf("pool-migrate retire: %w", err), WithRecoveryStatus()), nil, nil
	}
	if rec != nil && poolRetireInFlight(rec.Phase) {
		return poolRetireResume(ctx, d, input, rec), nil, nil
	}
	if rec != nil && rec.Phase == poolRetireRetired && !poolRetireNameRetaken(ctx, d.poolMigrateDeps, rec) {
		corpus, _ := workflow.LoadAtomCorpus()
		resp := poolRetireRecordAnswer(poolRetireBase(corpus, rec), rec)
		resp.Action = actionRetire
		return jsonResult(resp), nil, nil
	}
	return poolRetireFresh(ctx, d, input), nil, nil
}

// poolRetireNameRetaken reports a live service, other than the one the
// record retired, holding the dev's hostname by the direct list: a call
// naming it is about that service.
func poolRetireNameRetaken(ctx context.Context, d poolMigrateDeps, rec *poolRetireRecord) bool {
	if d.client == nil {
		return false
	}
	live, err := d.client.ListServicesDirect(ctx, d.projectID)
	if err != nil {
		return false
	}
	return slices.ContainsFunc(live, func(s platform.ServiceStack) bool { return s.Name == rec.Dev && s.ID != rec.DevServiceID })
}

// poolRetireFresh is a retire with no record in flight: every guard read
// fresh, the diagnosis, then — acknowledged — the record and the phases.
func poolRetireFresh(ctx context.Context, d poolRetireDeps, input WorkflowInput) *mcp.CallToolResult {
	g, result := poolRetireRead(ctx, d, input)
	if result != nil {
		return result
	}
	if len(g.refusals) > 0 {
		return jsonResult(poolRetireRefused(g.resp, g.refusals...))
	}
	expected := g.destruction(input)
	if err := ValidateDestructiveAck(input.ConfirmDestructive, expected); err != nil {
		return convertError(err, WithWouldDestroy(&expected), WithRecoveryStatus())
	}
	force := input.Force.Bool()
	if len(g.unsaved) > 0 && !force {
		return jsonResult(poolRetireRefused(g.resp, poolRetireUnsavedRefusal(g.call.dev.Name, g.unsaved)))
	}
	release, err := lockPoolRetirePair(ctx, d.stateDir, g.call.dev.Name, g.pools()...)
	if err != nil {
		return jsonResult(poolRetireRefused(g.resp, poolMigrateRefusal{Reason: poolRetireRefusePairBusy, Message: "The retire did not start: " + pairHeldReason(g.call.dev.Name, err) + ". Call again."}))
	}
	defer release()
	// Another call may have started — or finished — this retire while this
	// one waited for the pair: its record answers, never a second start. A
	// retired or aborted record of another id is a dev that held the
	// hostname before (PM-7): it answers nothing for this one, and it and its
	// bundle — the abort says the bundle is kept — are kept apart, never
	// written over.
	other, err := readPoolRetireRecord(d.stateDir, g.call.dev.Name)
	if err != nil {
		return convertError(fmt.Errorf("pool-migrate retire: %w", err), WithRecoveryStatus())
	}
	switch {
	case other == nil, other.Phase == poolRetireAborted && other.DevServiceID == g.devID:
	case (other.Phase == poolRetireRetired || other.Phase == poolRetireAborted) && other.DevServiceID != g.devID:
		if err := keepRetiredApart(d.stateDir, other); err != nil {
			return convertError(fmt.Errorf("pool-migrate retire: %w", err), WithRecoveryStatus())
		}
	default:
		resp := poolRetireRecordAnswer(g.resp, other)
		resp.Action = actionRetire
		return jsonResult(resp)
	}
	rec := &poolRetireRecord{
		Dev:          g.call.dev.Name,
		DevServiceID: g.devID,
		Stages:       g.call.meta.StageHostnames(),
		Origin:       poolMigrateDevOrigin(g.call.meta, d.poolMigrateDeps),
		Phase:        poolRetireStarted,
		Forced:       force && len(g.unsaved) > 0,
		StartedAt:    time.Now().UTC().Format(time.RFC3339),
		DevServices:  g.devServices,
		CodeVolume:   g.volume,
	}
	if len(g.parts) == 1 {
		rec.Pool, rec.PoolServiceID, rec.PoolSetup = g.parts[0].pool.Name, g.parts[0].pool.ID, g.parts[0].poolSetup
	} else {
		for _, p := range g.parts {
			rec.Parts = append(rec.Parts, poolRetirePart{Stage: p.stage, Pool: p.pool.Name, PoolServiceID: p.pool.ID, PoolSetup: p.poolSetup})
		}
	}
	if err := savePoolRetireRecord(d.stateDir, rec); err != nil {
		return convertError(fmt.Errorf("pool-migrate retire: %w", err), WithRecoveryStatus())
	}
	run := &poolRetireRun{d: d, rec: rec, resp: g.resp, force: force, forceNow: force}
	return run.advance(ctx)
}

// poolRetireGuards is what a retire read before it acts: the flow's own
// answer (its step and census), the guards' refusals, and the facts the
// record takes.
type poolRetireGuards struct {
	call     *poolMigrateCall
	resp     poolMigrateResponse
	refusals []poolMigrateRefusal
	devID    string
	// parts are the parts whose pools take the pair: the one, or every part
	// of a dev serving several.
	parts   []poolRetirePartGuard
	unsaved []string
	notKept []string
	// unread are the env variables zcp could not read for a reference to
	// the dev (masked from its token).
	unread []string
	// devServices and volume are a variant-B pair's further dev services
	// and code volume, each by the id read directly; nil for variant A.
	devServices []poolRetireService
	volume      *poolRetireVolume
}

// targets are what the retire deletes, in order: the dev, a variant-B pair's
// further dev services, then its code volume.
func (g *poolRetireGuards) targets() []poolRetireService {
	out := append([]poolRetireService{{Hostname: g.call.dev.Name, ServiceID: g.devID}}, g.devServices...)
	if g.volume != nil {
		out = append(out, poolRetireService{Hostname: g.volume.Hostname, ServiceID: g.volume.ServiceID})
	}
	return out
}

// poolRetirePartGuard is one part as the retire's guards read it: its stage,
// its pool, the setup the pool runs, the work's branch, and the lease of the
// pool zcp holds there that passed verify.
type poolRetirePartGuard struct {
	stage     string
	pool      platform.ServiceStack
	poolSetup string
	branch    string
	lease     string
}

// pools are the guarded parts' pools.
func (g *poolRetireGuards) pools() []string {
	out := make([]string, 0, len(g.parts))
	for _, p := range g.parts {
		out = append(out, p.pool.Name)
	}
	return out
}

// poolRetireRead re-runs every check of the read side and every guard of the
// retire (docs/spec-workflows.md §13 PM-6): the dev read by id — its id is
// the one the delete uses — and not DELETING, no live process referencing
// it, no active launch naming it, no env variable referencing it, no domain
// routed to it, and every part's pool in place and past its image step, with
// a held lease of the pool on the work's branch that passes verify — a dev
// serving several parts waits for every one (PM-12). It writes nothing. A
// result is an answer to give as it is.
func poolRetireRead(ctx context.Context, d poolRetireDeps, input WorkflowInput) (*poolRetireGuards, *mcp.CallToolResult) {
	dr, final, failed := readPoolMigrateDev(ctx, d.poolMigrateDeps, input)
	switch {
	case failed != nil:
		return nil, failed
	case final != nil:
		final.Action = actionRetire
		return nil, jsonResult(*final)
	}
	answers := make([]poolMigrateResponse, len(dr.parts))
	calls := make([]*poolMigrateCall, len(dr.parts))
	var notReady []int
	for i := range dr.parts {
		if answers[i], calls[i], failed = dr.partAnswer(ctx, d.poolMigrateDeps, i); failed != nil {
			return nil, failed
		}
		if calls[i] == nil || !slices.Contains([]string{poolMigrateStepMove, poolMigrateStepLease, poolMigrateStepReadyToRetire}, answers[i].Step) ||
			answers[i].WorkBranch == nil || answers[i].WorkBranch.Branch == "" {
			notReady = append(notReady, i)
		}
	}
	resp := answers[0]
	if dr.multiPart() {
		first := -1
		if len(notReady) > 0 {
			first = notReady[0]
		}
		resp = dr.partsAnswer(answers, first)
	}
	resp.Action = actionRetire
	// The read side's warnings speak of its own step; the retire says its own.
	resp.Warnings = nil
	switch {
	case len(notReady) > 0 && !dr.multiPart():
		return nil, jsonResult(poolRetireNotReady(resp))
	case len(notReady) > 0:
		return nil, jsonResult(poolRetirePartsNotReady(resp, dr, answers, notReady))
	}
	g := &poolRetireGuards{call: calls[0], resp: resp}
	for i, call := range calls {
		part := poolRetirePartGuard{stage: dr.parts[i].stage, pool: call.pool, poolSetup: call.pool.Name, branch: answers[i].WorkBranch.Branch}
		if img := answers[i].Image; img != nil && img.Setup != "" {
			part.poolSetup = img.Setup
		}
		g.parts = append(g.parts, part)
	}
	g.unsaved = poolRetireBlocking(resp.Census)
	g.notKept = poolRetireNotKept(g.call.dev.Name, poolMigrateCodeRoot(g.call.meta), resp.Census, nil)
	g.readDev(ctx, d)
	if g.call.meta.CodeVolume != nil {
		g.readCodeVolume(ctx, d, dr.byName)
		g.notKept = append(g.notKept, poolRetireCodeVolumeNotKept(g.devServices, g.call.meta.CodeVolume.Hostname)...)
	}
	g.readActivity(ctx, d)
	g.readLaunches(d)
	g.readReferences(ctx, d)
	g.readDomains(ctx, d)
	for i := range g.parts {
		g.readLease(ctx, d, &g.parts[i])
	}
	return g, nil
}

// poolRetirePartsNotReady refuses the retire of a dev serving several parts
// while a part is not ready: the retire waits for every part, and names each
// part it waits for with that part's step.
func poolRetirePartsNotReady(resp poolMigrateResponse, dr *poolMigrateDevRead, answers []poolMigrateResponse, notReady []int) poolMigrateResponse {
	waits := make([]string, 0, len(notReady))
	for _, i := range notReady {
		wait := fmt.Sprintf("the part of %s (pool %s) is at its %s step", dr.parts[i].stage, dr.parts[i].pool, answers[i].Step)
		if wb := answers[i].WorkBranch; wb != nil && wb.Missing != "" {
			wait += ", with no work's branch named yet"
		}
		waits = append(waits, wait)
	}
	why := fmt.Sprintf("The retire deletes %s only once every part it serves runs on its own pool, leased on the work's branch: %s. Walk each with zerops_workflow workflow=%q service=%q pool=\"<its pool>\", then call the retire again.",
		dr.meta.Hostname, strings.Join(waits, "; "), workflowPoolMigrate, dr.meta.Hostname)
	return poolRetireRefused(resp, poolMigrateRefusal{Reason: poolRetireRefuseNotReady, Message: why})
}

func (g *poolRetireGuards) refuse(reason, message string) {
	g.refusals = append(g.refusals, poolMigrateRefusal{Reason: reason, Message: message})
}

// readDev reads the dev by id: the id the delete acts on, never one a search
// resolved from its hostname.
func (g *poolRetireGuards) readDev(ctx context.Context, d poolRetireDeps) {
	dev := g.call.dev
	state, err := poolRetireDevState(ctx, d.client, dev.ID)
	switch {
	case err != nil:
		g.refuse(poolMigrateRefuseDevNotLive, fmt.Sprintf("%s could not be read by its id %s: %v. Call again.", dev.Name, dev.ID, err))
	case state == poolRetireDevGone:
		g.refuse(poolMigrateRefuseDevNotLive, fmt.Sprintf("%s (id %s) no longer exists: there is nothing to retire.", dev.Name, dev.ID))
	case state == poolRetireDevDeleting:
		g.refuse(poolRetireRefuseDevDeleting, fmt.Sprintf("%s (id %s) reads DELETING by its id: a delete outside the retire is running. Call the flow again once it finishes.", dev.Name, dev.ID))
	default:
		g.devID = dev.ID
	}
}

func (g *poolRetireGuards) readActivity(ctx context.Context, d poolRetireDeps) {
	dev := g.call.dev
	ids := map[string]string{dev.ID: dev.Name}
	for _, s := range g.targets()[1:] {
		ids[s.ServiceID] = s.Hostname
	}
	activity, err := ops.ProjectActivity(ctx, d.client, d.projectID, ids)
	if err != nil {
		g.refuse(poolRetireRefuseDevBusy, fmt.Sprintf("the project's processes could not be read: %v. Call again.", err))
		return
	}
	for _, s := range append([]poolRetireService{{Hostname: dev.Name}}, g.targets()[1:]...) {
		var live []string
		for _, op := range activity[s.Hostname] {
			live = append(live, fmt.Sprintf("%s (%s, %s)", op.ProcessID, op.Action, op.Status))
		}
		if len(live) > 0 {
			g.refuse(poolRetireRefuseDevBusy, fmt.Sprintf("%s runs %s. Wait for it — zerops_process action=%q service=%q — then call again.", s.Hostname, strings.Join(live, ", "), poolMigrateProcessWait, s.Hostname))
		}
	}
}

func (g *poolRetireGuards) readLaunches(d poolRetireDeps) {
	_, active, err := findActiveLaunchState(d.stateDir, d.projectID)
	if err != nil {
		g.refuse(poolRetireRefuseLaunch, fmt.Sprintf("the launch state could not be read: %v.", err))
		return
	}
	names := append([]string{g.call.dev.Name}, g.call.meta.StageHostnames()...)
	names = append(names, g.call.meta.CodeVolume.DevServiceHostnames()...)
	for _, l := range active {
		if l.TargetServiceHostname == "" || slices.Contains(names, l.TargetServiceHostname) {
			g.refuse(poolRetireRefuseLaunch, fmt.Sprintf("the production launch %q of this project is %s and reads the part's services. Finish it (zerops_workflow workflow=%q action=%q) or reset it first.", l.TargetProjectName, l.Status, workflowLaunchProduction, "status"))
		}
	}
}

// readReferences refuses while an env variable references a service the
// retire deletes: the dev, and a variant-B pair's further dev services and
// code volume.
func (g *poolRetireGuards) readReferences(ctx context.Context, d poolRetireDeps) {
	targets := g.targets()
	retired := make([]string, 0, len(targets))
	retired = append(retired, g.call.dev.Name)
	for _, s := range targets[1:] {
		retired = append(retired, s.Hostname)
	}
	refs, unread, err := ops.PoolMigrateEnvReferencesTo(ctx, d.client, d.projectID, retired, g.call.services)
	if err != nil {
		g.refuse(poolRetireRefuseEnvReference, fmt.Sprintf("the env variables that could reference %s could not be read: %v. Call again.", strings.Join(retired, ", "), err))
		return
	}
	for _, host := range retired {
		if len(refs[host]) > 0 {
			g.refuse(poolRetireRefuseEnvReference, fmt.Sprintf("%s reference %s (${%s_…}): a reference to a deleted service stays its literal text once its reader restarts. Point each elsewhere first.", strings.Join(refs[host], ", "), host, host))
		}
	}
	if len(unread) > 0 {
		g.unread = unread
		g.resp.Warnings = append(g.resp.Warnings, poolRetireUnreadNote(unread, strings.Join(retired, ", ")))
	}
}

// readDomains refuses while a domain routes to a dev service the retire
// deletes — the dev, or a variant-B pair's further dev service.
func (g *poolRetireGuards) readDomains(ctx context.Context, d poolRetireDeps) {
	devs := append([]poolRetireService{{Hostname: g.call.dev.Name, ServiceID: g.call.dev.ID}}, g.devServices...)
	for _, dev := range devs {
		if g.call.meta.PublicAccessFor(dev.Hostname).Intent == topology.PublicAccessDomain {
			g.refuse(poolRetireRefuseDomain, fmt.Sprintf("%s's public access is a custom domain: moving a domain to the stage or the pool is the person's dashboard work, before the retire.", dev.Hostname))
			continue
		}
		domains, err := ops.PoolMigrateDevDomains(ctx, d.client, d.projectID, dev.ServiceID)
		switch {
		case err != nil:
			g.refuse(poolRetireRefuseDomain, fmt.Sprintf("the domains routed to %s could not be read: %v. Call again.", dev.Hostname, err))
		case len(domains) > 0:
			g.refuse(poolRetireRefuseDomain, fmt.Sprintf("%s route to %s: moving a domain is the person's dashboard work, before the retire.", strings.Join(domains, ", "), dev.Hostname))
		}
	}
}

// readLease finds a lease zcp holds of the part's pool on the work's branch of
// the dev's origin — its record, then the pool's orchestrator saying it is
// still held — and verifies it as zerops_verify does: it answers over SSH and
// no dev server of it crashed.
func (g *poolRetireGuards) readLease(ctx context.Context, d poolRetireDeps, part *poolRetirePartGuard) {
	pool, branch := part.pool.Name, part.branch
	acquire := fmt.Sprintf("zerops_lease action=%q pool=%q branch=%q, then zerops_verify serviceHostname=\"<alias>\"", leaseProbeAction, pool, branch)
	records, err := ops.PoolMigrateLeases(d.stateDir, pool, poolMigrateDevOrigin(g.call.meta, d.poolMigrateDeps), branch)
	if err != nil {
		g.refuse(poolRetireRefuseLeaseUnreadable, fmt.Sprintf("zcp's lease records could not be read: %v.", err))
		return
	}
	if len(records) == 0 {
		g.refuse(poolRetireRefuseNoLease, fmt.Sprintf("zcp holds no lease of %s on %s: the retire proves the pool works before the dev service goes. Lease one — %s — then call again.", pool, branch, acquire))
		return
	}
	if d.swarm == nil {
		g.refuse(poolRetireRefuseLeaseUnreadable, fmt.Sprintf("this zcp cannot ask the orchestrator of %s whether its lease is still held, and the retire relies on no lease it cannot check.", pool))
		return
	}
	views, err := ops.LeaseList(ctx, ops.LeaseDeps{Client: d.client, ProjectID: d.projectID, StateDir: d.stateDir, Swarm: d.swarm, Env: d.leaseEnv}, pool)
	if err != nil {
		g.refuse(poolRetireRefuseLeaseUnreadable, fmt.Sprintf("the orchestrator of %s could not be asked about its leases: %v. Call again.", pool, err))
		return
	}
	var lost, failed, unverified []string
	for _, rec := range records {
		i := slices.IndexFunc(views, func(v ops.LeaseView) bool { return v.Alias == rec.Alias })
		if i >= 0 && views[i].Unverified {
			// Maybe another holder's container by now (DP-2): never reached.
			unverified = append(unverified, rec.Alias+" ("+views[i].Note+")")
			continue
		}
		if i < 0 || (views[i].State != ops.LeaseStateHeld && views[i].State != ops.LeaseStateUnmounted) {
			note := "the orchestrator does not list it"
			if i >= 0 {
				note = views[i].State + ": " + views[i].Note
			}
			lost = append(lost, rec.Alias+" ("+note+")")
			continue
		}
		if why := poolRetireVerifyLease(ctx, d.ssh, rec); why != "" {
			failed = append(failed, rec.Alias+": "+why)
			continue
		}
		part.lease = rec.Alias
		return
	}
	if len(unverified) > 0 {
		g.refuse(poolRetireRefuseLeaseUnreadable, fmt.Sprintf("the orchestrator of %s did not answer whether zcp's lease on %s is still zcp's — %s — and the retire relies on no lease it cannot check. Call again once it answers.", pool, branch, strings.Join(unverified, "; ")))
		return
	}
	if len(failed) > 0 {
		g.refuse(poolRetireRefuseLeaseUnverified, fmt.Sprintf("the lease of %s on %s does not pass verify — %s. Fix it (zerops_verify serviceHostname=\"<alias>\" names what fails), then call again.", pool, branch, strings.Join(failed, "; ")))
		return
	}
	g.refuse(poolRetireRefuseNoLease, fmt.Sprintf("zcp's lease of %s on %s is no longer held: %s. Lease one again — %s — then call again.", pool, branch, strings.Join(lost, "; "), acquire))
}

// poolRetireVerifyLease is why the lease does not pass verify, "" when it
// does: the same probe and dev-server read zerops_verify makes of a lease.
func poolRetireVerifyLease(ctx context.Context, ssh ops.SSHDeployer, lease ops.LeaseTargetInfo) string {
	unreachable := ""
	probeCtx, cancel := context.WithTimeout(ctx, leaseProbeTimeout)
	if _, err := ssh.ExecSSH(probeCtx, lease.Alias, "true"); err != nil {
		unreachable = withSSHStderr("ssh "+lease.Alias, err)
	}
	cancel()
	result := ops.VerifyLease(ctx, ssh, lease, unreachable)
	if result.Status == ops.StatusHealthy {
		return ""
	}
	var fails []string
	for _, c := range result.Checks {
		if c.Status == ops.CheckFail {
			fails = append(fails, c.Name+": "+c.Detail)
		}
	}
	return result.Status + " (" + strings.Join(fails, "; ") + ")"
}

// destruction is what the retire states it erases (the diagnose-before-
// destruct gate): the dev service, by the id it is deleted by, the unsaved
// work only the bundle will keep, and what nothing keeps.
func (g *poolRetireGuards) destruction(input WorkflowInput) DiagnosedDestruction {
	dev := g.call.dev.Name
	var takes string
	if len(g.parts) == 1 {
		p := g.parts[0]
		takes = fmt.Sprintf("%s takes the pair, its lease %s on %s verified", p.pool.Name, p.lease, p.branch)
	} else {
		each := make([]string, 0, len(g.parts))
		for _, p := range g.parts {
			each = append(each, fmt.Sprintf("%s takes the part of %s, its lease %s on %s verified", p.pool.Name, p.stage, p.lease, p.branch))
		}
		takes = "the pair splits into one pair per part — " + strings.Join(each, "; ") + " —"
	}
	cause := fmt.Sprintf("service id %s; %s; a verified bundle of %s's repository is written to this container first",
		g.devID, takes, dev)
	if g.volume != nil {
		cause += "; " + poolRetireCodeVolumeCause(g.devServices, *g.volume)
	}
	if len(g.unread) > 0 {
		cause += "; " + poolRetireUnreadNote(g.unread, dev)
	}
	return poolRetireDestructionOf(g.targets(), g.unsaved, g.notKept, input, cause)
}

// poolRetireUnreadNote names the env variables zcp could not read for a
// reference to dev.
func poolRetireUnreadNote(unread []string, dev string) string {
	return fmt.Sprintf("%s could not be read for a reference to %s (zcp's token reads them masked): check them yourself", strings.Join(unread, ", "), dev)
}

// poolRetireDestructionOf is the destruction the retire states for targets —
// the dev first, then a variant-B pair's further dev services and its code
// volume — each by the id it is deleted by.
func poolRetireDestructionOf(targets []poolRetireService, unsaved, notKept []string, input WorkflowInput, cause string) DiagnosedDestruction {
	dev := targets[0].Hostname
	names := make([]string, 0, len(targets))
	ids := make([]string, 0, len(targets))
	for _, s := range targets {
		names = append(names, s.Hostname)
		ids = append(ids, s.ServiceID)
	}
	d := DiagnosedDestruction{
		Operation: poolRetireOperation,
		Targets:   names,
		Loss: DestructionLoss{
			ServiceStacks:   slices.Clone(names),
			ServiceIDs:      ids,
			UncommittedCode: len(unsaved) > 0,
			UnsavedWork:     unsaved,
			NotKept:         notKept,
		},
		Diagnoses: []TargetDiagnosis{{Hostname: dev, Cause: cause}},
	}
	args := map[string]any{
		"workflow": workflowPoolMigrate, "action": actionRetire, "service": dev,
		"confirmDestructive": map[string]any{"operation": poolRetireOperation, "acknowledgedTargets": slices.Clone(names)},
	}
	if pool := strings.TrimSpace(input.Pool); pool != "" {
		args["pool"] = pool
	}
	d.Retry = &RetryCall{Tool: "zerops_workflow", Args: args}
	if len(unsaved) > 0 {
		d.Retry.PatchHints = []string{"Add force=true only once the person agreed, by name, to leaving the work in wouldDestroy.unsavedWork on the dev service — the bundle keeps a copy; otherwise push it and call again without force."}
		d.Then = "push the unsaved work you keep (census names it), then call the retire again"
	}
	return d
}

// poolRetireSuggestion is the retire's first-call refusal: what goes, that
// the person agrees by name first, and the acknowledgment.
func poolRetireSuggestion(expected DiagnosedDestruction) string {
	ack, err := json.Marshal(struct {
		Operation           string   `json:"operation"`
		AcknowledgedTargets []string `json:"acknowledgedTargets"`
	}{expected.Operation, append([]string{}, expected.Targets...)})
	if err != nil {
		return "Re-call with confirmDestructive matching wouldDestroy."
	}
	text := fmt.Sprintf("Retiring deletes %s for good, by its id; wouldDestroy names what goes. Ask the person; only once they agreed to deleting %s by name, re-send the call with confirmDestructive=%s.",
		expected.JoinTargets(), expected.JoinTargets(), ack)
	if len(expected.Loss.UnsavedWork) > 0 {
		text += " It holds work its origin lacks (wouldDestroy.unsavedWork): push what you keep and call again, or — only if the person agreed to leaving it — add force=true; the retire writes a verified bundle of the repository before the delete either way."
	}
	return text
}

// poolRetireUnsavedRefusal refuses an acknowledged retire without force over
// unsaved work.
func poolRetireUnsavedRefusal(dev string, unsaved []string) poolMigrateRefusal {
	return poolMigrateRefusal{
		Reason: poolRetireRefuseUnsaved,
		Message: fmt.Sprintf("%s holds work its origin lacks: %s. Push what you keep and call again; or, only once the person agreed to leaving it, re-send with force=true — a verified bundle of the repository is written before the delete either way.",
			dev, strings.Join(unsaved, "; ")),
	}
}

// poolRetireNotReady refuses a retire the flow has not reached.
func poolRetireNotReady(resp poolMigrateResponse) poolMigrateResponse {
	why := fmt.Sprintf("The part is at the %s step: the retire comes after the image step, with a lease of the pool on the work's branch.", resp.Step)
	if resp.WorkBranch != nil && resp.WorkBranch.Missing != "" {
		why += " No work's branch is named yet: " + resp.WorkBranch.Missing
	}
	if resp.Next != nil {
		why += fmt.Sprintf(" Make the next call (%s) first.", resp.Next.Tool)
	} else if resp.NextUnavailable != "" {
		why += " " + resp.NextUnavailable
	}
	return poolRetireRefused(resp, poolMigrateRefusal{Reason: poolRetireRefuseNotReady, Message: why})
}

// poolRetireRefused answers a retire refused before it started: nothing was
// written, and the flow's own step stays in the answer.
func poolRetireRefused(resp poolMigrateResponse, refusals ...poolMigrateRefusal) poolMigrateResponse {
	resp.Status = poolMigrateStatusRetireRefused
	resp.Action = actionRetire
	resp.Refusals = refusals
	return resp
}

// poolRetireBlocking names the census's unsaved work that holds the retire
// unless forced: every item but the commits only a reflog reaches, which the
// bundle carries as refs, and Mate's own refs and ignored files, which never
// count. A submodule holds it too: work inside one is invisible to the census
// and the bundle never carries it (poolRetireSubmodules).
func poolRetireBlocking(c *ops.PoolMigrateCensus) []string {
	if c == nil {
		return nil
	}
	var out []string
	for _, t := range c.Trees {
		paths := func(kind string, p *ops.PoolMigratePaths) {
			if p != nil && p.Count > 0 {
				out = append(out, fmt.Sprintf("%s in %s: %d (%s)", kind, t.Path, p.Count, strings.Join(p.Paths, ", ")))
			}
		}
		paths("staged", t.Staged)
		paths("modified", t.Modified)
		paths("conflicted", t.Conflicted)
		paths("untracked", t.Untracked)
		for _, op := range t.InProgress {
			out = append(out, fmt.Sprintf("an unfinished %s in %s", op, t.Path))
		}
		for _, f := range t.EnvFiles {
			out = append(out, fmt.Sprintf("%s in %s: a .env file, named and never archived", f, t.Path))
		}
		if t.DetachedCommits > 0 {
			out = append(out, fmt.Sprintf("%d commits only the detached HEAD of %s holds", t.DetachedCommits, t.Path))
		}
		if t.Submodules != nil && t.Submodules.Count > 0 {
			out = append(out, fmt.Sprintf("%s %d (%s): %s — push each one's commits, branches and stashes to its own remote",
				poolRetireSubmodules, t.Submodules.Count, strings.Join(t.Submodules.Paths, ", "), poolRetireSubmodulesWhy(t.Path)))
		}
	}
	for _, b := range c.Branches {
		out = append(out, fmt.Sprintf("branch %s: %d commits on no remote branch", b.Name, b.Unpushed))
	}
	for _, s := range c.Stashes {
		out = append(out, fmt.Sprintf("%s: %s", s.Ref, s.Message))
	}
	return out
}

// poolRetireSubmodules opens the census item naming a tree's submodules.
const poolRetireSubmodules = "submodules"

// poolRetireSubmodulesWhy says what a tree's submodules lose with the dev.
func poolRetireSubmodulesWhy(tree string) string {
	return "in " + tree + ", each a repository of its own whose commits, branches, stashes and files the bundle does not carry"
}

// poolRetireKeptInBundle names the unsaved work whose only copy, once the dev
// is deleted, is the bundle: the blocking work but the .env files, the
// commits only a reflog reaches, and Mate's own refs.
func poolRetireKeptInBundle(c *ops.PoolMigrateCensus) []string {
	if c == nil {
		return nil
	}
	var out []string
	for _, item := range poolRetireBlocking(c) {
		if !strings.Contains(item, "a .env file") && !strings.HasPrefix(item, "an unfinished ") && !strings.HasPrefix(item, poolRetireSubmodules) {
			out = append(out, item)
		}
	}
	if c.ReflogOnly != nil && c.ReflogOnly.Count > 0 {
		noun := "commits"
		if c.ReflogOnly.Count == 1 {
			noun = "commit"
		}
		out = append(out, fmt.Sprintf("%d %s only a reflog reaches (reset, amended or rebased away), each a ref in the bundle", c.ReflogOnly.Count, noun))
	}
	if len(c.T3Refs) > 0 {
		var ns []string
		for _, k := range slices.Sorted(func(yield func(string) bool) {
			for k := range c.T3Refs {
				if !yield(k) {
					return
				}
			}
		}) {
			ns = append(ns, fmt.Sprintf("%s %d", k, c.T3Refs[k]))
		}
		out = append(out, "refs under refs/t3/: "+strings.Join(ns, ", "))
	}
	return out
}

// poolRetireNotKept names what no copy keeps once the dev is deleted; root is
// where the bundle reads its repository — /var/www, or a variant-B pair's code
// volume mount — so every file of the dev's container outside it is lost.
func poolRetireNotKept(dev, root string, c *ops.PoolMigrateCensus, b *ops.PoolMigrateBundle) []string {
	out := []string{
		dev + "'s own URLs: its subdomain embeds its hostname, and dies with it",
		"the repository's reflog history beyond each reflog's tip, its config (remote URLs among it), hooks and info/exclude",
		"files on " + dev + "'s container outside " + root,
	}
	if c != nil {
		for _, t := range c.Trees {
			for _, f := range t.EnvFiles {
				out = append(out, fmt.Sprintf("%s in %s: a .env file is named, never archived", f, t.Path))
			}
			for _, op := range t.InProgress {
				out = append(out, fmt.Sprintf("the unfinished %s in %s: its index and files are kept, not the operation", op, t.Path))
			}
			if t.Ignored != nil && t.Ignored.Count > 0 {
				out = append(out, fmt.Sprintf("ignored files in %s (%s): a checkout reproduces them", t.Path, strings.Join(t.Ignored.Paths, ", ")))
			}
			if t.Submodules != nil && t.Submodules.Count > 0 {
				out = append(out, fmt.Sprintf("%s %d (%s): %s", poolRetireSubmodules, t.Submodules.Count, strings.Join(t.Submodules.Paths, ", "), poolRetireSubmodulesWhy(t.Path)))
			}
		}
	}
	if b != nil {
		for _, n := range b.Nested {
			out = append(out, n+": an untracked repository of its own, named, not carried")
		}
	}
	return out
}

// lockPoolRetirePair holds the pair locks of the dev and of every pool taking
// its pair, in a fixed order, so no HQ pass, stand-up or describe of zcp runs
// git on the pair from the fence through the delete.
func lockPoolRetirePair(ctx context.Context, stateDir, dev string, pools ...string) (func(), error) {
	names := append([]string{dev}, pools...)
	slices.Sort(names)
	names = slices.Compact(names)
	var releases []func()
	release := func() {
		for i := len(releases) - 1; i >= 0; i-- {
			releases[i]()
		}
	}
	for _, name := range names {
		r, err := holdPairCheckout(ctx, stateDir, name)
		if err != nil {
			release()
			return nil, err
		}
		releases = append(releases, r)
	}
	return release, nil
}

// poolRetireDevState reads a service by id: live, DELETING, or gone — a
// deleted service's by-id read answers HTTP 400 serviceStackNotFound, not
// only a 404 (ops.IsServiceGone).
func poolRetireDevState(ctx context.Context, client platform.Client, id string) (string, error) {
	svc, err := client.GetService(ctx, id)
	if err != nil {
		if ops.IsServiceGone(err) {
			return poolRetireDevGone, nil
		}
		return "", fmt.Errorf("read service %s: %w", id, err)
	}
	switch {
	case platform.ServiceDeleting(svc.Status):
		return poolRetireDevDeleting, nil
	case platform.ServiceDeleted(svc.Status):
		return poolRetireDevGone, nil
	}
	return poolRetireDevLive, nil
}

// poolRetireRun walks a retire's phases.
type poolRetireRun struct {
	d    poolRetireDeps
	rec  *poolRetireRecord
	resp poolMigrateResponse
	// force is the person's agreement to leaving the census's unsaved work:
	// on this call, or recorded by the call that started the retire.
	// forceNow is this call's alone.
	force, forceNow bool
	// unmounted are the dev services whose mount this call took down beside
	// the dev's: an abort puts them back.
	unmounted []string
}

func (r *poolRetireRun) move() workflow.PoolPairMove {
	return workflow.PoolPairMove{Dev: r.rec.Dev, Pool: r.rec.Pool, PoolServiceID: r.rec.PoolServiceID, PoolSetup: r.rec.PoolSetup}
}

// rekey keys the pair by the pool, or splits it into one pool-keyed pair per
// part; a re-key already done is a no-op.
func (r *poolRetireRun) rekey() error {
	if len(r.rec.Parts) > 0 {
		_, err := workflow.MigratePairToPools(r.d.stateDir, r.rec.Dev, r.rec.partMoves())
		return err
	}
	_, err := workflow.MigratePairToPool(r.d.stateDir, r.move())
	return err
}

// revert keys the pair by the dev again: the inverse of rekey, applied to
// what the pair's files hold now.
func (r *poolRetireRun) revert() error {
	if len(r.rec.Parts) > 0 {
		_, err := workflow.RevertPoolPairs(r.d.stateDir, r.rec.Dev, r.rec.partMoves())
		return err
	}
	_, err := workflow.RevertPoolPair(r.d.stateDir, r.move())
	return err
}

// enter saves the phase before the work it names.
func (r *poolRetireRun) enter(phase string) error {
	r.rec.Phase = phase
	return savePoolRetireRecord(r.d.stateDir, r.rec)
}

// advance runs the phases from the record's, each saved first.
func (r *poolRetireRun) advance(ctx context.Context) *mcp.CallToolResult {
	for {
		switch r.rec.Phase {
		case poolRetireStarted:
			if err := r.enter(poolRetireFenced); err != nil {
				return r.saveFailed(err)
			}
			if err := r.fence(ctx); err != nil {
				return r.abort(ctx, poolRetireAbortFence, fmt.Sprintf("%s's mount on this container could not be taken down: %v. Nothing writes to its checkout through zcp while the bundle is taken, or the retire does not go on.", r.rec.Dev, err))
			}
		case poolRetireFenced:
			if err := r.enter(poolRetireBundled); err != nil {
				return r.saveFailed(err)
			}
			if result := r.bundle(ctx); result != nil {
				return result
			}
		case poolRetireBundled:
			if err := r.enter(poolRetireRewritten); err != nil {
				return r.saveFailed(err)
			}
			if err := r.rekey(); err != nil {
				return r.abort(ctx, poolRetireAbortRekey, fmt.Sprintf("the pair could not be %s: %v.", r.rec.keyedBy(), err))
			}
		case poolRetireRewritten:
			// A cut between the saved phase and the rename is finished
			// here: the move is a no-op once done.
			if err := r.rekey(); err != nil {
				return r.abort(ctx, poolRetireAbortRekey, fmt.Sprintf("the pair could not be %s: %v.", r.rec.keyedBy(), err))
			}
			if err := r.enter(poolRetireDeleting); err != nil {
				return r.saveFailed(err)
			}
		case poolRetireDeleting:
			if r.rec.CodeVolume != nil {
				return r.deleteDevs(ctx)
			}
			return r.delete(ctx)
		case poolRetireDeletingVolume:
			return r.deleteVolume(ctx)
		default:
			return jsonResult(poolRetireRecordAnswer(r.resp, r.rec))
		}
	}
}

// fence takes the dev's mount on this container down — every dev service's,
// for a variant-B pair, whose mounts all show the volume's checkout.
func (r *poolRetireRun) fence(ctx context.Context) error {
	if r.d.mounter == nil {
		return nil
	}
	for _, s := range r.rec.devs() {
		res, err := ops.UnmountService(ctx, r.d.client, r.d.projectID, r.d.mounter, s.Hostname)
		if err != nil {
			return fmt.Errorf("unmount %s: %w", s.Hostname, err)
		}
		if s.Hostname != r.rec.Dev && res != nil && res.Status == poolRetireMountTaken && !slices.Contains(r.unmounted, s.Hostname) {
			r.unmounted = append(r.unmounted, s.Hostname)
		}
	}
	return nil
}

func (r *poolRetireRun) workDir() string {
	if r.d.workDir != "" {
		return r.d.workDir
	}
	return ops.PoolMigrateWorkDir
}

// bundle takes the final census and the verified bundle with its
// fingerprint; nil once both are recorded.
func (r *poolRetireRun) bundle(ctx context.Context) *mcp.CallToolResult {
	dev, root := r.rec.Dev, r.rec.root()
	census, err := ops.ReadPoolMigrateCensus(ctx, r.d.ssh, dev, root)
	if err != nil {
		return r.abort(ctx, poolMigrateRefuseDevUnreadable, withSSHStderr("could not read "+dev+"'s repository over SSH", err)+".")
	}
	if crew := poolMigrateCrewHandles(census, root); len(crew) > 0 {
		return r.abort(ctx, poolMigrateRefuseCrewWorktrees, fmt.Sprintf("%s holds crew worktrees now (%s); crew lanes stay on a plain dev service.", dev, strings.Join(crew, ", ")))
	}
	if blocking := poolRetireBlocking(census); len(blocking) > 0 && !r.force {
		return r.abort(ctx, poolRetireRefuseUnsaved, poolRetireUnsavedRefusal(dev, blocking).Message)
	}
	var volumeReading *ops.PoolMigrateFingerprint
	if v := r.rec.CodeVolume; v != nil {
		// The volume's own reading of its checkout, taken before the bundle
		// streams: once every dev service is gone the volume's container is
		// the only reader left, and its delete is checked against this. A
		// write while the bundle streams is in no bundle, so it must not be
		// in this reading either — read after, it would be.
		fp, err := ops.ReadPoolMigrateFingerprint(ctx, r.d.ssh, v.Hostname, ops.CodeVolumeDataRoot, r.workDir())
		if err != nil {
			return r.abort(ctx, poolRetireAbortUnreadable, withSSHStderr("the checkout on the code volume "+v.Hostname+" could not be read in the volume's own container at "+ops.CodeVolumeDataRoot, err)+
				". The volume is deleted last, once no dev service is left to read it through, so the retire goes no further without that reading.")
		}
		volumeReading = &fp
	}
	b, err := ops.BuildPoolMigrateBundle(ctx, r.d.ssh, ops.PoolMigrateBundleInput{
		Dev: dev, Root: root, WorkDir: r.workDir(), Path: poolRetireBundlePath(r.d.stateDir, dev),
	})
	if err != nil {
		return r.abort(ctx, poolRetireAbortBundle, withSSHStderr("the bundle of "+dev+"'s repository could not be written and verified", err)+". The retire deletes nothing without one.")
	}
	if v := r.rec.CodeVolume; v != nil {
		v.Fingerprint = volumeReading
	}
	r.rec.Bundle = b
	r.rec.Restore = ops.PoolMigrateRestoreSteps(b, r.rec.Origin)
	r.rec.KeptInBundle = poolRetireKeptInBundle(census)
	r.rec.NotKept = append(poolRetireNotKept(dev, r.rec.root(), census, b), poolRetireCodeVolumeNotKept(r.rec.DevServices, r.rec.volumeName())...)
	r.rec.Forced = r.rec.Forced || (r.force && len(poolRetireBlocking(census)) > 0)
	if err := savePoolRetireRecord(r.d.stateDir, r.rec); err != nil {
		return r.saveFailed(err)
	}
	return nil
}

// delete re-checks the fingerprint and deletes the dev by the recorded id,
// then reads it by id: gone finishes the retire; anything else keeps the
// record for the next call.
func (r *poolRetireRun) delete(ctx context.Context) *mcp.CallToolResult {
	state, err := poolRetireDevState(ctx, r.d.client, r.rec.DevServiceID)
	switch {
	case err != nil:
		return r.callAgain(fmt.Sprintf("%s could not be read by its id: %v", r.rec.Dev, err))
	case state == poolRetireDevGone:
		return r.finish(ctx)
	case state == poolRetireDevDeleting:
		return r.callAgain(fmt.Sprintf("%s reads DELETING by its id", r.rec.Dev))
	}
	if err := r.fence(ctx); err != nil {
		return r.abort(ctx, poolRetireAbortFence, fmt.Sprintf("%s's mount could not be taken down again: %v.", r.rec.Dev, err))
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
	proc, saveFailed, err := r.sendDelete(ctx, r.rec.DevServiceID)
	switch {
	case saveFailed != nil:
		return saveFailed
	case err != nil:
		if st, _ := poolRetireDevState(ctx, r.d.client, r.rec.DevServiceID); st == poolRetireDevGone {
			return r.finish(ctx)
		}
		return r.callAgain(fmt.Sprintf("the delete of %s (id %s) failed: %v", r.rec.Dev, r.rec.DevServiceID, err))
	}
	pollManageProcess(ctx, r.d.client, proc, nil)
	state, err = r.awaitGone(ctx, r.rec.DevServiceID)
	switch {
	case err != nil:
		return r.callAgain(fmt.Sprintf("%s could not be read by its id after the delete: %v", r.rec.Dev, err))
	case state == poolRetireDevGone:
		return r.finish(ctx)
	case state == poolRetireDevDeleting:
		return r.callAgain(fmt.Sprintf("the delete of %s runs: it reads DELETING by its id", r.rec.Dev))
	}
	return r.callAgain(fmt.Sprintf("the delete of %s did not take: it still reads live by its id", r.rec.Dev))
}

// sendDelete deletes a dev service by its recorded id, the send saved in the
// record before it goes out and its process id once it answers: a later call
// that reads the service live then knows a delete of it may still be queued
// (deletePending). A save that fails answers saveFailed instead.
func (r *poolRetireRun) sendDelete(ctx context.Context, serviceID string) (proc *platform.Process, saveFailed *mcp.CallToolResult, err error) {
	if r.rec.DeletesSent == nil {
		r.rec.DeletesSent = map[string]string{}
	}
	r.rec.DeletesSent[serviceID] = ""
	if err := savePoolRetireRecord(r.d.stateDir, r.rec); err != nil {
		return nil, r.saveFailed(err), nil
	}
	proc, err = ops.DeleteByID(ctx, r.d.client, serviceID)
	if err != nil {
		return nil, nil, err
	}
	if proc != nil && proc.ID != "" {
		r.rec.DeletesSent[serviceID] = proc.ID
		if err := savePoolRetireRecord(r.d.stateDir, r.rec); err != nil {
			return proc, r.saveFailed(err), nil
		}
	}
	return proc, nil, nil
}

// deletePending says why a delete the retire sent earlier may still run,
// "" when none can: its process, read by the id the record holds, still
// live (PENDING, RUNNING); or — its id never recorded, or unreadable — any
// live process on that service by the project's direct process list. A
// delete still queued reads the service live, and a resume that aborted then
// would re-key the pair onto a dev the delete removes after all (PM-7).
func (r *poolRetireRun) deletePending(ctx context.Context) string {
	hosts := map[string]string{}
	for _, s := range r.rec.devs() {
		hosts[s.ServiceID] = s.Hostname
	}
	unknown := map[string]string{}
	for _, id := range slices.Sorted(maps.Keys(r.rec.DeletesSent)) {
		host := cmp.Or(hosts[id], id)
		if pid := r.rec.DeletesSent[id]; pid != "" {
			if p, err := r.d.client.GetProcess(ctx, pid); err == nil {
				if ops.IsProcessLive(p.Status) {
					return fmt.Sprintf("the delete of %s sent earlier (process %s) is still %s", host, pid, p.Status)
				}
				continue
			}
		}
		unknown[id] = host
	}
	if len(unknown) == 0 {
		return ""
	}
	live, err := ops.ProjectActivity(ctx, r.d.client, r.d.projectID, unknown)
	if err != nil {
		return fmt.Sprintf("whether the delete sent earlier still runs could not be read: %v", err)
	}
	for _, host := range slices.Sorted(maps.Keys(live)) {
		op := live[host][0]
		return fmt.Sprintf("%s has a live process after the delete sent earlier (%s %s, process %s)", host, op.Action, op.Status, op.ProcessID)
	}
	return ""
}

// finish ends a retire whose dev is gone by id: the pair keyed by the pool
// (re-keyed now when the dev went before the retire got there — the stage
// never stays under a dead key), the dev's HQ backoff record dropped, any
// stale mount cleared.
func (r *poolRetireRun) finish(ctx context.Context) *mcp.CallToolResult {
	if err := r.rekey(); err != nil && !errors.Is(err, workflow.ErrServiceMetaNotFound) {
		return r.callAgain(fmt.Sprintf("%s is gone, and the pair could not be %s yet: %v", r.rec.Dev, r.rec.keyedBy(), err))
	}
	if r.rec.CodeVolume != nil {
		// A variant-B retire finishes only once its code volume reads gone:
		// only now does the pair stop naming it.
		if err := workflow.SettleMigratedCodeVolume(r.d.stateDir, r.rec.Dev, r.rec.pools(), true); err != nil {
			return r.callAgain(fmt.Sprintf("the code volume %s is gone, and the pair could not forget it yet: %v", r.rec.CodeVolume.Hostname, err))
		}
	}
	if err := forgetHQPairState(r.d.stateDir, r.rec.Dev); err != nil {
		r.resp.Warnings = append(r.resp.Warnings, err.Error())
	}
	if r.d.mounter != nil {
		for _, s := range r.rec.devs() {
			_, _ = ops.UnmountService(ctx, r.d.client, r.d.projectID, r.d.mounter, s.Hostname)
		}
	}
	r.rec.RetiredAt = time.Now().UTC().Format(time.RFC3339)
	if err := r.enter(poolRetireRetired); err != nil {
		return r.saveFailed(err)
	}
	return jsonResult(poolRetireRecordAnswer(r.resp, r.rec))
}

// abort ends a retire whose dev still reads live, before any delete took:
// the pair keyed by the dev again — the inverse applied to what the record
// of the pair holds now — the mount put back, the record aborted.
func (r *poolRetireRun) abort(ctx context.Context, reason, message string) *mcp.CallToolResult {
	if len(r.rec.DeletesSent) > 0 {
		// Nothing is reverted, and nothing said deleted or not, while a
		// delete this retire sent may still run.
		if why := r.deletePending(ctx); why != "" {
			return r.callAgain(fmt.Sprintf("The retire would stop (%s), but %s", message, why))
		}
	}
	if r.rec.Phase == poolRetireRewritten || r.rec.Phase == poolRetireDeleting {
		// A pair keyed by the dev already — a re-key that never took — has
		// nothing to revert, whatever else names the pool's hostname.
		if err := r.revert(); err != nil && !poolRetireKeyedByDev(r.d.stateDir, r.rec.Dev) {
			return convertError(platform.NewPlatformError(platform.ErrAPIError,
				fmt.Sprintf("The retire of %s stopped (%s) and the pair could not be keyed by %s again: %v", r.rec.Dev, message, r.rec.Dev, err),
				fmt.Sprintf(`Call zerops_workflow workflow="pool-migrate" action="retire" service=%q again: the retire resumes from %s.`, r.rec.Dev, r.rec.Phase),
			), WithRecoveryStatus())
		}
	}
	resp := r.resp
	if r.d.mounter != nil {
		for _, host := range append([]string{r.rec.Dev}, r.unmounted...) {
			if _, err := ops.MountService(ctx, r.d.client, r.d.projectID, r.d.mounter, host); err != nil {
				resp.Warnings = append(resp.Warnings, fmt.Sprintf("%s's mount was not put back: %v; zerops_mount puts it back.", host, err))
			}
		}
	}
	r.rec.AbortReason = reason + ": " + message
	if err := r.enter(poolRetireAborted); err != nil {
		return r.saveFailed(err)
	}
	resp.Status = poolMigrateStatusAborted
	resp.Action = actionRetire
	resp.Step = ""
	resp.Steps = nil
	resp.Next = nil
	resp.Refusals = []poolMigrateRefusal{{Reason: reason, Message: message + fmt.Sprintf(" Nothing was deleted: the pair is keyed by %s again and its mount put back.", r.rec.Dev)}}
	resp.Retire = r.rec.view()
	return jsonResult(resp)
}

// poolRetireKeyedByDev reports a pair keyed by dev, its dev half the dev.
func poolRetireKeyedByDev(stateDir, dev string) bool {
	m, err := workflow.FindServiceMeta(stateDir, dev)
	return err == nil && m != nil && m.Hostname == dev && !m.DevIsPool()
}

// callAgain answers a retire left in flight: the record stays for the next
// call.
func (r *poolRetireRun) callAgain(why string) *mcp.CallToolResult {
	resp := poolMigrateAt(r.resp, poolMigrateStepRetiring, nil,
		fmt.Sprintf("%s. The retire keeps its record at %s: call again — zerops_workflow workflow=%q action=%q service=%q with the same acknowledgment — and it resumes against %s's state by id.",
			why, r.rec.Phase, workflowPoolMigrate, actionRetire, r.rec.Dev, r.rec.Dev))
	resp.Action = actionRetire
	resp.Retire = r.rec.view()
	return jsonResult(resp)
}

func (r *poolRetireRun) saveFailed(err error) *mcp.CallToolResult {
	return convertError(fmt.Errorf("pool-migrate retire of %s at %s: %w", r.rec.Dev, r.rec.Phase, err), WithRecoveryStatus())
}

// rekeyBegun reports a re-key that has begun: a pair keyed by one of the
// record's pools moved from the dev, or no pair keyed by the dev any more. A
// record at rewritten whose re-key never began — the phase is saved before
// its work — is still before the split, and its resume reads the guards. A
// pair file zcp cannot read counts as begun: the resume finishes the re-key.
func (rec *poolRetireRecord) rekeyBegun(stateDir string) bool {
	for _, pool := range rec.pools() {
		m, err := workflow.ReadServiceMeta(stateDir, pool)
		if err != nil || (m.DevIsPool() && m.DevPool.MigratedFrom != nil && m.DevPool.MigratedFrom.Hostname == rec.Dev) {
			return true
		}
	}
	return !poolRetireKeyedByDev(stateDir, rec.Dev)
}

// poolRetireResume continues a retire in flight from its record, against the
// dev's state by id (docs/spec-workflows.md §13 PM-7): DELETING keeps the
// record; gone finishes — the pair re-keyed whatever the phase; live resumes
// from the phase on the acknowledged call, the guards re-run before the pair
// is re-keyed (at rewritten too, while its re-key has not begun), the dev
// re-fenced, and the bundle taken again from bundled or wherever the record
// holds no verified bundle (resumeRebundles).
func poolRetireResume(ctx context.Context, d poolRetireDeps, input WorkflowInput, rec *poolRetireRecord) *mcp.CallToolResult {
	corpus, _ := workflow.LoadAtomCorpus()
	run := &poolRetireRun{d: d, rec: rec, resp: poolRetireBase(corpus, rec), force: input.Force.Bool() || rec.Forced, forceNow: input.Force.Bool()}
	run.resp.Action = actionRetire
	state, err := poolRetireDevState(ctx, d.client, rec.DevServiceID)
	if err != nil {
		return run.callAgain(fmt.Sprintf("%s could not be read by its id: %v", rec.Dev, err))
	}
	if state == poolRetireDevDeleting {
		return run.callAgain(fmt.Sprintf("%s reads DELETING by its id", rec.Dev))
	}
	if rec.CodeVolume != nil && (state == poolRetireDevGone || rec.Phase == poolRetireDeletingVolume) {
		// The dev is gone: what is left of a variant-B retire — further dev
		// services, the code volume — resumes from their own states by id.
		return run.resumeCodeVolume(ctx, input)
	}
	if state == poolRetireDevGone {
		return run.held(ctx, func() *mcp.CallToolResult { return run.finish(ctx) })
	}
	// A delete sent earlier may be queued behind another process while the
	// dev reads live: nothing re-fences, re-reads or aborts until it ran.
	if why := run.deletePending(ctx); why != "" {
		return run.callAgain(why)
	}

	var g *poolRetireGuards
	var expected DiagnosedDestruction
	switch {
	case rec.Phase == poolRetireStarted, rec.Phase == poolRetireFenced, rec.Phase == poolRetireBundled,
		rec.Phase == poolRetireRewritten && !rec.rekeyBegun(d.stateDir):
		// Still before the split: a record cut between saving rewritten and
		// the re-key reads every guard again too (PM-12).
		in := input
		in.Service, in.Pool = rec.Dev, rec.Pool
		var result *mcp.CallToolResult
		if g, result = poolRetireRead(ctx, d, in); result != nil {
			return result
		}
		run.resp = g.resp
		expected = g.destruction(in)
	default:
		expected = rec.resumedDestruction(input)
	}
	if err := ValidateDestructiveAck(input.ConfirmDestructive, expected); err != nil {
		return convertError(err, WithWouldDestroy(&expected), WithRecoveryStatus())
	}
	return run.held(ctx, func() *mcp.CallToolResult {
		if g != nil && len(g.refusals) > 0 {
			return run.abort(ctx, g.refusals[0].Reason, poolRetireJoinRefusals(g.refusals))
		}
		if g != nil && len(g.unsaved) > 0 && !run.force {
			return run.abort(ctx, poolRetireRefuseUnsaved, poolRetireUnsavedRefusal(rec.Dev, g.unsaved).Message)
		}
		if err := run.fence(ctx); err != nil {
			return run.abort(ctx, poolRetireAbortFence, fmt.Sprintf("%s's mount could not be taken down: %v.", rec.Dev, err))
		}
		if rec.resumeRebundles() {
			if result := run.bundle(ctx); result != nil {
				return result
			}
		}
		return run.advance(ctx)
	})
}

// held runs act under both pair locks, on the record as it is once they are
// held: a record another call moved meanwhile answers as it is now instead.
func (r *poolRetireRun) held(ctx context.Context, act func() *mcp.CallToolResult) *mcp.CallToolResult {
	release, err := lockPoolRetirePair(ctx, r.d.stateDir, r.rec.Dev, r.rec.pools()...)
	if err != nil {
		return r.callAgain("the pair is held: " + pairHeldReason(r.rec.Dev, err))
	}
	defer release()
	now, err := readPoolRetireRecord(r.d.stateDir, r.rec.Dev)
	switch {
	case err != nil:
		return r.saveFailed(err)
	case now == nil:
		return r.callAgain("the retire record is gone")
	case now.Phase != r.rec.Phase || now.UpdatedAt != r.rec.UpdatedAt:
		resp := poolRetireRecordAnswer(r.resp, now)
		resp.Action = actionRetire
		return jsonResult(resp)
	}
	return act()
}

func poolRetireBundleName(rec *poolRetireRecord) string {
	if rec.Bundle == nil {
		return "(none)"
	}
	return rec.Bundle.Path
}

// poolRetireRestoreFrom names the bundle a retired answer restores from: the
// one holding the newest state of the repository. A retire forced past a code
// volume whose checkout moved after the first bundle took a second one from
// the volume itself, which holds the late writes; the first is named as the
// older, so following the sentence never restores the older checkout.
func poolRetireRestoreFrom(rec *poolRetireRecord) string {
	if v := rec.CodeVolume; v != nil && v.Bundle != nil {
		return fmt.Sprintf("The newer bundle of its repository is %s, taken from the code volume %s itself after its checkout moved, so it holds the late writes; retire.codeVolume.restore puts it back. The older one, %s, was taken before the dev services went and lacks them (retire.restore).",
			v.Bundle.Path, v.Hostname, poolRetireBundleName(rec))
	}
	return fmt.Sprintf("The bundle of its repository is %s; retire.restore puts it back.", poolRetireBundleName(rec))
}

func poolRetireJoinRefusals(refusals []poolMigrateRefusal) string {
	parts := make([]string, 0, len(refusals))
	for _, r := range refusals {
		parts = append(parts, r.Reason+": "+r.Message)
	}
	return strings.Join(parts, " ")
}

// poolRetireResumeArgs are the retire call that resumes rec: naming the pool
// of a single part, the dev alone for one serving several.
func poolRetireResumeArgs(rec *poolRetireRecord) map[string]string {
	args := map[string]string{"workflow": workflowPoolMigrate, "action": actionRetire, "service": rec.Dev}
	if rec.Pool != "" {
		args["pool"] = rec.Pool
	}
	return args
}

// poolRetireBase is the answer frame of a retire named by its record.
func poolRetireBase(corpus []workflow.KnowledgeAtom, rec *poolRetireRecord) poolMigrateResponse {
	guidance := atomBody(corpus, poolMigrateAtomID)
	if retire := atomBody(corpus, poolRetireAtomID); retire != "" {
		guidance += "\n\n" + retire
	}
	return poolMigrateResponse{
		Workflow: workflowPoolMigrate,
		Phase:    workflow.PhasePoolMigrateActive,
		Service:  rec.Dev,
		Stages:   rec.Stages,
		Pool:     rec.Pool,
		Parts:    rec.partStates(""),
		Guidance: guidance,
		Inputs:   poolMigrateInputs{Service: rec.Dev, Pool: rec.Pool},
	}
}

// poolRetireRecordAnswer answers from the record: retiring while it is in
// flight — the retire call resumes it — retired once done.
func poolRetireRecordAnswer(resp poolMigrateResponse, rec *poolRetireRecord) poolMigrateResponse {
	if rec.Phase == poolRetireAborted {
		reason, message, _ := strings.Cut(rec.AbortReason, ": ")
		resp.Status = poolMigrateStatusAborted
		resp.Step, resp.Steps, resp.Next = "", nil, nil
		resp.Refusals = []poolMigrateRefusal{{Reason: reason, Message: message}}
		resp.Retire = rec.view()
		return resp
	}
	if rec.Phase == poolRetireRetired {
		deploys := "the stage deploys from a lease of the pool"
		if len(rec.Parts) > 0 {
			deploys = "each stage deploys from a lease of its part's pool"
		}
		deleted := fmt.Sprintf("%s (id %s) is deleted", rec.Dev, rec.DevServiceID)
		if v := rec.CodeVolume; v != nil {
			deleted = fmt.Sprintf("%s — with %s and, last, the code volume %s (id %s), which the pair no longer names — are deleted", rec.Dev, poolRetireServiceNames(rec.DevServices, "no further dev service"), v.Hostname, v.ServiceID)
		}
		resp = poolMigrateAt(resp, poolMigrateStepRetired, nil,
			fmt.Sprintf("The retire is done: %s and the pair is %s; %s. %s",
				deleted, rec.keyedBy(), deploys, poolRetireRestoreFrom(rec)))
	} else {
		resp = poolMigrateAt(resp, poolMigrateStepRetiring, &poolMigrateNext{
			Tool: "zerops_workflow",
			Args: poolRetireResumeArgs(rec),
			Why:  fmt.Sprintf("Resumes the retire of %s from %s against its state by id; the call answers the acknowledgment it needs.", rec.Dev, rec.Phase),
		}, "")
	}
	resp.Parts = rec.partStates(resp.Step)
	resp.Retire = rec.view()
	return resp
}
