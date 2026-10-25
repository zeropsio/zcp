package tools

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gopkg.in/yaml.v3"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// workflowPoolMigrate routes zerops_workflow workflow="pool-migrate": a
// stateless multi-call narrowing that walks one part's dev service onto a dev
// pool (docs/spec-workflows.md §13). Every call re-reads live state and names
// the part's current step and the next exact call; the read side writes
// nothing anywhere.
const workflowPoolMigrate = "pool-migrate"

// The steps a part walks, in order (docs/spec-workflows.md §13.1).
const (
	poolMigrateStepProvision     = "provision"
	poolMigrateStepEnv           = "env"
	poolMigrateStepImage         = "image"
	poolMigrateStepMove          = "move"
	poolMigrateStepLease         = "lease"
	poolMigrateStepReadyToRetire = "ready-to-retire"
	poolMigrateStepRetiring      = "retiring"
	poolMigrateStepRetired       = "retired"
)

// poolMigrateStepOrder is the step sequence the response's steps list marks.
func poolMigrateStepOrder() []string {
	return []string{
		poolMigrateStepProvision, poolMigrateStepEnv, poolMigrateStepImage, poolMigrateStepMove,
		poolMigrateStepLease, poolMigrateStepReadyToRetire, poolMigrateStepRetiring, poolMigrateStepRetired,
	}
}

// A step's state in the response's steps list.
const (
	poolMigrateStateDone    = "done"
	poolMigrateStateCurrent = "current"
	poolMigrateStatePending = "pending"
)

// Response statuses: a candidate's current step, or a refusal.
const (
	poolMigrateStatusStep         = "step"
	poolMigrateStatusNotCandidate = "not-a-candidate"
)

// Refusal reasons — each names why a service is not a candidate
// (docs/spec-workflows.md §13 PM-9).
const (
	poolMigrateRefuseNotInContainer  = "not-in-container"
	poolMigrateRefusePoolOrBox       = "pool-or-box"
	poolMigrateRefuseNotAdopted      = "not-adopted"
	poolMigrateRefuseSimpleMode      = "simple-mode"
	poolMigrateRefuseLocalMode       = "local-mode"
	poolMigrateRefuseUnsupportedMode = "unsupported-mode"
	poolMigrateRefusePoolNotAPart    = "pool-not-a-part"
	poolMigrateRefuseNoOrigin        = "no-origin"
	poolMigrateRefuseDevNotLive      = "dev-not-live"
	poolMigrateRefusePoolInvalid     = "pool-hostname-invalid"
	poolMigrateRefusePoolTaken       = "pool-hostname-taken"
	poolMigrateRefuseDevUnreadable   = "dev-unreadable"
	poolMigrateRefuseCrewWorktrees   = "crew-worktrees"
)

// The part's pool, as the provision step imports it: `<stem>pool`, no
// container until a lease asks for one, at most three (docs/spec-workflows.md
// §13.1).
const (
	poolMigratePoolType      = "swarm@1"
	poolMigratePoolSuffix    = "pool"
	poolMigrateMinContainers = 0
	poolMigrateMaxContainers = 3
)

// The atoms carrying the flow's guidance: the plan on every answer, the move
// step's on every answer that carries the census, the env step's on its
// answers and the step's own, and the image step's once the pool holds the
// dev service's env.
const (
	poolMigrateAtomID      = "pool-migrate-plan"
	poolMigrateMoveAtomID  = "pool-migrate-move"
	poolMigrateEnvAtomID   = "pool-migrate-env"
	poolMigrateImageAtomID = "pool-migrate-image"
)

// envActionRequest is zerops_env's action that asks the person for a value.
const envActionRequest = "request"

// poolMigrateProcessWait is zerops_process's action that waits for a
// service's live processes to finish.
const poolMigrateProcessWait = "wait"

// actionPoolEnv is the env step's own call: zerops_workflow
// workflow="pool-migrate" action="pool-env" (docs/spec-workflows.md §13 PM-4).
const actionPoolEnv = "pool-env"

// poolMigrateOrigin is the remote a lease of the pool clones: the dev
// service's origin.
const poolMigrateOrigin = "origin"

// Where the work's branch comes from (docs/spec-workflows.md §13 PM-2).
const (
	poolMigrateWorkFromChange   = "change"
	poolMigrateWorkFromCurrent  = "current"
	poolMigrateWorkFromUpstream = "upstream"
)

// poolMigrateCrewDir is the folder a crew lane's linked worktree lives in,
// below /var/www or below the repository's root: `/var/www/.crew/<handle>`.
const poolMigrateCrewDir = "/.crew/"

// poolMigrateDevSuffix is the suffix a dev hostname drops to name its stem:
// `apidev` → `api`.
const poolMigrateDevSuffix = "dev"

type poolMigrateResponse struct {
	Workflow string         `json:"workflow"`
	Phase    workflow.Phase `json:"phase"`
	Status   string         `json:"status"`
	// Service is the part's dev service — the pair's dev half when the call
	// named its stage.
	Service string   `json:"service"`
	Stages  []string `json:"stages,omitempty"`
	Pool    string   `json:"pool,omitempty"`
	// Part is the stage of the part the answer walks, when the dev serves
	// several; Parts lists every one of them with its pool and step.
	Part  string                 `json:"part,omitempty"`
	Parts []poolMigratePartState `json:"parts,omitempty"`
	Step  string                 `json:"step,omitempty"`
	// Steps marks every step done, current or pending.
	Steps []poolMigrateStepState `json:"steps,omitempty"`
	Next  *poolMigrateNext       `json:"next,omitempty"`
	// NextUnavailable says why a step names no next call.
	NextUnavailable string `json:"nextUnavailable,omitempty"`
	// Census is the dev service's unsaved work, read on every step.
	Census *ops.PoolMigrateCensus `json:"census,omitempty"`
	// WorkBranch is the branch a lease of the pool checks out.
	WorkBranch *poolMigrateWorkBranch `json:"workBranch,omitempty"`
	// Action is "pool-env" on the answer of the env step's own call.
	Action string `json:"action,omitempty"`
	// Env is the pool's env against the dev's, on every step once the pool
	// is settled; after action="pool-env", what the step did.
	Env *ops.PoolMigrateEnv `json:"env,omitempty"`
	// EnvRequests are the zerops_env action="request" calls for the secrets
	// zcp could not read on the dev service.
	EnvRequests []poolMigrateNext `json:"envRequests,omitempty"`
	// Image is the image step's reading of the pool setup in the commit its
	// deploy ships and of the app version the pool runs, on every step once
	// the pool holds the dev service's env.
	Image *ops.PoolMigrateImage `json:"image,omitempty"`
	// Retire is the retire's record — its phase, the bundle of the dev's
	// repository and what it kept — on every answer once a retire started.
	Retire   *poolRetireView      `json:"retire,omitempty"`
	Refusals []poolMigrateRefusal `json:"refusals,omitempty"`
	Warnings []string             `json:"warnings,omitempty"`
	Guidance string               `json:"guidance"`
	Inputs   poolMigrateInputs    `json:"inputs"`
}

type poolMigrateStepState struct {
	Step  string `json:"step"`
	State string `json:"state"`
}

type poolMigrateNext struct {
	Tool string            `json:"tool"`
	Args map[string]string `json:"args,omitempty"`
	Why  string            `json:"why"`
}

type poolMigrateRefusal struct {
	Reason   string             `json:"reason"`
	Message  string             `json:"message"`
	Recovery *topology.Recovery `json:"recovery,omitempty"`
}

// poolMigrateWorkBranch is the work's branch: the branch on the origin a lease
// of the pool checks out, and where it comes from — or, in Missing, why none is
// named yet and what names one.
type poolMigrateWorkBranch struct {
	Branch  string `json:"branch,omitempty"`
	Source  string `json:"source,omitempty"`
	Missing string `json:"missing,omitempty"`
}

// poolMigrateInputs echoes what the next call passes again.
type poolMigrateInputs struct {
	Service string `json:"service"`
	Pool    string `json:"pool,omitempty"`
}

// poolMigrateDeps is what a pool-migrate call works with.
type poolMigrateDeps struct {
	projectID string
	client    platform.Client
	ssh       ops.SSHDeployer
	stateDir  string
	rt        runtime.Info
	// hqAddress is the address of this Mate's HQ, "" without one: where the
	// dev service's origin is in a Mate.
	hqAddress func() string
}

// poolMigrateCall is what a call read before it acts on a settled pool.
type poolMigrateCall struct {
	resp   poolMigrateResponse
	corpus []workflow.KnowledgeAtom
	meta   *workflow.ServiceMeta
	dev    platform.ServiceStack
	// partDev is the dev service the part runs on — the dev, or for a
	// variant-B pair the dev service of the part — whose env and scale the
	// part's pool takes.
	partDev platform.ServiceStack
	pool    platform.ServiceStack
	stages  []platform.ServiceStack
	// services is the project's every service by the direct list.
	services []platform.ServiceStack
	// partPools are every part's pool when the dev serves several, nil for
	// a single part: the image step checks the declaration names them.
	partPools []string
}

// handlePoolMigrate is the read side of workflow="pool-migrate": it names the
// part's current step from direct reads only — the service list and process
// list a just-made import cannot be missing from — renders the pool stanza
// for the provision step, carries the census of the dev service's unsaved
// work and the work's branch on every step, compares the pool's env with the
// dev's once the pool is settled, and refuses a non-candidate with every
// reason it finds. It writes nothing: no state file, no meta, no env, no
// ref; the one SSH call is the census, read-only git on the dev service, and
// the env comparison reads no secret.
func handlePoolMigrate(
	ctx context.Context,
	projectID string,
	client platform.Client,
	sshDeployer ops.SSHDeployer,
	input WorkflowInput,
	stateDir string,
	rt runtime.Info,
) (*mcp.CallToolResult, any, error) {
	return poolMigrateRun(ctx, poolMigrateDeps{projectID: projectID, client: client, ssh: sshDeployer, stateDir: stateDir, rt: rt, hqAddress: hqAddress}, input)
}

func poolMigrateRun(ctx context.Context, d poolMigrateDeps, input WorkflowInput) (*mcp.CallToolResult, any, error) {
	dr, final, failed := readPoolMigrate(ctx, d, input)
	switch {
	case failed != nil:
		return failed, nil, nil
	case final != nil:
		return jsonResult(*final), nil, nil
	}
	resp, failed := dr.answer(ctx, d)
	if failed != nil {
		return failed, nil, nil
	}
	return jsonResult(resp), nil, nil
}

// handlePoolMigrateEnv is the env step, zerops_workflow workflow="pool-migrate"
// action="pool-env": it re-runs every check of the read side, then gives the
// settled pool the dev service's own env (ops.ApplyPoolMigrateEnv) — secrets
// read on the dev and written by zcp, so no value passes through the agent —
// and answers what it did with the step that follows.
func handlePoolMigrateEnv(
	ctx context.Context,
	projectID string,
	client platform.Client,
	sshDeployer ops.SSHDeployer,
	input WorkflowInput,
	stateDir string,
	rt runtime.Info,
) (*mcp.CallToolResult, any, error) {
	return poolMigrateEnvRun(ctx, poolMigrateDeps{projectID: projectID, client: client, ssh: sshDeployer, stateDir: stateDir, rt: rt, hqAddress: hqAddress}, input)
}

func poolMigrateEnvRun(ctx context.Context, d poolMigrateDeps, input WorkflowInput) (*mcp.CallToolResult, any, error) {
	if wf := strings.TrimSpace(input.Workflow); wf != "" && wf != workflowPoolMigrate {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf(`action="pool-env" belongs to workflow="pool-migrate", not %q`, wf),
			`Call zerops_workflow workflow="pool-migrate" action="pool-env" service="<dev>".`,
		), WithRecoveryStatus()), nil, nil
	}
	dr, final, failed := readPoolMigrate(ctx, d, input)
	switch {
	case failed != nil:
		return failed, nil, nil
	case final != nil:
		final.Action = actionPoolEnv
		return jsonResult(*final), nil, nil
	}
	// A dev serving several parts reads every part first: the answer lists
	// them, and a call naming none copies the env of the part the read side
	// walks.
	part := dr.selected
	var answers []poolMigrateResponse
	if dr.multiPart() {
		if answers, failed = dr.partAnswers(ctx, d); failed != nil {
			return failed, nil, nil
		}
		if part < 0 {
			part = max(dr.current(answers), 0)
		}
	}
	answered := func(resp poolMigrateResponse) (*mcp.CallToolResult, any, error) {
		if dr.multiPart() {
			answers[part] = resp
			resp = dr.partsAnswer(answers, part)
		}
		resp.Action = actionPoolEnv
		return jsonResult(resp), nil, nil
	}
	call, final, failed := dr.partRead(ctx, d, part)
	switch {
	case failed != nil:
		return failed, nil, nil
	case final != nil:
		if final.Status == poolMigrateStatusStep {
			final.Warnings = append(final.Warnings, "Nothing was written: the pool's env is copied once the pool exists and its own processes have finished.")
		}
		return answered(*final)
	}
	env, err := ops.ApplyPoolMigrateEnv(ctx, d.client, d.ssh, call.envInput(d, input))
	if err != nil {
		return convertError(fmt.Errorf("pool-migrate: copy the env to the pool: %w", err), WithRecoveryStatus()), nil, nil
	}
	return answered(poolMigrateSettledAnswer(ctx, d, call, env))
}

// readPoolMigrate answers a part whose retire started from the retire's
// record — retiring while it is in flight, retired once done — and runs every
// other call's checks of the dev service (readPoolMigrateDev). Reading the
// record writes nothing and reaches no service.
func readPoolMigrate(ctx context.Context, d poolMigrateDeps, input WorkflowInput) (*poolMigrateDevRead, *poolMigrateResponse, *mcp.CallToolResult) {
	if service := strings.TrimSpace(input.Service); service != "" {
		rec, err := findPoolRetireRecord(d.stateDir, service)
		if err != nil {
			return nil, nil, convertError(fmt.Errorf("pool-migrate: %w", err), WithRecoveryStatus())
		}
		if rec != nil && (poolRetireInFlight(rec.Phase) || (rec.Phase == poolRetireRetired && !poolRetireNameRetaken(ctx, d, rec))) {
			corpus, err := workflow.LoadAtomCorpus()
			if err != nil {
				return nil, nil, convertError(err, WithRecoveryStatus())
			}
			resp := poolRetireRecordAnswer(poolRetireBase(corpus, rec), rec)
			return nil, &resp, nil
		}
	}
	return readPoolMigrateDev(ctx, d, input)
}

// readPoolMigrateDev runs every check a call makes of the dev service before
// any part's pool: the meta, the direct service list, the parts and their
// pools' hostnames, the census and the work's branch. It answers a final
// response (a refusal), an error result, or the dev read the parts go on
// from.
func readPoolMigrateDev(ctx context.Context, d poolMigrateDeps, input WorkflowInput) (*poolMigrateDevRead, *poolMigrateResponse, *mcp.CallToolResult) {
	client, sshDeployer, rt, stateDir := d.client, d.ssh, d.rt, d.stateDir
	service := strings.TrimSpace(input.Service)
	poolInput := strings.TrimSpace(input.Pool)
	if service == "" {
		return nil, nil, convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			`workflow="pool-migrate" needs service: the dev service to move onto a dev pool`,
			poolMigrateCandidatesHint(stateDir),
		), WithRecoveryStatus())
	}
	corpus, err := workflow.LoadAtomCorpus()
	if err != nil {
		return nil, nil, convertError(err, WithRecoveryStatus())
	}
	resp := poolMigrateResponse{
		Workflow: workflowPoolMigrate,
		Phase:    workflow.PhasePoolMigrateActive,
		Service:  service,
		Guidance: atomBody(corpus, poolMigrateAtomID),
		Inputs:   poolMigrateInputs{Service: service, Pool: poolInput},
	}
	refused := func(refusals ...poolMigrateRefusal) (*poolMigrateDevRead, *poolMigrateResponse, *mcp.CallToolResult) {
		r := poolMigrateRefused(resp, refusals...)
		return nil, &r, nil
	}

	if !rt.InContainer || sshDeployer == nil {
		return refused(poolMigrateRefusal{
			Reason:  poolMigrateRefuseNotInContainer,
			Message: "pool-migrate runs from the zcp service inside the project: it reads the dev service over SSH, and a pool's containers answer only there. Nothing was read or changed.",
		})
	}
	if client == nil {
		return nil, nil, convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"Platform client unavailable — pool-migrate reads the project's services",
			"Ensure ZCP is configured with a Zerops API key (ZCP_API_KEY).",
		), WithRecoveryStatus())
	}

	// The direct read, never the ES search: a pool imported a moment ago is
	// absent from the search for seconds (docs/spec-workflows.md §3.5).
	live, err := client.ListServicesDirect(ctx, d.projectID)
	if err != nil {
		return nil, nil, convertError(fmt.Errorf("pool-migrate: list services: %w", err), WithRecoveryStatus())
	}
	byName := make(map[string]platform.ServiceStack, len(live))
	for _, svc := range live {
		byName[svc.Name] = svc
	}
	if svc, ok := byName[service]; ok && topology.IsPoolOrSandboxType(svc.ServiceStackTypeInfo.ServiceStackTypeVersionName) {
		return refused(poolMigrateRefusal{
			Reason:  poolMigrateRefusePoolOrBox,
			Message: fmt.Sprintf("%s is a %s service: already a pool or a Box, not a dev service to move onto one.", service, svc.ServiceStackTypeInfo.ServiceStackTypeVersionName),
		})
	}

	meta, err := workflow.FindServiceMeta(stateDir, service)
	if err != nil {
		return nil, nil, convertError(fmt.Errorf("pool-migrate: read the service's meta: %w", err), WithRecoveryStatus())
	}
	if meta == nil || !meta.IsComplete() {
		return refused(poolMigrateRefusal{
			Reason:  poolMigrateRefuseNotAdopted,
			Message: fmt.Sprintf("%s is not a dev service zcp tracks, or its bootstrap has not finished. Adopt it with zerops_workflow action=\"start\" workflow=\"bootstrap\" route=\"adopt\" (or finish the bootstrap), then call again.", service),
		})
	}
	dev := meta.Hostname
	resp.Service = dev
	resp.Inputs.Service = dev
	resp.Stages = meta.StageHostnames()

	refusals := poolMigrateMetaRefusals(meta, rt)
	devSvc, devLive := byName[dev]
	if !devLive {
		refusals = append(refusals, poolMigrateRefusal{
			Reason:  poolMigrateRefuseDevNotLive,
			Message: fmt.Sprintf("%s is not among the project's services (read directly); there is no dev service to move.", dev),
		})
	}
	parts, selected, partRefusals := poolMigrateParts(meta, service, poolInput, byName)
	refusals = append(refusals, partRefusals...)
	switch {
	case len(parts) == 1:
		resp.Pool = parts[0].pool
	case selected >= 0:
		resp.Pool, resp.Part = parts[selected].pool, parts[selected].stage
	}
	if len(refusals) > 0 {
		return refused(refusals...)
	}

	// A variant-B pair's repository is the code volume's checkout, read
	// through the dev's mount of it.
	root := poolMigrateCodeRoot(meta)
	census, err := ops.ReadPoolMigrateCensus(ctx, sshDeployer, dev, root)
	if err != nil {
		return refused(poolMigrateRefusal{
			Reason:  poolMigrateRefuseDevUnreadable,
			Message: withSSHStderr("could not read "+dev+"'s repository at "+root+" over SSH", err) + ". A dev service the flow cannot read does not move: what it holds would be unknown.",
		})
	}
	if crew := poolMigrateCrewHandles(census, root); len(crew) > 0 {
		return refused(poolMigrateRefusal{
			Reason:  poolMigrateRefuseCrewWorktrees,
			Message: fmt.Sprintf("%s holds crew worktrees (%s). Crew lanes stay on a plain dev service until crewmates work on leases of a pool, so it does not move onto one; nothing was changed.", dev, strings.Join(crew, ", ")),
		})
	}

	resp.Census = census
	resp.WorkBranch = poolMigrateWorkBranchOf(meta, census, rt, hqChanges(stateDir))
	if move := atomBody(corpus, poolMigrateMoveAtomID); move != "" {
		resp.Guidance += "\n\n" + move
	}
	return &poolMigrateDevRead{
		input: input, resp: resp, corpus: corpus, meta: meta, dev: devSvc,
		byName: byName, services: live, parts: parts, selected: selected,
	}, nil, nil
}

// partRead runs every check of part i before its pool is settled: it answers
// the part's provision step — the pool absent — or a wait on the pool's own
// processes, an error result, or the call to go on with on the settled pool.
func (dr *poolMigrateDevRead) partRead(ctx context.Context, d poolMigrateDeps, i int) (*poolMigrateCall, *poolMigrateResponse, *mcp.CallToolResult) {
	part := dr.parts[i]
	resp := dr.resp
	resp.Pool = part.pool
	if dr.multiPart() {
		resp.Part = part.stage
		resp.Inputs.Pool = part.pool
	}
	pool := part.pool
	poolSvc, poolLive := dr.byName[pool]
	partDev := dr.dev
	if part.dev != "" && part.dev != dr.dev.Name {
		if svc, ok := dr.byName[part.dev]; ok {
			partDev = svc
		} else {
			resp.Warnings = append(resp.Warnings, fmt.Sprintf("%s, the dev service the part of %s runs on, is not among the project's services (read directly): the part's pool takes %s's env and scale.", part.dev, part.stage, dr.dev.Name))
		}
	}
	if !poolLive {
		stanza, warnings, err := renderPoolStanza(ctx, d.client, partDev, pool, d.rt.ServiceName)
		if err != nil {
			return nil, nil, convertError(fmt.Errorf("pool-migrate: render the pool stanza: %w", err), WithRecoveryStatus())
		}
		resp.Warnings = append(resp.Warnings, warnings...)
		r := poolMigrateAt(resp, poolMigrateStepProvision, &poolMigrateNext{
			Tool: "zerops_import",
			Args: map[string]string{"content": stanza},
			Why:  "Imports the part's pool. An import runs inside a develop task.",
		}, "")
		return nil, &r, nil
	}

	activity, err := ops.ProjectActivity(ctx, d.client, d.projectID, map[string]string{poolSvc.ID: pool})
	if err != nil {
		return nil, nil, convertError(fmt.Errorf("pool-migrate: read the project's processes: %w", err), WithRecoveryStatus())
	}
	if live := activity[pool]; len(live) > 0 {
		// A build of the pool is its image step's deploy; any other process
		// of its own is still the provision step's.
		step, why := poolMigrateStepProvision, "The pool's own processes are still running; the next step starts once they finish."
		if _, building := standupBuildOp(live); building {
			step, why = poolMigrateStepImage, "A build of the pool is running; the image step reads the app version the pool runs once it finishes."
		}
		r := poolMigrateAt(resp, step, &poolMigrateNext{
			Tool: "zerops_process",
			Args: map[string]string{"action": poolMigrateProcessWait, "service": pool},
			Why:  why,
		}, "")
		return nil, &r, nil
	}
	// A part of several compares its pool with its own stage only.
	stageNames := dr.meta.StageHostnames()
	var partPools []string
	if dr.multiPart() {
		stageNames = []string{part.stage}
		partPools = dr.partPools()
	}
	var stages []platform.ServiceStack
	for _, stage := range stageNames {
		if svc, ok := dr.byName[stage]; ok {
			stages = append(stages, svc)
		}
	}
	return &poolMigrateCall{resp: resp, corpus: dr.corpus, meta: dr.meta, dev: dr.dev, partDev: partDev, pool: poolSvc, stages: stages, services: dr.services, partPools: partPools}, nil, nil
}

// partAnswer is the read side's answer for part i: its provision or wait
// step, or — its pool settled — the step the pool's env, image and zcp's
// lease records name, with the call on the settled pool.
func (dr *poolMigrateDevRead) partAnswer(ctx context.Context, d poolMigrateDeps, i int) (poolMigrateResponse, *poolMigrateCall, *mcp.CallToolResult) {
	call, final, failed := dr.partRead(ctx, d, i)
	switch {
	case failed != nil:
		return poolMigrateResponse{}, nil, failed
	case final != nil:
		return *final, nil, nil
	}
	env, err := ops.ReadPoolMigrateEnv(ctx, d.client, call.envInput(d, dr.input))
	if err != nil {
		return poolMigrateResponse{}, nil, convertError(fmt.Errorf("pool-migrate: compare the pool's env: %w", err), WithRecoveryStatus())
	}
	return poolMigrateSettledAnswer(ctx, d, call, env), call, nil
}

// answer is the read side's answer: the one part's, or — for a dev serving
// several — the part the call named or the first not yet ready to retire,
// with every part listed.
func (dr *poolMigrateDevRead) answer(ctx context.Context, d poolMigrateDeps) (poolMigrateResponse, *mcp.CallToolResult) {
	if !dr.multiPart() {
		resp, _, failed := dr.partAnswer(ctx, d, 0)
		return resp, failed
	}
	answers, failed := dr.partAnswers(ctx, d)
	if failed != nil {
		return poolMigrateResponse{}, failed
	}
	return dr.partsAnswer(answers, dr.current(answers)), nil
}

// envInput is what the env step reads for this call: the env of the dev
// service the part runs on, and the pair's GIT_TOKEN from its dev half — the
// service the pair pushes from, where git-push-setup writes it — since every
// part of a variant-B pair is one repository (docs/spec-workflows.md §13
// PM-13).
func (c *poolMigrateCall) envInput(d poolMigrateDeps, input WorkflowInput) ops.PoolMigrateEnvInput {
	return ops.PoolMigrateEnvInput{
		Dev:          c.partDev,
		GitTokenFrom: c.dev,
		Pool:         c.pool,
		Stages:       c.stages,
		InMate:       d.rt.MateEnabled,
		CopyGitToken: input.CopyGitToken.Bool(),
		Origin:       poolMigrateDevOrigin(c.meta, d),
		StateDir:     d.stateDir,
	}
}

// poolMigrateDevOrigin is the repository the dev service's work moves
// through, the one a lease of the pool clones: in a Mate the pair's HQ
// repository, elsewhere its git-push remote. "" when zcp cannot name it.
func poolMigrateDevOrigin(meta *workflow.ServiceMeta, d poolMigrateDeps) string {
	if !d.rt.MateEnabled {
		return meta.RemoteURL
	}
	if meta.HQ == nil || meta.HQ.Repo == "" || d.hqAddress == nil {
		return ""
	}
	address := d.hqAddress()
	if address == "" {
		return ""
	}
	return hq.RepoURLAt(address, meta.HQ.AppID, meta.HQ.Repo)
}

// poolMigrateSettledAnswer answers a settled pool: the env step while the
// pool's env is not the dev's, the image step once it is.
func poolMigrateSettledAnswer(ctx context.Context, d poolMigrateDeps, call *poolMigrateCall, env *ops.PoolMigrateEnv) poolMigrateResponse {
	resp := call.resp
	shown := *env
	shown.Requests = nil
	resp.Env = &shown
	for _, req := range env.Requests {
		resp.EnvRequests = append(resp.EnvRequests, poolMigrateNext{
			Tool: "zerops_env",
			Args: map[string]string{
				"action": envActionRequest, "serviceHostname": call.pool.Name,
				"key": req.Key, "label": req.Label, "why": req.Why, "sensitive": "true",
			},
			Why: fmt.Sprintf("zcp could not read %s on %s, so the person gives the pool its value.", req.Key, call.partDev.Name),
		})
	}
	if envAtom := atomBody(call.corpus, poolMigrateEnvAtomID); envAtom != "" {
		resp.Guidance += "\n\n" + envAtom
	}
	if env.Done {
		return poolMigrateImageAnswer(ctx, d, call, resp)
	}
	args := map[string]string{"workflow": workflowPoolMigrate, "action": actionPoolEnv, "service": call.dev.Name, "pool": call.pool.Name}
	return poolMigrateAt(resp, poolMigrateStepEnv, &poolMigrateNext{
		Tool: "zerops_workflow",
		Args: args,
		Why:  "zcp gives the pool the dev service's own env itself, secrets included, writing only what differs; no value passes through you.",
	}, "")
}

// poolMigrateImageAnswer answers a pool that holds the dev service's env: the
// image step, which checks the pool setup in the dev checkout's HEAD commit
// (ops.ReadPoolMigrateImage — one read-only git round trip on the dev) and
// names `zerops_deploy ... sha=<commit>` once the commit passes every check,
// until the pool runs an app version newer than the commit's last change of
// its config file; then the move step. It writes nothing: the agent commits
// and deploys.
func poolMigrateImageAnswer(ctx context.Context, d poolMigrateDeps, call *poolMigrateCall, resp poolMigrateResponse) poolMigrateResponse {
	var head ops.PoolMigrateTree
	if resp.Census != nil && len(resp.Census.Trees) > 0 {
		head = resp.Census.Trees[0]
	}
	image, err := ops.ReadPoolMigrateImage(ctx, d.ssh, ops.PoolMigrateImageInput{
		Dev:                   call.dev.Name,
		Root:                  poolMigrateCodeRoot(call.meta),
		Head:                  head,
		Pool:                  call.pool,
		DevSetup:              call.meta.PrimarySetupName,
		FallbackMaxContainers: poolMigrateMaxContainers,
		PartPools:             call.partPools,
	})
	if err != nil {
		// The census just read the dev, so this is no refusal: the step
		// stays, and names no deploy until it has checked the commit.
		return poolMigrateAt(resp, poolMigrateStepImage, nil,
			withSSHStderr("could not read the pool setup in "+call.dev.Name+"'s commit "+poolMigrateShortCommit(head.Head)+" over SSH", err)+
				". Call again: the image step names a deploy only once it has checked the commit.")
	}
	resp.Image = image
	if imageAtom := atomBody(call.corpus, poolMigrateImageAtomID); imageAtom != "" {
		resp.Guidance += "\n\n" + imageAtom
	}
	short := poolMigrateShortCommit(image.Commit)
	switch {
	case image.Done:
		return poolMigrateAfterImage(d, call, resp)
	case len(image.Missing) > 0:
		why := "The pool setup is not ready: fix what image.missing names, commit it (zcp never commits), then call again."
		if image.Commit != "" {
			why = fmt.Sprintf("The pool setup in commit %s is not ready: fix what image.missing names, commit it (zcp never commits), then call again — the deploy ships a commit, never the working tree.", short)
		}
		if len(image.Uncommitted) > 0 {
			why += fmt.Sprintf(" Your changes to %s are not committed yet.", strings.Join(image.Uncommitted, " and "))
		}
		return poolMigrateAt(resp, poolMigrateStepImage, nil, why)
	}
	why := fmt.Sprintf("Builds the pool's image from setup %q in commit %s, never the dev service's working tree; it reaches only containers the pool creates after it.", image.Setup, short)
	if image.Deployed != nil {
		why += " The app version the pool runs was not created after the last change of " + image.File + " up to that commit."
	}
	if len(image.Uncommitted) > 0 {
		why += fmt.Sprintf(" Your changes to %s are not in that commit: commit them first and call again to ship them.", strings.Join(image.Uncommitted, " and "))
	}
	return poolMigrateAt(resp, poolMigrateStepImage, &poolMigrateNext{
		Tool: "zerops_deploy",
		Args: map[string]string{"sourceService": call.dev.Name, "targetService": call.pool.Name, "setup": image.Setup, "sha": image.Commit},
		Why:  why,
	}, "")
}

// poolMigrateAfterImage answers a part whose pool runs the commit's pool
// setup: move while the dev holds work its origin lacks and zcp holds no lease
// of the pool on the work's branch; lease, naming the acquire, once nothing is
// left to move; ready-to-retire, naming the retire's diagnosis, once zcp holds
// such a lease by its own records. The retire itself asks the pool's
// orchestrator and verifies the lease; this reads neither, and writes nothing.
func poolMigrateAfterImage(d poolMigrateDeps, call *poolMigrateCall, resp poolMigrateResponse) poolMigrateResponse {
	if retire := atomBody(call.corpus, poolRetireAtomID); retire != "" {
		resp.Guidance += "\n\n" + retire
	}
	dev, pool := call.dev.Name, call.pool.Name
	wb := resp.WorkBranch
	if wb == nil || wb.Branch == "" {
		missing := "zcp names none yet"
		if wb != nil && wb.Missing != "" {
			missing = wb.Missing
		}
		return poolMigrateAt(resp, poolMigrateStepMove, nil, "The image step is done. A lease of the pool needs the work's branch: "+missing)
	}
	branch := wb.Branch
	leases, err := ops.PoolMigrateLeases(d.stateDir, pool, poolMigrateDevOrigin(call.meta, d), branch)
	if err != nil {
		resp.Warnings = append(resp.Warnings, fmt.Sprintf("zcp's lease records could not be read: %v.", err))
	}
	unsaved := poolRetireBlocking(resp.Census)
	switch {
	case len(leases) > 0:
		if len(unsaved) > 0 {
			resp.Warnings = append(resp.Warnings, fmt.Sprintf("%s still holds work its origin lacks (census): the retire refuses until you push it, or until the person agrees to leaving it (force); a verified bundle keeps a copy either way.", dev))
		}
		return poolMigrateAt(resp, poolMigrateStepReadyToRetire, &poolMigrateNext{
			Tool: "zerops_workflow",
			Args: map[string]string{"workflow": workflowPoolMigrate, "action": actionRetire, "service": dev, "pool": pool},
			Why: fmt.Sprintf("zcp holds %s, a lease of %s on %s. The retire's first call reads every guard again and names in wouldDestroy what deleting %s erases, changing nothing; ask the person, and only once they agreed by name send the acknowledged call it names.",
				leases[0].Alias, pool, branch, poolMigrateRetireDeletes(call.meta)),
		}, "")
	case len(unsaved) > 0:
		return poolMigrateAt(resp, poolMigrateStepMove, nil,
			fmt.Sprintf("The image step is done. %s holds work its origin lacks (census): push each branch you keep, then call again. Once it is moved — or the person chose to leave the rest — lease the pool on the work's branch: zerops_lease action=%q pool=%q branch=%q.", dev, leaseProbeAction, pool, branch))
	}
	return poolMigrateAt(resp, poolMigrateStepLease, &poolMigrateNext{
		Tool: "zerops_lease",
		Args: map[string]string{"action": leaseProbeAction, "pool": pool, "branch": branch},
		Why:  fmt.Sprintf("Leases a container of %s on %s from the dev's origin; start its dev servers (zerops_dev_server hostname=\"<alias>\") and prove it runs with zerops_verify serviceHostname=\"<alias>\" — the retire verifies it again.", pool, branch),
	}, "")
}

// poolMigrateRetireDeletes names what the retire deletes, in its order, as
// its diagnosis names them: the dev, and for a variant-B pair every further
// dev service and, last, the code volume (docs/spec-workflows.md §13 PM-13).
func poolMigrateRetireDeletes(meta *workflow.ServiceMeta) string {
	names := []string{meta.Hostname}
	if cv := meta.CodeVolume; cv != nil {
		names = append(names, cv.DevServiceHostnames()...)
		names = append(names, "the code volume "+cv.Hostname)
	}
	return poolRetireJoinAnd(names)
}

// poolMigrateShortCommit is how much of a commit the flow's messages name.
func poolMigrateShortCommit(sha string) string {
	return sha[:min(10, len(sha))]
}

// poolMigrateMetaRefusals are the refusals the meta alone decides: the mode,
// and an origin to move the work through. A variant-B pair moves like a dev
// serving several parts: its repository is the code volume's checkout, and
// the retire deletes every dev service, then the volume (PM-13).
func poolMigrateMetaRefusals(meta *workflow.ServiceMeta, rt runtime.Info) []poolMigrateRefusal {
	dev := meta.Hostname
	// A pair already keyed by its dev pool, named by its stage: its dev half
	// is the pool, which the census must never reach over ssh (§8 PS-3).
	if meta.DevIsPool() {
		return []poolMigrateRefusal{{
			Reason:  poolMigrateRefusePoolOrBox,
			Message: fmt.Sprintf("The pair of %s is already keyed by its dev pool %s: its dev half is a pool, with no dev service left to move onto one.", strings.Join(meta.StageHostnames(), ", "), dev),
		}}
	}
	var out []poolMigrateRefusal
	mode := meta.Mode
	if mode == "" {
		mode = topology.ModeStandard
	}
	switch mode {
	case topology.ModeDev, topology.ModeStandard:
	case topology.ModeSimple:
		out = append(out, poolMigrateRefusal{
			Reason:  poolMigrateRefuseSimpleMode,
			Message: fmt.Sprintf("%s runs in simple mode: it deploys onto itself and serves what it runs. Only a dev service, alone or the dev half of a dev/stage pair, moves onto a pool.", dev),
		})
	case topology.ModeLocalStage, topology.ModeLocalOnly:
		out = append(out, poolMigrateRefusal{
			Reason:  poolMigrateRefuseLocalMode,
			Message: fmt.Sprintf("%s is tracked in %s mode: its code lives on a local machine, not in a dev service, so there is nothing to move onto a pool.", dev, mode),
		})
	case topology.ModeStage:
		out = append(out, poolMigrateRefusal{
			Reason:  poolMigrateRefuseUnsupportedMode,
			Message: fmt.Sprintf("%s is tracked in %s mode; only a dev service moves onto a pool.", dev, mode),
		})
	}
	if r := poolMigrateOriginRefusal(meta, rt); r != nil {
		out = append(out, *r)
	}
	return out
}

// poolMigrateOriginRefusal refuses a dev service whose work has no origin to
// move through: in a Mate, a pair not wired to HQ; elsewhere, a pair without
// git-push or with an origin a lease cannot clone (https only, no userinfo).
func poolMigrateOriginRefusal(meta *workflow.ServiceMeta, rt runtime.Info) *poolMigrateRefusal {
	dev := meta.Hostname
	if rt.MateEnabled {
		if meta.HQ != nil && meta.HQ.Repo != "" {
			return nil
		}
		return &poolMigrateRefusal{
			Reason:  poolMigrateRefuseNoOrigin,
			Message: fmt.Sprintf("%s is not wired to this Mate's HQ yet, so its work has no origin to move through. A develop task wires it (zerops_workflow action=\"start\" workflow=\"develop\"); then call again.", dev),
		}
	}
	setup := &topology.Recovery{Tool: "zerops_workflow", Action: actionGitPushSetup, Args: map[string]string{"service": dev}}
	if meta.GitPushState != topology.GitPushConfigured || meta.RemoteURL == "" {
		return &poolMigrateRefusal{
			Reason:   poolMigrateRefuseNoOrigin,
			Message:  fmt.Sprintf("%s has no git origin to move its work through. Set one up with zerops_workflow action=\"git-push-setup\" service=%q, then call again.", dev, dev),
			Recovery: setup,
		}
	}
	if u, err := url.Parse(meta.RemoteURL); err != nil || u.Scheme != "https" || u.User != nil {
		return &poolMigrateRefusal{
			Reason:   poolMigrateRefuseNoOrigin,
			Message:  fmt.Sprintf("%s's origin is not a plain https URL, and a pool's lease clones only https origins without credentials in the URL. Point it at the repository's https URL with zerops_workflow action=\"git-push-setup\" service=%q, then call again.", dev, dev),
			Recovery: setup,
		}
	}
	return nil
}

// poolMigrateCodeRoot is where the dev service's repository lives: /var/www,
// or for a variant-B pair the code volume's mount every dev service of the
// pair shares (docs/spec-mate.md §2.8a.4a).
func poolMigrateCodeRoot(meta *workflow.ServiceMeta) string {
	if meta != nil && meta.CodeVolume != nil && meta.CodeVolume.MountPath != "" {
		return meta.CodeVolume.MountPath
	}
	return topology.CodeMountRoot
}

// poolMigrateDefaultPool is the part's pool hostname: the dev hostname without
// its `dev` suffix, then `pool` (`appdev` → `apppool`, `web` → `webpool`).
func poolMigrateDefaultPool(dev string) string {
	stem := strings.TrimSuffix(dev, poolMigrateDevSuffix)
	if stem == "" {
		stem = dev
	}
	return stem + poolMigratePoolSuffix
}

// poolMigrateCrewHandles names the crew lanes a dev service holds: every
// linked worktree registered under /var/www/.crew/ — or under .crew/ of the
// repository's root, a variant-B pair's code volume — a missing (prunable)
// one included, by handle.
func poolMigrateCrewHandles(census *ops.PoolMigrateCensus, root string) []string {
	var handles []string
	for _, t := range census.Trees {
		rest, ok := strings.CutPrefix(t.Path, topology.CodeMountRoot+poolMigrateCrewDir)
		if !ok {
			rest, ok = strings.CutPrefix(t.Path, strings.TrimSuffix(root, "/")+poolMigrateCrewDir)
		}
		if !ok {
			continue
		}
		handle, _, _ := strings.Cut(rest, "/")
		if handle != "" && !slices.Contains(handles, handle) {
			handles = append(handles, handle)
		}
	}
	sort.Strings(handles)
	return handles
}

// poolMigrateWorkBranchOf names the work's branch — what a lease of the pool
// checks out from the origin (docs/spec-workflows.md §13 PM-2): in a Mate the
// one open change the ledger (changes) keeps in the dev's repository, else
// the dev checkout's own branch when HQ takes a push to it; outside a Mate
// the dev checkout's branch's upstream on origin. Where none is named yet,
// Missing says what names one.
func poolMigrateWorkBranchOf(meta *workflow.ServiceMeta, census *ops.PoolMigrateCensus, rt runtime.Info, changes []workflow.HQChange) *poolMigrateWorkBranch {
	if rt.MateEnabled && meta.HQ != nil {
		if open := changesIn(changes, meta.HQ.AppID, meta.HQ.Repo); len(open) == 1 && open[0].Branch != "" {
			return &poolMigrateWorkBranch{Branch: open[0].Branch, Source: poolMigrateWorkFromChange}
		}
	}
	var main ops.PoolMigrateTree
	if len(census.Trees) > 0 {
		main = census.Trees[0]
	}
	branch := main.Branch
	if branch == "" {
		return &poolMigrateWorkBranch{Missing: "the dev service's checkout is on no branch (a detached HEAD), so a lease has none to check out: put the work on a branch of your own (`git switch -c <branch>`) and push it (`git push -u origin <branch>`)."}
	}
	if rt.MateEnabled {
		if branch == hqBase || strings.HasPrefix(branch, "mate/") {
			return &poolMigrateWorkBranch{Missing: fmt.Sprintf("the dev service's checkout is on %q, a branch HQ takes no push to: %q moves only by a person's merge, and a mate/ branch only as an open change's. Move the work onto a branch of your own (`git switch -c <branch>`) and push it (`git push -u origin <branch>`).", branch, hqBase)}
		}
		return &poolMigrateWorkBranch{Branch: branch, Source: poolMigrateWorkFromCurrent}
	}
	switch up := main.Upstream; {
	case up == nil:
		return &poolMigrateWorkBranch{Missing: fmt.Sprintf("the dev service's checkout is on %q, which tracks no branch on origin, so a lease has none to check out: push it (`git push -u origin %s`).", branch, branch)}
	case up.Remote != poolMigrateOrigin:
		return &poolMigrateWorkBranch{Missing: fmt.Sprintf("the dev service's checkout is on %q, which tracks %s/%s, and a lease clones origin: push it there (`git push -u origin %s`).", branch, up.Remote, up.Branch, branch)}
	default:
		return &poolMigrateWorkBranch{Branch: up.Branch, Source: poolMigrateWorkFromUpstream}
	}
}

// poolStanzaDoc is the provision step's import: the part's pool alone.
type poolStanzaDoc struct {
	Services []poolStanzaService `yaml:"services"`
}

type poolStanzaService struct {
	Hostname            string              `yaml:"hostname"`
	Type                string              `yaml:"type"`
	MinContainers       int                 `yaml:"minContainers"`
	MaxContainers       int                 `yaml:"maxContainers"`
	VerticalAutoscaling *poolStanzaVertical `yaml:"verticalAutoscaling,omitempty"`
	// Vault holds the pool's own service env. A service-level envVariables
	// block is deprecated in the import schema, and sshIsolation reaches the
	// pool's containers only as a service env variable.
	Vault map[string]string `yaml:"vault"`
}

type poolStanzaVertical struct {
	CPUMode string  `yaml:"cpuMode,omitempty"`
	MinCPU  int     `yaml:"minCpu,omitempty"`
	MaxCPU  int     `yaml:"maxCpu,omitempty"`
	MinRAM  float64 `yaml:"minRam,omitempty"`
	MaxRAM  float64 `yaml:"maxRam,omitempty"`
	MinDisk float64 `yaml:"minDisk,omitempty"`
	MaxDisk float64 `yaml:"maxDisk,omitempty"`
}

// renderPoolStanza renders the import the provision step names: the pool
// hostname, swarm@1, no containers until a lease asks for one, at most
// poolMigrateMaxContainers, the dev service's own vertical scale (read by id,
// a direct read), and the sshIsolation that admits this zcp service. It never
// carries a credential: GIT_TOKEN reaches the pool in the env step.
func renderPoolStanza(ctx context.Context, client platform.Client, dev platform.ServiceStack, pool, zcpHostname string) (string, []string, error) {
	svc := poolStanzaService{
		Hostname:      pool,
		Type:          poolMigratePoolType,
		MinContainers: poolMigrateMinContainers,
		MaxContainers: poolMigrateMaxContainers,
		Vault:         map[string]string{"sshIsolation": "vpn service@" + zcpHostname},
	}
	var warnings []string
	scaling, err := ops.FetchServiceScaling(ctx, client, dev.ID)
	switch {
	case err != nil:
		warnings = append(warnings, fmt.Sprintf("%s's scale could not be read (%v); the stanza leaves the pool's verticalAutoscaling to the platform's defaults.", dev.Name, err))
	case scaling != nil:
		v := poolStanzaVertical{
			CPUMode: scaling.CPUMode,
			MinCPU:  scaling.MinCPU, MaxCPU: scaling.MaxCPU,
			MinRAM: scaling.MinRAM, MaxRAM: scaling.MaxRAM,
			MinDisk: scaling.MinDisk, MaxDisk: scaling.MaxDisk,
		}
		if v != (poolStanzaVertical{}) {
			svc.VerticalAutoscaling = &v
		}
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(poolStanzaDoc{Services: []poolStanzaService{svc}}); err != nil {
		return "", nil, fmt.Errorf("encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return "", nil, fmt.Errorf("encode: %w", err)
	}
	return buf.String(), warnings, nil
}

// poolMigrateAt answers a candidate's current step.
func poolMigrateAt(resp poolMigrateResponse, step string, next *poolMigrateNext, nextUnavailable string) poolMigrateResponse {
	resp.Status = poolMigrateStatusStep
	resp.Step = step
	resp.Next = next
	resp.NextUnavailable = nextUnavailable
	resp.Steps = nil
	order := poolMigrateStepOrder()
	current := slices.Index(order, step)
	for i, s := range order {
		state := poolMigrateStatePending
		switch {
		case i < current:
			state = poolMigrateStateDone
		case i == current:
			state = poolMigrateStateCurrent
		}
		resp.Steps = append(resp.Steps, poolMigrateStepState{Step: s, State: state})
	}
	return resp
}

// poolMigrateRefused answers a non-candidate: every reason found, no step and
// no next call.
func poolMigrateRefused(resp poolMigrateResponse, refusals ...poolMigrateRefusal) poolMigrateResponse {
	resp.Status = poolMigrateStatusNotCandidate
	resp.Refusals = refusals
	return resp
}

// poolMigrateCandidatesHint names the dev services this project tracks, for a
// call that named none.
func poolMigrateCandidatesHint(stateDir string) string {
	metas, _ := workflow.ListServiceMetas(stateDir)
	var devs []string
	for _, m := range metas {
		switch m.Mode {
		case "", topology.ModeDev, topology.ModeStandard:
			devs = append(devs, m.Hostname)
		case topology.ModeStage, topology.ModeSimple, topology.ModeLocalStage, topology.ModeLocalOnly:
		}
	}
	sort.Strings(devs)
	if len(devs) == 0 {
		return `Pass service="<dev hostname>"; this project tracks no dev service yet.`
	}
	return fmt.Sprintf(`Pass service="<dev hostname>". Dev services this project tracks: %s.`, strings.Join(devs, ", "))
}
